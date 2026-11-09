package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

func TestInterpretLine(t *testing.T) {
	if got := interpretLine(`{"type":"thread.started","thread_id":"t1"}`, i18n.EN).events; len(got) != 1 || got[0].ThreadID != "t1" {
		t.Fatalf("thread: %+v", got)
	}
	step := interpretLine(`{"type":"item.completed","item":{"type":"command_execution","command":"/bin/zsh -lc 'nl -ba a.ts'","exit_code":1}}`, i18n.EN)
	if step.events[0].Text != "$ nl -ba a.ts  (exit 1)" {
		t.Fatalf("step: %q", step.events[0].Text)
	}
	edit := interpretLine(`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"src/a.ts"},{"path":"b.ts"}]}}`, i18n.EN)
	if edit.events[0].Text != "Edited src/a.ts, b.ts" {
		t.Fatalf("edit: %q", edit.events[0].Text)
	}
	message := interpretLine(`{"type":"item.completed","item":{"type":"agent_message","text":"{}"}}`, i18n.EN)
	if message.finalMessage != "{}" {
		t.Fatalf("message: %+v", message)
	}
	if got := interpretLine(`{"type":"turn.failed","error":{"message":"quota"}}`, i18n.EN).failure; !errors.Is(got, ErrLimit) {
		t.Fatal("failure not read")
	}
	if got := interpretLine("not json", i18n.EN); len(got.events) != 0 || got.failure != nil {
		t.Fatal("noise must be ignored")
	}
	if interpretLine(`{"type":"error","message":"Reconnecting 1/5"}`, i18n.EN).failure != nil {
		t.Fatal("a transient notice is not a failure")
	}
}

