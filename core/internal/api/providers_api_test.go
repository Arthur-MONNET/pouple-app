package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type descriptorConnector struct{ d connector.Descriptor }

func (c descriptorConnector) Descriptor() connector.Descriptor { return c.d }
func (c descriptorConnector) Validate(context.Context, connector.Values) (connector.Profile, error) {
	return connector.Profile{Name: c.d.Name}, nil
}

type fakeTester struct {
	calls  []string
	result engine.ProviderTest
	err    error
}

func (f *fakeTester) TestProvider(_ context.Context, id providers.ID, model string) (engine.ProviderTest, error) {
	f.calls = append(f.calls, string(id)+":"+model)
	return f.result, f.err
}

// providerServer has Slack (required) and the two AIs (alternatives), the settings and provider routes.
func providerServer(t *testing.T, tester ProviderTester) (*Server, *config.File, *connector.Registry) {
	t.Helper()
	file := config.Open(filepath.Join(t.TempDir(), "config.json"))
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: file},
		descriptorConnector{connector.Descriptor{ID: "slack", Name: "Slack", Required: true, Order: 1}},
		descriptorConnector{connector.Descriptor{ID: "codex", Name: "Codex", Choice: "ai", Order: 2}},
		descriptorConnector{connector.Descriptor{ID: "claude", Name: "Claude", Choice: "ai", Order: 3}},
	)
	mountSettings, mountProviders := MountSettings(file), MountProviders(file, registry, tester, fakeCatalog{})
	server := New(Options{
		Registry: registry, Token: "secret-token",
		Language: func() string { s, _ := file.Load(); return s.UILanguage },
		Mount:    func(mux *http.ServeMux) { mountSettings(mux); mountProviders(mux) },
	}, port)
	return server, file, registry
}

func connect(t *testing.T, registry *connector.Registry, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := registry.Save(context.Background(), id, connector.Values{}); err != nil {
			t.Fatal(err)
		}
	}
}

func decode(t *testing.T, body string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("%v in %s", err, body)
	}
}

func TestSetupNeedsSlackAndOneOfTheTwoAIs(t *testing.T) {
	server, _, registry := providerServer(t, &fakeTester{})
	complete := func() bool {
		var state struct {
			SetupComplete bool `json:"setupComplete"`
			Steps         []struct {
				ID       string `json:"id"`
				Required bool   `json:"required"`
				Choice   string `json:"choice"`
			} `json:"steps"`
		}
		decode(t, call(server, "GET", "/api/state", "", nil).Body.String(), &state)
		for _, step := range state.Steps {
			if step.ID != "slack" && (step.Required || step.Choice != "ai") {
				t.Errorf("an AI is optional on its own and part of the ai choice: %+v", step)
			}
		}
		return state.SetupComplete
	}
	if complete() {
		t.Fatal("nothing connected")
	}
	connect(t, registry, "slack")
	if complete() {
		t.Fatal("Slack alone is not enough")
	}
	connect(t, registry, "claude")
	if !complete() {
		t.Fatal("Slack and Claude is a complete setup")
	}
	if err := registry.Remove("claude"); err != nil {
		t.Fatal(err)
	}
	connect(t, registry, "codex")
	if !complete() {
		t.Fatal("Slack and Codex is a complete setup")
	}
}

func TestSettingsExposeAndChangeTheProviderAndModels(t *testing.T) {
	server, _, registry := providerServer(t, &fakeTester{})
	var view Settings
	decode(t, call(server, "GET", "/api/settings", "", nil).Body.String(), &view)
	if view.Provider != "codex" || len(view.Models) != 2 || view.Models["codex"] != "" || view.Models["claude"] != "" {
		t.Fatalf("defaults: %+v", view)
	}

	connect(t, registry, "codex", "claude")
	r := put(server, `{"provider":"claude","models":{"claude":" opus "}}`)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	decode(t, r.Body.String(), &view)
	if view.Provider != "claude" || view.Models["claude"] != "opus" || view.Models["codex"] != "" {
		t.Fatalf("after: %+v", view)
	}
	// A partial update keeps what it does not name; "" goes back to the connected one and clears a model.
	decode(t, put(server, `{"models":{"codex":"gpt-x"}}`).Body.String(), &view)
	if view.Provider != "claude" || view.Models["claude"] != "opus" || view.Models["codex"] != "gpt-x" {
		t.Fatalf("partial: %+v", view)
	}
	decode(t, put(server, `{"provider":"","models":{"claude":""}}`).Body.String(), &view)
	if view.Provider != "codex" || view.Models["claude"] != "" || view.Models["codex"] != "gpt-x" {
		t.Fatalf("reset: %+v", view)
	}
}

