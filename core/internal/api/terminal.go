package api

import (
	"net/http"

	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// MountTerminal is used only for the macOS app's authenticated web view.
func MountTerminal(mux *http.ServeMux, service *engine.Service) {
	mux.HandleFunc("POST /api/assistant/requests/{id}/terminal", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			Action, Session, Data           string
			Revision, Offset, CommandOffset uint64
			Rows, Cols                      uint16
		}
		if err := decodeBody(r, &body); err != nil || len(body.Data) > 32<<10 || (body.Action != "input" && body.Action != "run" && body.Action != "cd" && body.Action != "open" && body.Data != "") {
			writeEngineError(w, i18n.FromContext(r.Context()), engine.ErrInput)
			return
		}
		if body.Action == "list" {
			views, err := service.ListTerminals(id)
			if err != nil {
				writeEngineError(w, i18n.FromContext(r.Context()), err)
				return
			}
			writeJSON(w, http.StatusOK, views)
			return
		}
		var view engine.TerminalView
		var err error
		if body.Action == "open" {
			view, err = service.OpenTerminal(id, body.Data)
		} else {
			view, err = service.Terminal(id, body.Session, body.Action, body.Revision, body.Offset, body.CommandOffset, body.Data, body.Rows, body.Cols)
		}
		if err != nil {
			writeEngineError(w, i18n.FromContext(r.Context()), err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
}
