// Package codex connects Pouple to the local Codex CLI: finds it, installs it when it is missing, signs in with
// Pouple's own Codex home, and reads who is signed in from the login itself.
package codex

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
)

// ID is the connector id.
const ID = "codex"

// Exec runs a script in a login shell (so `codex`, `node` and `brew` resolve like in a terminal), calling onLine
// for every line of output. It returns an error when the script fails.
type Exec func(ctx context.Context, env []string, script string, onLine func(string)) error

// Connector is the Codex connection. Home is the folder used as CODEX_HOME: Pouple signs in on its own, so it
// never touches the login of the interactive CLI.
type Connector struct {
	Home string
	Exec Exec
}

func New(home string) *Connector { return &Connector{Home: home, Exec: loginShell} }

// loginShell runs the script with `$SHELL -lc`.
func loginShell(ctx context.Context, env []string, script string, onLine func(string)) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-lc", script)
	cmd.Env = append(os.Environ(), env...)
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
		ID:       ID,
		Name:     "Codex",
		Tagline:  "The assistant that looks into your requests",
		Required: true,
		Order:    2,
		Guide: []connector.GuideStep{
			{Title: "Install the Codex CLI", Body: "Pouple uses OpenAI's Codex on your Mac. If it is not installed, one click installs it."},
			{Title: "Sign in with ChatGPT", Body: "A browser page opens: sign in with the account you want Pouple to use. It has its own login and never touches your usual Codex."},
			{Title: "Check the account", Body: "Pouple shows who is signed in, so you can be sure it is the right one."},
		},
		Actions: []connector.ActionInfo{{ID: "install", Label: "Install Codex"}, {ID: "login", Label: "Sign in with ChatGPT"}},
	}
}

func (c *Connector) env() []string { return []string{"CODEX_HOME=" + c.Home} }

func (c *Connector) Action(id string) (connector.Action, bool) {
	switch id {
	case "install":
		return c.install, true
	case "login":
		return c.login, true
	}
	return nil, false
}

// installScript prefers npm (the official package), then Homebrew.
const installScript = `
if command -v npm >/dev/null 2>&1; then
  echo "Installing with npm…"; npm install -g @openai/codex
elif command -v brew >/dev/null 2>&1; then
  echo "Installing with Homebrew…"; brew install codex
else
  echo "Neither npm nor Homebrew was found. Install Homebrew from https://brew.sh, then try again."; exit 3
fi`

func (c *Connector) install(ctx context.Context, emit func(connector.Event)) error {
	err := c.Exec(ctx, nil, installScript, func(line string) { emit(connector.Event{Type: "log", Text: line}) })
	if err != nil {
		return errors.New("Codex could not be installed: see the output above")
	}
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
	err := c.Exec(ctx, c.env(), "codex login", func(line string) {
		if url := urlPattern.FindString(line); url != "" && !opened {
			opened = true
			// Codex opens the browser by itself; the address is also given to the interface as a fallback.
			emit(connector.Event{Type: "open", URL: strings.TrimRight(url, ".,)")})
			return
		}
		emit(connector.Event{Type: "log", Text: line})
	})
	if err != nil {
		return errors.New("Sign-in did not finish. Try again.")
	}
	return nil
}

type authFile struct {
	AuthMode string `json:"auth_mode"`
	APIKey   string `json:"OPENAI_API_KEY"`
	Tokens   struct {
		IDToken string `json:"id_token"`
	} `json:"tokens"`
}

type idClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Auth  struct {
		Plan          string `json:"chatgpt_plan_type"`
		ActiveUntil   string `json:"chatgpt_subscription_active_until"`
		Organizations []struct {
			Title     string `json:"title"`
			IsDefault bool   `json:"is_default"`
		} `json:"organizations"`
	} `json:"https://api.openai.com/auth"`
}

// decodeClaims reads the payload of a JWT. The signature is not checked: the token only supplies what is shown on
// the confirmation card, and the CLI is the one that uses it.
func decodeClaims(token string) (idClaims, error) {
	var claims idClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("the login file has no identity token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return claims, err
	}
	return claims, json.Unmarshal(payload, &claims)
}

func (c *Connector) Validate(ctx context.Context, _ connector.Values) (connector.Profile, error) {
	version := ""
	err := c.Exec(ctx, c.env(), "codex --version", func(line string) {
		if version == "" && strings.TrimSpace(line) != "" {
			version = strings.TrimSpace(line)
		}
	})
	if err != nil || version == "" {
		return connector.Profile{}, connector.Invalid("", "The Codex CLI is not installed yet. Click “Install Codex”.")
	}
	data, err := os.ReadFile(filepath.Join(c.Home, "auth.json"))
	if err != nil {
		return connector.Profile{}, connector.Invalid("", "Not signed in yet. Click “Sign in with ChatGPT”.")
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return connector.Profile{}, connector.Invalid("", "The Codex login file is unreadable. Sign in again.")
	}
	facts := []connector.Fact{{Label: "Codex", Value: strings.TrimPrefix(version, "codex-cli ")}}
	if auth.Tokens.IDToken == "" {
		if auth.APIKey == "" {
			return connector.Profile{}, connector.Invalid("", "Not signed in yet. Click “Sign in with ChatGPT”.")
		}
		return connector.Profile{Name: "OpenAI API key", Subtitle: "Billed per use", Facts: facts}, nil
	}
	claims, err := decodeClaims(auth.Tokens.IDToken)
	if err != nil {
		return connector.Profile{}, connector.Invalid("", "The Codex login is damaged. Sign in again.")
	}
	profile := connector.Profile{Name: firstNonEmpty(claims.Name, claims.Email), Subtitle: claims.Email}
	if claims.Auth.Plan != "" {
		facts = append([]connector.Fact{{Label: "Plan", Value: strings.ToUpper(claims.Auth.Plan[:1]) + claims.Auth.Plan[1:]}}, facts...)
	}
	for _, organization := range claims.Auth.Organizations {
		if organization.IsDefault {
			facts = append(facts, connector.Fact{Label: "Organization", Value: organization.Title})
		}
	}
	if claims.Auth.ActiveUntil != "" {
		if until, err := time.Parse(time.RFC3339, claims.Auth.ActiveUntil); err == nil {
			facts = append(facts, connector.Fact{Label: "Subscription until", Value: until.Format("2 Jan 2006")})
			if until.Before(time.Now()) {
				profile.Warnings = append(profile.Warnings, connector.Warning{Title: "Subscription ended", Detail: fmt.Sprintf("This ChatGPT plan ended on %s: Codex may refuse requests.", until.Format("2 Jan 2006"))})
			}
		}
	}
	profile.Facts = facts
	return profile, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
