package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

func TestSettingsRoundTripWithPrivateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	file := Open(path)
	if settings, err := file.Load(); err != nil || settings.PollSeconds != 60 || settings.AutoApprove != "off" {
		t.Fatalf("defaults: %+v %v", settings, err)
	}
	store := Connectors{File: file}
	saved := connector.Saved{Values: map[string]string{"region": "eu"}, Profile: connector.Profile{Name: "Ada"}, ConnectedAt: time.Unix(10, 0).UTC()}
	if err := store.Save("fake", saved); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Load("fake")
	if err != nil || !ok || got.Profile.Name != "Ada" || got.Values["region"] != "eu" {
		t.Fatalf("load: %+v %v %v", got, ok, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("the settings file must be private: %v", info.Mode())
	}
	if err := store.Remove("fake"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Load("fake"); ok {
		t.Fatal("not removed")
	}
}

func TestCorruptSettingsAreReportedNotSilentlyReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("{broken"), 0o600)
	if _, err := Open(path).Load(); err == nil {
		t.Fatal("a corrupt file must be reported")
	}
	if _, err := Open(path).Update(func(*Settings) {}); err == nil {
		t.Fatal("an update must not overwrite a file that could not be read")
	}
}

func TestActiveProviderFollowsTheChoiceThenWhatIsConnected(t *testing.T) {
	connected := func(ids ...string) Settings {
		s := Defaults()
		for _, id := range ids {
			s.Connectors[id] = ConnectorState{}
		}
		return s
	}
	cases := []struct {
		name     string
		settings Settings
		want     providers.ID
	}{
		{"nothing connected defaults to Codex", connected(), providers.Codex},
		{"only Claude", connected("claude"), providers.Claude},
		{"only Codex", connected("codex"), providers.Codex},
		{"both: Codex is preferred", connected("codex", "claude"), providers.Codex},
		{"a chosen connected provider wins", func() Settings { s := connected("codex", "claude"); s.Provider = "claude"; return s }(), providers.Claude},
		{"a choice that is no longer connected is not run", func() Settings { s := connected("codex"); s.Provider = "claude"; return s }(), providers.Codex},
		{"an unknown choice is ignored", func() Settings { s := connected("claude"); s.Provider = "gemini"; return s }(), providers.Claude},
		{"nothing connected keeps the choice", func() Settings { s := connected(); s.Provider = "claude"; return s }(), providers.Claude},
	}
	for _, c := range cases {
		if got := c.settings.ActiveProvider(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestSelectionCarriesTheModelOfTheActiveProviderOnly(t *testing.T) {
	s := Defaults()
	s.Connectors["codex"], s.Connectors["claude"] = ConnectorState{}, ConnectorState{}
	s.Models = map[string]string{"codex": "gpt-x", "claude": "opus"}
	if got := s.Selection(); got.Provider != providers.Codex || got.Model != "gpt-x" {
		t.Fatalf("%+v", got)
	}
	s.Provider = "claude"
	if got := s.Selection(); got.Provider != providers.Claude || got.Model != "opus" {
		t.Fatalf("%+v", got)
	}
	s.Models = nil
	if got := s.Selection(); got.Model != "" {
		t.Fatalf("a missing model is the CLI's default: %+v", got)
	}
}

func TestSettingsWrittenBeforeProvidersExistedStillLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"pollSeconds":30,"connectors":{"codex":{"profile":{"name":"x","facts":[],"warnings":[]}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := Open(path).Load()
	if err != nil || settings.Models == nil || settings.Provider != "" || settings.ActiveProvider() != providers.Codex {
		t.Fatalf("%+v %v", settings, err)
	}
}
