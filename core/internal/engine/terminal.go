package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
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
	ID            string `json:"id"`
	User          bool   `json:"user"`
	Busy          bool   `json:"busy"`
	Waiting       bool   `json:"waiting"`
	ShellExited   bool   `json:"shellExited"`
	Revision      uint64 `json:"revision"`
	Output        []byte `json:"output"`
	Offset        uint64 `json:"offset"`
	Commands      string `json:"commands"`
	CommandOffset uint64 `json:"commandOffset"`
}

type terminalSession struct {
	id               string
	user             bool
	revision         uint64
	changed          chan struct{}
	shell            *exec.Cmd
	master           *os.File
	agentInput       io.WriteCloser
	agentDir         string
	socket           string
	exited           bool
	output, commands terminalOutput
}

func (t *terminalSession) notify() {
	close(t.changed)
	t.changed = make(chan struct{})
}

func (s *Service) terminalDir(request Stored) string {
	if s.deps.ProjectsRoot != nil {
		var projects []string
		if request.Proposal != nil {
			projects = append(projects, request.Proposal.ProjectID)
		}
		if request.Plan != nil {
			for _, action := range request.Plan.Actions {
				if action.Scope.Service == ScopeCode {
					projects = append(projects, action.Scope.ID)
				}
			}
		}
		for _, grant := range request.Grants {
			if grant.Scope.Service == ScopeCode {
				projects = append(projects, grant.Scope.ID)
			}
		}
		for _, project := range projects {
			if dir := s.projectPath(project); dir != "" {
				return dir
			}
		}
	}
	return s.workDir()
}

func startTerminal(dir string) (*terminalSession, error) {
	// A short, owner-only directory also fits the macOS Unix socket path limit.
	agentDir, err := os.MkdirTemp("/tmp", "pouple-ssh-")
	if err != nil {
		return nil, err
	}
	t := &terminalSession{id: rand.Text(), revision: 1, changed: make(chan struct{}), agentDir: agentDir, socket: filepath.Join(agentDir, "agent")}
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

	shell := "/bin/zsh"
	args := []string{"-f", "-i"}
	if _, err := os.Stat(shell); err != nil {
		shell, args = "/bin/bash", []string{"--noprofile", "--norc", "-i"}
	}
	t.shell = exec.Command(shell, args...)
	t.shell.Dir = dir
	t.shell.Env = []string{"TERM=xterm-256color", "SSH_AUTH_SOCK=" + t.socket, "HISTFILE=/dev/null", "SHELL=" + shell}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "PATH", "USER", "LOGNAME", "LANG", "TMPDIR":
			t.shell.Env = append(t.shell.Env, entry)
		default:
			if strings.HasPrefix(name, "LC_") {
				t.shell.Env = append(t.shell.Env, entry)
			}
		}
	}

	master, err := pty.StartWithSize(t.shell, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.close()
		return nil, err
	}
	// Rewrap the master as a nonblocking descriptor so reads can be cancelled and writes can have deadlines.
	fd, err := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		err = unix.SetNonblock(fd, true)
		if err == nil {
			t.master = os.NewFile(uintptr(fd), "terminal")
		} else {
			_ = unix.Close(fd)
		}
	}
	_ = master.Close()
	if err != nil {
		t.close()
		_ = t.shell.Wait()
		return nil, err
	}
	return t, nil
}

func (t *terminalSession) foreground() (int, error) {
	control, err := t.master.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var ioctlErr error
	err = control.Control(func(fd uintptr) { pid, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) })
	return pid, errors.Join(err, ioctlErr)
}

func (t *terminalSession) close() {
	if t.master != nil {
		if pid, err := t.foreground(); err == nil && pid > 0 {
			_ = unix.Kill(-pid, unix.SIGHUP)
		}
		_ = t.master.Close()
	}
	if t.shell != nil && t.shell.Process != nil && !t.exited {
		_ = t.shell.Process.Kill()
	}
	if t.agentInput != nil {
		_ = t.agentInput.Close()
	}
	_ = os.RemoveAll(t.agentDir)
	t.notify()
}

func (s *Service) OpenTerminal(id string) (TerminalView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	request, err := s.require(id)
	if err != nil {
		return TerminalView{}, err
	}
	if s.closing {
		return TerminalView{}, ErrState
	}
	t := s.terminals[id]
	if t == nil {
		t, err = startTerminal(s.terminalDir(request))
		if err != nil {
			return TerminalView{}, err
		}
		if s.terminals == nil {
			s.terminals = make(map[string]*terminalSession)
		}
		t.user = s.terminalRuns[id] == 0
		s.terminals[id] = t
		go s.readTerminal(t)
	}
	return s.terminalView(id, t, 0, 0), nil
}

