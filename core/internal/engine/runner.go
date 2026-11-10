package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Sandbox is how much a turn may touch.
type Sandbox string

const (
	ReadOnly       Sandbox = "read-only"
	WorkspaceWrite Sandbox = "workspace-write"
)

// Event is what an AI CLI reports while it works, reduced to what the chat shows.
type Event struct {
	Type     string // thread | step | message
	ThreadID string
	Text     string
}

// Turn is one run of the AI.
type Turn struct {
	Prompt  string
	Dir     string
	Sandbox Sandbox
	// Model is the model to use; "" leaves the choice to the CLI.
	Model string
	// ResumeThread continues an earlier conversation of the same provider.
	ResumeThread string
	// SchemaFile holds the JSON schema the final answer must follow; "" asks for free text.
	SchemaFile string
	Images     []string
	// Web lets the turn search the web and read public pages; only analysis turns the user allowed it for.
	Web     bool
	OnEvent func(Event)
}

// TurnResult is what a turn produced.
type TurnResult struct {
	ThreadID string
	// Model is the model the CLI says it used, when it says (Claude does); "" otherwise.
	Model string
	// FinalMessage is the last agent message, empty when there was none.
	FinalMessage string
}

// Provider runs turns for one AI (the Codex CLI or the Claude Code CLI). The interface lets the service be tested
// without a process, and lets it pick the AI at each turn.
type Provider interface {
	Run(ctx context.Context, turn Turn) (TurnResult, error)
}

// Failure kinds. They are distinct because each asks for something different from the user.
var (
	// ErrCancelled: the user stopped the turn.
	ErrCancelled = errors.New("stopped")
	// ErrLimit: the account used up what its plan allows for now.
	ErrLimit = errors.New("usage limit reached")
	// ErrLogin: the AI is not signed in, or its login expired.
	ErrLogin = errors.New("not signed in")
)

var (
	limitPattern = regexp.MustCompile(`(?i)usage limit|rate.?limit|quota|too many requests|\b429\b|try again (later|in|at)|hit your .{0,20}limit|limit reached|credit balance|out of (extra )?usage`)
	loginPattern = regexp.MustCompile(`(?i)not logged in|log ?in again|please (log|sign) ?in|please run /login|login expired|unauthori[sz]ed|\b401\b|token (has )?expired|authentication|invalid (x-)?api[- ]key`)
)

// ClassifyFailure turns what an AI CLI said into an error of the right kind.
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
	shared = append(shared, "--json", "--skip-git-repo-check")
	if turn.SchemaFile != "" {
		shared = append(shared, "--output-schema", turn.SchemaFile)
	}
	shared = append(shared,
		// Never wait for an approval nobody can give, and keep user hooks out of automated turns.
		"-c", `approval_policy="never"`,
		"-c", fmt.Sprintf(`sandbox_mode="%s"`, turn.Sandbox),
		"-c", "notify=[]",
		// Nothing from the user's own config may widen what an automated turn reaches.
		"-c", "mcp_servers={}",
		// Codex ships a built-in "apps" connector whose search tool reads any web page, whatever web_search says: found
		// by testing with web_search="disabled". The web is opened only through web_search, and only when allowed.
		"-c", "features.apps=false",
		"-c", fmt.Sprintf(`web_search="%s"`, webSearchMode(turn.Web)),
		"-c", "sandbox_workspace_write.network_access=false",
		"-c", `shell_environment_policy.inherit="core"`,
	)
	if model := firstNonEmpty(turn.Model, r.opts.Model); model != "" {
		shared = append(shared, "-m", model)
	}
	if turn.ResumeThread != "" {
		return append(append([]string{"exec", "resume"}, shared...), turn.ResumeThread, "-")
	}
	return append(append([]string{"exec", "--sandbox", string(turn.Sandbox)}, shared...), "-")
}

// webSearchMode is Codex's own web search tool: "live" searches and reads pages as they are now. It runs on OpenAI's
// side, not in the sandbox, which keeps its network closed.
func webSearchMode(allowed bool) string {
	if allowed {
		return "live"
	}
	return "disabled"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
			return line{failure: i18n.NewError("%s turn failed", "Codex")}
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
	command := r.opts.Command
	if r.opts.ResolveCommand != nil {
		command = r.opts.ResolveCommand()
	}
	spec := processSpec{
		Name: "Codex", Shell: r.opts.Shell, Command: command, Args: r.BuildArgs(turn), Dir: turn.Dir,
		// `exec "$0" "$@"` runs the binary inside a login shell with the arguments passed as data, not as shell syntax.
		Script: `exec "$0" "$@"`,
		Env:    os.Environ(), Stdin: turn.Prompt, Timeout: r.opts.Timeout, KillGrace: r.opts.KillGrace,
	}
	if r.opts.Home != "" {
		spec.Env = append(spec.Env, "CODEX_HOME="+r.opts.Home)
	}

	result := TurnResult{ThreadID: turn.ResumeThread}
	var failure error
	lang := i18n.FromContext(ctx)
	end, err := runProcess(ctx, spec, func(raw string) error {
		parsed := interpretLine(raw, lang)
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
		return nil
	})
	if err != nil {
		return TurnResult{}, err
	}
	if err := end.settle(spec, failure); err != nil {
		return TurnResult{}, err
	}
	return result, nil
}
