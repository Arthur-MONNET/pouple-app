// Package claude connects Pouple to the local Claude Code CLI: finds it, installs it with Anthropic's official
// installer when it is missing, signs in with Pouple's own configuration folder (CLAUDE_CONFIG_DIR), and reads who is
// signed in from `claude auth status`.
//
// Pouple's login is separate from the one of the interactive CLI: on macOS Claude Code keys its keychain entry to the
// configuration folder, so a folder of Pouple's own means a login of Pouple's own, and ~/.claude is never read.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// ID is the connector id, which is also the provider id.
const ID = "claude"

// Exec runs a script in a login shell (so an existing `claude` resolves like in a terminal), calling onLine for every
// line of output. It returns an error when the script fails.
type Exec func(ctx context.Context, env []string, script string, onLine func(string)) error

// Connector is the Claude connection. Home is the folder used as CLAUDE_CONFIG_DIR.
type Connector struct {
	Home string
	Exec Exec
}

func New(home string) *Connector { return &Connector{Home: home, Exec: loginShell} }

// loginShell runs the script with `$SHELL -lc`, in an environment that cannot choose another account or endpoint.
func loginShell(ctx context.Context, env []string, script string, onLine func(string)) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-lc", script)
	cmd.Env = providers.ChildEnv(os.Environ(), env...)
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
	}()
	err := cmd.Run()
	_ = writer.Close()
	<-done
	return err
}

func (c *Connector) Descriptor() connector.Descriptor {
	return connector.Descriptor{
		ID:      ID,
		Name:    "Claude",
		Tagline: "The assistant that looks into your requests",
		// The assistant needs Codex or Claude, whichever the user prefers: see connector.SetupGaps.
		Required: false,
		Choice:   "ai",
		Order:    3,
		Guide: []connector.GuideStep{
			{Title: "Install Claude Code", Body: "Pouple uses Anthropic's Claude Code on your Mac. If it is not installed, one click installs it with Anthropic's official installer."},
			{Title: "Sign in with Claude", Body: "A browser page opens: sign in with the Claude account you want Pouple to use. It has its own login and never touches your usual Claude Code."},
			{Title: "Check the account", Body: "Pouple shows who is signed in, so you can be sure it is the right one."},
		},
		Actions: []connector.ActionInfo{{ID: "install", Label: "Install Claude Code"}, {ID: "login", Label: "Sign in with Claude"}},
	}
}

func (c *Connector) env() []string { return []string{"CLAUDE_CONFIG_DIR=" + c.Home} }

// script runs the CLI with arguments. Anthropic's installer puts the CLI in ~/.local/bin, which a login shell does not
// always list: it is appended to the PATH, after the user's own. exec replaces the shell, so stopping the action
// stops the CLI itself.
func script(args string) string {
	return `export PATH="$PATH:$HOME/.local/bin"; exec claude ` + args
}

// installScript is Anthropic's official installer for macOS and Linux. It needs no administrator rights and installs
// the CLI for the current user only.
const installScript = `set -o pipefail; curl -fsSL https://claude.ai/install.sh | bash`

func (c *Connector) Action(id string) (connector.Action, bool) {
	switch id {
	case "install":
		return c.install, true
	case "login":
		return c.login, true
	}
	return nil, false
}

// version is what `claude --version` says ("2.1.272"); ok is false when the CLI cannot be run.
func (c *Connector) version(ctx context.Context) (string, bool) {
	version := ""
	err := c.Exec(ctx, c.env(), script("--version"), func(line string) {
		if version == "" && strings.TrimSpace(line) != "" {
			version = strings.TrimSpace(line)
		}
	})
	version = strings.TrimSpace(strings.TrimSuffix(version, "(Claude Code)"))
	return version, err == nil && version != ""
}

