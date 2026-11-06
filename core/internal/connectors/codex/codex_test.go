package codex

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
)

func jwt(payload string) string {
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
}

func writeAuth(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

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

func TestValidateReadsTheSignedInAccount(t *testing.T) {
	home := t.TempDir()
	claims := `{"email":"gpt@example.test","name":"Example Account","https://api.openai.com/auth":{"chatgpt_plan_type":"plus","chatgpt_subscription_active_until":"2999-12-05T08:13:05+00:00","organizations":[{"title":"EXAMPLE WORKSPACE","is_default":true}]}}`
	writeAuth(t, home, `{"auth_mode":"chatgpt","tokens":{"id_token":"`+jwt(claims)+`"}}`)
	c := &Connector{Home: home, Exec: fakeExec(map[string][]string{"--version": {"codex-cli 0.164.0"}})}

	profile, err := c.Validate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Example Account" || profile.Subtitle != "gpt@example.test" {
		t.Fatalf("unexpected profile %+v", profile)
	}
	want := map[string]string{"Plan": "Plus", "Codex": "0.164.0", "Organization": "EXAMPLE WORKSPACE"}
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

func TestValidateExplainsWhatIsMissing(t *testing.T) {
	home := t.TempDir()
	missing := &Connector{Home: home, Exec: fakeExec(nil, "--version")}
	if _, err := missing.Validate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("got %v", err)
	}
	signedOut := &Connector{Home: home, Exec: fakeExec(map[string][]string{"--version": {"codex-cli 0.164.0"}})}
	if _, err := signedOut.Validate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "Not signed in") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateWarnsWhenTheSubscriptionEnded(t *testing.T) {
	home := t.TempDir()
	writeAuth(t, home, `{"tokens":{"id_token":"`+jwt(`{"email":"a@b.c","https://api.openai.com/auth":{"chatgpt_plan_type":"plus","chatgpt_subscription_active_until":"2020-01-01T00:00:00+00:00"}}`)+`"}}`)
	c := &Connector{Home: home, Exec: fakeExec(map[string][]string{"--version": {"codex-cli 1"}})}
	profile, err := c.Validate(context.Background(), nil)
	if err != nil || len(profile.Warnings) != 1 {
		t.Fatalf("want one warning, got %+v, %v", profile.Warnings, err)
	}
}

func TestLoginForwardsTheSignInAddressOnce(t *testing.T) {
	c := &Connector{Home: filepath.Join(t.TempDir(), "codex"), Exec: fakeExec(map[string][]string{
		"codex login": {"Starting local login server", "If your browser did not open, visit https://auth.openai.com/oauth/authorize?x=1", "https://auth.openai.com/other"},
	})}
	action, ok := c.Action("login")
	if !ok {
		t.Fatal("no login action")
	}
	var events []connector.Event
	if err := action(context.Background(), func(e connector.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	opens := 0
	for _, e := range events {
		if e.Type == "open" {
			opens++
			if !strings.HasPrefix(e.URL, "https://auth.openai.com/oauth/authorize") {
				t.Errorf("unexpected URL %q", e.URL)
			}
		}
	}
	if opens != 1 {
		t.Fatalf("want one open event, got %d in %+v", opens, events)
	}
}

func TestActionFailuresAreExplained(t *testing.T) {
	c := &Connector{Home: t.TempDir(), Exec: fakeExec(nil, "codex login", "npm")}
	for _, id := range []string{"login", "install"} {
		action, _ := c.Action(id)
		if err := action(context.Background(), func(connector.Event) {}); err == nil {
			t.Errorf("%s should fail", id)
		}
	}
	if _, ok := c.Action("nope"); ok {
		t.Fatal("unknown action accepted")
	}
}
