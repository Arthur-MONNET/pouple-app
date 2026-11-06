package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Sandbox is how much a Codex turn may touch.
type Sandbox string

const (
	ReadOnly       Sandbox = "read-only"
	WorkspaceWrite Sandbox = "workspace-write"
)

// Event is what Codex reports while it works, reduced to what the chat shows.
type Event struct {
	Type     string // thread | step | message
	ThreadID string
	Text     string
}

// Turn is one Codex run.
type Turn struct {
	Prompt  string
	Dir     string
	Sandbox Sandbox
	// ResumeThread continues an earlier Codex conversation.
	ResumeThread string
	SchemaFile   string
	Images       []string
	OnEvent      func(Event)
}

// TurnResult is what a turn produced.
type TurnResult struct {
	ThreadID string
	// FinalMessage is the last agent message, empty when there was none.
	FinalMessage string
}

// Codex runs turns. The interface lets the service be tested without a process.
type Codex interface {
	Run(ctx context.Context, turn Turn) (TurnResult, error)
}

// Failure kinds. They are distinct because each asks for something different from the user.
var (
	// ErrCancelled: the user stopped the turn.
	ErrCancelled = errors.New("stopped")
	// ErrLimit: the Codex account used up what its plan allows for now.
	ErrLimit = errors.New("codex usage limit reached")
	// ErrLogin: Codex is not signed in, or its login expired.
	ErrLogin = errors.New("codex is not signed in")
)

var (
	limitPattern = regexp.MustCompile(`(?i)usage limit|rate.?limit|quota|too many requests|\b429\b|try again (later|in|at)`)
	loginPattern = regexp.MustCompile(`(?i)not logged in|log ?in again|please (log|sign) ?in|unauthori[sz]ed|\b401\b|token (has )?expired|authentication`)
)

// ClassifyFailure turns what Codex said into an error of the right kind.
func ClassifyFailure(message string) error {
	short := message
	if len(short) > 160 {
		short = short[:160]
	}
	switch {
	case limitPattern.MatchString(message):
		return fmt.Errorf("%w (%s)", ErrLimit, short)
	case loginPattern.MatchString(message):
		return fmt.Errorf("%w (%s)", ErrLogin, short)
	}
	return errors.New(message)
}

// RunnerOptions configures the process runner.
type RunnerOptions struct {
	// Shell is a login shell, so `codex` resolves like in a terminal even when the app starts from the Dock.
	Shell   string
	Command string
	Model   string
	// ResolveCommand lets the app pick up an installation made after the runner starts.
	ResolveCommand func() string
	// Home is the CODEX_HOME: Pouple has its own Codex login.
	Home    string
	Timeout time.Duration
	// KillGrace is how long a stopped process has to leave before it is forced.
	KillGrace time.Duration
}

// Runner runs the Codex CLI.
type Runner struct{ opts RunnerOptions }

func NewRunner(opts RunnerOptions) *Runner {
	if opts.Shell == "" {
		opts.Shell = "/bin/zsh"
	}
	if opts.Command == "" {
		opts.Command = "codex"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.KillGrace == 0 {
		opts.KillGrace = 5 * time.Second
	}
	return &Runner{opts: opts}
}

// BuildArgs is the argument list of a turn. The prompt is not in it: it goes through stdin, so untrusted text never
// reaches the process list.
func (r *Runner) BuildArgs(turn Turn) []string {
	var shared []string
	// Images come first: the option takes values up to the next flag, so the prompt marker is never swallowed.
	for _, image := range turn.Images {
		shared = append(shared, "--image", image)
	}
	shared = append(shared,
		"--json", "--skip-git-repo-check", "--output-schema", turn.SchemaFile,
		// Never wait for an approval nobody can give, and keep user hooks out of automated turns.
		"-c", `approval_policy="never"`,
		"-c", fmt.Sprintf(`sandbox_mode="%s"`, turn.Sandbox),
		"-c", "notify=[]",
		// Nothing from the user's own config may widen what an automated turn reaches.
		"-c", "mcp_servers={}",
		"-c", `web_search="disabled"`,
		"-c", "sandbox_workspace_write.network_access=false",
		"-c", `shell_environment_policy.inherit="core"`,
	)
	if r.opts.Model != "" {
		shared = append(shared, "-m", r.opts.Model)
	}
	if turn.ResumeThread != "" {
		return append(append([]string{"exec", "resume"}, shared...), turn.ResumeThread, "-")
	}
	return append(append([]string{"exec", "--sandbox", string(turn.Sandbox)}, shared...), "-")
}

type line struct {
	events       []Event
	failure      error
	finalMessage string
}

const maxStepChars = 400

var shellWrapper = regexp.MustCompile(`^(?:/bin/)?(?:ba|z)?sh -lc (['"])([\s\S]*)['"]$`)

func readableCommand(command string) string {
	if match := shellWrapper.FindStringSubmatch(strings.TrimSpace(command)); match != nil {
		return strings.ReplaceAll(match[2], `\"`, `"`)
	}
	return command
}

func shortLine(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	runes := []rune(collapsed)
	if len(runes) > maxStepChars {
		return string(runes[:maxStepChars-1]) + "…"
	}
	return collapsed
}

// interpretLine turns one JSONL line of `codex exec --json` into chat events; unknown shapes are ignored.
func interpretLine(raw string, lang i18n.Lang) line {
	var event struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
		Error    struct {
			Message string `json:"message"`
		} `json:"error"`
		Item map[string]any `json:"item"`
	}
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return line{}
	}
	switch event.Type {
	case "thread.started":
		if event.ThreadID != "" {
			return line{events: []Event{{Type: "thread", ThreadID: event.ThreadID}}}
		}
	case "turn.failed":
		if event.Error.Message == "" {
			return line{failure: i18n.NewError("Codex turn failed")}
		}
		return line{failure: ClassifyFailure(event.Error.Message)}
	case "item.completed":
		return interpretItem(event.Item, lang)
	}
	return line{}
}

