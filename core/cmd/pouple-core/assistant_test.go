package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slack"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type fakeConnector struct {
	d       connector.Descriptor
	profile string
}

func (c fakeConnector) Descriptor() connector.Descriptor { return c.d }
func (c fakeConnector) Validate(context.Context, connector.Values) (connector.Profile, error) {
	if c.profile != "" {
		return connector.Profile{Name: c.profile}, nil
	}
	return connector.Profile{Name: c.d.Name}, nil
}

// slackOnly is the real Slack descriptor with a fake validation, so that the setup reads what Slack really declares.
type slackOnly struct{ fakeConnector }

func (slackOnly) Descriptor() connector.Descriptor { return slack.New().Descriptor() }

func TestTheAssistantNeedsOneAIAndSlackIsOptional(t *testing.T) {
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: config.Open(filepath.Join(t.TempDir(), "config.json"))},
		slackOnly{fakeConnector{profile: "Arthur Monnet"}},
		fakeConnector{d: connector.Descriptor{ID: "codex", Name: "Codex", Choice: "ai", Order: 2}},
		fakeConnector{d: connector.Descriptor{ID: "claude", Name: "Claude", Choice: "ai", Order: 3}},
	)
	if got := inactiveReason(registry, i18n.EN); got != "Connect one of these first: Codex, Claude" {
		t.Fatalf("nothing connected: %q", got)
	}
	if got := inactiveReason(registry, i18n.FR); !strings.HasPrefix(got, "Connecte d’abord") {
		t.Fatalf("French: %q", got)
	}
	if _, err := registry.Save(context.Background(), "claude", connector.Values{}); err != nil {
		t.Fatal(err)
	}
	if got := inactiveReason(registry, i18n.EN); got != "" {
		t.Fatalf("Claude without Slack: %q", got)
	}
	statuses, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if !connector.SetupComplete(statuses) {
		t.Fatal("the setup is complete with one AI")
	}
}

func TestTheUserIsCalledByTheFirstConnectedProfileThenTheMacsName(t *testing.T) {
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: config.Open(filepath.Join(t.TempDir(), "config.json"))},
		fakeConnector{d: connector.Descriptor{ID: "slack", Name: "Slack", Order: 1}, profile: "Arthur Slack"},
		fakeConnector{d: connector.Descriptor{ID: "gitlab", Name: "GitLab", Order: 10}, profile: "Arthur-from-GitLab Monnet"},
	)
	mac := func() string { return "Mac User" }
	if got := firstNameOf(registry, mac); got != "Mac" {
		t.Fatalf("nothing connected: %q", got)
	}
	if got := firstNameOf(registry, func() string { return "  " }); got != "" {
		t.Fatalf("no name anywhere: %q", got)
	}
	if _, err := registry.Save(context.Background(), "gitlab", connector.Values{}); err != nil {
		t.Fatal(err)
	}
	if got := firstNameOf(registry, mac); got != "Arthur-from-GitLab" {
		t.Fatalf("GitLab only: %q", got)
	}
	if _, err := registry.Save(context.Background(), "slack", connector.Values{}); err != nil {
		t.Fatal(err)
	}
	if got := firstNameOf(registry, mac); got != "Arthur" {
		t.Fatalf("Slack first: %q", got)
	}
}

// Every secret of a connected connector is redacted, whatever the connector: not only the ones Pouple knew of before.
func TestTheRedactorKnowsTheSecretsOfEveryConnectedConnector(t *testing.T) {
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: config.Open(filepath.Join(t.TempDir(), "config.json"))},
		fakeConnector{d: connector.Descriptor{ID: "github", Name: "GitHub", Order: 11, Fields: []connector.Field{
			{ID: "url", Label: "Address", Optional: true},
			{ID: "token", Label: "Token", Secret: true},
		}}},
		fakeConnector{d: connector.Descriptor{ID: "other", Name: "Other", Order: 12, Fields: []connector.Field{
			{ID: "apiKey", Label: "Key", Secret: true},
		}}},
	)
	if _, err := registry.Save(context.Background(), "github", connector.Values{"url": "https://api.github.com", "token": "ghp_secretgithubtoken000000000"}); err != nil {
		t.Fatal(err)
	}
	got := connectorSecrets(registry)
	if len(got) != 1 || got[0] != "ghp_secretgithubtoken000000000" {
		t.Fatalf("secrets %v", got)
	}
	redacted := engine.NewRedactor(got...)("the token is ghp_secretgithubtoken000000000 at https://api.github.com")
	if strings.Contains(redacted, "secretgithubtoken") || !strings.Contains(redacted, "https://api.github.com") {
		t.Fatalf("redacted %q", redacted)
	}
}

func TestClaudeLoginTokensAreKnownToTheRedactor(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".credentials.json")
	if got := claudeSecrets(path); got != nil {
		t.Fatalf("no file, no secret: %v", got)
	}
	if err := os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-access","refreshToken":"sk-ant-ort01-refresh"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := claudeSecrets(path)
	if len(got) != 2 || got[0] != "sk-ant-oat01-access" || got[1] != "sk-ant-ort01-refresh" {
		t.Fatalf("%v", got)
	}
}

// Slack is optional: while it is not connected, its inbox says there is nothing to read rather than failing.
func TestSlackThatIsNotConnectedIsASourceOff(t *testing.T) {
	file := config.Open(filepath.Join(t.TempDir(), "config.json"))
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: file}, slackOnly{})
	source := &slackSource{registry: registry, settings: file}
	if _, err := source.Poll(context.Background(), nil); !errors.Is(err, engine.ErrSourceOff) {
		t.Fatalf("poll: %v", err)
	}
	if _, err := source.OwnMessages(context.Background(), 10); !errors.Is(err, engine.ErrSourceOff) {
		t.Fatalf("own messages: %v", err)
	}
	if _, err := source.Fetch(context.Background(), engine.PendingFile{ID: "F1"}); !errors.Is(err, engine.ErrSourceOff) {
		t.Fatalf("fetch: %v", err)
	}
}
