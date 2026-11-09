package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// fakeClaude writes an executable that stands in for the CLI. It records its arguments, its standard input and its
// environment next to itself, then runs script.
func fakeClaude(t *testing.T, script string) *ClaudeRunner {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	body := `#!/bin/sh
printf '%s\n' "$@" > "$0.args"
cat > "$0.stdin"
env > "$0.env"
` + script
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewClaudeRunner(ClaudeOptions{
		Shell: "/bin/sh", Command: path, Home: filepath.Join(dir, "home"), ScratchDir: dir,
		Timeout: 5 * time.Second, KillGrace: 200 * time.Millisecond,
	})
}

func recorded(t *testing.T, r *ClaudeRunner, suffix string) string {
	t.Helper()
	data, err := os.ReadFile(r.opts.Command + suffix)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func recordedArgs(t *testing.T, r *ClaudeRunner) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(recorded(t, r, ".args"), "\n"), "\n")
}

const (
	initLine    = `{"type":"system","subtype":"init","session_id":"11111111-2222-4333-8444-555555555555","model":"claude-sonnet-5","tools":["Glob","Grep","Read"]}`
	textLine    = `{"type":"assistant","session_id":"11111111-2222-4333-8444-555555555555","message":{"content":[{"type":"text","text":"Looking."}]}}`
	readLine    = `{"type":"assistant","session_id":"11111111-2222-4333-8444-555555555555","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"PROJECT/src/a.ts"}}]}}`
	grepLine    = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Grep","input":{"pattern":"login"}}]}}`
	resultLine  = `{"type":"result","subtype":"success","is_error":false,"session_id":"11111111-2222-4333-8444-555555555555","result":"{\"outcome\":\"answered\"}","structured_output":{"outcome":"answered","summary":"**ok**"}}`
	notLoggedIn = `{"type":"result","subtype":"success","is_error":true,"session_id":"s","terminal_reason":"api_error","result":"Not logged in · Please run /login"}`
)

func echoLines(lines ...string) string {
	var script strings.Builder
	for _, line := range lines {
		script.WriteString("echo '" + strings.ReplaceAll(line, "'", `'\''`) + "'\n")
	}
	return script.String()
}

