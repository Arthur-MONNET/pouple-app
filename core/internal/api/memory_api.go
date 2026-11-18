package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/Arthur-MONNET/pouple/core/internal/clilink"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Memory is what the HTTP layer needs for the Memory page.
type Memory interface {
	MemoryView(ctx context.Context) (engine.MemoryView, error)
	ReviewMemory(note, line string, keep bool) error
	TidyMemory() error
}

// MemoryOptions are what the memory routes need besides the engine.
type MemoryOptions struct {
	// Home is the user's home folder, where their own Claude Code and Codex read their instructions.
	Home string
	// Reveal shows a folder in the Finder.
	Reveal func(path string) error
}

// memoryPage is the Memory page: the engine's view, and the user's CLIs.
type memoryPage struct {
	engine.MemoryView
	CLIs []clilink.Status `json:"clis"`
}

// MountMemory adds the memory routes, on the web interface's server only: the local socket is for conversations.
func MountMemory(memory Memory, options MemoryOptions) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		handle := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
			mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
				if err := fn(w, r); err != nil {
					writeEngineError(w, i18n.FromContext(r.Context()), err)
				}
			})
		}
		page := func(w http.ResponseWriter, r *http.Request) error {
			view, err := memory.MemoryView(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, memoryPage{MemoryView: view, CLIs: clilink.List(options.Home, view.Folder)})
			return nil
		}
		handle("GET /api/memory", page)
		handle("POST /api/memory/review", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Note     string `json:"note"`
				Line     string `json:"line"`
				Decision string `json:"decision"`
			}
			if err := decodeBody(r, &body); err != nil || (body.Decision != "keep" && body.Decision != "drop") {
				return i18n.Wrap(engine.ErrInput, `"decision" must be "keep" or "drop"`)
			}
			if err := memory.ReviewMemory(body.Note, body.Line, body.Decision == "keep"); err != nil {
				return err
			}
			return page(w, r)
		})
		handle("POST /api/memory/tidy", func(w http.ResponseWriter, r *http.Request) error {
			return accepted(w, memory.TidyMemory())
		})
		handle("POST /api/memory/reveal", func(w http.ResponseWriter, r *http.Request) error {
			view, err := memory.MemoryView(r.Context())
			if err != nil {
				return err
			}
			if !view.Exists {
				return engine.ErrNotFound
			}
			return noContent(w, options.Reveal(view.Folder))
		})
		// Connecting writes in a file of the user's own (outside Pouple), only when they click; disconnecting takes the
		// same lines out.
		handle("POST /api/memory/clis/{id}", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Connected *bool `json:"connected"`
			}
			if err := decodeBody(r, &body); err != nil || body.Connected == nil {
				return i18n.Wrap(engine.ErrInput, `"connected" must be true or false`)
			}
			view, err := memory.MemoryView(r.Context())
			if err != nil {
				return err
			}
			if *body.Connected {
				if !view.Enabled {
					return i18n.Wrap(engine.ErrState, "turn the memory on before connecting your tools to it")
				}
				err = clilink.Connect(options.Home, r.PathValue("id"), view.Folder)
			} else {
				err = clilink.Disconnect(options.Home, r.PathValue("id"))
			}
			switch {
			case errors.Is(err, clilink.ErrUnknown):
				return engine.ErrNotFound
			case err != nil:
				return i18n.Wrap(engine.ErrState, "%s", err)
			}
			return page(w, r)
		})
	}
}
