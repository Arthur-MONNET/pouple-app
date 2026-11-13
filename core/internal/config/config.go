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
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
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
	// DailyTurnLimit caps the AI turns per day, whichever AI runs them; 0 means no limit.
	DailyTurnLimit int `json:"dailyTurnLimit"`
	// Provider is the AI the user chose ("codex" or "claude"); empty means automatic: see ActiveProvider.
	Provider string `json:"provider"`
	// Models is the model chosen for each provider; empty (or missing) leaves the choice to the CLI.
	Models map[string]string `json:"models"`
	// WebDisabled stops the AI from searching the web and reading public pages while it investigates. The web is on
	// unless the user turns it off, so the zero value is the default; what comes back from it is untrusted text, which
	// is why a proposal made after a web search is never applied automatically. It replaces the earlier "webAccess"
	// key, whose stored false could not tell a choice from a default.
	WebDisabled bool `json:"webDisabled"`
	// ProjectsRoot is the folder with one folder per project: where the AI looks, and where approved changes go.
	ProjectsRoot string `json:"projectsRoot"`
	// Language is the language of summaries and questions ("French"); empty uses the system language.
	Language string `json:"language"`
	// UILanguage is the language of the whole app: "fr" (default) or "en".
	UILanguage string `json:"uiLanguage"`
	// AutoApprove is "off" (always ask), "own-notes" (apply a change proposed from a note to oneself) or "all" (apply every
	// proposed change without asking, whoever asked for it).
	AutoApprove string `json:"autoApprove"`
	// Keywords are words that make a Slack message a request wherever it is posted.
	Keywords []string `json:"keywords"`
	// IgnoreChannels are channel ids whose messages are never requests.
	IgnoreChannels []string `json:"ignoreChannels"`
	// GitLabPipelines is which finished GitLab pipelines of the user become requests: "all" (the default),
	// "failures" or "off". It belongs to the GitLab connector and is set on its card.
	GitLabPipelines string `json:"gitlabPipelines"`
}

func Defaults() Settings {
	return Settings{Connectors: map[string]ConnectorState{}, PollSeconds: 60, DailyTurnLimit: 150, Models: map[string]string{}, UILanguage: "fr", AutoApprove: "off", Keywords: []string{}, IgnoreChannels: []string{}, GitLabPipelines: "all"}
}

// Connected reports whether a connector is set up.
func (s Settings) Connected(id string) bool {
	_, ok := s.Connectors[id]
	return ok
}

// ActiveProvider is the AI that runs the next turn: the user's choice while it is connected, otherwise the first
// connected one in preference order (Codex, then Claude). With none connected it is the choice, else Codex.
func (s Settings) ActiveProvider() providers.ID {
	chosen, hasChoice := providers.Parse(s.Provider)
	if hasChoice && s.Connected(string(chosen)) {
		return chosen
	}
	for _, id := range providers.All() {
		if s.Connected(string(id)) {
			return id
		}
	}
	if hasChoice {
		return chosen
	}
	return providers.Codex
}

// ModelOf is the model chosen for a provider; "" leaves it to the CLI.
func (s Settings) ModelOf(id providers.ID) string { return s.Models[string(id)] }

// Selection is what the next turn runs on.
func (s Settings) Selection() providers.Selection {
	active := s.ActiveProvider()
	return providers.Selection{Provider: active, Model: s.ModelOf(active)}
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
	if settings.Models == nil {
		settings.Models = map[string]string{}
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
