package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
	"github.com/Arthur-MONNET/pouple/core/internal/updates"
)

const (
	updateHead   = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	updateLatest = "def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// updateGit is a fake git for a checkout that is three commits behind.
type updateGit struct {
	mu      sync.Mutex
	root    string
	fetches int
}

func (g *updateGit) run(_ context.Context, cmd updates.Command) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch strings.Join(cmd.Args[5:], " ") {
	case "rev-parse --show-toplevel":
		return g.root + "\n", nil
	case "remote get-url origin":
		return "https://github.com/Arthur-MONNET/pouple-app.git\n", nil
	case "fetch --quiet origin main":
		g.fetches++
	case "rev-parse --abbrev-ref HEAD":
		return "main\n", nil
	case "rev-parse --verify --quiet origin/main^{commit}":
		return updateLatest + "\n", nil
	case "rev-parse --verify --quiet " + updateHead + "^{commit}":
		return updateHead + "\n", nil
	case "rev-list --count " + updateHead + "..origin/main --":
		return "3\n", nil
	case "rev-list --count origin/main..HEAD --":
		return "0\n", nil
	case "log --format=%s -n 15 " + updateHead + "..origin/main --":
		return "feat: one\nfix: two\nchore: three\n", nil
	case "status --porcelain --untracked-files=no":
		return " M README.md\n", nil
	}
	return "", nil
}

func updateServer(t *testing.T, installed bool, language func() string) (*Server, *updateGit) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"scripts/install-local.sh", "core/go.mod"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, file)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	if installed {
		record, _ := json.Marshal(map[string]string{"sourceRoot": root, "commit": updateHead, "version": "0.1.0", "installedAt": "2026-11-01T09:30:00Z"})
		if err := os.WriteFile(filepath.Join(dir, "install.json"), record, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := &updateGit{root: root}
	checker := updates.New(updates.Options{DataDir: dir, Git: git.run, Now: time.Now})
	file := config.Open(filepath.Join(dir, "config.json"))
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: file})
	ui := fstest.MapFS{"index.html": {Data: []byte("<head><!--POUPLE_BOOT--></head>")}}
	return New(Options{Registry: registry, Token: "secret-token", UI: ui, Language: language, Mount: MountUpdates(checker)}, port), git
}

func TestUpdateRoutesAreProtectedLikeEveryOtherRoute(t *testing.T) {
	s, git := updateServer(t, true, nil)
	for _, route := range [][2]string{{"GET", "/api/update"}, {"POST", "/api/update/check"}} {
		method, path := route[0], route[1]
		if got := call(s, method, path, "", map[string]string{"Authorization": ""}).Code; got != 401 {
			t.Errorf("%s %s without a token: %d", method, path, got)
		}
		if got := call(s, method, path, "", map[string]string{"Authorization": "Bearer nope"}).Code; got != 401 {
			t.Errorf("%s %s with a wrong token: %d", method, path, got)
		}
		if got := call(s, method, path, "", map[string]string{"Sec-Fetch-Site": "cross-site"}).Code; got != 403 {
			t.Errorf("%s %s cross-site: %d", method, path, got)
		}
		rebinding := httptest.NewRequest(method, "http://evil.example:4310"+path, nil)
		rebinding.Header.Set("Authorization", "Bearer secret-token")
		recorder := httptest.NewRecorder()
		s.ServeHTTP(recorder, rebinding)
		if recorder.Code != 403 {
			t.Errorf("%s %s foreign host: %d", method, path, recorder.Code)
		}
	}
	if git.fetches != 0 {
		t.Error("a refused call must not reach git")
	}
}

func TestUpdateStatusShapeAndMethods(t *testing.T) {
	s, git := updateServer(t, true, nil)
	got := call(s, "GET", "/api/update", "", nil)
	if got.Code != 200 || !strings.HasPrefix(got.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("%d %v", got.Code, got.Header())
	}
	var before map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if before["supported"] != true || before["checkedAt"] != nil || before["latest"] != nil || before["available"] != false || git.fetches != 0 {
		t.Errorf("GET before any check reports validation only: %v (fetches %d)", before, git.fetches)
	}

	checked := call(s, "POST", "/api/update/check", "", nil)
	if checked.Code != 200 || git.fetches != 1 {
		t.Fatalf("%d %s (fetches %d)", checked.Code, checked.Body, git.fetches)
	}
	var status updates.Status
	if err := json.Unmarshal(checked.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Supported || !status.Available || status.Behind != 3 || status.Latest == nil || status.Latest.Commit != "def5678" ||
		status.Installed == nil || status.Installed.Commit != "1111111" || status.Installed.Version != "0.1.0" ||
		len(status.Changes) != 3 || status.CheckedAt == nil || status.Error != nil || status.Checking {
		t.Errorf("%+v", status)
	}
	if len(status.Blockers) != 1 || status.Blockers[0].Code != "local-changes" || !strings.Contains(status.Blockers[0].Message, "modifications locales") {
		t.Errorf("blockers: %+v", status.Blockers)
	}

	// GET now serves the cache, and a second POST within 30 s does not fetch again.
	if again := call(s, "GET", "/api/update", "", nil); again.Body.String() != checked.Body.String() {
		t.Errorf("GET after a check:\n%s\n%s", again.Body, checked.Body)
	}
	call(s, "POST", "/api/update/check", "", nil)
	call(s, "GET", "/api/update/check", "", nil)
	if git.fetches != 1 {
		t.Errorf("fetches after a second POST and a GET of the check route: %d", git.fetches)
	}

	for _, c := range []struct{ method, path string }{{"PUT", "/api/update"}, {"DELETE", "/api/update"}, {"POST", "/api/update"}} {
		if code := call(s, c.method, c.path, "", nil).Code; code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
			t.Errorf("%s %s: %d", c.method, c.path, code)
		}
	}
}

func TestUpdateStatusWhenNotInstalledFromSource(t *testing.T) {
	s, git := updateServer(t, false, nil)
	for _, r := range []*httptest.ResponseRecorder{call(s, "GET", "/api/update", "", nil), call(s, "POST", "/api/update/check", "", nil)} {
		var body map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil || r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body)
		}
		if body["supported"] != false || body["reason"] != "not-installed-from-source" || body["installed"] != nil || body["available"] != false {
			t.Errorf("%v", body)
		}
		if message, _ := body["message"].(string); !strings.Contains(message, "installé depuis ses sources") {
			t.Errorf("message: %q", message)
		}
	}
	if git.fetches != 0 {
		t.Error("nothing to fetch without an install")
	}
}

func TestUpdateTextsFollowTheInterfaceLanguage(t *testing.T) {
	lang := "fr"
	s, _ := updateServer(t, true, func() string { return lang })
	if body := call(s, "POST", "/api/update/check", "", nil).Body.String(); !strings.Contains(body, "modifications locales") {
		t.Errorf("French: %s", body)
	}
	lang = "en"
	if body := call(s, "GET", "/api/update", "", nil).Body.String(); !strings.Contains(body, "local changes") {
		t.Errorf("English: %s", body)
	}
}
