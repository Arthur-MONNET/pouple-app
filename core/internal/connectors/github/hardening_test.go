package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

func matches(pattern, text string) bool { return regexp.MustCompile(pattern).MatchString(text) }

func TestOnlyAPlainHTTPSAddressIsAccepted(t *testing.T) {
	good := map[string]Base{
		"":                                   {API: "https://api.github.com", Web: "github.com"},
		"https://api.github.com/":            {API: "https://api.github.com", Web: "github.com"},
		"github.com":                         {API: "https://api.github.com", Web: "github.com"},
		"https://GitHub.example.com":         {API: "https://github.example.com/api/v3", Web: "github.example.com"},
		"https://github.example.com/api/v3/": {API: "https://github.example.com/api/v3", Web: "github.example.com"},
		"https://github.example.com:8443":    {API: "https://github.example.com:8443/api/v3", Web: "github.example.com:8443"},
		"https://api.octocorp.ghe.com":       {API: "https://api.octocorp.ghe.com", Web: "octocorp.ghe.com"},
		"https://octocorp.ghe.com":           {API: "https://api.octocorp.ghe.com", Web: "octocorp.ghe.com"},
		"10.0.0.5":                           {API: "https://10.0.0.5/api/v3", Web: "10.0.0.5"},
	}
	for raw, want := range good {
		got, err := ParseBaseURL(raw)
		if err != nil || got != want {
			t.Errorf("ParseBaseURL(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"http://api.github.com",
		"https://user:pass@github.example.com",
		"https://github.example.com/some/path",
		"https://api.github.com/api/v3",
		"https://api.github.com?x=1",
		"https://api.github.com#x",
		"https://localhost",
		"https://127.0.0.1",
		"https://169.254.169.254",
		"https://[::1]",
		"https://2130706433",
		"https://0x7f.1",
	} {
		if _, err := ParseBaseURL(raw); fieldOf(err) != "url" {
			t.Errorf("ParseBaseURL(%q) must be refused on url, got %v", raw, err)
		}
	}
}

func TestPullRequestReferencesStayOnThisGitHub(t *testing.T) {
	c := &Client{api: "https://api.github.com", web: "github.com"}
	good := map[string]PRRef{
		"https://github.com/octo/app/pull/42":          {"octo", "app", 42},
		"https://github.com/octo/app.js/pull/42/files": {"octo", "app.js", 42},
		"octo/app#7": {"octo", "app", 7},
	}
	for text, want := range good {
		if got, ok := c.ParsePullRequest(text); !ok || got != want {
			t.Errorf("ParsePullRequest(%q) = %+v %v", text, got, ok)
		}
	}
	for _, text := range []string{
		"https://evil.example/octo/app/pull/42",
		"http://github.com/octo/app/pull/42",
		"https://github.com.evil.example/octo/app/pull/42",
		"https://user@github.com/octo/app/pull/42",
		"https://github.com/octo/app/issues/42",
		"https://github.com/octo/../pull/42",
		"https://github.com/octo/app/pull/0",
		"https://github.com/-octo/app/pull/1",
		"octo/..#1",
		"octo/app%2F..#1",
		"https://github.com/octo/app/pull/99999999999",
	} {
		if ref, ok := c.ParsePullRequest(text); ok {
			t.Errorf("ParsePullRequest(%q) must be refused, got %+v", text, ref)
		}
	}
}

func TestOnlyLinksToThisGitHubReachTheAssistant(t *testing.T) {
	c := &Client{api: "https://github.example.com/api/v3", web: "github.example.com"}
	for raw, want := range map[string]string{
		"https://github.example.com/o/r/pull/3": "https://github.example.com/o/r/pull/3",
		"https://evil.example/login":            "",
		"http://github.example.com/x":           "",
		"https://github.example.com.evil.io/x":  "",
		"javascript:alert(1)":                   "",
	} {
		if got := c.ownLink(raw); got != want {
			t.Errorf("ownLink(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestPicturesLoadOnlyFromGitHubAvatarHosts(t *testing.T) {
	base := Base{API: "https://git.company.example/api/v3", Web: "git.company.example"}
	for raw, want := range map[string]string{
		"https://avatars.githubusercontent.com/u/1?v=4":       "https://avatars.githubusercontent.com/u/1?v=4",
		"https://git.company.example/avatars/u/1":             "https://git.company.example/avatars/u/1",
		"https://avatars.git.company.example/u/1":             "https://avatars.git.company.example/u/1",
		"https://raw.githubusercontent.com/evil/x/main/a.svg": "",
		"http://avatars.githubusercontent.com/u/1":            "",
		"https://169.254.169.254/latest":                      "",
	} {
		if got := avatar(base, raw); got != want {
			t.Errorf("avatar(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestARedirectToAnotherHostIsNotFollowedWithTheToken(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte(`{"login":"evil"}`))
	}))
	defer elsewhere.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/user", http.StatusFound)
	}))
	defer api.Close()
	// The public client has the same redirect rule as the guarded one, without the dial guard that would refuse the
	// test servers on this Mac.
	client := &Client{api: api.URL, web: "github.com", token: goodToken, http: svchttp.NewPublicClient()}
	if _, _, err := client.User(context.Background()); !errors.Is(err, svchttp.ErrRedirect) {
		t.Fatalf("err %v", err)
	}
	if reached.Load() != 0 {
		t.Fatal("the other host must never be asked")
	}
}

func TestTheGuardedClientRefusesThisMac(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a server on this Mac must not be reached")
	}))
	defer api.Close()
	client := &Client{api: api.URL, web: "github.com", token: goodToken, http: svchttp.NewClient()}
	if _, _, err := client.User(context.Background()); err == nil {
		t.Fatal("the dial guard must refuse a loopback address")
	}
}
