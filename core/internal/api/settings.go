package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Settings is the part of the configuration the interface can read and change.
type Settings struct {
	PollSeconds    int      `json:"pollSeconds"`
	DailyTurnLimit int      `json:"dailyTurnLimit"`
	ProjectsRoot   string   `json:"projectsRoot"`
	Language       string   `json:"language"`
	UILanguage     string   `json:"uiLanguage"`
	AutoApprove    string   `json:"autoApprove"`
	Keywords       []string `json:"keywords"`
	IgnoreChannels []string `json:"ignoreChannels"`
}

func viewOf(s config.Settings) Settings {
	return Settings{s.PollSeconds, s.DailyTurnLimit, s.ProjectsRoot, s.Language, string(i18n.Parse(s.UILanguage)), s.AutoApprove, s.Keywords, s.IgnoreChannels}
}

// MountSettings adds GET and PUT /api/settings. A PUT carries only the fields to change.
func MountSettings(file *config.File) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
			lang := i18n.FromContext(r.Context())
			settings, err := file.Load()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(lang, "The settings could not be read")})
				return
			}
			writeJSON(w, http.StatusOK, viewOf(settings))
		})
		mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
			lang := i18n.FromContext(r.Context())
			var patch struct {
				PollSeconds    *int      `json:"pollSeconds"`
				DailyTurnLimit *int      `json:"dailyTurnLimit"`
				ProjectsRoot   *string   `json:"projectsRoot"`
				Language       *string   `json:"language"`
				UILanguage     *string   `json:"uiLanguage"`
				AutoApprove    *string   `json:"autoApprove"`
				Keywords       *[]string `json:"keywords"`
				IgnoreChannels *[]string `json:"ignoreChannels"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&patch); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(lang, "The request is not valid JSON")})
				return
			}
			if problem := validate(lang, patch.PollSeconds, patch.DailyTurnLimit, patch.ProjectsRoot, patch.AutoApprove, patch.UILanguage); problem != "" {
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
				if patch.UILanguage != nil {
					s.UILanguage = *patch.UILanguage
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
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(lang, "The settings could not be saved")})
				return
			}
			writeJSON(w, http.StatusOK, viewOf(settings))
		})
	}
}

func validate(lang i18n.Lang, poll, limit *int, root, autoApprove, uiLanguage *string) string {
	switch {
	case poll != nil && (*poll < 15 || *poll > 3600):
		return i18n.T(lang, "Slack is read every 15 to 3600 seconds")
	case limit != nil && *limit < 0:
		return i18n.T(lang, "The daily limit cannot be negative (0 means no limit)")
	case uiLanguage != nil && *uiLanguage != "fr" && *uiLanguage != "en":
		return i18n.T(lang, `The interface language must be "fr" or "en"`)
	case autoApprove != nil && *autoApprove != "off" && *autoApprove != "own-notes":
		return i18n.T(lang, `Automatic approval must be "off" or "own-notes"`)
	case root != nil && strings.TrimSpace(*root) != "":
		path := strings.TrimSpace(*root)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return i18n.T(lang, "“%s” is not a folder", path)
		}
		// Too wide: every folder of the disk (or of the home folder, with its keys and settings) would be a project.
		home, _ := os.UserHomeDir()
		if clean := filepath.Clean(path); !filepath.IsAbs(clean) || clean == "/" || (home != "" && clean == filepath.Clean(home)) {
			return i18n.T(lang, "Choose a folder that holds only your projects, not / or your home folder")
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
