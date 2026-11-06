package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

func settingsServer(t *testing.T) *Server {
	t.Helper()
	file := config.Open(filepath.Join(t.TempDir(), "config.json"))
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: file})
	return New(Options{Registry: registry, Token: "secret-token", UI: nil, Mount: MountSettings(file)}, port)
}

func put(s *Server, body string) *httptest.ResponseRecorder {
	return call(s, "PUT", "/api/settings", body, nil)
}

func TestSettingsDefaultsAndPartialUpdates(t *testing.T) {
	s := settingsServer(t)
	got := call(s, "GET", "/api/settings", "", nil).Body.String()
	for _, want := range []string{`"uiLanguage":"fr"`, `"autoApprove":"off"`, `"pollSeconds":60`} {
		if !strings.Contains(got, want) {
			t.Errorf("defaults: missing %s in %s", want, got)
		}
	}
	if r := put(s, `{"uiLanguage":"en","dailyTurnLimit":10}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"uiLanguage":"en"`) {
		t.Fatalf("update: %d %s", r.Code, r.Body)
	}
	after := call(s, "GET", "/api/settings", "", nil).Body.String()
	if !strings.Contains(after, `"pollSeconds":60`) || !strings.Contains(after, `"dailyTurnLimit":10`) {
		t.Fatalf("a partial update must keep the other values: %s", after)
	}
}

func TestSettingsRefuseWhatWouldBeDangerousOrMeaningless(t *testing.T) {
	s := settingsServer(t)
	home, _ := os.UserHomeDir()
	for name, body := range map[string]string{
		"language":        `{"uiLanguage":"de"}`,
		"approve":         `{"autoApprove":"always"}`,
		"poll too fast":   `{"pollSeconds":1}`,
		"negative limit":  `{"dailyTurnLimit":-1}`,
		"missing folder":  `{"projectsRoot":"/definitely/not/here"}`,
		"the disk root":   `{"projectsRoot":"/"}`,
		"the home folder": `{"projectsRoot":"` + home + `"}`,
		"not json":        `nope`,
	} {
		if code := put(s, body).Code; code != 422 && code != 400 {
			t.Errorf("%s: status %d, want a refusal", name, code)
		}
	}
	folder := t.TempDir()
	if r := put(s, `{"projectsRoot":"`+folder+`"}`); r.Code != 200 {
		t.Fatalf("a real folder must be accepted: %d %s", r.Code, r.Body)
	}
}
