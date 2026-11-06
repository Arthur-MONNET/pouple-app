package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// ProviderTester runs a minimal turn on a provider (the engine does, and counts it in the daily quota).
type ProviderTester interface {
	TestProvider(ctx context.Context, id providers.ID, model string) (engine.ProviderTest, error)
}

type suggestedModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type providerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	// Active is true for the provider that runs the turns now.
	Active bool `json:"active"`
	// Model is the model chosen for this provider; "" is the CLI's own default.
	Model           string           `json:"model"`
	SuggestedModels []suggestedModel `json:"suggestedModels"`
	// Note explains what can be typed in the model field.
	Note string `json:"note"`
}

// MountProviders adds GET /api/providers (the AIs, which one runs, their models) and
// POST /api/providers/{id}/test (one minimal read-only turn with the chosen model).
func MountProviders(file *config.File, registry *connector.Registry, tester ProviderTester) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) {
			lang := i18n.FromContext(r.Context())
			settings, err := file.Load()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(lang, "The settings could not be read")})
				return
			}
			active := settings.ActiveProvider()
			views := make([]providerView, 0, len(providers.All()))
			for _, id := range providers.All() {
				suggestions := []suggestedModel{}
				for _, suggestion := range providers.Suggestions(id) {
					suggestions = append(suggestions, suggestedModel{ID: suggestion.ID, Label: i18n.T(lang, suggestion.Label)})
				}
				views = append(views, providerView{
					ID: string(id), Name: id.Name(), Connected: settings.Connected(string(id)), Active: id == active,
					Model: settings.ModelOf(id), SuggestedModels: suggestions, Note: i18n.T(lang, providers.ModelNote(id)),
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{"active": string(active), "providers": views})
		})

		mux.HandleFunc("POST /api/providers/{id}/test", func(w http.ResponseWriter, r *http.Request) {
			lang := i18n.FromContext(r.Context())
			id, ok := providers.Parse(r.PathValue("id"))
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(lang, "The provider is unknown")})
				return
			}
			// The body is optional: it lets the interface test a model before saving it.
			var body struct {
				Model *string `json:"model"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(lang, "The request is not valid JSON")})
				return
			}
			settings, err := file.Load()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(lang, "The settings could not be read")})
				return
			}
			model := settings.ModelOf(id)
			if body.Model != nil {
				model = strings.TrimSpace(*body.Model)
			}
			writeJSON(w, http.StatusOK, testProvider(r.Context(), lang, tester, settings, id, model))
		})
	}
}

// providerTestResult is the answer of a test. A test that fails is still a normal answer: ok is false and message says
// why in the interface language; code lets the interface offer the right next step.
type providerTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	Model     string `json:"model"`
	Message   string `json:"message"`
	// Code is empty when the test passed, otherwise one of: not-connected, invalid-model, login, limit, quota,
	// timeout, failed.
	Code string `json:"code"`
}

func testProvider(ctx context.Context, lang i18n.Lang, tester ProviderTester, settings config.Settings, id providers.ID, model string) providerTestResult {
	failure := func(code string, err error) providerTestResult {
		return providerTestResult{Model: model, Message: i18n.ErrorText(lang, err), Code: code}
	}
	switch {
	case !providers.ValidModel(model):
		return failure("invalid-model", i18n.NewError("The model name “%s” is not valid", model))
	case !settings.Connected(string(id)):
		// Nothing is run, so nothing is counted in the quota.
		return failure("not-connected", i18n.NewError("%s is not connected yet. Connect it in Connectors.", id.Name()))
	}
	test, err := tester.TestProvider(i18n.WithLanguage(ctx, lang), id, model)
	if err != nil {
		var quota *engine.QuotaError
		switch {
		case errors.Is(err, engine.ErrLogin):
			return failure("login", err)
		case errors.Is(err, engine.ErrLimit):
			return failure("limit", err)
		case errors.As(err, &quota):
			return failure("quota", err)
		case errors.Is(err, engine.ErrTestTimeout):
			return failure("timeout", err)
		}
		return failure("failed", err)
	}
	return providerTestResult{OK: true, LatencyMS: test.Latency.Milliseconds(), Model: test.Model, Message: test.Reply}
}
