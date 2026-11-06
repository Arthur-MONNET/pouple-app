package api

import (
	"context"
	"net/http"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/updates"
)

// UpdateChecker is what the update routes need from updates.Checker.
type UpdateChecker interface {
	Current(ctx context.Context) updates.Result
	Check(ctx context.Context) updates.Result
}

// MountUpdates adds GET /api/update (the last known status) and POST /api/update/check (look again now; the
// checker rate limits real fetches). Like every /api route they sit behind the token, the Host check and the
// cross-site check, and they are mounted on the main listener only, never on the local assistant socket.
func MountUpdates(checker UpdateChecker) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, checker.Current(r.Context()).Status(i18n.FromContext(r.Context())))
		})
		mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, checker.Check(r.Context()).Status(i18n.FromContext(r.Context())))
		})
	}
}
