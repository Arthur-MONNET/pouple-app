package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Arthur-MONNET/pouple/core/internal/api"
	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/asana"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type required struct{ id string }

func (r required) Descriptor() connector.Descriptor {
	return connector.Descriptor{ID: r.id, Name: r.id, Required: true, Order: 1, Fields: []connector.Field{{ID: "token", Label: "token", Secret: true}}}
}
func (required) Validate(context.Context, connector.Values) (connector.Profile, error) {
	return connector.Profile{Name: "Arthur"}, nil
}

// fakeServices answers every optional service well, wherever the connector looks.
func fakeServices(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			_, _ = w.Write([]byte(`{"id":1,"username":"arthur","name":"Arthur Monnet"}`))
		case strings.HasSuffix(r.URL.Path, "/personal_access_tokens/self"):
			_, _ = w.Write([]byte(`{"scopes":["read_api"],"active":true}`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/users/me"):
			_, _ = w.Write([]byte(`{"data":{"gid":"u1","name":"Arthur Monnet","workspaces":[]}}`))
		case strings.HasSuffix(r.URL.Path, "/me"):
			_, _ = w.Write([]byte(`{"displayName":"Arthur Monnet","email":"a@example.com","organizationName":"Acme"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func optionalSetup(t *testing.T) (*api.Server, *secrets.Memory, *config.File, *connector.Registry) {
	t.Helper()
	services := fakeServices(t)
	vault := secrets.NewMemory()
	file := config.Open(t.TempDir() + "/config.json")
	registry := connector.NewRegistry(vault, config.Connectors{File: file},
		required{"slack"}, required{"codex"},
		&gitlab.Connector{HTTP: services.Client(), API: services.URL + "/api/v4"},
		&asana.Connector{HTTP: services.Client(), Base: services.URL},
		&slite.Connector{HTTP: services.Client(), Base: services.URL},
	)
	ui := fstest.MapFS{"index.html": {Data: []byte("<head><!--POUPLE_BOOT--></head>")}}
	return api.New(api.Options{Registry: registry, Token: "secret-token", UI: ui, Language: func() string { return "fr" }}, 4310), vault, file, registry
}

func do(s *api.Server, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1:4310"+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, request)
	return recorder
}

func TestOptionalConnectorsAreListedConnectedAndRemovedWithoutARestart(t *testing.T) {
	server, vault, file, registry := optionalSetup(t)

	var listed struct {
		Connectors []connector.Status `json:"connectors"`
	}
	response := do(server, "GET", "/api/connectors", "")
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	order := map[string]int{}
	for _, status := range listed.Connectors {
		if status.Descriptor.Required {
			continue
		}
		order[status.Descriptor.ID] = status.Descriptor.Order
		if status.Connected || status.Descriptor.Tagline == "" || len(status.Descriptor.Guide) == 0 {
			t.Errorf("unexpected %+v", status)
		}
	}
	if len(order) != 3 || order["gitlab"] != 10 || order["asana"] != 11 || order["slite"] != 12 {
		t.Fatalf("optional connectors: %v", order)
	}
	if !strings.Contains(response.Body.String(), "Tes merge requests et tes relectures") {
		t.Fatalf("the descriptor is served in French: %s", response.Body)
	}

	state := func() bool {
		var body struct {
			SetupComplete bool `json:"setupComplete"`
		}
		_ = json.Unmarshal(do(server, "GET", "/api/state", "").Body.Bytes(), &body)
		return body.SetupComplete
	}
	if state() {
		t.Fatal("setup is not complete before the required connectors")
	}
	for _, id := range []string{"slack", "codex"} {
		if saved := do(server, "PUT", "/api/connectors/"+id, `{"values":{"token":"required-token"}}`); saved.Code != 200 {
			t.Fatal(saved.Body)
		}
	}
	if !state() {
		t.Fatal("setup depends on the two required connectors only")
	}

	gitlabToken, asanaToken, sliteKey := "glpat-"+"abcdefghij0123456789", "1/1234567890/abcdef:0123456789abcdef", "slite-key-0123456789"
	for id, body := range map[string]string{
		"gitlab": `{"values":{"url":"","token":"` + gitlabToken + `"}}`,
		"asana":  `{"values":{"token":"` + asanaToken + `"}}`,
		"slite":  `{"values":{"url":"https://your-team.slite.com","token":"` + sliteKey + `"}}`,
	} {
		saved := do(server, "PUT", "/api/connectors/"+id, body)
		if saved.Code != 200 || !strings.Contains(saved.Body.String(), "Arthur Monnet") {
			t.Fatalf("%s: %d %s", id, saved.Code, saved.Body)
		}
		// The secret is in the keychain only: not in the answer, not in config.json.
		if strings.Contains(saved.Body.String(), "abcdef") || strings.Contains(saved.Body.String(), "slite-key") {
			t.Fatalf("%s: a secret came back: %s", id, saved.Body)
		}
	}
	if !state() {
		t.Fatal("connecting optional services changes nothing to the setup")
	}
	settings, _ := file.Load()
	raw, _ := json.Marshal(settings)
	for _, secret := range []string{gitlabToken, asanaToken, sliteKey} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("a secret reached config.json: %s", raw)
		}
		if strings.Contains(string(do(server, "GET", "/api/connectors", "").Body.Bytes()), secret) {
			t.Fatal("a secret reached the API")
		}
	}
	if got, _ := vault.Get("gitlab.token"); got != gitlabToken {
		t.Fatalf("the GitLab token must be in the keychain: %q", got)
	}
	// What the engine reads at each request, secrets included, straight from the registry.
	values, err := registry.Values("gitlab")
	if err != nil || values["url"] != "https://gitlab.com" || values["token"] != gitlabToken {
		t.Fatalf("values %v, %v", values, err)
	}

	if removed := do(server, "DELETE", "/api/connectors/asana", ""); removed.Code != 204 {
		t.Fatal(removed.Body)
	}
	if _, err := vault.Get("asana.token"); err == nil {
		t.Fatal("a removed connector must not leave its secret behind")
	}
	if _, err := registry.Values("asana"); err == nil {
		t.Fatal("a removed connector is not connected any more")
	}
}

func TestAnAddressThatIsRefusedStopsTheConnection(t *testing.T) {
	server, vault, _, _ := optionalSetup(t)
	saved := do(server, "PUT", "/api/connectors/gitlab", `{"values":{"url":"https://169.254.169.254","token":"glpat-abcdefghij0123456789"}}`)
	if saved.Code != 422 || !strings.Contains(saved.Body.String(), `"field":"url"`) {
		t.Fatalf("%d %s", saved.Code, saved.Body)
	}
	if _, err := vault.Get("gitlab.token"); err == nil {
		t.Fatal("nothing is stored for a refused connection")
	}
}
