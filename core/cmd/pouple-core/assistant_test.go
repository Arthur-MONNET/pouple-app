package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type fakeConnector struct{ d connector.Descriptor }

func (c fakeConnector) Descriptor() connector.Descriptor { return c.d }
func (c fakeConnector) Validate(context.Context, connector.Values) (connector.Profile, error) {
	return connector.Profile{Name: c.d.Name}, nil
}

func TestTheAssistantNeedsSlackAndOneAI(t *testing.T) {
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: config.Open(filepath.Join(t.TempDir(), "config.json"))},
		fakeConnector{connector.Descriptor{ID: "slack", Name: "Slack", Required: true, Order: 1}},
		fakeConnector{connector.Descriptor{ID: "codex", Name: "Codex", Choice: "ai", Order: 2}},
		fakeConnector{connector.Descriptor{ID: "claude", Name: "Claude", Choice: "ai", Order: 3}},
	)
	connect := func(id string) {
		t.Helper()
		if _, err := registry.Save(context.Background(), id, connector.Values{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := inactiveReason(registry, i18n.EN); got != "Slack is not connected yet" {
		t.Fatalf("nothing connected: %q", got)
	}
	connect("slack")
	if got := inactiveReason(registry, i18n.EN); got != "Connect one of these first: Codex, Claude" {
		t.Fatalf("Slack only: %q", got)
	}
	if got := inactiveReason(registry, i18n.FR); !strings.HasPrefix(got, "Connecte d’abord") {
		t.Fatalf("French: %q", got)
	}
	connect("claude")
	if got := inactiveReason(registry, i18n.EN); got != "" {
		t.Fatalf("Slack and Claude: %q", got)
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
