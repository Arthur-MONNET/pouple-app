package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const goodToken = "github_pat_" + "abcdefghij0123456789"

// fakeGitHub answers the few API routes Pouple uses, the way api.github.com does.
type fakeGitHub struct {
	mu         sync.Mutex
	scopes     *string // X-OAuth-Scopes; nil = a fine-grained token (no header)
	sso        string
	expiration string
	userStatus int
	userBody   string
	searchCode int
	seen       []string
	api        string
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.URL.String())
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		if strings.Contains(r.URL.String(), "github_pat") {
			t.Errorf("the token must travel in the Authorization header only: %s", r.URL)
		}
		if f.scopes != nil {
			w.Header().Set("X-OAuth-Scopes", *f.scopes)
		}
		if f.sso != "" {
			w.Header().Set("X-GitHub-SSO", f.sso)
		}
		if f.expiration != "" {
			w.Header().Set("GitHub-Authentication-Token-Expiration", f.expiration)
		}
		switch {
		case r.URL.Path == "/user":
			if f.userStatus != 0 {
				w.WriteHeader(f.userStatus)
				return
			}
			if f.userBody != "" {
				_, _ = w.Write([]byte(f.userBody))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "arthur-monnet", "name": "Arthur Monnet", "avatar_url": "https://avatars.githubusercontent.com/u/1?v=4", "type": "User"})
		case r.URL.Path == "/search/issues":
			if f.searchCode != 0 {
				w.WriteHeader(f.searchCode)
				return
			}
			total := 4
			if strings.Contains(r.URL.Query().Get("q"), "review-requested:@me") {
				total = 2
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": total, "items": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	f.api = server.URL
	return server
}

func (f *fakeGitHub) connector(t *testing.T) *Connector {
	server := f.server(t)
	return &Connector{HTTP: server.Client(), API: server.URL, now: func() time.Time { return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) }}
}

func scopes(value string) *string { return &value }

func validate(t *testing.T, f *fakeGitHub, values connector.Values) (connector.Profile, error) {
	t.Helper()
	if values == nil {
		values = connector.Values{"token": goodToken}
	}
	return f.connector(t).Validate(context.Background(), values)
}

func fieldOf(err error) string {
	var invalid *connector.ValidationError
	if errors.As(err, &invalid) {
		return invalid.Field
	}
	return ""
}

func warningTitles(profile connector.Profile) string {
	var titles []string
	for _, warning := range profile.Warnings {
		titles = append(titles, warning.Title)
	}
	return strings.Join(titles, " | ")
}

func TestAFineGrainedTokenShowsTheAccountAndThePermissionsItNeeds(t *testing.T) {
	f := &fakeGitHub{expiration: "2027-02-01 00:00:00 UTC"}
	profile, err := validate(t, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Arthur Monnet" || profile.Subtitle != "@arthur-monnet" || profile.AvatarURL != "https://avatars.githubusercontent.com/u/1?v=4" {
		t.Fatalf("profile %+v", profile)
	}
	facts := map[string]string{}
	for _, fact := range profile.Facts {
		facts[fact.Label] = fact.Value
	}
	if facts["Host"] != "github.com" || facts["Open pull requests"] != "4" || facts["Reviews requested"] != "2" || facts["Token expires"] != "1 Feb 2027" {
		t.Fatalf("facts %+v", facts)
	}
	if got := warningTitles(profile); got != "Check the token's permissions" {
		t.Fatalf("warnings %q", got)
	}
	if !strings.Contains(profile.Warnings[0].Detail, "Pull requests, Checks, Commit statuses, Contents and Metadata") {
		t.Errorf("the needed permissions are listed: %q", profile.Warnings[0].Detail)
	}
}

func TestClassicTokenScopesAreReadFromTheHeader(t *testing.T) {
	cases := map[string]string{
		"repo, read:org": "Token can change things",
		"public_repo":    "Public repositories only",
		"":               "Missing repository scope",
		"read:user":      "Missing repository scope",
	}
	for header, want := range cases {
		profile, err := validate(t, &fakeGitHub{scopes: scopes(header)}, nil)
		if err != nil || warningTitles(profile) != want {
			t.Errorf("scopes %q: %q %v", header, warningTitles(profile), err)
		}
	}
}

func TestSingleSignOnAndExpiryAreWarnings(t *testing.T) {
	f := &fakeGitHub{scopes: scopes("repo"), sso: "partial-results; organizations=21955855", expiration: "2026-11-25 10:00:00 UTC"}
	profile, err := validate(t, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := warningTitles(profile)
	for _, want := range []string{"Single sign-on authorization needed", "Token expires soon"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestErrorsLandOnTheRightField(t *testing.T) {
	cases := []struct {
		name   string
		fake   *fakeGitHub
		values connector.Values
		field  string
	}{
		{"bad token", &fakeGitHub{}, connector.Values{"token": "ghp_" + "wrongwrongwrongwrongwrong"}, "token"},
		{"forbidden", &fakeGitHub{userStatus: http.StatusForbidden}, nil, "token"},
		{"not an API", &fakeGitHub{userStatus: http.StatusNotFound}, nil, "url"},
		{"not JSON", &fakeGitHub{userBody: "<html>"}, nil, "url"},
		{"no login", &fakeGitHub{userBody: `{"name":"x"}`}, nil, "url"},
		{"search refused", &fakeGitHub{searchCode: http.StatusForbidden}, nil, "token"},
		{"a path", &fakeGitHub{}, connector.Values{"token": goodToken, "url": "https://api.github.com/repos"}, "url"},
		{"credentials", &fakeGitHub{}, connector.Values{"token": goodToken, "url": "https://user:pass@github.example.com"}, "url"},
	}
	for _, c := range cases {
		_, err := validate(t, c.fake, c.values)
		if fieldOf(err) != c.field {
			t.Errorf("%s: field %q (%v), want %q", c.name, fieldOf(err), err, c.field)
		}
	}
	_, err := validate(t, &fakeGitHub{}, connector.Values{"token": "ghp_" + "wrongwrongwrongwrongwrong"})
	if got := i18n.ErrorText(i18n.FR, err); !strings.HasPrefix(got, "GitHub n’accepte pas ce jeton") {
		t.Errorf("French message: %q", got)
	}
}

func TestTheDescriptorMatchesTheConnectionForm(t *testing.T) {
	d := New().Descriptor()
	if d.ID != "github" || d.Required || d.Order != 11 || len(d.Fields) != 2 {
		t.Fatalf("%+v", d)
	}
	token, address := d.Fields[0], d.Fields[1]
	if token.ID != "token" || !token.Secret || token.Optional || address.ID != "url" || address.Secret || !address.Optional || address.Default != DefaultURL {
		t.Fatalf("fields %+v", d.Fields)
	}
	for _, good := range []string{"ghp_abcdefghijklmnopqrstuvwx", "github_pat_11ABCDEFG0abcdefghijkl_mnopqrstuvwxyz"} {
		if !matches(token.Pattern, good) {
			t.Errorf("%q is a GitHub token", good)
		}
	}
	for _, bad := range []string{"glpat-abcdefghijklmnopqrstu", "ghp_short", "gho_abcdefghijklmnopqrstuvwx"} {
		if matches(token.Pattern, bad) {
			t.Errorf("%q is not a personal access token", bad)
		}
	}
}
