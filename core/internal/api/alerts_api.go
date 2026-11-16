package api

import (
	"net/http"

	"github.com/Arthur-MONNET/pouple/core/internal/alerts"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Alerts is what the HTTP layer needs to let the user make, move and cancel alerts. The list itself travels in the
// assistant snapshot, so every window sees a change at once.
type Alerts interface {
	CreateAlert(title, note, at string) (alerts.Alert, error)
	UpdateAlert(id string, edit engine.AlertEdit) (alerts.Alert, error)
	DeleteAlert(id string) error
}

// MountAlerts adds the alert routes. They are only on the web interface's server: the local socket is for conversations.
func MountAlerts(service Alerts) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		handle := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
			mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
				if err := fn(w, r); err != nil {
					writeEngineError(w, i18n.FromContext(r.Context()), err)
				}
			})
		}
		handle("POST /api/alerts", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Title string `json:"title"`
				Note  string `json:"note"`
				At    string `json:"at"`
			}
			if err := decodeBody(r, &body); err != nil {
				return i18n.Wrap(engine.ErrInput, "the body is not valid JSON")
			}
			alert, err := service.CreateAlert(body.Title, body.Note, body.At)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusCreated, alert)
			return nil
		})
		handle("PATCH /api/alerts/{id}", func(w http.ResponseWriter, r *http.Request) error {
			// Only the fields that are present change.
			var body struct {
				Title *string `json:"title"`
				Note  *string `json:"note"`
				At    *string `json:"at"`
			}
			if err := decodeBody(r, &body); err != nil {
				return i18n.Wrap(engine.ErrInput, "the body is not valid JSON")
			}
			alert, err := service.UpdateAlert(r.PathValue("id"), engine.AlertEdit{Title: body.Title, Note: body.Note, At: body.At})
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, alert)
			return nil
		})
		handle("DELETE /api/alerts/{id}", func(w http.ResponseWriter, r *http.Request) error {
			return noContent(w, service.DeleteAlert(r.PathValue("id")))
		})
	}
}
