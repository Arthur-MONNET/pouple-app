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

func interactiveLookup(command string) func(context.Context, Turn) (TurnResult, error) {
	return reply(map[string]any{
		"outcome": LookupOutcome,
		"lookups": []Lookup{{Service: "terminal", Action: "run", Target: command}},
	})
}

func terminalHarness(t *testing.T, steps ...func(context.Context, Turn) (TurnResult, error)) *harness {
	t.Helper()
	if _, err := os.Stat("/usr/bin/ssh-agent"); err != nil {
		t.Skip("OpenSSH is unavailable")
	}
	h := newHarness(t, steps...)
	h.svc.deps.InteractiveTerminal = true
	h.svc.deps.Store.Add(Stored{Request: Request{
		ID: "terminal-test", Status: StatusReady, Grants: Grants{{}},
	}})
	t.Cleanup(h.svc.Shutdown)
	return h
}

const terminalTestID = "terminal-test"

func waitForTerminal(t *testing.T, s *Service, matches func(TerminalView) bool) TerminalView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var view TerminalView
	for time.Now().Before(deadline) {
		next, err := s.Terminal(terminalTestID, "", "read", 0, 0, 0, "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		view = next
		if matches(view) {
			return view
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unexpected terminal state: %+v, output %q", view, view.Output)
	return TerminalView{}
}

func TestInteractiveSSHAddResumesWithStatusOnly(t *testing.T) {
	key := filepath.Join(t.TempDir(), "test-key")
	const passphrase = "test-passphrase-pty-724"
	if out, err := exec.Command("/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	h := terminalHarness(t, interactiveLookup("ssh-add '"+key+"'"), reply(nil))
	if err := h.svc.Message(terminalTestID, "Load the key", nil); err != nil {
		t.Fatal(err)
	}
	active := waitForTerminal(t, h.svc, func(v TerminalView) bool {
		return v.Running && strings.Contains(string(v.Output), "Enter passphrase")
	})
	if h.codex.count() != 1 {
		t.Fatal("assistant resumed before the command completed")
	}
	if _, err := h.svc.Terminal(terminalTestID, active.ID, "input", active.Revision, 0, 0, passphrase+"\r", 0, 0); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	finished := waitForTerminal(t, h.svc, func(v TerminalView) bool { return v.Result != nil })
	if finished.Result.Status != "success" || strings.Contains(string(finished.Output), passphrase) || !strings.Contains(string(finished.Output), "Identity added") {
		t.Fatalf("unexpected completion: %+v", finished.Result)
	}
	turns := h.codex.turns
	if len(turns) != 2 || turns[1].ResumeThread != "thread-1" || !strings.Contains(turns[1].Prompt, `{"status":"success"}`) {
		t.Fatal("assistant did not resume with structured success")
	}
	if strings.Contains(turns[1].Prompt, "Identity added") || strings.Contains(turns[1].Prompt, passphrase) {
		t.Fatal("terminal transcript reached the assistant")
	}
	snapshot, err := json.Marshal(h.svc.Snapshot())
	if err != nil || strings.Contains(string(snapshot), passphrase) || strings.Contains(string(snapshot), "Identity added") {
		t.Fatal("terminal data reached conversation history")
	}
	spec := turns[1].withSSHAgent(processSpec{Script: `exec /usr/bin/ssh-add -l`})
	cmd := exec.Command("/bin/sh", "-c", spec.Script)
	cmd.Env = spec.Env
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ED25519") {
		t.Fatalf("identity missing from the resumed turn's agent: %v: %s", err, out)
	}
}

func TestInteractiveStatusAndStop(t *testing.T) {
	for _, tc := range []struct{ name, command, status string }{
		{"error", "sh -c 'exit 17'", `{"status":"exit_error","exitCode":17}`},
		{"signal", "sh -c 'kill -TERM $$'", `{"status":"signaled","signal":"SIGTERM"}`},
		{"interrupt", "sh -c 'echo waiting; exec sleep 30'", `{"status":"interrupted"}`},
		{"stop", "sh -c 'echo waiting; exec sleep 30'", `{"status":"cancelled"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := terminalHarness(t, interactiveLookup(tc.command), reply(nil))
			if err := h.svc.Message(terminalTestID, "Investigate", nil); err != nil {
				t.Fatal(err)
			}
			if tc.name == "interrupt" || tc.name == "stop" {
				active := waitForTerminal(t, h.svc, func(v TerminalView) bool {
					return v.Running && strings.Contains(string(v.Output), "waiting")
				})
				if tc.name == "interrupt" {
					if _, err := h.svc.Terminal(terminalTestID, active.ID, "interrupt", active.Revision, 0, 0, "", 0, 0); err != nil {
						t.Fatal(err)
					}
				} else if err := h.svc.Stop(terminalTestID); err != nil {
					t.Fatal(err)
				}
				if _, err := h.svc.Terminal(terminalTestID, active.ID, "input", active.Revision-1, 0, 0, "stale", 0, 0); !errors.Is(err, ErrState) {
					t.Fatalf("stale input: %v", err)
				}
			}
			h.svc.Idle()
			finished := waitForTerminal(t, h.svc, func(v TerminalView) bool { return v.Result != nil })
			if got := finished.Result.String(); got != tc.status {
				t.Fatalf("got %s, want %s", got, tc.status)
			}
			if tc.name == "stop" {
				request, _ := h.svc.require(terminalTestID)
				if h.codex.count() != 1 || !strings.Contains(request.StopCheckpoint, tc.status) {
					t.Fatalf("expected a stopped checkpoint, got %q", request.StopCheckpoint)
				}
				if err := h.svc.Message(terminalTestID, "I needed to switch keys", nil); err != nil {
					t.Fatal(err)
				}
				h.svc.Idle()
				if next := h.codex.turns[1]; next.ResumeThread != "thread-1" || !strings.Contains(next.Prompt, tc.status) || !strings.Contains(next.Prompt, "switch keys") {
					t.Fatal("explicit resume lost the checkpoint or the reason")
				}
			} else if h.codex.count() != 2 || !strings.Contains(h.codex.turns[1].Prompt, tc.status) {
				t.Fatal("assistant did not continue with the command result")
			}
		})
	}
}

func TestStopBetweenInteractiveCommands(t *testing.T) {
	between := make(chan struct{})
	h := terminalHarness(t, interactiveLookup("true"), func(ctx context.Context, turn Turn) (TurnResult, error) {
		close(between)
		<-ctx.Done()
		return interactiveLookup("false")(ctx, turn)
	}, reply(nil))
	if err := h.svc.Message(terminalTestID, "Investigate", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-between:
	case <-time.After(5 * time.Second):
		t.Fatal("assistant did not reach the next turn")
	}
	if err := h.svc.Stop(terminalTestID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	request, _ := h.svc.require(terminalTestID)
	if !strings.Contains(request.StopCheckpoint, `{"status":"success"}`) {
		t.Fatalf("checkpoint lost completed work: %q", request.StopCheckpoint)
	}
	view := waitForTerminal(t, h.svc, func(v TerminalView) bool { return v.Result != nil })
	if view.Command != "true" {
		t.Fatalf("another command ran after stop: %s", view.Command)
	}
	if err := h.svc.Retry(terminalTestID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if next := h.codex.turns[2]; next.ResumeThread != "thread-1" || !strings.Contains(next.Prompt, `{"status":"success"}`) {
		t.Fatal("resume lost the checkpoint")
	}
}

func TestRestrictedTurnCannotStartInteractiveCommand(t *testing.T) {
	h := terminalHarness(t, interactiveLookup("true"), reply(nil))
	h.svc.deps.Store.Update(terminalTestID, func(r *Stored) { r.Grants = nil })
	if err := h.svc.Message(terminalTestID, "Investigate", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	view := waitForTerminal(t, h.svc, func(v TerminalView) bool { return v.ID == "" })
	if view.Running || h.codex.count() != 2 || !strings.Contains(h.codex.turns[1].Prompt, `{"status":"unavailable"}`) {
		t.Fatal("restricted turn escaped its sandbox")
	}
}
