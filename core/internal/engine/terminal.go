package engine

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const terminalBuffer = 256 << 10

type terminalOutput struct {
	data []byte
	end  uint64
}

func (o *terminalOutput) append(data []byte) {
	o.end += uint64(len(data))
	if len(data) >= terminalBuffer {
		o.data = append(o.data[:0], data[len(data)-terminalBuffer:]...)
		return
	}
	if excess := len(o.data) + len(data) - terminalBuffer; excess > 0 {
		copy(o.data, o.data[excess:])
		o.data = o.data[:len(o.data)-excess]
	}
	o.data = append(o.data, data...)
}

func (o *terminalOutput) since(offset uint64) []byte {
	start := o.end - uint64(len(o.data))
	if offset < start || offset > o.end {
		offset = start
	}
	return append([]byte(nil), o.data[offset-start:]...)
}

// TerminalView is returned only by the terminal API. The buffers never enter a request or a provider prompt.
type TerminalView struct {
	ID            string             `json:"id"`
	Command       string             `json:"command"`
	Running       bool               `json:"running"`
	Result        *interactiveResult `json:"result"`
	Revision      uint64             `json:"revision"`
	Output        []byte             `json:"output"`
	Offset        uint64             `json:"offset"`
	Commands      string             `json:"commands"`
	CommandOffset uint64             `json:"commandOffset"`
}