func (s *Service) readTerminal(t *terminalSession) {
	buffer := make([]byte, 32<<10)
	for {
		n, err := t.master.Read(buffer)
		if n != 0 {
			s.mu.Lock()
			t.output.append(buffer[:n])
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	_ = t.shell.Wait()
	s.mu.Lock()
	t.exited = true
	t.notify()
	s.mu.Unlock()
}

func (s *Service) terminalView(id string, t *terminalSession, offset, commandOffset uint64) TerminalView {
	return TerminalView{
		ID: t.id, User: t.user, Busy: s.terminalRuns[id] != 0, Waiting: s.terminalWaiting[id] != 0,
		ShellExited: t.exited, Revision: t.revision, Output: t.output.since(offset), Offset: t.output.end,
		Commands: string(t.commands.since(commandOffset)), CommandOffset: t.commands.end,
	}
}

// Terminal uses a session ID and an input revision so stale browser events cannot write after a handoff.
func (s *Service) Terminal(id, session, action string, revision, offset, commandOffset uint64, data string, rows, cols uint16) (TerminalView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.terminals[id]
	if t == nil || t.id != session {
		return TerminalView{}, ErrNotFound
	}
	switch action {
	case "read":
	case "close":
		s.closeTerminal(id)
		return TerminalView{}, nil
	case "stop":
		cancel := s.running[id]
		if cancel == nil || (s.terminalRuns[id] == 0 && s.terminalWaiting[id] == 0) {
			return TerminalView{}, ErrState
		}
		cancel()
	case "resize":
		if rows == 0 || cols == 0 || rows > 1000 || cols > 1000 {
			return TerminalView{}, ErrInput
		}
		if !t.exited {
			control, err := t.master.SyscallConn()
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
	case "take":
		if revision != t.revision || s.terminalRuns[id] != 0 || t.exited {
			return TerminalView{}, ErrState
		}
		t.user = true
		t.revision++
	case "release":
		if revision != t.revision || !t.user {
			return TerminalView{}, ErrState
		}
		if !t.exited {
			pid, err := t.foreground()
			if err != nil || pid != t.shell.Process.Pid {
				return TerminalView{}, i18n.Wrap(ErrState, "wait for the terminal command to finish or interrupt it")
			}
		}
		t.user = false
		t.revision++
		t.notify()
	case "input", "interrupt":
		if revision != t.revision || !t.user || t.exited {
			return TerminalView{}, ErrState
		}
		if action == "interrupt" {
			data = "\x03"
		}
		if err := t.master.SetWriteDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			return TerminalView{}, err
		}
		if _, err := io.WriteString(t.master, data); err != nil {
			return TerminalView{}, err
		}
	default:
		return TerminalView{}, ErrInput
	}
	return s.terminalView(id, t, offset, commandOffset), nil
}

// closeTerminal is called with s.mu held.
func (s *Service) closeTerminal(id string) {
	if t := s.terminals[id]; t != nil {
		delete(s.terminals, id)
		t.close()
	}
}

type terminalAccess struct {
	socket  string
	report  func(string)
	release func()
}

type terminalAccessKey struct{}

func (s *Service) acquireTerminal(ctx context.Context, id string, shareAgent bool) (terminalAccess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if ctx.Err() != nil {
			return terminalAccess{}, ErrCancelled
		}
		t := s.terminals[id]
		if t == nil || !t.user {
			if s.terminalRuns == nil {
				s.terminalRuns = make(map[string]int)
			}
			s.terminalRuns[id]++
			// Only an already authorized turn receives the socket. The provider still gets its original sandbox.
			var access terminalAccess
			if t != nil && shareAgent {
				access.socket = t.socket
			}
			access.report = func(text string) {
				s.mu.Lock()
				defer s.mu.Unlock()
				if active := s.terminals[id]; active != nil {
					active.commands.append([]byte(text))
				}
			}
			access.release = func() {
				s.mu.Lock()
				s.terminalRuns[id]--
				if s.terminalRuns[id] == 0 {
					delete(s.terminalRuns, id)
				}
				s.mu.Unlock()
			}
			return access, nil
		}
		if s.terminalWaiting == nil {
			s.terminalWaiting = make(map[string]int)
		}
		s.terminalWaiting[id]++
		changed := t.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		s.mu.Lock()
		s.terminalWaiting[id]--
		if s.terminalWaiting[id] == 0 {
			delete(s.terminalWaiting, id)
		}
	}
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
