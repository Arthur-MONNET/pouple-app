package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/remote"
)

// RemoteAdmin is what the settings need to manage the access from the phones.
type RemoteAdmin interface {
	OpenPairing() (remote.PairingOffer, error)
	CancelPairing()
	Devices() []remote.DeviceInfo
	RevokeDevice(id string) error
	Settings() (remote.SettingsView, error)
	UpdateSettings(update remote.SettingsUpdate) (remote.SettingsView, error)
}

// MountRemoteAdmin adds the remote access settings. They are on the loopback API only, behind its token: a phone can
// never pair another device, list or revoke devices, or turn the access on.
func MountRemoteAdmin(admin RemoteAdmin) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("POST /api/remote/pairing", func(w http.ResponseWriter, r *http.Request) {
			offer, err := admin.OpenPairing()
			if err != nil {
				writeRemoteError(w, r, err)
				return
			}
			writeJSON(w, http.StatusCreated, offer)
		})
		mux.HandleFunc("DELETE /api/remote/pairing", func(w http.ResponseWriter, r *http.Request) {
			admin.CancelPairing()
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("GET /api/remote/devices", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string][]remote.DeviceInfo{"devices": admin.Devices()})
		})
		mux.HandleFunc("DELETE /api/remote/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
			if err := admin.RevokeDevice(r.PathValue("id")); err != nil {
				writeRemoteError(w, r, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("GET /api/remote/settings", func(w http.ResponseWriter, r *http.Request) {
			view, err := admin.Settings()
			if err != nil {
				writeRemoteError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, view)
		})
		mux.HandleFunc("PUT /api/remote/settings", func(w http.ResponseWriter, r *http.Request) {
			var update remote.SettingsUpdate
			if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&update); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(i18n.FromContext(r.Context()), "The request is not valid JSON")})
				return
			}
			view, err := admin.UpdateSettings(update)
			if err != nil {
				writeRemoteError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, view)
		})
	}
}

func writeRemoteError(w http.ResponseWriter, r *http.Request, err error) {
	lang := i18n.FromContext(r.Context())
	switch {
	case errors.Is(err, remote.ErrUnknownDevice):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(lang, "This device is not paired.")})
	case errors.Is(err, remote.ErrDisabled):
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.ErrorText(lang, err)})
	case errors.Is(err, remote.ErrInvalidSettings):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": i18n.ErrorText(lang, err)})
	default:
		log.Printf("remote access settings: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(lang, "Something went wrong")})
	}
}
