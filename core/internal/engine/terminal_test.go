package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalPTYAndSSHAgentHandoff(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ssh-agent"); err != nil {
		t.Skip("OpenSSH is unavailable")
	}
	h := newHarness(t)
	s := h.svc
	t.Cleanup(s.Shutdown)
	const id = "terminal-test"
	s.deps.Store.Add(Stored{Request: Request{ID: id, Proposal: &Proposal{ProjectID: "login-app"}}})
	view, err := s.OpenTerminal(id)
	if err != nil {
		t.Fatal(err)
	}
	session := view.ID
	act := func(action, text string) (TerminalView, error) {
		return s.Terminal(id, session, action, view.Revision, 0, 0, text, 41, 93)
	}
	do := func(action, text string) TerminalView {
		t.Helper()
		result, err := act(action, text)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return result
	}
	waitFor := func(matches func(TerminalView) bool) TerminalView {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			result := do("read", "")
			if matches(result) {
				return result
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("unexpected terminal state: %+v", do("read", ""))
		return TerminalView{}
	}
	contains := func(text string) TerminalView {
		t.Helper()
		return waitFor(func(v TerminalView) bool { return strings.Contains(string(v.Output), text) })
	}

	do("resize", "")
	do("input", "pwd; stty size\r")
	contains("41 93")
	contains(filepath.Join(h.root, "login-app"))
	do("input", "sleep 30\r")
	deadline := time.Now().Add(5 * time.Second)
	s.mu.Lock()
	terminal := s.terminals[id]
	s.mu.Unlock()
	for {
		pid, err := terminal.foreground()
		if err == nil && pid > 0 && pid != terminal.shell.Process.Pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no foreground command: pid %d, error %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := act("release", ""); !errors.Is(err, ErrState) {
		t.Fatalf("handoff while a command is in the foreground: %v", err)
	}
	do("interrupt", "")
	do("input", "printf 'interrupted=%s\\n' 1\r")
	contains("interrupted=1")

	key := filepath.Join(t.TempDir(), "test-key")
	const passphrase = "test-passphrase-pty-724"
	if out, err := exec.Command("/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	do("input", "ssh-add '"+key+"'\r")
	contains("Enter passphrase")
	do("input", passphrase+"\r")
	output := contains("Identity added:")
	if strings.Contains(string(output.Output), passphrase) {
		t.Fatal("ssh-add echoed its passphrase")
	}

	type acquired struct {
		access terminalAccess
		err    error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	acquisition := make(chan acquired, 1)
	go func() {
		access, err := s.acquireTerminal(ctx, id, true)
		acquisition <- acquired{access, err}
	}()
	waitFor(func(v TerminalView) bool { return v.Waiting && !v.Busy })
	oldRevision := view.Revision
	view = do("release", "")
	resumed := <-acquisition
	if resumed.err != nil {
		t.Fatal(resumed.err)
	}
	if _, err := s.Terminal(id, session, "input", oldRevision, 0, 0, "stale", 0, 0); !errors.Is(err, ErrState) {
		t.Fatalf("stale input: %v", err)
	}
	if _, err := act("take", ""); !errors.Is(err, ErrState) {
		t.Fatalf("take while assistant is busy: %v", err)
	}

	spec := (Turn{Sandbox: FullAccess, sshAgent: resumed.access.socket}).withSSHAgent(processSpec{
		Script: `exec "$0" "$@"`, Env: []string{"SSH_AUTH_SOCK=/wrong"},
	})
	command := exec.Command("/bin/sh", "-c", spec.Script, "/usr/bin/ssh-add", "-l")
	command.Env = spec.Env
	if out, err := command.CombinedOutput(); err != nil || !strings.Contains(string(out), "ED25519") {
		t.Fatalf("shared identity: %v: %s", err, out)
	}
	resumed.access.report("$ ssh-add -l\n")
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "git"), []byte("#!/bin/sh\nexec /usr/bin/ssh-add -l\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	planCtx := context.WithValue(ctx, terminalAccessKey{}, resumed.access)
	if out, err := runGit(planCtx, h.root, "fetch"); err != nil || !strings.Contains(out, "ED25519") {
		t.Fatalf("identity for approved git fetch: %v: %s", err, out)
	}
	resumed.access.release()
	if result := do("read", ""); !strings.Contains(result.Commands, "ssh-add -l") || strings.Contains(result.Commands, passphrase) {
		t.Fatal("terminal command stream did not stay separate")
	}

	restricted, err := s.acquireTerminal(ctx, id, false)
	if err != nil {
		t.Fatal(err)
	}
	restricted.release()
	if restricted.socket != "" {
		t.Fatal("restricted turn acquired the session agent")
	}

	view = do("take", "")
	do("input", "printf 'local-transcript-%s\\n' 907\r")
	contains("local-transcript-907")
	snapshot, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(snapshot), "local-transcript") || strings.Contains(string(snapshot), passphrase) {
		t.Fatal("interactive terminal text leaked into the conversation")
	}
	do("input", "exit\r")
	waitFor(func(v TerminalView) bool { return v.ShellExited })
	do("close", "")
	if _, err := os.Stat(terminal.socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("agent socket after close: %v", err)
	}
	if _, err := act("read", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read after close: %v", err)
	}
}
