package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slack"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

func TestInterfaceLanguageUpdatesConnectorsAndValidation(t *testing.T) {
	file := config.Open(filepath.Join(t.TempDir(), "config.json"))
	store := config.Connectors{File: file}
	if err := store.Save(slack.ID, connector.Saved{Profile: connector.Profile{
		Name: "Workspace",
		Facts: []connector.Fact{
			{Label: "Workspace", Value: "Workspace"},
			{Label: "Permissions", Value: "8 of 11 granted"},
		},
		Warnings: []connector.Warning{{Title: "Missing files:read", Detail: "Photos and files in messages are not read."}},
	}}); err != nil {
		t.Fatal(err)
	}
	server := New(Options{
		Registry: connector.NewRegistry(secrets.NewMemory(), store, slack.New()),
		Token:    "secret-token",
		Language: func() string {
			settings, _ := file.Load()
			return settings.UILanguage
		},
		Mount: MountSettings(file),
	}, port)
	assertCopy := func(guide, fact, warning, validation, settings string) {
		t.Helper()
		response := call(server, "GET", "/api/connectors", "", nil)
		for _, want := range []string{guide, fact, warning, `"name":"Workspace"`, `"value":"Workspace"`} {
			if !strings.Contains(response.Body.String(), want) {
				t.Errorf("missing %q in %s", want, response.Body.String())
			}
		}
		invalid := call(server, "POST", "/api/connectors/slack/validate", `{}`, nil)
		if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), validation) {
			t.Errorf("connector validation: %d %s", invalid.Code, invalid.Body)
		}
		invalid = put(server, `{"dailyTurnLimit":-1}`)
		if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), settings) {
			t.Errorf("settings validation: %d %s", invalid.Code, invalid.Body)
		}
	}
	assertCopy("Créer l’app Slack", "8 sur 11 accordées", "Manque files:read", "« Jeton OAuth utilisateur » est obligatoire", "La limite quotidienne")
	if response := put(server, `{"uiLanguage":"en"}`); response.Code != http.StatusOK {
		t.Fatalf("could not switch language: %d %s", response.Code, response.Body)
	}
	assertCopy("Create the Slack app", "8 of 11 granted", "Missing files:read", "User OAuth Token is required", "The daily limit")
}