func TestBuildArgsKeepPromptAndImagesSafe(t *testing.T) {
	r := NewRunner(RunnerOptions{})
	args := r.BuildArgs(Turn{Prompt: "secret prompt", Sandbox: ReadOnly, SchemaFile: "/s.json", Images: []string{"/a.png"}})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "secret prompt") || args[len(args)-1] != "-" {
		t.Fatalf("prompt must come through stdin: %v", args)
	}
	if !strings.Contains(joined, "--image /a.png --json") {
		t.Fatalf("images must come before other flags: %v", args)
	}
	for _, want := range []string{`mcp_servers={}`, `web_search="disabled"`, `shell_environment_policy.inherit="core"`, `approval_policy="never"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s", want)
		}
	}
	resumed := r.BuildArgs(Turn{Sandbox: ReadOnly, ResumeThread: "abc"})
	if resumed[0] != "exec" || resumed[1] != "resume" || resumed[len(resumed)-2] != "abc" {
		t.Fatalf("resume args: %v", resumed)
	}
}

func TestBuildArgsTakeTheModelOfTheTurnAndOmitAnAbsentSchema(t *testing.T) {
	r := NewRunner(RunnerOptions{Model: "fallback-model"})
	joined := strings.Join(r.BuildArgs(Turn{Sandbox: ReadOnly, Model: "turn-model"}), " ")
	if !strings.Contains(joined, "-m turn-model") || strings.Contains(joined, "fallback-model") || strings.Contains(joined, "--output-schema") {
		t.Fatalf("args: %s", joined)
	}
	if joined := strings.Join(r.BuildArgs(Turn{Sandbox: ReadOnly}), " "); !strings.Contains(joined, "-m fallback-model") {
		t.Fatalf("args: %s", joined)
	}
	if joined := strings.Join(NewRunner(RunnerOptions{}).BuildArgs(Turn{Sandbox: ReadOnly, SchemaFile: "/s.json"}), " "); strings.Contains(joined, " -m ") || !strings.Contains(joined, "--output-schema /s.json") {
		t.Fatalf("args: %s", joined)
	}
}

func TestClassifyFailure(t *testing.T) {
	cases := map[string]error{
		"You've hit your usage limit. Try again at 6:12 PM.": ErrLimit,
		"429 Too Many Requests":                              ErrLimit,
		"Not logged in. Please log in again":                 ErrLogin,
		"401 Unauthorized":                                   ErrLogin,
		"You've hit your limit · resets 3pm":                 ErrLimit,
		"Credit balance is too low":                          ErrLimit,
		"Not logged in · Please run /login":                  ErrLogin,
		"Invalid API key · Please run /login":                ErrLogin,
	}
	for message, want := range cases {
		if err := ClassifyFailure(message); !errors.Is(err, want) {
			t.Errorf("%q: got %v", message, err)
		}
	}
	if err := ClassifyFailure("segmentation fault"); errors.Is(err, ErrLimit) || errors.Is(err, ErrLogin) {
		t.Fatal("an ordinary failure was classified")
	}
}

// fakeCodex writes an executable that stands in for the CLI and returns a runner using it.
func fakeCodex(t *testing.T, script string) *Runner {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewRunner(RunnerOptions{Shell: "/bin/sh", Command: path, Timeout: 5 * time.Second, KillGrace: 200 * time.Millisecond})
}

func TestRunStreamsEventsAndReadsStdin(t *testing.T) {
	r := fakeCodex(t, `cat > "$0.stdin"
echo '{"type":"thread.started","thread_id":"t9"}'
echo '{"type":"item.completed","item":{"type":"command_execution","command":"ls"}}'
echo '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}'
`)
	var events []string
	result, err := r.Run(context.Background(), Turn{Prompt: "hello", Dir: t.TempDir(), Sandbox: ReadOnly, SchemaFile: "/s", OnEvent: func(e Event) { events = append(events, e.Type) }})
	if err != nil {
		t.Fatal(err)
	}
	if result.ThreadID != "t9" || result.FinalMessage != "done" || strings.Join(events, ",") != "thread,step,message" {
		t.Fatalf("result %+v events %v", result, events)
	}
	stdin, _ := os.ReadFile(filepath.Join(filepath.Dir(r.opts.Command), "codex.stdin"))
	if string(stdin) != "hello" {
		t.Fatalf("stdin %q", stdin)
	}
}

func TestRunReportsFailures(t *testing.T) {
	limit := fakeCodex(t, `echo '{"type":"turn.failed","error":{"message":"You have hit your usage limit"}}'`)
	if _, err := limit.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit: %v", err)
	}
	crash := fakeCodex(t, `echo boom >&2; exit 2`)
	if _, err := crash.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("crash: %v", err)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	r := fakeCodex(t, `sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	started := time.Now()
	_, err := r.Run(ctx, Turn{Dir: t.TempDir(), Sandbox: ReadOnly})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("the process was not stopped")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := r.Run(cancelled, Turn{Dir: t.TempDir()}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("already cancelled: %v", err)
	}
}

func TestRunTimesOut(t *testing.T) {
	r := fakeCodex(t, `sleep 30`)
	r.opts.Timeout = 300 * time.Millisecond
	if _, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err == nil || !strings.Contains(err.Error(), "was stopped") {
		t.Fatalf("got %v", err)
	}
}

func TestCodexSearchesTheWebOnlyWhenTheTurnMayAndNeverOpensTheSandboxNetwork(t *testing.T) {
	r := NewRunner(RunnerOptions{})
	off := strings.Join(r.BuildArgs(Turn{Sandbox: ReadOnly}), " ")
	on := strings.Join(r.BuildArgs(Turn{Sandbox: ReadOnly, Web: true}), " ")
	if !strings.Contains(off, `web_search="disabled"`) || !strings.Contains(on, `web_search="live"`) || strings.Contains(on, `web_search="disabled"`) {
		t.Fatalf("off: %s\non: %s", off, on)
	}
	if !strings.Contains(on, "sandbox_workspace_write.network_access=false") || !strings.Contains(on, `sandbox_mode="read-only"`) || !strings.Contains(on, "mcp_servers={}") {
		t.Errorf("the sandbox stays closed: %s", on)
	}
}
