package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
)

const goodToken = "xoxp-" + "1111111111-2222222222-abcdefabcdef"
const goodAppToken = "xapp-1111111111-2222222222-abcdefabcdef"

func fakeSlack(t *testing.T, scopes string, authOK bool) *Connector {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps.connections.open" {
			if r.Header.Get("Authorization") == "Bearer "+goodAppToken {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "wss://example.com/connection"})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			t.Errorf("token not sent in the header: %q", r.Header.Get("Authorization"))
		}
		if strings.Contains(r.URL.String(), "xoxp") {
			t.Errorf("token leaked into the address: %s", r.URL)
		}
		w.Header().Set("X-OAuth-Scopes", scopes)
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "auth.test":
			if !authOK {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user_id": "U1", "user": "arthur", "team": "Example Workspace", "team_id": "T1"})
		case "users.info":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": map[string]any{"real_name": "Arthur Monnet", "profile": map[string]any{"display_name": "arthur", "title": "Developer", "image_192": "https://img/a.png"}}})
		}
	}))
	t.Cleanup(server.Close)
	return &Connector{HTTP: server.Client(), Base: server.URL}
}

func TestOptionalAppTokenIsValidatedSeparately(t *testing.T) {
	client := fakeSlack(t, allScopes(), true)
	values := connector.Values{"token": goodToken, AppTokenField: goodAppToken}
	if _, err := client.Validate(context.Background(), values); err != nil {
		t.Fatal(err)
	}
	values[AppTokenField] = "xapp-invalid"
	_, err := client.Validate(context.Background(), values)
	var invalid *connector.ValidationError
	if !errorsAs(err, &invalid) || invalid.Field != AppTokenField {
		t.Fatalf("want an app-token error, got %v", err)
	}
}

func allScopes() string {
	names := make([]string, len(Scopes))
	for i, s := range Scopes {
		names[i] = s.Name
	}
	return strings.Join(names, ",")
}

func TestValidateReturnsTheProfile(t *testing.T) {
	profile, err := fakeSlack(t, allScopes(), true).Validate(context.Background(), connector.Values{"token": goodToken})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Arthur Monnet" || profile.Subtitle != "Developer" || profile.AvatarURL == "" {
		t.Fatalf("unexpected profile %+v", profile)
	}
	if profile.Facts[0] != (connector.Fact{Label: "Workspace", Value: "Example Workspace"}) || len(profile.Warnings) != 0 {
		t.Fatalf("unexpected facts or warnings %+v", profile)
	}
}

func TestOptionalScopesBecomeWarnings(t *testing.T) {
	scopes := strings.ReplaceAll(allScopes(), ",files:read", "")
	profile, err := fakeSlack(t, scopes, true).Validate(context.Background(), connector.Values{"token": goodToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Warnings) != 1 || profile.Warnings[0].Title != "Missing files:read" {
		t.Fatalf("unexpected warnings %+v", profile.Warnings)
	}
}

func TestMissingRequiredScopeIsRefused(t *testing.T) {
	_, err := fakeSlack(t, "search:read,users:read", true).Validate(context.Background(), connector.Values{"token": goodToken})
	var invalid *connector.ValidationError
	if err == nil || !errorsAs(err, &invalid) || invalid.Field != "token" || !strings.Contains(invalid.Message, "im:history") {
		t.Fatalf("want a token error naming the missing scopes, got %v", err)
	}
}

func TestRejectedAndBotTokens(t *testing.T) {
	_, err := fakeSlack(t, "", false).Validate(context.Background(), connector.Values{"token": goodToken})
	if err == nil || !strings.Contains(err.Error(), "does not accept this token") {
		t.Fatalf("got %v", err)
	}
	_, err = (&Connector{}).Validate(context.Background(), connector.Values{"token": "xoxb-1111111111-2222-abc"})
	if err == nil || !strings.Contains(err.Error(), "bot token") {
		t.Fatalf("got %v", err)
	}
}

func TestManifestAsksForEveryScope(t *testing.T) {
	var manifest struct {
		OAuth struct {
			Scopes struct {
				User []string `json:"user"`
			} `json:"scopes"`
		} `json:"oauth_config"`
	}
	if err := json.Unmarshal([]byte(Manifest()), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.OAuth.Scopes.User) != len(Scopes) {
		t.Fatalf("manifest has %d scopes, want %d", len(manifest.OAuth.Scopes.User), len(Scopes))
	}
	if !strings.HasPrefix(CreateAppURL(), "https://api.slack.com/apps?new_app=1&manifest_json=") {
		t.Fatal("unexpected create URL")
	}
}

func errorsAs(err error, target **connector.ValidationError) bool {
	v, ok := err.(*connector.ValidationError)
	if ok {
		*target = v
	}
	return ok
}
