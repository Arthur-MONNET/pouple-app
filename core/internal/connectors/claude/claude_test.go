package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// fakeExec answers by what the script runs: output lines for a command, and which commands fail.
func fakeExec(lines map[string][]string, failing ...string) Exec {
	return func(_ context.Context, _ []string, script string, onLine func(string)) error {
		for key, output := range lines {
			if strings.Contains(script, key) {
				for _, line := range output {
					onLine(line)
				}
			}
		}
		for _, f := range failing {
			if strings.Contains(script, f) {
				return errors.New("exit 1")
			}
		}
		return nil
	}
}

const signedIn = `{
  "loggedIn": true,
  "authMethod": "claude.ai",
  "apiProvider": "firstParty",
  "email": "claude@example.test",
  "orgId": "00000000-0000-0000-0000-000000000002",
  "orgName": "Example Org",
  "subscriptionType": "max"
}`

func TestValidateReadsTheSignedInAccountFromAuthStatus(t *testing.T) {
	c := &Connector{Home: t.TempDir(), Exec: fakeExec(map[string][]string{
		"--version":   {"2.1.272 (Claude Code)"},
		"auth status": strings.Split(signedIn, "\n"),
	})}
	profile, err := c.Validate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "claude@example.test" || profile.Subtitle != "Max" || len(profile.Warnings) != 0 {
		t.Fatalf("unexpected profile %+v", profile)
	}
	want := map[string]string{"Plan": "Max", "Organization": "Example Org", "Claude Code": "2.1.272"}
	for _, fact := range profile.Facts {
		if expected, ok := want[fact.Label]; ok && expected != fact.Value {
			t.Errorf("%s = %q, want %q", fact.Label, fact.Value, expected)
		}
		delete(want, fact.Label)
	}
	if len(want) != 0 {
		t.Fatalf("missing facts %v in %+v", want, profile.Facts)
	}
}

func TestValidateWithoutIdentityShowsTheVersionOnly(t *testing.T) {
	c := &Connector{Home: t.TempDir(), Exec: fakeExec(map[string][]string{
		"--version":   {"2.1.272 (Claude Code)"},
		"auth status": {`{"loggedIn": true, "authMethod": "claude.ai", "email": null, "orgName": null}`},
	})}
	profile, err := c.Validate(context.Background(), nil)
	if err != nil || profile.Name != "Claude" || len(profile.Facts) != 1 || profile.Facts[0].Label != "Claude Code" {
		t.Fatalf("profile %+v, %v", profile, err)
	}
}

func TestValidateWarnsWhenTheLoginIsNotASubscription(t *testing.T) {
	c := &Connector{Home: t.TempDir(), Exec: fakeExec(map[string][]string{
		"--version":   {"2.1.272 (Claude Code)"},
		"auth status": {`{"loggedIn": true, "authMethod": "console", "email": "a@b.c"}`},
	})}
	profile, err := c.Validate(context.Background(), nil)
	if err != nil || len(profile.Warnings) != 1 || profile.Subtitle != "Billed per use" {
		t.Fatalf("want one warning, got %+v, %v", profile, err)
	}
}

func TestValidateExplainsWhatIsMissing(t *testing.T) {
	missing := &Connector{Home: t.TempDir(), Exec: fakeExec(nil, "--version")}
	if _, err := missing.Validate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("got %v", err)
	}
	// A signed-out CLI exits with an error and still prints the JSON.
	signedOut := &Connector{Home: t.TempDir(), Exec: fakeExec(map[string][]string{
		"--version":   {"2.1.272 (Claude Code)"},
		"auth status": {`{"loggedIn": false, "authMethod": "none"}`},
	}, "auth status")}
	if _, err := signedOut.Validate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "Not signed in") {
		t.Fatalf("got %v", err)
	}
	old := &Connector{Home: t.TempDir(), Exec: fakeExec(map[string][]string{
		"--version":   {"1.0.0 (Claude Code)"},
		"auth status": {"error: unknown command 'auth'"},
	}, "auth status")}
	if _, err := old.Validate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "could not tell") {
		t.Fatalf("got %v", err)
	}
}

