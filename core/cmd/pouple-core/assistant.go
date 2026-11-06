package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
)

// newAssistant builds the engine on top of the connectors and the settings.
func newAssistant(dir string, registry *connector.Registry, settings *config.File, logf func(format string, args ...any)) (*engine.Service, *engine.Attachments, error) {
	store, err := engine.OpenStore(filepath.Join(dir, "requests.json"), func(err error) { logf("conversations not saved: %v", err) })
	if err != nil {
		return nil, nil, err
	}
	meter, err := engine.NewUsageMeter(filepath.Join(dir, "usage.json"), nil)
	if err != nil {
		return nil, nil, err
	}
	schemaFile := filepath.Join(dir, "outcome.schema.json")
	if err := os.WriteFile(schemaFile, []byte(engine.OutcomeSchema), 0o600); err != nil {
		return nil, nil, err
	}
	scratch := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return nil, nil, err
	}
	attachments := engine.NewAttachments(filepath.Join(dir, "attachments"))
	slack := &slackSource{registry: registry, settings: settings}

	load := func() config.Settings {
		current, err := settings.Load()
		if err != nil {
			logf("settings unreadable: %v", err)
			return config.Defaults()
		}
		return current
	}
	codex := &engine.Metered{
		Inner: engine.NewRunner(engine.RunnerOptions{Shell: os.Getenv("SHELL"), Home: filepath.Join(dir, "codex")}),
		Meter: meter,
		Limit: func() int { return load().DailyTurnLimit },
	}
	projectsRoot := func() string { return load().ProjectsRoot }

	service := engine.NewService(engine.Deps{
		Store: store, Inbox: slack, Codex: codex, Attachments: attachments, Fetch: slack.Fetch,
		Persona:      func() engine.Persona { return personaOf(registry, load()) },
		ProjectsRoot: projectsRoot,
		WorkDir: func() string {
			if root := projectsRoot(); root != "" {
				return root
			}
			return scratch
		},
		SchemaFile: schemaFile,
		Redact: func() engine.Redactor {
			secrets := codexSecrets(filepath.Join(dir, "codex", "auth.json"))
			if values, err := registry.Values("slack"); err == nil {
				secrets = append(secrets, values["token"], values["appToken"])
			}
			return engine.NewRedactor(secrets...)
		},
		Usage: func() engine.Usage {
			usage := engine.Usage{Today: meter.Today()}
			if limit := load().DailyTurnLimit; limit > 0 {
				usage.Limit = &limit
			}
			return usage
		},
		Inactive:            func() string { return inactiveReason(registry) },
		AutoApproveOwnNotes: func() bool { return load().AutoApprove == "own-notes" },
		SettleDelay:         15 * time.Second,
		OnError:             func(err error) { logf("assistant: %v", err) },
	})
	return service, attachments, nil
}

// inactiveReason says why the assistant cannot work yet: both required connectors must be set up.
func inactiveReason(registry *connector.Registry) string {
	statuses, err := registry.List()
	if err != nil {
		return "The connectors could not be read"
	}
	for _, status := range statuses {
		if status.Descriptor.Required && !status.Connected {
			return status.Descriptor.Name + " is not connected yet"
		}
	}
	return ""
}

// personaOf calls the user by the first name of their Slack profile, in the language they chose or the system's.
func personaOf(registry *connector.Registry, settings config.Settings) engine.Persona {
	persona := engine.Persona{Language: settings.Language}
	if status, err := registry.Status("slack"); err == nil && status.Profile != nil {
		if fields := strings.Fields(status.Profile.Name); len(fields) > 0 {
			persona.Name = fields[0]
		}
	}
	if persona.Language == "" {
		persona.Language = systemLanguage()
	}
	return persona
}

var languageNames = map[string]string{"fr": "French", "en": "English", "es": "Spanish", "de": "German", "it": "Italian", "pt": "Portuguese", "nl": "Dutch"}

// systemLanguage reads the first preferred language of macOS ("fr-FR" gives French); English when unknown.
func systemLanguage() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return "English"
	}
	for _, field := range strings.FieldsFunc(string(out), func(r rune) bool { return r == '"' || r == ',' || r == '(' || r == ')' || r == '\n' || r == ' ' }) {
		code := strings.ToLower(strings.SplitN(field, "-", 2)[0])
		if name, ok := languageNames[code]; ok {
			return name
		}
	}
	return "English"
}

// runAssistant polls Slack until ctx ends.
func runAssistant(ctx context.Context, service *engine.Service, settings *config.File) {
	service.Recover()
	service.Run(ctx, func() time.Duration {
		current, err := settings.Load()
		if err != nil || current.PollSeconds < 15 {
			return time.Minute
		}
		return time.Duration(current.PollSeconds) * time.Second
	})
}

// codexSecrets reads the tokens of Pouple's own Codex login, so that a model that read that file cannot repeat them.
func codexSecrets(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var auth struct {
		APIKey string `json:"OPENAI_API_KEY"`
		Tokens struct {
			ID      string `json:"id_token"`
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &auth) != nil {
		return nil
	}
	return []string{auth.APIKey, auth.Tokens.ID, auth.Tokens.Access, auth.Tokens.Refresh}
}
