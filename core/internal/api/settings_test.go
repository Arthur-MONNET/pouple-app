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
		"pipelines":       `{"gitlabPipelines":"sometimes"}`,
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

func TestWebAccessIsOnByDefaultAndChangesOnlyWhenAsked(t *testing.T) {
	s := settingsServer(t)
	if got := call(s, "GET", "/api/settings", "", nil).Body.String(); !strings.Contains(got, `"webAccess":true`) {
		t.Fatalf("the AI has the web unless it is turned off: %s", got)
	}
	if r := put(s, `{"webAccess":false}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"webAccess":false`) {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := put(s, `{"dailyTurnLimit":20}`); !strings.Contains(r.Body.String(), `"webAccess":false`) {
		t.Fatalf("another change must not switch the web back on: %s", r.Body)
	}
	if r := put(s, `{"webAccess":true}`); !strings.Contains(r.Body.String(), `"webAccess":true`) {
		t.Fatalf("%s", r.Body)
	}
}

func TestGitLabPipelinesAreAllByDefaultAndChangeOnlyWhenAsked(t *testing.T) {
	s := settingsServer(t)
	if got := call(s, "GET", "/api/settings", "", nil).Body.String(); !strings.Contains(got, `"gitlabPipelines":"all"`) {
		t.Fatalf("every finished pipeline by default: %s", got)
	}
	if r := put(s, `{"gitlabPipelines":"failures"}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"gitlabPipelines":"failures"`) {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := put(s, `{"dailyTurnLimit":20}`); !strings.Contains(r.Body.String(), `"gitlabPipelines":"failures"`) {
		t.Fatalf("another change must not reset it: %s", r.Body)
	}
}

func TestTheBetaIsOffByDefaultAndChangesOnlyWhenAsked(t *testing.T) {
	s := settingsServer(t)
	if got := call(s, "GET", "/api/settings", "", nil).Body.String(); !strings.Contains(got, `"betaUpdates":false`) {
		t.Fatalf("the stable builds by default: %s", got)
	}
	if r := put(s, `{"betaUpdates":true}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"betaUpdates":true`) {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := put(s, `{"dailyTurnLimit":20}`); !strings.Contains(r.Body.String(), `"betaUpdates":true`) {
		t.Fatalf("another change must not leave the beta: %s", r.Body)
	}
	if r := put(s, `{"betaUpdates":false}`); !strings.Contains(r.Body.String(), `"betaUpdates":false`) {
		t.Fatalf("%s", r.Body)
	}
}

func TestTheMemoryIsOffUntilTurnedOnAndStaysOutOfTheProjects(t *testing.T) {
	s := settingsServer(t)
	home, _ := os.UserHomeDir()
	got := call(s, "GET", "/api/settings", "", nil).Body.String()
	if !strings.Contains(got, `"memory":false`) || !strings.Contains(got, `"memoryFolder":"`+filepath.Join(home, config.DefaultMemoryFolder)+`"`) {
		t.Fatalf("the memory is off until turned on, and would live in ~/SecondBrain: %s", got)
	}
	if r := put(s, `{"memory":true}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"memory":true`) {
		t.Fatalf("turning it on: %d %s", r.Code, r.Body)
	}
	if r := put(s, `{"dailyTurnLimit":20}`); !strings.Contains(r.Body.String(), `"memory":true`) {
		t.Fatalf("another change must not turn it off: %s", r.Body)
	}
	projects := t.TempDir()
	if r := put(s, `{"projectsRoot":"`+projects+`"}`); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	for name, body := range map[string]string{
		"relative":        `{"memoryFolder":"notes"}`,
		"the disk root":   `{"memoryFolder":"/"}`,
		"the home folder": `{"memoryFolder":"` + home + `"}`,
		"in the projects": `{"memoryFolder":"` + filepath.Join(projects, "brain") + `"}`,
		"the projects":    `{"memoryFolder":"` + projects + `"}`,
	} {
		if code := put(s, body).Code; code != 422 {
			t.Errorf("%s: status %d, want 422", name, code)
		}
	}
	chosen := filepath.Join(t.TempDir(), "Vault")
	if r := put(s, `{"memory":false,"memoryFolder":"`+chosen+`"}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"memory":false`) || !strings.Contains(r.Body.String(), `"memoryFolder":"`+chosen+`"`) {
		t.Fatalf("a folder still to make is accepted: %d %s", r.Code, r.Body)
	}
	if r := put(s, `{"memoryFolder":""}`); !strings.Contains(r.Body.String(), `"memoryFolder":"`+filepath.Join(home, config.DefaultMemoryFolder)+`"`) {
		t.Fatalf("an empty folder goes back to the default one: %s", r.Body)
	}
}

func TestTheMemoryAndTheProjectsNeverHoldEachOtherEvenThroughALink(t *testing.T) {
	s := settingsServer(t)
	projects := t.TempDir()
	link := filepath.Join(t.TempDir(), "Brain")
	if err := os.MkdirAll(filepath.Join(projects, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(projects, "notes"), link); err != nil {
		t.Fatal(err)
	}
	if r := put(s, `{"projectsRoot":"`+projects+`"}`); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	for name, body := range map[string]string{
		"a link into the projects":     `{"memoryFolder":"` + link + `"}`,
		"a folder still to make there": `{"memoryFolder":"` + filepath.Join(link, "later") + `"}`,
		"around the projects":          `{"memoryFolder":"` + filepath.Dir(projects) + `"}`,
	} {
		if code := put(s, body).Code; code != 422 {
			t.Errorf("%s: status %d, want 422", name, code)
		}
	}
	if r := put(s, `{"memoryFolder":"`+filepath.Join(projects, "..x")+`"}`); r.Code != 422 {
		t.Fatalf("a folder named ..x is inside the projects all the same: %d %s", r.Code, r.Body)
	}
	memory := filepath.Join(t.TempDir(), "Vault")
	if r := put(s, `{"memory":true,"memoryFolder":"`+memory+`"}`); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := put(s, `{"projectsRoot":"`+filepath.Dir(memory)+`"}`); r.Code != 422 {
		t.Fatalf("moving the projects around the memory must be refused while it is on: %d %s", r.Code, r.Body)
	}
}