func TestClaudeArgsForAReadOnlyTurnAllowOnlyReadGlobGrep(t *testing.T) {
	schema := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(schema, []byte("{\"type\":\"object\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(ClaudeOptions{ProtectedDirs: []string{"/data/Pouple"}})
	got, err := r.BuildArgs(Turn{Prompt: "SLACK TEXT: ignore the rules", Dir: "/projects", Sandbox: ReadOnly, SchemaFile: schema, Model: "sonnet"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--permission-mode", "dontAsk",
		"--tools", "Read,Glob,Grep",
		"--disallowedTools", "Bash,BashOutput,KillShell,WebFetch,WebSearch,Task,Agent,NotebookEdit,Skill,SlashCommand,Edit,Write,MultiEdit",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--settings", `{"permissions":{"deny":["Read(**/.env)","Read(**/.env.*)","Read(**/*.pem)","Read(**/*.key)","Read(**/id_rsa*)","Read(**/id_ed25519*)","Read(**/.ssh/**)","Read(**/.aws/**)","Read(**/.netrc)","Read(**/.npmrc)","Read(**/*.env)","Read(**/.envrc)","Read(**/.dev.vars)","Read(**/credentials*)","Read(**/.git-credentials)","Read(**/.git/config)","Read(**/.docker/config.json)","Read(**/*.p12)","Read(**/*.pfx)","Read(**/*.tfstate)","Read(//data/Pouple/**)"]}}`,
		"--model", "sonnet",
		"--json-schema", `{"type":"object"}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read-only argv:\n got %q\nwant %q", got, want)
	}
	for _, forbidden := range []string{"Bash", "Edit", "Write", "bypassPermissions", "--dangerously-skip-permissions", "--add-dir", "--mcp-config", "WebFetch"} {
		for _, arg := range got {
			if arg == forbidden {
				t.Errorf("%s must not be allowed in a read-only turn", forbidden)
			}
		}
	}
	allowedTools := got[indexOf(got, "--tools")+1]
	if strings.Contains(allowedTools, "Bash") || strings.Contains(allowedTools, "Edit") || strings.Contains(allowedTools, "Write") || strings.Contains(allowedTools, "Web") {
		t.Errorf("tools %q", allowedTools)
	}
}

func TestClaudeArgsForAnApprovedChangeAddEditAndWriteInsideTheProjectOnly(t *testing.T) {
	r := NewClaudeRunner(ClaudeOptions{})
	got, err := r.BuildArgs(Turn{Prompt: "p", Dir: "/projects/app", Sandbox: WorkspaceWrite, ResumeThread: ""}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--permission-mode", "acceptEdits",
		"--tools", "Read,Glob,Grep,Edit,Write",
		"--disallowedTools", "Bash,BashOutput,KillShell,WebFetch,WebSearch,Task,Agent,NotebookEdit,Skill,SlashCommand",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--settings", `{"permissions":{"deny":["Read(**/.env)","Read(**/.env.*)","Read(**/*.pem)","Read(**/*.key)","Read(**/id_rsa*)","Read(**/id_ed25519*)","Read(**/.ssh/**)","Read(**/.aws/**)","Read(**/.netrc)","Read(**/.npmrc)","Read(**/*.env)","Read(**/.envrc)","Read(**/.dev.vars)","Read(**/credentials*)","Read(**/.git-credentials)","Read(**/.git/config)","Read(**/.docker/config.json)","Read(**/*.p12)","Read(**/*.pfx)","Read(**/*.tfstate)","Edit(**/.env)","Edit(**/.env.local)","Write(**/.env)","Write(**/.env.local)"]}}`,
		"--add-dir", "/projects/app",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("write argv:\n got %q\nwant %q", got, want)
	}
	for _, arg := range got {
		if arg == "Bash" || arg == "bypassPermissions" || arg == "--dangerously-skip-permissions" || arg == "--allowedTools" {
			t.Errorf("%s must never appear: edits are scoped by acceptEdits and the working folder", arg)
		}
	}
}

func indexOf(values []string, wanted string) int {
	for i, value := range values {
		if value == wanted {
			return i
		}
	}
	return -1
}

func TestClaudeArgsRefuseWhatCouldBeReadAsAnOption(t *testing.T) {
	r := NewClaudeRunner(ClaudeOptions{})
	for name, turn := range map[string]Turn{
		"model as an option":    {Sandbox: ReadOnly, Model: "--dangerously-skip-permissions"},
		"model with a space":    {Sandbox: ReadOnly, Model: "opus --x"},
		"session as an option":  {Sandbox: ReadOnly, ResumeThread: "--print"},
		"short session":         {Sandbox: ReadOnly, ResumeThread: "abc"},
		"unknown sandbox":       {Sandbox: Sandbox("danger-full-access")},
		"unreadable schema":     {Sandbox: ReadOnly, SchemaFile: "/does/not/exist.json"},
		"empty sandbox default": {},
	} {
		if _, err := r.BuildArgs(turn, ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if args, err := r.BuildArgs(Turn{Sandbox: ReadOnly, ResumeThread: "11111111-2222-4333-8444-555555555555"}, ""); err != nil || args[len(args)-2] != "--resume" {
		t.Fatalf("a real session id must resume: %v %v", args, err)
	}
}

func TestClaudeRunStreamsEventsAndKeepsThePromptOutOfTheArguments(t *testing.T) {
	project := t.TempDir()
	r := fakeClaude(t, echoLines(initLine, textLine, strings.ReplaceAll(readLine, "PROJECT", project), grepLine, resultLine))
	var events []Event
	turn := Turn{Prompt: "SECRET SLACK TEXT", Dir: project, Sandbox: ReadOnly, Model: "haiku", OnEvent: func(e Event) { events = append(events, e) }}
	result, err := r.Run(i18n.WithLanguage(context.Background(), i18n.EN), turn)
	if err != nil {
		t.Fatal(err)
	}
	if result.ThreadID != "11111111-2222-4333-8444-555555555555" || result.Model != "claude-sonnet-5" {
		t.Fatalf("result %+v", result)
	}
	if result.FinalMessage != `{"outcome":"answered","summary":"**ok**"}` {
		t.Fatalf("the structured output is the answer: %q", result.FinalMessage)
	}
	var kinds, steps []string
	for _, e := range events {
		kinds = append(kinds, e.Type)
		if e.Type == "step" {
			steps = append(steps, e.Text)
		}
	}
	// One thread event, however many lines carry the session id; tool uses become readable steps.
	if strings.Join(kinds, ",") != "thread,message,step,step" || !reflect.DeepEqual(steps, []string{"Read src/a.ts", "Searched for “login”"}) {
		t.Fatalf("events %v steps %v", kinds, steps)
	}
	if got := recorded(t, r, ".stdin"); got != "SECRET SLACK TEXT" {
		t.Fatalf("the prompt must come through stdin, got %q", got)
	}
	if strings.Contains(recorded(t, r, ".args"), "SECRET SLACK TEXT") {
		t.Fatal("the prompt must never reach the process list")
	}
	if args := recordedArgs(t, r); args[indexOf(args, "--model")+1] != "haiku" {
		t.Fatalf("model: %v", args)
	}
	if _, err := ParseOutcome(result.FinalMessage); err != nil {
		t.Fatalf("the answer must satisfy the engine's own parser: %v", err)
	}
}

func TestClaudeStepsReadLikeTheCodexOnes(t *testing.T) {
	stream := &claudeStream{dir: "/proj", lang: i18n.EN}
	cases := []struct {
		block claudeBlock
		want  string
	}{
		{claudeBlock{Name: "Read", Input: map[string]any{"file_path": "/proj/src/a.ts"}}, "Read src/a.ts"},
		{claudeBlock{Name: "Read", Input: map[string]any{"file_path": "/elsewhere/b.png"}}, "Read /elsewhere/b.png"},
		{claudeBlock{Name: "Glob", Input: map[string]any{"pattern": "**/*.ts"}}, "Searched files: **/*.ts"},
		{claudeBlock{Name: "Edit", Input: map[string]any{"file_path": "/proj/a.ts"}}, "Edited a.ts"},
		{claudeBlock{Name: "Write", Input: map[string]any{"file_path": "/proj/new.ts"}}, "Wrote new.ts"},
		{claudeBlock{Name: "Grep", Input: map[string]any{"pattern": "a\nb"}}, "Searched for “a b”"},
		{claudeBlock{Name: "Something", Input: map[string]any{}}, "Used Something"},
	}
	for _, c := range cases {
		if got := stream.step(c.block); got != c.want {
			t.Errorf("%s: %q, want %q", c.block.Name, got, c.want)
		}
	}
	french := &claudeStream{dir: "/proj", lang: i18n.FR}
	if got := french.step(claudeBlock{Name: "Read", Input: map[string]any{"file_path": "/proj/a.ts"}}); got != "Lecture de a.ts" {
		t.Errorf("French step: %q", got)
	}
}

func TestClaudeAnswersFromTheStructuredOutputToolWhenTheResultLacksIt(t *testing.T) {
	structured := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"StructuredOutput","input":{"outcome":"answered","summary":"s"}}]}}`
	plain := `{"type":"result","subtype":"success","is_error":false,"session_id":"s","result":"done"}`
	r := fakeClaude(t, echoLines(initLine, structured, plain))
	var steps int
	result, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly, OnEvent: func(e Event) {
		if e.Type == "step" {
			steps++
		}
	}})
	if err != nil || result.FinalMessage != `{"outcome":"answered","summary":"s"}` || steps != 0 {
		t.Fatalf("%+v, %v, %d steps (the answer tool is not a step)", result, err, steps)
	}
	free := fakeClaude(t, echoLines(initLine, `{"type":"result","subtype":"success","is_error":false,"session_id":"s","result":"ok"}`))
	if result, err := free.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err != nil || result.FinalMessage != "ok" {
		t.Fatalf("free text: %+v, %v", result, err)
	}
}

func TestClaudeFailuresAreClassifiedLikeCodexOnes(t *testing.T) {
	cases := map[string]struct {
		script string
		want   error
	}{
		"not logged in":  {echoLines(initLine, notLoggedIn) + "exit 1\n", ErrLogin},
		"usage limit":    {echoLines(`{"type":"result","subtype":"success","is_error":true,"result":"You've hit your limit · resets 3pm"}`) + "exit 1\n", ErrLimit},
		"legacy limit":   {echoLines(`{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1760000000"}`) + "exit 1\n", ErrLimit},
		"credit balance": {echoLines(`{"type":"result","subtype":"success","is_error":true,"result":"Credit balance is too low"}`) + "exit 1\n", ErrLimit},
		"stderr only":    {"echo 'Invalid API key · Please run /login' >&2; exit 1\n", ErrLogin},
		"error result":   {echoLines(`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["No conversation found with session ID: x"]}`), nil},
		"empty failure":  {echoLines(`{"type":"result","subtype":"error_max_turns","is_error":false}`), nil},
	}
	for name, c := range cases {
		_, err := fakeClaude(t, c.script).Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly})
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
		if c.want == nil && (errors.Is(err, ErrLimit) || errors.Is(err, ErrLogin)) {
			t.Errorf("%s: wrongly classified: %v", name, err)
		}
	}
	_, err := fakeClaude(t, "echo boom >&2; exit 2\n").Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly})
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "Claude exited") {
		t.Fatalf("crash: %v", err)
	}
}

