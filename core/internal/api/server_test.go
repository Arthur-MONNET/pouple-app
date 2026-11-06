package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type fakeConnector struct{ fail bool }

func (f fakeConnector) Descriptor() connector.Descriptor {
	return connector.Descriptor{ID: "fake", Name: "Fake", Required: true, Order: 1, Fields: []connector.Field{
		{ID: "token", Label: "Token", Secret: true, Pattern: `^tok-[a-z]+$`, PatternHint: "must start with tok-"},
		{ID: "region", Label: "Region"},
	}}
}

func (f fakeConnector) Validate(_ context.Context, values connector.Values) (connector.Profile, error) {
	if f.fail {
		return connector.Profile{}, connector.Invalid("token", "refused")
	}
	return connector.Profile{Name: "Ada", Facts: []connector.Fact{{Label: "Token", Value: "ok"}}}, nil
}

func (f fakeConnector) Action(id string) (connector.Action, bool) {
	if id != "go" {
		return nil, false
	}
	return func(_ context.Context, emit func(connector.Event)) error {
		emit(connector.Event{Type: "log", Text: "working"})
		return nil
	}, true
}

const port = 4310

func newServer(t *testing.T, fail bool) (*Server, *secrets.Memory, *config.File) {
	t.Helper()
	vault := secrets.NewMemory()
	file := config.Open(t.TempDir() + "/config.json")
	registry := connector.NewRegistry(vault, config.Connectors{File: file}, fakeConnector{fail: fail})
	ui := fstest.MapFS{"index.html": {Data: []byte("<head><!--POUPLE_BOOT--></head>")}, "app.js": {Data: []byte("1")}}
	return New(Options{Registry: registry, Token: "secret-token", UI: ui}, port), vault, file
}

func call(s *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1:4310"+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret-token")
	for k, v := range headers {
		if v == "" {
			request.Header.Del(k)
		} else {
			request.Header.Set(k, v)
		}
	}
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, request)
	return recorder
}

func TestRefusesCallsThatDoNotComeFromTheApp(t *testing.T) {
	s, _, _ := newServer(t, false)
	if got := call(s, "GET", "/api/state", "", map[string]string{"Authorization": ""}).Code; got != 401 {
		t.Errorf("no token: %d", got)
	}
	if got := call(s, "GET", "/api/state", "", map[string]string{"Authorization": "Bearer nope"}).Code; got != 401 {
		t.Errorf("wrong token: %d", got)
	}
	if got := call(s, "GET", "/api/state", "", map[string]string{"Sec-Fetch-Site": "cross-site"}).Code; got != 403 {
		t.Errorf("cross-site: %d", got)
	}
	rebinding := httptest.NewRequest("GET", "http://evil.example:4310/api/state", nil)
	rebinding.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, rebinding)
	if recorder.Code != 403 {
		t.Errorf("foreign host: %d", recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("a CORS header must never be sent")
	}
}

func TestPageInjectsTheTokenAndFallsBackToIndex(t *testing.T) {
	s, _, _ := newServer(t, false)
	for _, path := range []string{"/", "/conversations/12"} {
		body := call(s, "GET", path, "", map[string]string{"Authorization": ""}).Body.String()
		if !strings.Contains(body, `window.__POUPLE__={"token":"secret-token"}`) {
			t.Errorf("%s: token not injected: %s", path, body)
		}
	}
	if got := call(s, "GET", "/app.js", "", nil).Body.String(); got != "1" {
		t.Errorf("asset: %q", got)
	}
	if got := call(s, "GET", "/missing.js", "", nil).Code; got != 404 {
		t.Errorf("missing asset: %d", got)
	}
}

func TestSaveStoresSecretsInTheVaultAndNeverAnswersThemBack(t *testing.T) {
	s, vault, file := newServer(t, false)

	bad := call(s, "PUT", "/api/connectors/fake", `{"values":{"token":"nope","region":"eu"}}`, nil)
	if bad.Code != 422 || !strings.Contains(bad.Body.String(), "must start with tok-") {
		t.Fatalf("pattern not enforced: %d %s", bad.Code, bad.Body)
	}
	if _, err := vault.Get("fake.token"); err == nil {
		t.Fatal("a refused value must not be stored")
	}

	ok := call(s, "PUT", "/api/connectors/fake", `{"values":{"token":"tok-abc","region":"eu"}}`, nil)
	if ok.Code != 200 {
		t.Fatalf("save: %d %s", ok.Code, ok.Body)
	}
	if got, _ := vault.Get("fake.token"); got != "tok-abc" {
		t.Fatalf("secret not in the vault: %q", got)
	}
	settings, _ := file.Load()
	raw, _ := json.Marshal(settings)
	if strings.Contains(string(raw), "tok-abc") {
		t.Fatalf("secret written to the settings file: %s", raw)
	}
	for _, path := range []string{"/api/connectors", "/api/state"} {
		if body := call(s, "GET", path, "", nil).Body.String(); strings.Contains(body, "tok-abc") {
			t.Errorf("%s returned the secret", path)
		}
	}
	state := call(s, "GET", "/api/state", "", nil).Body.String()
	if !strings.Contains(state, `"setupComplete":true`) {
		t.Errorf("setup should be complete: %s", state)
	}
	if call(s, "DELETE", "/api/connectors/fake", "", nil).Code != 204 {
		t.Fatal("remove failed")
	}
	if _, err := vault.Get("fake.token"); err == nil {
		t.Fatal("remove must erase the secret")
	}
}

func TestValidationErrorsAreForTheUser(t *testing.T) {
	s, _, _ := newServer(t, true)
	response := call(s, "POST", "/api/connectors/fake/validate", `{"values":{"token":"tok-abc","region":"eu"}}`, nil)
	if response.Code != 422 || !strings.Contains(response.Body.String(), `"field":"token"`) {
		t.Fatalf("got %d %s", response.Code, response.Body)
	}
	if call(s, "POST", "/api/connectors/zzz/validate", `{}`, nil).Code != 404 {
		t.Fatal("unknown connector should be 404")
	}
}

func TestActionsStreamEvents(t *testing.T) {
	s, _, _ := newServer(t, false)
	response := call(s, "POST", "/api/connectors/fake/actions/go", "", nil)
	body, _ := io.ReadAll(response.Body)
	text := string(body)
	if !strings.Contains(text, `"type":"log"`) || !strings.HasSuffix(strings.TrimSpace(text), `{"type":"done"}`) {
		t.Fatalf("unexpected stream %q", text)
	}
	if call(s, "POST", "/api/connectors/fake/actions/nope", "", nil).Code != http.StatusNotFound {
		t.Fatal("unknown action should be 404")
	}
}

func TestThePageNeverCarriesTheTokenWhenTheShellInjectsIt(t *testing.T) {
	s, _, _ := newServer(t, false)
	s.opts.TokenFromShell = true
	recorder := call(s, "GET", "/", "", map[string]string{"Authorization": ""})
	if strings.Contains(recorder.Body.String(), "secret-token") || strings.Contains(recorder.Body.String(), "__POUPLE__") {
		t.Fatalf("the page leaked the token: %s", recorder.Body)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("no session cookie may be handed to an unauthenticated visitor")
	}
	// the standalone development mode keeps its convenience, and it is the only one
	s.opts.TokenFromShell = false
	if !strings.Contains(call(s, "GET", "/", "", map[string]string{"Authorization": ""}).Body.String(), "secret-token") {
		t.Fatal("standalone mode should still inject the token")
	}
}