func TestEveryCommandRunsInPouplesOwnConfigurationFolder(t *testing.T) {
	home := filepath.Join(t.TempDir(), "claude")
	var envs [][]string
	c := &Connector{Home: home, Exec: func(_ context.Context, env []string, script string, onLine func(string)) error {
		envs = append(envs, env)
		if strings.Contains(script, "--version") {
			onLine("2.1.272 (Claude Code)")
		}
		if strings.Contains(script, "auth status") {
			onLine(signedIn)
		}
		return nil
	}}
	if _, err := c.Validate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	login, _ := c.Action("login")
	if err := login(context.Background(), func(connector.Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(envs) != 3 {
		t.Fatalf("expected three runs, got %d", len(envs))
	}
	for _, env := range envs {
		if len(env) != 1 || env[0] != "CLAUDE_CONFIG_DIR="+home {
			t.Errorf("env %v", env)
		}
	}
	if info, err := os.Stat(home); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the folder must exist and be private: %v %v", info, err)
	}
}

func TestLoginForwardsTheSignInAddressOnceAndHidesThePastePrompt(t *testing.T) {
	c := &Connector{Home: filepath.Join(t.TempDir(), "claude"), Exec: fakeExec(map[string][]string{
		"auth login --claudeai": {
			"Opening browser to sign in…",
			"If the browser didn't open, visit: https://claude.com/cai/oauth/authorize?code=true&state=1",
			"https://claude.com/other",
			"Paste code here if prompted > ",
		},
	})}
	login, ok := c.Action("login")
	if !ok {
		t.Fatal("no login action")
	}
	var events []connector.Event
	if err := login(context.Background(), func(e connector.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	opens := 0
	for _, e := range events {
		if e.Type == "open" {
			opens++
			if !strings.HasPrefix(e.URL, "https://claude.com/cai/oauth/authorize") {
				t.Errorf("unexpected URL %q", e.URL)
			}
		}
		if strings.Contains(e.Text, "Paste code") {
			t.Errorf("the paste prompt must not be shown: %+v", e)
		}
	}
	if opens != 1 {
		t.Fatalf("want one open event, got %d in %+v", opens, events)
	}
}

func TestInstallUsesTheOfficialInstallerOnlyWhenNeeded(t *testing.T) {
	var scripts []string
	installed := false
	c := &Connector{Home: t.TempDir(), Exec: func(_ context.Context, _ []string, script string, onLine func(string)) error {
		scripts = append(scripts, script)
		switch {
		case strings.Contains(script, "install.sh"):
			installed = true
			onLine("Installing…")
		case strings.Contains(script, "--version"):
			if !installed {
				return errors.New("claude: command not found")
			}
			onLine("2.1.272 (Claude Code)")
		}
		return nil
	}}
	install, _ := c.Action("install")
	var logs []string
	if err := install(context.Background(), func(e connector.Event) { logs = append(logs, e.Text) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(scripts, "\n"), "https://claude.ai/install.sh") || strings.Contains(strings.Join(scripts, "\n"), "sudo") {
		t.Fatalf("scripts %v", scripts)
	}
	scripts = nil
	if err := install(context.Background(), func(e connector.Event) { logs = append(logs, e.Text) }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(scripts, "\n"), "install.sh") {
		t.Fatalf("an installed CLI must not be installed again: %v", scripts)
	}
}

func TestActionFailuresAreExplained(t *testing.T) {
	c := &Connector{Home: t.TempDir(), Exec: fakeExec(nil, "auth login", "install.sh")}
	login, _ := c.Action("login")
	if err := login(context.Background(), func(connector.Event) {}); err == nil {
		t.Error("login should fail")
	}
	notInstalled := &Connector{Home: t.TempDir(), Exec: fakeExec(nil, "--version", "install.sh")}
	install, _ := notInstalled.Action("install")
	if err := install(context.Background(), func(connector.Event) {}); err == nil {
		t.Error("install should fail")
	}
	if _, ok := c.Action("nope"); ok {
		t.Fatal("unknown action accepted")
	}
}

func TestTheConnectorIsAnAlternativeToCodexAndTranslated(t *testing.T) {
	descriptor := (&Connector{}).Descriptor()
	if descriptor.Required || descriptor.Choice != "ai" || descriptor.ID != "claude" {
		t.Fatalf("%+v", descriptor)
	}
	english, french := descriptor.Localized(i18n.EN), descriptor.Localized(i18n.FR)
	for i := range english.Guide {
		if english.Guide[i].Title == french.Guide[i].Title || english.Guide[i].Body == french.Guide[i].Body {
			t.Errorf("guide step %d is not translated: %+v", i, french.Guide[i])
		}
	}
	for i := range english.Actions {
		if english.Actions[i].Label == french.Actions[i].Label {
			t.Errorf("action %d is not translated", i)
		}
	}
}