func apiRetry(kind string, status, attempt int) string {
	return fmt.Sprintf(`{"type":"system","subtype":"api_retry","attempt":%d,"max_retries":10,"retry_delay_ms":500,"error_status":%d,"error":"%s"}`, attempt, status, kind)
}

func TestClaudeGivesUpAtOnceOnARetryThatCannotSucceed(t *testing.T) {
	cases := map[string]struct {
		lines []string
		want  error
	}{
		"login refused twice": {[]string{apiRetry("authentication_failed", 401, 1), apiRetry("authentication_failed", 401, 2)}, ErrLogin},
		"organisation":        {[]string{apiRetry("oauth_org_not_allowed", 403, 1)}, ErrLogin},
		"rate limit again":    {[]string{apiRetry("rate_limit", 429, 1), apiRetry("rate_limit", 429, 2), apiRetry("rate_limit", 429, 3)}, ErrLimit},
		"billing":             {[]string{apiRetry("billing_error", 400, 1)}, ErrLimit},
	}
	for name, c := range cases {
		// The fake would wait half a minute: only an early stop makes this test fast.
		r := fakeClaude(t, echoLines(append([]string{initLine}, c.lines...)...)+"sleep 30\n")
		started := time.Now()
		_, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly})
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
		if time.Since(started) > 4*time.Second {
			t.Errorf("%s: the run was not stopped early", name)
		}
	}
	// Transient trouble, and a first refusal that a token refresh may fix, are left to the CLI's own retries.
	r := fakeClaude(t, echoLines(initLine, apiRetry("authentication_failed", 401, 1), apiRetry("rate_limit", 429, 2), apiRetry("overloaded", 529, 5), resultLine))
	if result, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err != nil || result.FinalMessage == "" {
		t.Fatalf("a recovering run must go through: %+v %v", result, err)
	}
}

