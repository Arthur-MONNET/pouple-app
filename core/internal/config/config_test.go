package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
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
