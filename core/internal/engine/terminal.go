package engine

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	Dir           string             `json:"dir"`
	Branch        string             `json:"branch"`
	ID            string             `json:"id"`
	Command       string             `json:"command"`
	Interactive   bool               `json:"interactive"`
	Running       bool               `json:"running"`
	Result        *interactiveResult `json:"result"`
	Revision      uint64             `json:"revision"`
	Output        []byte             `json:"output"`
	Offset        uint64             `json:"offset"`
	Commands      string             `json:"commands"`
	CommandOffset uint64             `json:"commandOffset"`
}

type interactiveResult struct {
	Started  bool   `json:"started,omitempty"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Signal   string `json:"signal,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type terminalSession struct {
	id, agentDir, socket string
	selected             bool
	dir, branch          string
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
	cwd         *os.File
	interrupted bool
	assistant   bool
	finished    chan struct{}
}

func startTerminal(dir string) (*terminalSession, error) {
	// A short, owner-only directory also fits the macOS Unix socket path limit.
	agentDir, err := os.MkdirTemp("/tmp", "pouple-ssh-")
	if err != nil {
		return nil, err
	}
	t := &terminalSession{dir: dir, branch: terminalBranch(dir), id: rand.Text(), revision: 1, size: pty.Winsize{Rows: 24, Cols: 80}, agentDir: agentDir, socket: filepath.Join(agentDir, "agent")}
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

func (t *terminalSession) startProcess(command string) (*interactiveProcess, error) {
	cwd, err := os.CreateTemp(t.agentDir, "cwd-")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(cwd.Name()); err != nil {
		_ = cwd.Close()
		return nil, err
	}
	// Record the shell's final working directory on a separate descriptor. The transcript
	// and command status remain independent of this metadata.
	cmd := exec.Command("/bin/sh", "-c", "trap 'command pwd -P >&3' EXIT\n"+command)
	cmd.ExtraFiles = []*os.File{cwd}
	cmd.Dir = t.dir
	cmd.Env = []string{"TERM=xterm-256color", "SSH_AUTH_SOCK=" + t.socket, "HISTFILE=/dev/null"}
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
	master, err := pty.StartWithSize(cmd, &t.size)
	if err != nil {
		_ = cwd.Close()
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
		_ = cwd.Close()
		return nil, err
	}
	return &interactiveProcess{cmd: cmd, master: masterFile, cwd: cwd, finished: make(chan struct{})}, nil
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
		t.active.interrupted = true
		t.active.signal(unix.SIGKILL)
		_ = t.active.master.Close()
	}
	if t.agentInput != nil {
		_ = t.agentInput.Close()
	}
	_ = os.RemoveAll(t.agentDir)
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

func terminalBranch(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

func resolveTerminalDir(base, path string) (string, error) {
	if path == "" {
		path = base
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot open terminal directory: %w", errors.Join(ErrInput, err))
	}
	if !info.IsDir() {
		return "", fmt.Errorf("terminal path is not a directory: %w", ErrInput)
	}
	return path, nil
}

func (entry Entry) terminalCommand() string {
	if entry.Role != "assistant" || entry.Kind != "step" || !strings.HasPrefix(entry.Text, "$ ") {
		return ""
	}
	if entry.Command != "" {
		return "$ " + entry.Command + "\n"
	}
	return entry.Text + "\n"
}

func (s *Service) OpenTerminal(id, path string) (TerminalView, error) {
	s.terminalHistoryMu.Lock()
	defer s.terminalHistoryMu.Unlock()
	request, err := s.require(id)
	if err != nil {
		return TerminalView{}, err
	}
	dir, err := resolveTerminalDir(s.terminalDir(request), path)
	if err != nil {
		return TerminalView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return TerminalView{}, fmt.Errorf("terminal service is shutting down: %w", ErrState)
	}
	t, err := startTerminal(dir)
	if err != nil {
		return TerminalView{}, fmt.Errorf("could not start terminal session: %w", err)
	}
	for _, entry := range request.Entries {
		t.commands.append([]byte(entry.terminalCommand()))
	}
	if s.terminals == nil {
		s.terminals = make(map[string][]*terminalSession)
	}
	s.terminals[id] = append(s.terminals[id], t)
	return t.view(0, 0), nil
}

func (s *Service) ListTerminals(id string) ([]TerminalView, error) {
	if _, err := s.require(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]TerminalView, 0, len(s.terminals[id]))
	for _, t := range s.terminals[id] {
		views = append(views, t.view(t.output.end, t.commands.end))
	}
	return views, nil
}

func (s *Service) terminalLocked(id, session string) *terminalSession {
	for _, t := range s.terminals[id] {
		if t.id == session {
			return t
		}
	}
	return nil
}

func (t *terminalSession) view(offset, commandOffset uint64) TerminalView {
	return TerminalView{
		ID: t.id, Dir: t.dir, Branch: t.branch, Command: t.command, Running: t.active != nil, Interactive: t.active != nil && t.active.assistant, Result: t.result,
		Revision: t.revision, Output: t.output.since(offset), Offset: t.output.end,
		Commands: string(t.commands.since(commandOffset)), CommandOffset: t.commands.end,
	}
}

func (s *Service) Terminal(id, session, action string, revision, offset, commandOffset uint64, data string, rows, cols uint16) (TerminalView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.terminalLocked(id, session)
	if t == nil {
		return TerminalView{}, ErrNotFound
	}
	switch action {
	case "read":
	case "select":
		for _, candidate := range s.terminals[id] {
			candidate.selected = candidate == t
		}
	case "close":
		t.close()
		s.terminals[id] = slices.DeleteFunc(s.terminals[id], func(candidate *terminalSession) bool { return candidate == t })
		if len(s.terminals[id]) == 0 {
			delete(s.terminals, id)
		}
		return TerminalView{}, nil
	case "run", "cd":
		if revision != t.revision || t.active != nil || s.closing {
			return TerminalView{}, ErrState
		}
		if action == "cd" {
			if err := t.changeDir(data); err != nil {
				return TerminalView{}, err
			}
		} else {
			if strings.TrimSpace(data) == "" || strings.ContainsRune(data, 0) {
				return TerminalView{}, ErrInput
			}
			if p := t.start(data); p != nil {
				go s.finishInteractive(context.Background(), t, p)
			}
		}
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
	for _, t := range s.terminals[id] {
		t.close()
	}
	delete(s.terminals, id)
}

func (t *terminalSession) changeDir(path string) error {
	if path == "" {
		path = "~"
	}
	dir, err := resolveTerminalDir(t.dir, path)
	if err != nil {
		return err
	}
	t.commands.append([]byte("$ cd " + path + "\n"))
	t.dir, t.branch = dir, terminalBranch(dir)
	t.command, t.result = "", nil
	t.revision++
	return nil
}

func (t *terminalSession) start(command string) *interactiveProcess {
	t.command, t.result = command, nil
	t.revision++
	p, err := t.startProcess(command)
	if err != nil {
		result := terminalRejected(err.Error())
		result.Status = "start_failed"
		t.result = &result
		return nil
	}
	t.commands.append([]byte("$ " + command + "\n"))
	t.output.append([]byte("\r\n\x1b[36m" + printableTerminalText(t.dir) + "\x1b[0m" + "\r\n$ " + printableTerminalText(command) + "\r\n"))
	t.active = p
	return p
}

func printableTerminalText(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, text)
}

func terminalRejected(reason string) interactiveResult {
	return interactiveResult{Status: "unavailable", Reason: reason}
}

func (s *Service) runInteractive(ctx context.Context, id, session, action, command string) interactiveResult {
	s.terminalHistoryMu.Lock()
	s.mu.Lock()
	t := s.terminalLocked(id, session)
	var p *interactiveProcess
	var result interactiveResult
	switch {
	case ctx.Err() != nil:
		result.Status = "cancelled"
	case s.closing:
		result = terminalRejected("terminal service is shutting down")
	case t == nil:
		result = terminalRejected("session not found in this conversation")
	case t.active != nil:
		result = terminalRejected("session is already running a command")
	case action == "cd":
		if err := t.changeDir(command); err != nil {
			result = terminalRejected(err.Error())
		} else {
			result.Status = "success"
			if command == "" {
				command = "~"
			}
			command = "cd " + command
		}
	case action == "run":
		p = t.start(command)
		if p == nil {
			result = *t.result
		} else {
			p.assistant = true
		}
	default:
		result = terminalRejected("expected terminal action run or cd")
	}
	s.mu.Unlock()
	if p != nil || result.Status == "success" {
		entry := s.entry("assistant", "step", "$ "+s.deps.Redact()(command))
		s.quietUpdate(id, func(r *Stored) { r.Entries = append(r.Entries, entry) })
	}
	s.terminalHistoryMu.Unlock()
	if p != nil {
		result = s.finishInteractive(ctx, t, p)
		result.Started = true
	}
	result.Reason = s.deps.Redact()(result.Reason)
	return result
}

func (s *Service) finishInteractive(ctx context.Context, t *terminalSession, p *interactiveProcess) interactiveResult {
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
	dir := t.dir
	path, _ := io.ReadAll(io.NewSectionReader(p.cwd, 0, 4096))
	_ = p.cwd.Close()
	if path, ok := strings.CutSuffix(string(path), "\n"); ok && filepath.IsAbs(path) {
		if current, err := resolveTerminalDir(dir, path); err == nil {
			dir = current
		}
	}
	branch := terminalBranch(dir)
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
	t.active, t.result, t.dir, t.branch = nil, &result, dir, branch
	return result
}

func (r interactiveResult) String() string {
	data, _ := json.Marshal(r)
	return string(data)
}

func terminalLookupResult(session string, result interactiveResult) string {
	data, _ := json.Marshal(struct {
		Session string `json:"session"`
		interactiveResult
	}{session, result})
	return string(data)
}

func (s *Service) preferredTerminalLocked(id string) *terminalSession {
	for _, t := range s.terminals[id] {
		if t.selected {
			return t
		}
	}
	if len(s.terminals[id]) > 0 {
		return s.terminals[id][0]
	}
	return nil
}

func (s *Service) defaultTerminal(id string) (TerminalView, error) {
	s.mu.Lock()
	t := s.preferredTerminalLocked(id)
	if t != nil && t.active == nil && !s.closing {
		view := t.view(t.output.end, t.commands.end)
		s.mu.Unlock()
		return view, nil
	}
	s.mu.Unlock()
	return s.OpenTerminal(id, ".")
}

func (s *Service) terminalPrompt(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	preferred := s.preferredTerminalLocked(id)
	type sessionInfo struct {
		ID      string `json:"id"`
		Dir     string `json:"dir"`
		Running bool   `json:"running"`
	}
	sessions := make([]sessionInfo, 0, len(s.terminals[id]))
	if preferred != nil {
		sessions = append(sessions, sessionInfo{preferred.id, preferred.dir, preferred.active != nil})
	}
	for _, t := range s.terminals[id] {
		if t != preferred {
			sessions = append(sessions, sessionInfo{t.id, t.dir, t.active != nil})
		}
	}
	data, _ := json.Marshal(sessions)
	return string(data)
}

type terminalAccess struct {
	socket string
	report func(string)
}

type terminalAccessKey struct{}

func (s *Service) acquireTerminal(ctx context.Context, id, session string, shareAgent bool) (terminalAccess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return terminalAccess{}, ErrCancelled
	}
	t := s.terminalLocked(id, session)
	if session == "" {
		t = s.preferredTerminalLocked(id)
	}
	var socket string
	if t != nil && shareAgent {
		socket = t.socket
	}
	return terminalAccess{socket: socket, report: func(text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		destination := t
		if destination == nil || s.terminalLocked(id, destination.id) != destination {
			destination = s.preferredTerminalLocked(id)
		}
		if destination != nil {
			destination.commands.append([]byte(text))
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