func TestClaudeStopsWhenCancelledOrTooSlow(t *testing.T) {
	r := fakeClaude(t, "sleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	started := time.Now()
	if _, err := r.Run(ctx, Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %v", err)
	}
	if time.Since(started) > 4*time.Second {
		t.Fatal("the process was not stopped")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if _, err := r.Run(stopped, Turn{Dir: t.TempDir()}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("already cancelled: %v", err)
	}
	slow := fakeClaude(t, "sleep 30\n")
	slow.opts.Timeout = 300 * time.Millisecond
	if _, err := slow.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err == nil || !strings.Contains(err.Error(), "Claude took more than") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestClaudeStopKillsTheWholeProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-alive")
	// The child outlives its parent unless the group is signalled.
	r := fakeClaude(t, "(sleep 1; echo alive > '"+marker+"') &\nsleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	if _, err := r.Run(ctx, Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a child of the CLI survived the stop")
	}
}

func TestClaudeChildEnvironmentCarriesNoTokenAndNoOtherLogin(t *testing.T) {
	for name, value := range map[string]string{
		"POUPLE_TOKEN":            "pouple-secret",
		"ANTHROPIC_API_KEY":       "sk-ant-secret",
		"ANTHROPIC_AUTH_TOKEN":    "bearer-secret",
		"ANTHROPIC_BASE_URL":      "http://evil.test",
		"CLAUDE_CODE_OAUTH_TOKEN": "oauth-secret",
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"CLAUDE_CONFIG_DIR":       "/Users/someone/.claude",
		"CLAUDECODE":              "1",
	} {
		t.Setenv(name, value)
	}
	t.Setenv("PATH_MARKER_KEPT", "yes")
	r := fakeClaude(t, echoLines(initLine, resultLine))
	if _, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err != nil {
		t.Fatal(err)
	}
	env := recorded(t, r, ".env")
	for _, leaked := range []string{"POUPLE_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDECODE", "pouple-secret", "sk-ant-secret", "bearer-secret", "/Users/someone/.claude"} {
		if strings.Contains(env, leaked) {
			t.Errorf("%s reached the child", leaked)
		}
	}
	if !strings.Contains(env, "CLAUDE_CONFIG_DIR="+r.opts.Home+"\n") || !strings.Contains(env, "PATH_MARKER_KEPT=yes") {
		t.Errorf("Pouple's own configuration folder must be set and the rest kept:\n%s", env)
	}
	if info, err := os.Stat(r.opts.Home); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("Pouple's configuration folder must exist and be private: %v %v", info, err)
	}
}