func (c *Connector) install(ctx context.Context, emit func(connector.Event)) error {
	lang := i18n.FromContext(ctx)
	log := func(message string, args ...any) {
		emit(connector.Event{Type: "log", Text: i18n.T(lang, message, args...)})
	}
	if version, ok := c.version(ctx); ok {
		log("Claude Code is already installed (%s). You can now sign in.", version)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	log("Installing Claude Code with Anthropic’s official installer…")
	err := c.Exec(ctx, c.env(), installScript, func(line string) {
		emit(connector.Event{Type: "log", Text: line})
	})
	if err != nil {
		return i18n.Wrap(err, "Claude Code could not be installed. Check your internet connection and try again.")
	}
	if _, ok := c.version(ctx); !ok {
		return i18n.NewError("Claude Code was installed but cannot be started. Open a new Terminal window and run “claude --version” to see why.")
	}
	log("Claude Code is installed. You can now sign in.")
	return nil
}

var urlPattern = regexp.MustCompile(`https://\S+`)

func (c *Connector) login(ctx context.Context, emit func(connector.Event)) error {
	if err := os.MkdirAll(c.Home, 0o700); err != nil {
		return err
	}
	// A sign-in that nobody finishes must not wait forever.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	opened := false
	err := c.Exec(ctx, c.env(), script("auth login --claudeai"), func(line string) {
		if url := urlPattern.FindString(line); url != "" && !opened {
			opened = true
			// Claude Code opens the browser by itself; the address is also given to the interface as a fallback.
			emit(connector.Event{Type: "open", URL: strings.TrimRight(url, ".,)")})
			return
		}
		// The CLI also prints a prompt to paste a code by hand: there is nothing to answer it, so it is not shown.
		if strings.HasPrefix(strings.TrimSpace(line), "Paste code") {
			return
		}
		emit(connector.Event{Type: "log", Text: line})
	})
	if err != nil {
		return i18n.NewError("Sign-in did not finish. Try again.")
	}
	return nil
}

// authStatus is the part of `claude auth status` (JSON) that Pouple shows.
type authStatus struct {
	LoggedIn         bool    `json:"loggedIn"`
	AuthMethod       string  `json:"authMethod"`
	Email            *string `json:"email"`
	OrgName          *string `json:"orgName"`
	SubscriptionType *string `json:"subscriptionType"`
}

// parseStatus reads the JSON object out of the output, which may carry warnings around it.
func parseStatus(output string) (authStatus, bool) {
	var status authStatus
	start, end := strings.Index(output, "{"), strings.LastIndex(output, "}")
	if start < 0 || end < start {
		return status, false
	}
	return status, json.Unmarshal([]byte(output[start:end+1]), &status) == nil
}

func (c *Connector) Validate(ctx context.Context, _ connector.Values) (connector.Profile, error) {
	version, ok := c.version(ctx)
	if !ok {
		return connector.Profile{}, connector.Invalid("", "The Claude Code CLI is not installed yet. Click “Install Claude Code”.")
	}
	var output strings.Builder
	// A signed-out CLI exits with an error but still says so in the JSON: the output decides, not the exit code.
	_ = c.Exec(ctx, c.env(), script("auth status"), func(line string) { output.WriteString(line + "\n") })
	status, parsed := parseStatus(output.String())
	if !parsed {
		return connector.Profile{}, connector.Invalid("", "Claude Code could not tell who is signed in. Update Claude Code and try again.")
	}
	if !status.LoggedIn {
		return connector.Profile{}, connector.Invalid("", "Not signed in yet. Click “Sign in with Claude”.")
	}

	facts := []connector.Fact{}
	plan := ""
	if status.SubscriptionType != nil && *status.SubscriptionType != "" {
		plan = strings.ToUpper((*status.SubscriptionType)[:1]) + (*status.SubscriptionType)[1:]
		facts = append(facts, connector.Fact{Label: "Plan", Value: plan})
	}
	if status.OrgName != nil && *status.OrgName != "" {
		facts = append(facts, connector.Fact{Label: "Organization", Value: *status.OrgName})
	}
	facts = append(facts, connector.Fact{Label: "Claude Code", Value: version})

	profile := connector.Profile{Name: "Claude", Facts: facts}
	if status.Email != nil && *status.Email != "" {
		profile.Name, profile.Subtitle = *status.Email, plan
	}
	if status.AuthMethod != "claude.ai" {
		// A Console login is billed per use, which Pouple's daily limit does not protect against: say it.
		profile.Subtitle = "Billed per use"
		profile.Warnings = append(profile.Warnings, connector.Warning{
			Title:  "Not a Claude subscription",
			Detail: "This login is billed per use, not by a Claude plan. Sign in with a Claude subscription to avoid unexpected costs.",
		})
	}
	return profile, nil
}
