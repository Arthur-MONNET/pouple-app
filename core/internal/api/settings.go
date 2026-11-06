package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
)

// Settings is the part of the configuration the interface can read and change.
type Settings struct {
	PollSeconds    int      `json:"pollSeconds"`
	DailyTurnLimit int      `json:"dailyTurnLimit"`
	ProjectsRoot   string   `json:"projectsRoot"`
	Language       string   `json:"language"`
	AutoApprove    string   `json:"autoApprove"`
	Keywords       []string `json:"keywords"`
	IgnoreChannels []string `json:"ignoreChannels"`
}

func viewOf(s config.Settings) Settings {
	return Settings{s.PollSeconds, s.DailyTurnLimit, s.ProjectsRoot, s.Language, s.AutoApprove, s.Keywords, s.IgnoreChannels}
}

// MountSettings adds GET and PUT /api/settings. A PUT carries only the fields to change.
func MountSettings(file *config.File) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, _ *http.Request) {
			settings, err := file.Load()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "The settings could not be read"})
				return
			}
			writeJSON(w, http.StatusOK, viewOf(settings))
		})
		mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
			var patch struct {
				PollSeconds    *int      `json:"pollSeconds"`
				DailyTurnLimit *int      `json:"dailyTurnLimit"`
				ProjectsRoot   *string   `json:"projectsRoot"`
				Language       *string   `json:"language"`
				AutoApprove    *string   `json:"autoApprove"`
				Keywords       *[]string `json:"keywords"`
				IgnoreChannels *[]string `json:"ignoreChannels"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&patch); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The request is not valid JSON"})
				return
			}
			if problem := validate(patch.PollSeconds, patch.DailyTurnLimit, patch.ProjectsRoot, patch.AutoApprove); problem != "" {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": problem})
				return
			}
			settings, err := file.Update(func(s *config.Settings) {
				if patch.PollSeconds != nil {
					s.PollSeconds = *patch.PollSeconds
				}
				if patch.DailyTurnLimit != nil {
					s.DailyTurnLimit = *patch.DailyTurnLimit
				}
				if patch.ProjectsRoot != nil {
					s.ProjectsRoot = strings.TrimSpace(*patch.ProjectsRoot)
				}
				if patch.Language != nil {
					s.Language = strings.TrimSpace(*patch.Language)
				}
				if patch.AutoApprove != nil {
					s.AutoApprove = *patch.AutoApprove
				}
				if patch.Keywords != nil {
					s.Keywords = cleanList(*patch.Keywords)
				}
				if patch.IgnoreChannels != nil {
					s.IgnoreChannels = cleanList(*patch.IgnoreChannels)
				}
			})
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "The settings could not be saved"})
				return
			}
			writeJSON(w, http.StatusOK, viewOf(settings))
		})
	}
}

func validate(poll, limit *int, root, autoApprove *string) string {
	switch {
	case poll != nil && (*poll < 15 || *poll > 3600):
		return "Slack is read every 15 to 3600 seconds"
	case limit != nil && *limit < 0:
		return "The daily limit cannot be negative (0 means no limit)"
	case autoApprove != nil && *autoApprove != "off" && *autoApprove != "own-notes":
		return `Automatic approval must be "off" or "own-notes"`
	case root != nil && strings.TrimSpace(*root) != "":
		if info, err := os.Stat(strings.TrimSpace(*root)); err != nil || !info.IsDir() {
			return fmt.Sprintf("“%s” is not a folder", strings.TrimSpace(*root))
		}
	}
	return ""
}

func cleanList(values []string) []string {
	out := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