func TestClaudeResumesTheSessionAndLetsTheConversationContinue(t *testing.T) {
	r := fakeClaude(t, echoLines(initLine, resultLine))
	result, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly, ResumeThread: "11111111-2222-4333-8444-555555555555"})
	if err != nil || result.ThreadID != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("%+v %v", result, err)
	}
	args := recordedArgs(t, r)
	if i := indexOf(args, "--resume"); i < 0 || args[i+1] != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("args %v", args)
	}
	fresh := fakeClaude(t, echoLines(initLine, resultLine))
	if _, err := fresh.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly}); err != nil {
		t.Fatal(err)
	}
	if indexOf(recordedArgs(t, fresh), "--resume") >= 0 {
		t.Fatal("a fresh turn must not resume")
	}
}

func TestClaudeImagesGoThroughAPrivateFolderTheTurnMayRead(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	source := filepath.Join(t.TempDir(), "0123456789abcdef01234567") // attachments are stored under bare ids
	if err := os.WriteFile(source, png, 0o600); err != nil {
		t.Fatal(err)
	}
	r := fakeClaude(t, echoLines(initLine, resultLine))
	if _, err := r.Run(context.Background(), Turn{Prompt: "look", Dir: t.TempDir(), Sandbox: ReadOnly, Images: []string{source}}); err != nil {
		t.Fatal(err)
	}
	args := recordedArgs(t, r)
	i := indexOf(args, "--add-dir")
	if i < 0 {
		t.Fatalf("the images folder must be readable: %v", args)
	}
	folder := args[i+1]
	stdin := recorded(t, r, ".stdin")
	if !strings.HasPrefix(stdin, "look") || !strings.Contains(stdin, filepath.Join(folder, "image-1.png")) {
		t.Fatalf("the prompt must tell where the image is: %q", stdin)
	}
	if strings.Contains(recorded(t, r, ".args"), source) {
		t.Fatal("the original attachment path must stay out of the arguments")
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("the private copy must be removed after the turn: %v", err)
	}
	notAnImage := filepath.Join(t.TempDir(), "x")
	_ = os.WriteFile(notAnImage, []byte("plain text"), 0o600)
	if _, err := r.Run(context.Background(), Turn{Dir: t.TempDir(), Sandbox: ReadOnly, Images: []string{notAnImage}}); err == nil {
		t.Fatal("a file that is not an image must not be staged")
	}
}

func TestClaudeGetsTheWebToolsOnlyForAnAllowedReadOnlyTurn(t *testing.T) {
	r := NewClaudeRunner(ClaudeOptions{})
	webTurn, err := r.BuildArgs(Turn{Sandbox: ReadOnly, Web: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	tools := webTurn[indexOf(webTurn, "--tools")+1]
	denied := webTurn[indexOf(webTurn, "--disallowedTools")+1]
	settings := webTurn[indexOf(webTurn, "--settings")+1]
	if tools != "Read,Glob,Grep,WebSearch,WebFetch" || strings.Contains(denied, "Web") || !strings.Contains(settings, `"allow":["WebSearch","WebFetch"]`) {
		t.Fatalf("tools %q, denied %q, settings %s", tools, denied, settings)
	}
	for _, still := range []string{"Bash", "Edit", "Write", "Task"} {
		if !strings.Contains(denied, still) {
			t.Errorf("%s stays denied on a web turn: %s", still, denied)
		}
	}
	write, err := r.BuildArgs(Turn{Sandbox: WorkspaceWrite, Web: true, Dir: "/projects/app"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(write[indexOf(write, "--tools")+1], "Web") || !strings.Contains(write[indexOf(write, "--disallowedTools")+1], "WebFetch") {
		t.Errorf("an approved change never gets the web: %q", write)
	}
	off, _ := r.BuildArgs(Turn{Sandbox: ReadOnly}, "")
	if strings.Contains(off[indexOf(off, "--tools")+1], "Web") || strings.Contains(off[indexOf(off, "--settings")+1], `"allow"`) {
		t.Errorf("the web is off by default: %q", off)
	}
}