func TestSettingsRefuseAnUnknownConnectedOrMalformedChoice(t *testing.T) {
	server, _, registry := providerServer(t, &fakeTester{})
	connect(t, registry, "codex")
	for name, body := range map[string]string{
		"unknown provider":           `{"provider":"gemini"}`,
		"provider not connected":     `{"provider":"claude"}`,
		"unknown provider in models": `{"models":{"gemini":"x"}}`,
		"model as an option":         `{"models":{"claude":"--dangerously-skip-permissions"}}`,
		"model with a space":         `{"models":{"codex":"a b"}}`,
		"model too long":             `{"models":{"codex":"` + strings.Repeat("a", 81) + `"}}`,
		"model with a shell word":    `{"models":{"codex":"$(id)"}}`,
	} {
		if r := put(server, body); r.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d %s", name, r.Code, r.Body)
		}
	}
	// Nothing of a refused update is kept, even the valid part.
	if r := put(server, `{"provider":"gemini","models":{"codex":"ok"}}`); r.Code != http.StatusUnprocessableEntity {
		t.Fatal("expected a refusal")
	}
	var view Settings
	decode(t, call(server, "GET", "/api/settings", "", nil).Body.String(), &view)
	if view.Models["codex"] != "" {
		t.Fatalf("a refused update must change nothing: %+v", view)
	}
	if r := put(server, `{"uiLanguage":"en"}`); r.Code != 200 {
		t.Fatal(r.Body)
	}
	if r := put(server, `{"provider":"gemini"}`); !strings.Contains(r.Body.String(), "must be “codex” or “claude”") {
		t.Fatalf("messages follow the interface language: %s", r.Body)
	}
}

