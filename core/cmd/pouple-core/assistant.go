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
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/codex"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
	"github.com/Arthur-MONNET/pouple/core/internal/servicectx"
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
	// Both AIs count in the same daily quota. Which one runs, and with which model, is read from the settings at each
	// turn, so a change applies to the very next turn.
	limit := func() int { return load().DailyTurnLimit }
	codexHome := filepath.Join(dir, "codex")
	claudeHome := filepath.Join(dir, "claude")
	aiProviders := map[providers.ID]engine.Provider{
		providers.Codex: &engine.Metered{
			Inner: engine.NewRunner(engine.RunnerOptions{
				Shell:          os.Getenv("SHELL"),
				Home:           codexHome,
				ResolveCommand: func() string { return codex.Command(codexHome) },
			}),
			Meter: meter, Limit: limit,
		},
		providers.Claude: &engine.Metered{
			Inner: engine.NewClaudeRunner(engine.ClaudeOptions{
				Shell: os.Getenv("SHELL"),
				Home:  claudeHome,
				// Pouple's data folder holds the logins and the conversations: the model never reads it, even when
				// the projects folder happens to contain it.
				ProtectedDirs: []string{dir},
			}),
			Meter: meter, Limit: limit,
		},
	}
	projectsRoot := func() string { return load().ProjectsRoot }
	language := func() i18n.Lang { return i18n.Parse(load().UILanguage) }

	gatherer := servicectx.New(registry)
	service := engine.NewService(engine.Deps{
		Store: store, Inbox: slack, Providers: aiProviders, Selection: func() providers.Selection { return load().Selection() },
		Attachments: attachments, Fetch: slack.Fetch,
		// Connected optional services are read at each request: nothing is cached, so no restart is needed.
		Services:     gatherer.Gather,
		Lookups:      gatherer.Lookup,
		WebAccess:    func() bool { return load().WebAccess },
		Persona:      func() engine.Persona { return personaOf(registry, load()) },
		Language:     language,
		ProjectsRoot: projectsRoot,
		ProtectedDir: dir,
		WorkDir: func() string {
			if root := projectsRoot(); root != "" {
				return root
			}
			return scratch
		},
		SchemaFile: schemaFile,
		Redact: func() engine.Redactor {
			secrets := append(codexSecrets(filepath.Join(codexHome, "auth.json")), claudeSecrets(filepath.Join(claudeHome, ".credentials.json"))...)
			if values, err := registry.Values("slack"); err == nil {
				secrets = append(secrets, values["token"], values["appToken"])
			}
			// Tokens of the optional services never reach the model, and cannot come back out of it either.
			for _, id := range []string{"gitlab", "asana", "slite"} {
				if values, err := registry.Values(id); err == nil {
					secrets = append(secrets, values["token"])
				}
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
		Inactive:            func() string { return inactiveReason(registry, language()) },
		AutoApproveOwnNotes: func() bool { return load().AutoApprove == "own-notes" },
		SettleDelay:         3 * time.Second,
		OnError:             func(err error) { logf("assistant: %v", err) },
	})
	return service, attachments, nil
}

// inactiveReason says why the assistant cannot work yet: Slack and at least one AI (Codex or Claude) must be set up.
func inactiveReason(registry *connector.Registry, lang i18n.Lang) string {
	statuses, err := registry.List()
	if err != nil {
		return i18n.T(lang, "The connectors could not be read")
	}
	gaps := connector.SetupGaps(statuses)
	if len(gaps) == 0 {
		return ""
	}
	names := gaps[0].Names
	if len(names) > 1 {
		return i18n.T(lang, "Connect one of these first: %s", strings.Join(names, ", "))
	}
	return i18n.T(lang, "%s is not connected yet", names[0])
}

// personaOf calls the user by the first name of their Slack profile, in the language they chose or the system's.
func personaOf(registry *connector.Registry, settings config.Settings) engine.Persona {
	persona := engine.Persona{Language: settings.Language, Web: settings.WebAccess}
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

// runAssistant polls Slack until ctx ends. Socket Mode wakes the same polling loop.
func runAssistant(ctx context.Context, service *engine.Service, settings *config.File, wake <-chan struct{}) {
	service.Recover()
	service.Run(ctx, func() (time.Duration, time.Duration) {
		current, err := settings.Load()
		if err != nil || current.PollSeconds < 15 {
			current.PollSeconds = 60
		}
		// One poll uses three searches plus one per keyword. Stay below Slack's Tier 2 budget.
		cooldown := max(15, 4*(3+len(current.Keywords)))
		return time.Duration(max(current.PollSeconds, cooldown)) * time.Second, time.Duration(cooldown) * time.Second
	}, wake)
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

// claudeSecrets reads the tokens of Pouple's own Claude login when Claude Code keeps them in a file (it does when the
// keychain refuses the write), so that a model that read that file cannot repeat them. Usually they are in the
// keychain and this finds nothing.
func claudeSecrets(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var credentials struct {
		OAuth struct {
			Access  string `json:"accessToken"`
			Refresh string `json:"refreshToken"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &credentials) != nil {
		return nil
	}
	return []string{credentials.OAuth.Access, credentials.OAuth.Refresh}
}
