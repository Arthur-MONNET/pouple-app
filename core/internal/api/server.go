// Package api is the HTTP surface of the core. It listens on the loopback address only, and every call must prove
// it comes from the app: a bearer token chosen at launch, a Host that is the loopback address (against DNS
// rebinding) and no cross-site origin. No CORS header is ever sent, so another web page cannot read an answer.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
)

// Options configures a Server.
type Options struct {
	Registry *connector.Registry
	// Token is the bearer token every /api call must carry.
	Token string
	// UI serves the web interface; index.html receives the token.
	UI fs.FS
	// Mount adds more routes (the assistant engine) under the same protection.
	Mount func(mux *http.ServeMux)
}

// Server is the core's HTTP handler.
type Server struct {
	opts Options
	mux  *http.ServeMux
	port int
}

// New builds the handler. port is the port it will listen on, used to check the Host header.
func New(opts Options, port int) *Server {
	s := &Server{opts: opts, mux: http.NewServeMux(), port: port}
	s.mux.HandleFunc("GET /api/state", s.state)
	s.mux.HandleFunc("GET /api/connectors", s.connectors)
	s.mux.HandleFunc("POST /api/connectors/{id}/validate", s.validate)
	s.mux.HandleFunc("PUT /api/connectors/{id}", s.save)
	s.mux.HandleFunc("DELETE /api/connectors/{id}", s.remove)
	s.mux.HandleFunc("POST /api/connectors/{id}/actions/{action}", s.runAction)
	if opts.Mount != nil {
		opts.Mount(s.mux)
	}
	s.mux.HandleFunc("GET /", s.page)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.allowedHost(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if strings.HasPrefix(r.URL.Path, "/api/") && !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong token"})
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) allowedHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	return (name == "127.0.0.1" || name == "localhost") && port == fmt.Sprint(s.port)
}

func (s *Server) authorized(r *http.Request) bool {
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if given == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		// Reads only: an <img> cannot send a header, so the page's own session cookie is accepted for them.
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			given = cookie.Value
		}
	}
	return given != "" && subtle.ConstantTimeCompare([]byte(given), []byte(s.opts.Token)) == 1
}

const sessionCookie = "pouple_session"

// page serves the interface: real files as they are, any other path as index.html (the router lives in the page).
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(s.opts.UI, name); err == nil && !info.IsDir() {
			http.ServeFileFS(w, r, s.opts.UI, name)
			return
		}
		if strings.Contains(name, ".") {
			http.NotFound(w, r)
			return
		}
	}
	index, err := fs.ReadFile(s.opts.UI, "index.html")
	if err != nil {
		http.Error(w, "interface missing", http.StatusInternalServerError)
		return
	}
	boot, _ := json.Marshal(map[string]string{"token": s.opts.Token})
	script := "<script>window.__POUPLE__=" + strings.ReplaceAll(string(boot), "<", "\\u003c") + "</script>"
	index = bytes.Replace(index, []byte("<!--POUPLE_BOOT-->"), []byte(script), 1)
	// The cookie lets images and downloads load (they cannot carry a header); it is never sent cross-site.
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.opts.Token, Path: "/api/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

type step struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	Connected bool   `json:"connected"`
}

func (s *Server) state(w http.ResponseWriter, _ *http.Request) {
	statuses, err := s.opts.Registry.List()
	if err != nil {
		writeError(w, err)
		return
	}
	steps := make([]step, 0, len(statuses))
	complete := true
	for _, status := range statuses {
		d := status.Descriptor
		steps = append(steps, step{ID: d.ID, Name: d.Name, Required: d.Required, Connected: status.Connected})
		if d.Required && !status.Connected {
			complete = false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"setupComplete": complete, "steps": steps})
}

func (s *Server) connectors(w http.ResponseWriter, _ *http.Request) {
	statuses, err := s.opts.Registry.List()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectors": statuses})
}

type valuesBody struct {
	Values connector.Values `json:"values"`
}

func readValues(r *http.Request) (connector.Values, error) {
	var body valuesBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return nil, connector.Invalid("", "The request is not valid JSON")
	}
	if body.Values == nil {
		body.Values = connector.Values{}
	}
	return body.Values, nil
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	values, err := readValues(r)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	profile, err := s.opts.Registry.Validate(ctx, r.PathValue("id"), values)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	values, err := readValues(r)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	profile, err := s.opts.Registry.Save(ctx, r.PathValue("id"), values)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Registry.Remove(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runAction streams what a connector action does as server-sent events: one JSON event per line, then done or error.
func (s *Server) runAction(w http.ResponseWriter, r *http.Request) {
	c, err := s.opts.Registry.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	withActions, ok := c.(connector.WithActions)
	if !ok {
		http.NotFound(w, r)
		return
	}
	action, ok := withActions.Action(r.PathValue("action"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	emit := func(event connector.Event) {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	if err := action(r.Context(), emit); err != nil {
		emit(connector.Event{Type: "error", Text: err.Error()})
		return
	}
	emit(connector.Event{Type: "done"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError answers what the user can fix with 422 and its message; anything else is a plain 500 without details.
func writeError(w http.ResponseWriter, err error) {
	var invalid *connector.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusUnprocessableEntity, invalid)
	case errors.Is(err, connector.ErrUnknown):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"message": err.Error()})
	}
}