func TestProvidersListsStatusModelsAndSuggestions(t *testing.T) {
	server, _, registry := providerServer(t, &fakeTester{})
	connect(t, registry, "claude")
	put(server, `{"models":{"claude":"sonnet"}}`)
	var list struct {
		Active    string `json:"active"`
		Providers []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Connected       bool   `json:"connected"`
			Active          bool   `json:"active"`
			Model           string `json:"model"`
			SuggestedModels []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"suggestedModels"`
			Note string `json:"note"`
		} `json:"providers"`
	}
	r := call(server, "GET", "/api/providers", "", nil)
	if r.Code != 200 {
		t.Fatal(r.Body)
	}
	decode(t, r.Body.String(), &list)
	if list.Active != "claude" || len(list.Providers) != 2 {
		t.Fatalf("%+v", list)
	}
	codex, claude := list.Providers[0], list.Providers[1]
	if codex.ID != "codex" || codex.Name != "Codex" || codex.Connected || codex.Active || codex.Model != "" || len(codex.SuggestedModels) != 0 || codex.Note == "" {
		t.Fatalf("codex: %+v", codex)
	}
	if claude.ID != "claude" || !claude.Connected || !claude.Active || claude.Model != "sonnet" || claude.Note == "" {
		t.Fatalf("claude: %+v", claude)
	}
	ids := map[string]bool{}
	for _, s := range claude.SuggestedModels {
		ids[s.ID] = true
		if s.Label == "" {
			t.Errorf("a suggestion needs a label: %+v", s)
		}
	}
	if !ids["opus"] || !ids["sonnet"] || !ids["haiku"] || !ids["claude-opus-5-5"] {
		t.Fatalf("suggestions %v", ids)
	}
	if !strings.Contains(r.Body.String(), `"suggestedModels":[]`) {
		t.Fatal("a provider without suggestions lists an empty array, never null")
	}
	put(server, `{"uiLanguage":"en"}`)
	english := call(server, "GET", "/api/providers", "", nil).Body.String()
	put(server, `{"uiLanguage":"fr"}`)
	french := call(server, "GET", "/api/providers", "", nil).Body.String()
	if !strings.Contains(english, "always the latest") || !strings.Contains(french, "toujours le dernier") || strings.Contains(french, "always the latest") {
		t.Fatalf("labels and notes follow the language:\n%s\n%s", english, french)
	}
}

func TestTestingAProviderRunsOneTurnWithTheSelectedModel(t *testing.T) {
	tester := &fakeTester{result: engine.ProviderTest{Model: "claude-sonnet-5", Latency: 1234 * time.Millisecond, Reply: "ok"}}
	server, _, registry := providerServer(t, tester)
	connect(t, registry, "claude")
	put(server, `{"models":{"claude":"sonnet"}}`)

	var result providerTestResult
	r := call(server, "POST", "/api/providers/claude/test", "", nil)
	decode(t, r.Body.String(), &result)
	if r.Code != 200 || !result.OK || result.LatencyMS != 1234 || result.Model != "claude-sonnet-5" || result.Message != "ok" || result.Code != "" {
		t.Fatalf("%d %+v", r.Code, result)
	}
	// The body can name a model not saved yet.
	call(server, "POST", "/api/providers/claude/test", `{"model":" haiku "}`, nil)
	if strings.Join(tester.calls, ",") != "claude:sonnet,claude:haiku" {
		t.Fatalf("calls %v", tester.calls)
	}
	if r := call(server, "POST", "/api/providers/gemini/test", "", nil); r.Code != http.StatusNotFound {
		t.Fatalf("unknown provider: %d", r.Code)
	}
	if r := call(server, "POST", "/api/providers/claude/test", "nope", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", r.Code)
	}
}

func TestAFailedTestSaysWhyAndSpendsNothingWhenItCannotRun(t *testing.T) {
	tester := &fakeTester{}
	server, _, registry := providerServer(t, tester)
	run := func(id, body string) providerTestResult {
		var result providerTestResult
		r := call(server, "POST", "/api/providers/"+id+"/test", body, nil)
		if r.Code != 200 {
			t.Fatalf("a failed test is still an answer: %d %s", r.Code, r.Body)
		}
		decode(t, r.Body.String(), &result)
		if result.OK || result.Message == "" || result.Code == "" {
			t.Fatalf("a failure carries a message and a code: %+v", result)
		}
		return result
	}
	if r := run("claude", ""); r.Code != "not-connected" || !strings.Contains(r.Message, "Claude n’est pas encore connecté") {
		t.Fatalf("%+v", r)
	}
	connect(t, registry, "claude")
	if r := run("claude", `{"model":"--evil"}`); r.Code != "invalid-model" {
		t.Fatalf("%+v", r)
	}
	if len(tester.calls) != 0 {
		t.Fatalf("nothing may run, so nothing is counted: %v", tester.calls)
	}

	var quota error = &engine.QuotaError{Limit: 5}
	for code, err := range map[string]error{
		"login":   i18n.Wrap(engine.ErrLogin, "Claude is not signed in any more. Open Connectors to sign in again."),
		"limit":   i18n.Wrap(engine.ErrLimit, "Claude's usage limit is reached. Try again later."),
		"quota":   quota,
		"timeout": i18n.Wrap(engine.ErrTestTimeout, "The test of Claude took more than 90 seconds and was stopped"),
		"failed":  errors.New("segmentation fault"),
	} {
		tester.err = err
		if r := run("claude", ""); r.Code != code {
			t.Errorf("%v: code %q, want %q", err, r.Code, code)
		}
	}
	tester.err = quota
	if r := run("claude", ""); !strings.Contains(r.Message, "limite quotidienne de 5 tours") {
		t.Fatalf("the quota message is localized: %q", r.Message)
	}
}

type fakeCatalog struct{}

func (fakeCatalog) Models(_ context.Context, id providers.ID) ([]providers.Suggestion, error) {
	if id == providers.Codex {
		return []providers.Suggestion{{ID: "gpt-6.1-sol", Label: "GPT-6.1-Sol"}}, nil
	}
	return nil, errors.New("no catalog")
}

func TestModelsEndpointListsTheCatalogAndSaysWhenItCannotBeRead(t *testing.T) {
	s, _, _ := providerServer(t, &fakeTester{})
	got := call(s, "GET", "/api/providers/codex/models", "", nil)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"id":"gpt-6.1-sol"`) || !strings.Contains(got.Body.String(), `"message":""`) {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	got = call(s, "GET", "/api/providers/claude/models", "", nil)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"models":[]`) || strings.Contains(got.Body.String(), `"message":""`) {
		t.Fatalf("an unreadable catalog is an empty list and a message: %d %s", got.Code, got.Body)
	}
	if got := call(s, "GET", "/api/providers/nobody/models", "", nil); got.Code != 404 {
		t.Fatalf("%d", got.Code)
	}
}
