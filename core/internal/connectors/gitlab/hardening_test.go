package gitlab

import (
	"net/url"
	"testing"
)

func TestPicturesLoadOnlyFromTheServerOrKnownPictureHosts(t *testing.T) {
	base, _ := url.Parse("https://git.company.example")
	cases := map[string]string{
		"/uploads/avatar.png":                         "https://git.company.example/uploads/avatar.png",
		"https://secure.gravatar.com/avatar/abc":      "https://secure.gravatar.com/avatar/abc",
		"https://assets.gitlab-static.net/a.png":      "https://assets.gitlab-static.net/a.png",
		"http://git.company.example/a.png":            "",
		"http://127.0.0.1:8080/admin":                 "",
		"https://169.254.169.254/latest/meta-data":    "",
		"https://evil.example/pixel.gif":              "",
		"https://user:pass@git.company.example/a.png": "",
	}
	for raw, want := range cases {
		if got := avatar(base, raw); got != want {
			t.Errorf("avatar(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestOnlyLinksToTheConfiguredServerReachTheAssistant(t *testing.T) {
	c := &Client{api: "https://git.company.example/api/v4"}
	for raw, want := range map[string]string{
		"https://git.company.example/group/app/-/merge_requests/3": "https://git.company.example/group/app/-/merge_requests/3",
		"https://GIT.company.example/x":                            "https://GIT.company.example/x",
		"https://evil.example/login":                               "",
		"http://git.company.example/x":                             "",
		"https://git.company.example.evil.example/x":               "",
		"not a url \x7f":                                           "",
	} {
		if got := c.ownLink(raw); got != want {
			t.Errorf("ownLink(%q) = %q, want %q", raw, got, want)
		}
	}
}
