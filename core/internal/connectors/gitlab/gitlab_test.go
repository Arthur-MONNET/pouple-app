package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const goodToken = "glpat-" + "abcdefghij0123456789"

type fake struct {
	mu          sync.Mutex
	scopes      []string
	expires     string
	tokenStatus int // status of /personal_access_tokens/self, 0 = 200
	userStatus  int
	mrStatus    int
	userBody    string
	noPipeline  bool
	seen        []string
}

func (f *fake) connector(t *testing.T) *Connector {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.URL.String())
		f.mu.Unlock()
		if r.Header.Get("PRIVATE-TOKEN") != goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.URL.String(), "glpat") || r.Header.Get("Authorization") != "" {
			t.Errorf("the token must travel in the PRIVATE-TOKEN header only: %s", r.URL)
		}
		status := func(code int) bool {
			if code != 0 {
				w.WriteHeader(code)
				return true
			}
			return false
		}
		switch {
		case r.URL.Path == "/api/v4/user":
			if status(f.userStatus) {
				return
			}
			if f.userBody != "" {
				_, _ = w.Write([]byte(f.userBody))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "username": "arthur", "name": "Arthur Monnet", "email": "arthur@example.com", "avatar_url": "/uploads/avatar.png"})
		case r.URL.Path == "/api/v4/personal_access_tokens/self":
			if status(f.tokenStatus) {
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"scopes": f.scopes, "expires_at": f.expires, "active": true})
		case r.URL.Path == "/api/v4/merge_requests":
			if status(f.mrStatus) {
				return
			}
			w.Header().Set("X-Total", "4")
			if r.URL.Query().Get("per_page") == "1" {
				_, _ = w.Write([]byte(`[{"iid":1}]`))
				return
			}
			mr := map[string]any{"iid": 12, "project_id": 5, "title": "Fix login", "state": "opened", "web_url": "https://gitlab.example.com/group/sub/app/-/merge_requests/12",
				"source_branch": "fix", "target_branch": "main", "author": map[string]any{"username": "arthur"}, "detailed_merge_status": "mergeable", "updated_at": "2026-11-05T10:00:00Z"}
			if !f.noPipeline {
				mr["head_pipeline"] = map[string]any{"status": "failed"}
			}
			_ = json.NewEncoder(w).Encode([]any{mr, map[string]any{"nope": true}})
		case r.URL.Path == "/api/v4/projects/5/merge_requests/12":
			_, _ = w.Write([]byte(`{"iid":12,"head_pipeline":{"status":"success"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Connector{HTTP: server.Client(), API: server.URL + "/api/v4", now: func() time.Time { return time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC) }}
}

func values(url, token string) connector.Values { return connector.Values{"url": url, "token": token} }

func invalidField(t *testing.T, err error) (string, string) {
	t.Helper()
	invalid, ok := err.(*connector.ValidationError)
	if !ok {
		t.Fatalf("want a validation error, got %T %v", err, err)
	}
	return invalid.Field, invalid.Message
}

func TestValidateReturnsTheProfile(t *testing.T) {
	f := &fake{scopes: []string{"read_api"}, expires: "2027-03-01"}
	profile, err := f.connector(t).Validate(context.Background(), values("https://gitlab.example.com", goodToken))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Arthur Monnet" || profile.Subtitle != "@arthur" || profile.AvatarURL != "https://gitlab.example.com/uploads/avatar.png" {
		t.Fatalf("unexpected profile %+v", profile)
	}
	want := map[string]string{"Host": "gitlab.example.com", "E-mail": "arthur@example.com", "Token expires": "1 Mar 2027", "Open merge requests": "4"}
	for _, fact := range profile.Facts {
		if want[fact.Label] != fact.Value {
			t.Errorf("fact %q = %q", fact.Label, fact.Value)
		}
		delete(want, fact.Label)
	}
	if len(want) != 0 || len(profile.Warnings) != 0 {
		t.Fatalf("missing facts %v or unexpected warnings %+v", want, profile.Warnings)
	}
	for _, address := range f.seen {
		if strings.Contains(address, "glpat") {
			t.Fatalf("token in an address: %s", address)
		}
	}
}

func TestTheDefaultAddressIsGitLabDotCom(t *testing.T) {
	base, err := ParseBaseURL("")
	if err != nil || base.String() != "https://gitlab.com" {
		t.Fatalf("got %v, %v", base, err)
	}
	profile, err := (&fake{scopes: []string{"read_api"}}).connector(t).Validate(context.Background(), values("", goodToken))
	if err != nil || profile.Facts[0].Value != "gitlab.com" {
		t.Fatalf("got %+v, %v", profile, err)
	}
}

func TestWriteScopeAndNearExpiryAreWarnings(t *testing.T) {
	f := &fake{scopes: []string{"api"}, expires: "2026-11-10"}
	profile, err := f.connector(t).Validate(context.Background(), values("https://gitlab.example.com", goodToken))
	if err != nil || len(profile.Warnings) != 2 || profile.Warnings[0].Title != "Token can change things" || profile.Warnings[1].Title != "Token expires soon" {
		t.Fatalf("got %+v, %v", profile.Warnings, err)
	}
}

func TestBadTokenIsRefused(t *testing.T) {
	f := &fake{}
	_, err := f.connector(t).Validate(context.Background(), values("https://gitlab.example.com", "glpat-"+"wrongwrongwrongwrong1"))
	field, message := invalidField(t, err)
	if field != "token" || !strings.Contains(message, "does not accept") {
		t.Fatalf("got %s: %s", field, message)
	}
}

func TestMissingScopeIsRefusedWhicheverWayGitLabSaysIt(t *testing.T) {
	cases := map[string]*fake{
		"user refused":        {userStatus: 403},
		"scopes list":         {scopes: []string{"read_user"}},
		"merge request probe": {tokenStatus: 404, mrStatus: 403},
	}
	for name, f := range cases {
		_, err := f.connector(t).Validate(context.Background(), values("https://gitlab.example.com", goodToken))
		field, message := invalidField(t, err)
		if field != "token" || !strings.Contains(message, "read_api") {
			t.Errorf("%s: got %s: %s", name, field, message)
		}
	}
}

func TestAServerWithoutTokenInfoStillConnects(t *testing.T) {
	f := &fake{tokenStatus: 404}
	profile, err := f.connector(t).Validate(context.Background(), values("https://gitlab.example.com", goodToken))
	if err != nil || profile.Name != "Arthur Monnet" {
		t.Fatalf("got %+v, %v", profile, err)
	}
}

func TestMalformedAnswerMeansWrongAddress(t *testing.T) {
	for _, body := range []string{`<html>Sign in</html>`, `[]`, `{"id":0}`} {
		f := &fake{userBody: body}
		_, err := f.connector(t).Validate(context.Background(), values("https://gitlab.example.com", goodToken))
		if field, _ := invalidField(t, err); field != "url" {
			t.Errorf("%s: got field %q", body, field)
		}
	}
}

func TestAddressRefusals(t *testing.T) {
	refused := map[string]string{
		"http://gitlab.example.com":            "https://",
		"ftp://gitlab.example.com":             "https://",
		"https://user:pass@gitlab.example.com": "user name",
		"https://gitlab.example.com/gitlab":    "without a path",
		"https://gitlab.example.com/?a=b":      "without a path",
		"https://gitlab.example.com/#x":        "without a path",
		"https://localhost":                    "this Mac",
		"https://app.localhost:8443":           "this Mac",
		"https://127.0.0.1":                    "this Mac",
		"https://127.1.2.3:8443":               "this Mac",
		"https://[::1]":                        "this Mac",
		"https://0.0.0.0":                      "this Mac",
		"https://[::]":                         "this Mac",
		"https://169.254.169.254":              "this Mac",
		"https://169.254.169.254/latest":       "without a path",
		"https://[fe80::1]":                    "this Mac",
		"https://[::ffff:127.0.0.1]":           "this Mac",
		"https://2130706433":                   "valid web address",
		"https://0x7f000001":                   "valid web address",
		"https://":                             "valid web address",
	}
	for address, want := range refused {
		_, err := ParseBaseURL(address)
		field, message := invalidField(t, err)
		if field != "url" || !strings.Contains(message, want) {
			t.Errorf("%s: got %s: %s", address, field, message)
		}
	}
	allowed := map[string]string{
		"https://gitlab.example.com":         "https://gitlab.example.com",
		"gitlab.example.com/":                "https://gitlab.example.com",
		" https://GitLab.Example.com:8443/ ": "https://gitlab.example.com:8443",
		"https://10.0.3.4":                   "https://10.0.3.4",
		"https://192.168.1.10:8443":          "https://192.168.1.10:8443",
		"https://[fd12::5]":                  "https://[fd12::5]",
	}
	for address, want := range allowed {
		base, err := ParseBaseURL(address)
		if err != nil || base.String() != want {
			t.Errorf("%s: got %v, %v (want %s)", address, base, err, want)
		}
	}
}

func TestRefusedAddressIsNeverContacted(t *testing.T) {
	f := &fake{scopes: []string{"read_api"}}
	c := f.connector(t)
	if _, err := c.Validate(context.Background(), values("http://127.0.0.1:1", goodToken)); err == nil {
		t.Fatal("refused address accepted")
	}
	if len(f.seen) != 0 {
		t.Fatalf("a request was made: %v", f.seen)
	}
}

func TestOverviewCompletesMissingPipelines(t *testing.T) {
	f := &fake{noPipeline: true}
	c := f.connector(t)
	client := c.client(mustBase(t), goodToken)
	overview, err := client.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Mine) != 1 || overview.MineTotal != 4 || overview.ToReviewTotal != 4 || len(overview.ToReview) != 1 {
		t.Fatalf("unexpected overview %+v", overview)
	}
	mr := overview.Mine[0]
	if mr.ProjectPath != "group/sub/app" || mr.IID != 12 || mr.PipelineStatus != "success" || mr.MergeStatus != "mergeable" || mr.Author != "arthur" {
		t.Fatalf("unexpected merge request %+v", mr)
	}
}

func TestOverviewFailsWhenGitLabRefuses(t *testing.T) {
	f := &fake{mrStatus: 500}
	client := f.connector(t).client(mustBase(t), goodToken)
	if _, err := client.Overview(context.Background()); err == nil {
		t.Fatal("an error from GitLab must reach the caller")
	}
}

func TestEveryTextHasAFrenchTranslation(t *testing.T) {
	descriptor := (&Connector{}).Descriptor()
	keys := []string{descriptor.Tagline}
	for _, field := range descriptor.Fields {
		keys = append(keys, field.Label, field.Help, field.PatternHint)
	}
	for _, step := range descriptor.Guide {
		keys = append(keys, step.Title, step.Body)
		if step.Link != nil {
			keys = append(keys, step.Link.Label)
		}
	}
	for _, scenario := range []*fake{{scopes: []string{"api"}, expires: "2026-11-10"}, {}, {scopes: []string{"read_user"}}, {userBody: "x"}} {
		profile, err := scenario.connector(t).Validate(context.Background(), values("https://gitlab.example.com", goodToken))
		if err != nil {
			keys = append(keys, err.(*connector.ValidationError).Message)
		}
		for _, fact := range profile.Facts {
			keys = append(keys, fact.Label)
		}
		for _, warning := range profile.Warnings {
			keys = append(keys, warning.Title, warning.Detail)
		}
	}
	for _, address := range []string{"http://x", "https://u:p@x", "https://x/p", "https://localhost", "https://", "https://2130706433"} {
		_, err := ParseBaseURL(address)
		keys = append(keys, err.(*connector.ValidationError).Message)
	}
	for _, key := range keys {
		if key != "" && key != "Starts with glpat-. It stays in your Mac's keychain and is never shown again." && !i18n.Has(key) {
			t.Errorf("no French for %q", key)
		}
	}
	if !i18n.Has("Starts with glpat-. It stays in your Mac's keychain and is never shown again.") {
		t.Error("no French for the token help")
	}
}

func mustBase(t *testing.T) *url.URL {
	t.Helper()
	parsed, err := ParseBaseURL("https://gitlab.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
