package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type accessStore map[string]connector.Saved

func (s accessStore) Load(id string) (connector.Saved, bool, error) {
	saved, ok := s[id]
	return saved, ok, nil
}

func (s accessStore) Save(id string, saved connector.Saved) error {
	s[id] = saved
	return nil
}

func (s accessStore) Remove(id string) error {
	delete(s, id)
	return nil
}

func TestAccessWithOneOrTwoTokens(t *testing.T) {
	const extra = "glpat-abcdefghijklmnopqrst"
	for _, firstScope := range []string{"api", "read_api"} {
		t.Run(firstScope, func(t *testing.T) {
			ctx := context.Background()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token := r.Header.Get("Private-Token")
				switch r.URL.Path {
				case "/user":
					fmt.Fprint(w, `{"username":"arthur"}`)
				case "/personal_access_tokens/self":
					scope := firstScope
					if token == extra {
						scope = "api"
					}
					json.NewEncoder(w).Encode(TokenInfo{Scopes: []string{scope}})
				case "/merge_requests":
					fmt.Fprint(w, `[]`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			client := &Connector{HTTP: server.Client(), API: server.URL}
			storage := accessStore{}
			vault := secrets.NewMemory()
			registry := connector.NewRegistry(vault, storage, client)
			if _, err := registry.Save(ctx, ID, values("https://gitlab.example.com", goodToken)); err != nil {
				t.Fatal(err)
			}
			// A pre-feature connection has no persisted scopes.
			delete(storage[ID].Values, primaryScopes)
			current := func() connector.Values {
				t.Helper()
				values, err := registry.Values(ID)
				if err != nil {
					t.Fatal(err)
				}
				return values
			}
			update := func(update AccessUpdate) Access {
				t.Helper()
				access, err := client.UpdateAccess(ctx, registry, update)
				if err != nil {
					t.Fatal(err)
				}
				return access
			}
			if ReadToken(current()) != goodToken || AccessOf(current()).Mode != ReadOnly {
				t.Fatal("lost legacy connection")
			}
			primaryLabel := "Reviews"
			access := update(AccessUpdate{Verify: true, PrimaryName: &primaryLabel})
			if access.Tokens[0].Name != primaryLabel || access.Tokens[0].Scopes[0] != firstScope {
				t.Fatal("primary metadata not saved")
			}
			write, readOnly := Write, ReadOnly
			if firstScope == "api" {
				update(AccessUpdate{Mode: &write})
				if token, err := WriteToken(current()); err != nil || token != goodToken {
					t.Fatal("single api token did not enable writes", err)
				}
				update(AccessUpdate{Mode: &readOnly})
				if ReadToken(current()) != goodToken {
					t.Fatal("single api token lost read access")
				}
				return
			}
			if _, err := client.UpdateAccess(ctx, registry, AccessUpdate{Mode: &write}); err == nil {
				t.Fatal("read_api enabled write mode")
			}
			access = update(AccessUpdate{Name: "MR", Token: extra})
			saved, _ := json.Marshal(storage[ID])
			if len(access.Tokens) != 2 || ReadToken(current()) != goodToken || strings.Contains(string(saved), "glpat-") {
				t.Fatal("adding a token changed the active connection or persisted a secret")
			}
			additional, primary := "additional", "primary"
			update(AccessUpdate{Selected: &additional})
			if ReadToken(current()) != extra {
				t.Fatal("read did not use selected token")
			}
			update(AccessUpdate{Mode: &write})
			if err := CheckPush(registry, []string{"git@gitlab.example.com:group/app.git"}); err != nil {
				t.Fatal(err)
			}
			update(AccessUpdate{Selected: &primary})
			if _, err := WriteToken(current()); err == nil {
				t.Fatal("previous write mode persisted after changing token")
			}
			for _, target := range []string{"git@gitlab.example.com:group/app.git", "ssh://git@altssh.gitlab.com:443/group/app", "https://gitlab.com/group/app"} {
				if err := CheckPush(registry, []string{target}); err == nil {
					t.Fatalf("read-only allowed push to %s", target)
				}
			}
			if err := CheckPush(registry, []string{"https://github.com/group/app"}); err != nil {
				t.Fatal(err)
			}
			if err := registry.Remove(ID); err != nil {
				t.Fatal(err)
			}
			if _, err := vault.Get(connector.SecretName(ID, secondaryToken)); err != secrets.ErrNotFound {
				t.Fatal("disconnect kept the additional token")
			}
		})
	}
}

func TestMRChecksAccessAgainBeforePost(t *testing.T) {
	ctx := context.Background()
	settings := connector.Values{"token": goodToken, primaryScopes: "api", accessMode: Write}
	if _, err := WriteToken(settings); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			t.Error("MR POST after write mode was disabled")
		case strings.Contains(r.URL.Path, "/repository/branches/"):
			fmt.Fprint(w, `{"name":"fix/logo"}`)
		case r.URL.Path == "/personal_access_tokens/self":
			fmt.Fprint(w, `{"scopes":["api"]}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	client := &Client{api: server.URL, token: goodToken, http: server.Client()}
	_, err := client.OpenMergeRequestChecked(ctx, "group/app", "fix/logo", "main", "Fix logo", func() error {
		settings[accessMode] = ReadOnly
		_, err := WriteToken(settings)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only refusal, got %v", err)
	}
}
