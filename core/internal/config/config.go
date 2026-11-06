// Package config keeps Pouple's own settings: which connectors are set up, and who they belong to. Secrets are
// not here: they live in the keychain.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
)

// ConnectorState is what is remembered about a connected service.
type ConnectorState struct {
	// Values holds the fields that are not secret.
	Values      map[string]string `json:"values,omitempty"`
	Profile     connector.Profile `json:"profile"`
	ConnectedAt time.Time         `json:"connectedAt"`
}

// Settings is the content of config.json.
type Settings struct {
	Connectors map[string]ConnectorState `json:"connectors"`
	// PollSeconds is how often Slack is read.
	PollSeconds int `json:"pollSeconds"`
	// DailyTurnLimit caps the Codex turns per day; 0 means no limit.
	DailyTurnLimit int `json:"dailyTurnLimit"`
}

func Defaults() Settings {
	return Settings{Connectors: map[string]ConnectorState{}, PollSeconds: 60, DailyTurnLimit: 150}
}

// File reads and writes the settings, one writer at a time.
type File struct {
	path string
	mu   sync.Mutex
}

func Open(path string) *File { return &File{path: path} }

func (f *File) Load() (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.load()
}

func (f *File) load() (Settings, error) {
	settings := Defaults()
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return Defaults(), err
	}
	if settings.Connectors == nil {
		settings.Connectors = map[string]ConnectorState{}
	}
	return settings, nil
}

// Update applies change to the stored settings and writes them back atomically.
func (f *File) Update(change func(*Settings)) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	settings, err := f.load()
	if err != nil {
		return settings, err
	}
	change(&settings)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return settings, err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return settings, err
	}
	temporary := f.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return settings, err
	}
	return settings, os.Rename(temporary, f.path)
}