func interpretItem(item map[string]any, lang i18n.Lang) line {
	text := func(key string) string { s, _ := item[key].(string); return s }
	switch text("type") {
	case "agent_message":
		if message := text("text"); message != "" {
			return line{events: []Event{{Type: "message", Text: message}}, finalMessage: message}
		}
	case "command_execution":
		suffix := ""
		if code, ok := item["exit_code"].(float64); ok && code != 0 {
			suffix = i18n.T(lang, "  (exit %d)", int(code))
		}
		return line{events: []Event{{Type: "step", Text: "$ " + shortLine(readableCommand(text("command"))) + suffix}}}
	case "reasoning":
		if reasoning := strings.TrimSpace(text("text")); reasoning != "" {
			return line{events: []Event{{Type: "step", Text: shortLine(reasoning)}}}
		}
	case "file_change":
		var paths []string
		if changes, ok := item["changes"].([]any); ok {
			for _, change := range changes {
				if entry, ok := change.(map[string]any); ok {
					if path, ok := entry["path"].(string); ok {
						paths = append(paths, path)
					}
				}
			}
		}
		if len(paths) == 0 {
			return line{events: []Event{{Type: "step", Text: i18n.T(lang, "Edited files")}}}
		}
		return line{events: []Event{{Type: "step", Text: i18n.T(lang, "Edited %s", shortLine(strings.Join(paths, ", ")))}}}
	}
	return line{}
}

// Run runs one turn. Cancelling ctx stops the process and everything it started, and returns ErrCancelled.
func (r *Runner) Run(ctx context.Context, turn Turn) (TurnResult, error) {
	if ctx.Err() != nil {
		return TurnResult{}, ErrCancelled
	}
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancelTimeout()

	// `exec "$0" "$@"` runs the binary inside a login shell with the arguments passed as data, not as shell syntax.
	command := r.opts.Command
	if r.opts.ResolveCommand != nil {
		command = r.opts.ResolveCommand()
	}
	args := append([]string{"-lc", `exec "$0" "$@"`, command}, r.BuildArgs(turn)...)
	cmd := exec.Command(r.opts.Shell, args...)
	cmd.Dir = turn.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = os.Environ()
	if r.opts.Home != "" {
		cmd.Env = append(cmd.Env, "CODEX_HOME="+r.opts.Home)
	}
	cmd.Stdin = strings.NewReader(turn.Prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return TurnResult{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{buffer: &stderr, max: 2000}
	if err := cmd.Start(); err != nil {
		return TurnResult{}, i18n.Wrap(err, "Codex could not be started: %s", err)
	}

	finished := make(chan struct{})
	go func() {
		select {
		case <-timeoutCtx.Done():
			terminate(cmd, r.opts.KillGrace)
		case <-finished:
		}
	}()

	result := TurnResult{ThreadID: turn.ResumeThread}
	var failure error
	lang := i18n.FromContext(ctx)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		parsed := interpretLine(scanner.Text(), lang)
		for _, event := range parsed.events {
			if event.Type == "thread" {
				result.ThreadID = event.ThreadID
			}
			if turn.OnEvent != nil {
				turn.OnEvent(event)
			}
		}
		if parsed.finalMessage != "" {
			result.FinalMessage = parsed.finalMessage
		}
		if parsed.failure != nil {
			failure = parsed.failure
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	close(finished)

	switch {
	case ctx.Err() != nil:
		return TurnResult{}, ErrCancelled
	case timeoutCtx.Err() != nil:
		return TurnResult{}, i18n.NewError("Codex took more than %d min and was stopped", int(r.opts.Timeout.Minutes()))
	case failure != nil:
		return TurnResult{}, failure
	case waitErr != nil:
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			detail = ": " + shortLine(detail)
		}
		failure := ClassifyFailure(waitErr.Error() + detail)
		if errors.Is(failure, ErrLimit) || errors.Is(failure, ErrLogin) {
			return TurnResult{}, failure
		}
		return TurnResult{}, i18n.Wrap(waitErr, "Codex exited with %s%s", waitErr, detail)
	}
	return result, nil
}

// terminate stops the whole process group, then forces it if Codex does not leave.
func terminate(cmd *exec.Cmd, grace time.Duration) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(grace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
}

// limitedWriter keeps only the last max bytes written.
type limitedWriter struct {
	buffer *bytes.Buffer
	max    int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.buffer.Write(p)
	if over := w.buffer.Len() - w.max; over > 0 {
		w.buffer.Next(over)
	}
	return len(p), nil
}