type interactiveResult struct {
	Status   string `json:"status"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Signal   string `json:"signal,omitempty"`
}

type terminalSession struct {
	id, agentDir, socket string
	revision             uint64
	agentInput           io.WriteCloser
	output, commands     terminalOutput
	size                 pty.Winsize
	active               *interactiveProcess
	command              string
	result               *interactiveResult
}

type interactiveProcess struct {
	cmd         *exec.Cmd
	master      *os.File
	interrupted bool
	finished    chan struct{}
}

func startTerminal() (*terminalSession, error) {
	// A short, owner-only directory also fits the macOS Unix socket path limit.
	agentDir, err := os.MkdirTemp("/tmp", "pouple-ssh-")
	if err != nil {
		return nil, err
	}
	t := &terminalSession{id: rand.Text(), revision: 1, size: pty.Winsize{Rows: 24, Cols: 80}, agentDir: agentDir, socket: filepath.Join(agentDir, "agent")}
	// The helper terminates the agent when the core closes the pipe, including if the core crashes.
	agent := exec.Command("/usr/bin/ssh-agent", "-a", t.socket, "/bin/sh", "-c",
		`trap 'kill "$SSH_AGENT_PID" 2>/dev/null' EXIT; /bin/cat >/dev/null`)
	agent.Env = []string{"PATH=/usr/bin:/bin"}
	t.agentInput, err = agent.StdinPipe()
	if err != nil {
		t.close()
		return nil, err
	}
	if err := agent.Start(); err != nil {
		t.close()
		return nil, err
	}
	go func() { _ = agent.Wait() }()
	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.DialTimeout("unix", t.socket, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.close()
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}

	return t, nil
}

func startInteractive(dir, command, socket string, size pty.Winsize) (*interactiveProcess, error) {
	cmd := exec.Command("/bin/sh", "-c", "exec "+command)
	cmd.Dir = dir
	cmd.Env = []string{"TERM=xterm-256color", "SSH_AUTH_SOCK=" + socket, "HISTFILE=/dev/null"}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "PATH", "USER", "LOGNAME", "LANG", "TMPDIR":
			cmd.Env = append(cmd.Env, entry)
		default:
			if strings.HasPrefix(name, "LC_") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
	}
	master, err := pty.StartWithSize(cmd, &size)
	if err != nil {
		return nil, err
	}
	// Rewrap the master as a nonblocking descriptor so reads can be cancelled and writes can have deadlines.
	var masterFile *os.File
	fd, err := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		err = unix.SetNonblock(fd, true)
		if err == nil {
			masterFile = os.NewFile(uintptr(fd), "terminal")
		} else {
			_ = unix.Close(fd)
		}
	}
	_ = master.Close()
	if err != nil {
		_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		_ = cmd.Wait()
		return nil, err
	}
	return &interactiveProcess{cmd: cmd, master: masterFile, finished: make(chan struct{})}, nil
}

func (p *interactiveProcess) signal(signal unix.Signal) {
	select {
	case <-p.finished:
		return
	default:
	}
	// Commands have their own session. Signal both the foreground job and the session leader's group.
	control, err := p.master.SyscallConn()
	if err == nil {
		_ = control.Control(func(fd uintptr) {
			if group, err := unix.IoctlGetInt(int(fd), unix.TIOCGPGRP); err == nil && group > 0 && group != p.cmd.Process.Pid {
				_ = unix.Kill(-group, signal)
			}
		})
	}
	_ = unix.Kill(-p.cmd.Process.Pid, signal)
}

func (t *terminalSession) close() {
	if t.active != nil {
		t.active.signal(unix.SIGKILL)
		_ = t.active.master.Close()
	}
	if t.agentInput != nil {
		_ = t.agentInput.Close()
	}
	_ = os.RemoveAll(t.agentDir)
}

func (s *Service) openTerminalLocked(id string) (*terminalSession, error) {
	if s.closing {
		return nil, ErrState
	}
	if t := s.terminals[id]; t != nil {
		return t, nil
	}
	t, err := startTerminal()
	if err != nil {
		return nil, err
	}
	if s.terminals == nil {
		s.terminals = make(map[string]*terminalSession)
	}
	s.terminals[id] = t
	return t, nil
}

func (s *Service) OpenTerminal(id string) (TerminalView, error) {
	if _, err := s.require(id); err != nil {
		return TerminalView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.openTerminalLocked(id)
	if err != nil {
		return TerminalView{}, err
	}
	return t.view(0, 0), nil
}

func (t *terminalSession) view(offset, commandOffset uint64) TerminalView {
	return TerminalView{
		ID: t.id, Command: t.command, Running: t.active != nil, Result: t.result,
		Revision: t.revision, Output: t.output.since(offset), Offset: t.output.end,
		Commands: string(t.commands.since(commandOffset)), CommandOffset: t.commands.end,
	}
}

func (s *Service) Terminal(id, session, action string, revision, offset, commandOffset uint64, data string, rows, cols uint16) (TerminalView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.terminals[id]
	if action == "read" && session == "" {
		if t == nil {
			return TerminalView{}, nil
		}
		return t.view(0, 0), nil
	}
	if t == nil || t.id != session {
		return TerminalView{}, ErrNotFound
	}
	switch action {
	case "read":
	case "resize":
		if rows == 0 || cols == 0 || rows > 1000 || cols > 1000 {
			return TerminalView{}, ErrInput
		}
		t.size = pty.Winsize{Rows: rows, Cols: cols}
		if t.active != nil {
			control, err := t.active.master.SyscallConn()
			if err != nil {
				return TerminalView{}, err
			}
			var resizeErr error
			err = control.Control(func(fd uintptr) {
				resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
			})
			if err := errors.Join(err, resizeErr); err != nil {
				return TerminalView{}, err
			}
		}
	case "input", "interrupt":
		p := t.active
		if revision != t.revision || p == nil {
			return TerminalView{}, ErrState
		}
		if action == "interrupt" {
			p.interrupted = true
			p.signal(unix.SIGINT)
			// Raw-mode programs can ignore SIGINT. Escalate only while this command is still active.
			time.AfterFunc(time.Second, func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				if t.active == p {
					p.signal(unix.SIGKILL)
				}
			})
			break
		}
		if err := p.master.SetWriteDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			return TerminalView{}, err
		}
		if _, err := io.WriteString(p.master, data); err != nil {
			return TerminalView{}, err
		}
	default:
		return TerminalView{}, ErrInput
	}
	return t.view(offset, commandOffset), nil
}

// closeTerminal is called with s.mu held.
func (s *Service) closeTerminal(id string) {
	if t := s.terminals[id]; t != nil {
		delete(s.terminals, id)
		t.close()
	}
}

func (s *Service) runInteractive(ctx context.Context, id, dir, command string) interactiveResult {
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		return interactiveResult{Status: "cancelled"}
	}
	t, err := s.openTerminalLocked(id)
	if err != nil || t.active != nil {
		s.mu.Unlock()
		return interactiveResult{Status: "unavailable"}
	}
	t.command, t.result = command, nil
	t.output.data = nil
	t.revision++
	p, err := startInteractive(dir, command, t.socket, t.size)
	if err != nil {
		result := interactiveResult{Status: "start_failed"}
		t.result = &result
		s.mu.Unlock()
		return result
	}
	t.active = p
	s.mu.Unlock()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buffer := make([]byte, 32<<10)
		for {
			n, err := p.master.Read(buffer)
			if n > 0 {
				s.mu.Lock()
				t.output.append(buffer[:n])
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		_ = p.cmd.Wait()
		close(p.finished)
	}()
	select {
	case <-ctx.Done():
		s.mu.Lock()
		p.signal(unix.SIGKILL)
		s.mu.Unlock()
		<-p.finished
	case <-p.finished:
	}
	if err := p.master.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
		_ = p.master.Close()
	}
	<-drained
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = p.master.Close()
	result := interactiveResult{Status: "success"}
	switch status := p.cmd.ProcessState.Sys().(syscall.WaitStatus); {
	case ctx.Err() != nil:
		result.Status = "cancelled"
	case p.interrupted || status.Signal() == syscall.SIGINT:
		result.Status = "interrupted"
	case status.Signaled():
		result.Status, result.Signal = "signaled", unix.SignalName(status.Signal())
	case status.ExitStatus() != 0:
		code := status.ExitStatus()
		result.Status, result.ExitCode = "exit_error", &code
	}
	t.active, t.result = nil, &result
	return result
}

func (r interactiveResult) String() string {
	data, _ := json.Marshal(r)
	return string(data)
}

type terminalAccess struct {
	socket string
	report func(string)
}

type terminalAccessKey struct{}

func (s *Service) acquireTerminal(ctx context.Context, id string, shareAgent bool) (terminalAccess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return terminalAccess{}, ErrCancelled
	}
	var socket string
	if t := s.terminals[id]; t != nil && shareAgent {
		socket = t.socket
	}
	return terminalAccess{socket: socket, report: func(text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if t := s.terminals[id]; t != nil {
			t.commands.append([]byte(text))
		}
	}}, nil
}

// Restore the chosen socket after login startup without evaluating anything from the PTY.
func (turn Turn) withSSHAgent(spec processSpec) processSpec {
	if turn.sshAgent == "" {
		return spec
	}
	spec.Env = append(spec.Env, "POUPLE_TURN_SSH_AUTH_SOCK="+turn.sshAgent)
	spec.Script = `export SSH_AUTH_SOCK="$POUPLE_TURN_SSH_AUTH_SOCK"; unset POUPLE_TURN_SSH_AUTH_SOCK; ` + spec.Script
	return spec
}
