package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// Assistant is what the HTTP layer needs from the engine.
type Assistant interface {
	Snapshot() engine.Snapshot
	Subscribe() (<-chan struct{}, func())
	Answer(id string, answers []engine.Answer) error
	Message(id, text string, attachmentIDs []string) error
	DecideProposal(id string, approve bool, proposalID string) error
	Ignore(id string) error
	// Close hides a request's bubble until it needs the user again; seen is the update time the user saw, or "".
	Close(id, seen string) error
	Retry(id string) error
	// ContinueWith switches to another AI and investigates the request again with it.
	ContinueWith(id string, provider providers.ID) error
	Remove(id string) error
	Stop(id string) error
	// BrowserFrame is the last picture the browser drew while it worked for a conversation; false when none is open.
	BrowserFrame(id string) ([]byte, bool)
}

const (
	heartbeat     = 15 * time.Second
	coalesce      = 80 * time.Millisecond
	maxUploadSize = engine.MaxAttachmentBytes
)

// MountAssistant adds the conversation routes (the contract Cockpit's chat already used).
func MountAssistant(assistant Assistant, attachments *engine.Attachments) func(mux *http.ServeMux) {
	return mountAssistant(assistant, attachments, assistant.Message, false)
}

type localAssistantEngine interface {
	Assistant
	MessageFromLocalAPI(id, text string, attachmentIDs []string) error
}

// LocalAssistant exposes conversation operations to native clients on an owner-only Unix socket.
func LocalAssistant(assistant localAssistantEngine, attachments *engine.Attachments, language func() string) http.Handler {
	mux := http.NewServeMux()
	mountAssistant(assistant, attachments, assistant.MessageFromLocalAPI, true)(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Err() != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Host != "localhost" || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r.WithContext(i18n.WithLanguage(r.Context(), i18n.Parse(language()))))
	})
}

func mountAssistant(assistant Assistant, attachments *engine.Attachments, message func(string, string, []string) error, requireProposalID bool) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		handle := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
			mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
				if err := fn(w, r); err != nil {
					writeEngineError(w, i18n.FromContext(r.Context()), err)
				}
			})
		}
		mux.HandleFunc("GET /api/assistant", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, assistant.Snapshot())
		})
		mux.HandleFunc("GET /api/assistant/stream", func(w http.ResponseWriter, r *http.Request) { stream(assistant, w, r) })

		handle("POST /api/assistant/requests/{id}/answers", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Answers []engine.Answer `json:"answers"`
			}
			if err := decodeBody(r, &body); err != nil || len(body.Answers) == 0 {
				return i18n.Wrap(engine.ErrInput, `"answers" must be a non-empty array`)
			}
			return accepted(w, assistant.Answer(r.PathValue("id"), body.Answers))
		})
		handle("POST /api/assistant/requests/{id}/messages", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Text          string   `json:"text"`
				AttachmentIDs []string `json:"attachmentIds"`
			}
			if err := decodeBody(r, &body); err != nil {
				return i18n.Wrap(engine.ErrInput, "the body is not valid JSON")
			}
			return accepted(w, message(r.PathValue("id"), body.Text, body.AttachmentIDs))
		})
		handle("POST /api/assistant/requests/{id}/proposal", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Decision   string `json:"decision"`
				ProposalID string `json:"proposalId"`
			}
			if err := decodeBody(r, &body); err != nil || (body.Decision != "approve" && body.Decision != "decline") {
				return i18n.Wrap(engine.ErrInput, `"decision" must be "approve" or "decline"`)
			}
			if requireProposalID && body.ProposalID == "" {
				return i18n.Wrap(engine.ErrInput, `"proposalId" is required`)
			}
			return accepted(w, assistant.DecideProposal(r.PathValue("id"), body.Decision == "approve", body.ProposalID))
		})
		handle("POST /api/assistant/requests/{id}/ignore", func(w http.ResponseWriter, r *http.Request) error {
			return noContent(w, assistant.Ignore(r.PathValue("id")))
		})
		handle("POST /api/assistant/requests/{id}/close", func(w http.ResponseWriter, r *http.Request) error {
			// The body is optional: {"updatedAt": "…"} names the state the user closed, so a newer one stays open.
			var body struct {
				UpdatedAt string `json:"updatedAt"`
			}
			if err := decodeBody(r, &body); err != nil && !errors.Is(err, io.EOF) {
				return i18n.Wrap(engine.ErrInput, "the body is not valid JSON")
			}
			return noContent(w, assistant.Close(r.PathValue("id"), body.UpdatedAt))
		})
		handle("POST /api/assistant/requests/{id}/continue-with", func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Provider string `json:"provider"`
			}
			provider, known := providers.ID(""), false
			if err := decodeBody(r, &body); err == nil {
				provider, known = providers.Parse(body.Provider)
			}
			if !known {
				return i18n.Wrap(engine.ErrInput, `"provider" must be "codex" or "claude"`)
			}
			return accepted(w, assistant.ContinueWith(r.PathValue("id"), provider))
		})
		handle("POST /api/assistant/requests/{id}/retry", func(w http.ResponseWriter, r *http.Request) error {
			return accepted(w, assistant.Retry(r.PathValue("id")))
		})
		handle("POST /api/assistant/requests/{id}/stop", func(w http.ResponseWriter, r *http.Request) error {
			return accepted(w, assistant.Stop(r.PathValue("id")))
		})
		handle("DELETE /api/assistant/requests/{id}", func(w http.ResponseWriter, r *http.Request) error {
			return noContent(w, assistant.Remove(r.PathValue("id")))
		})

		// A file added in the chat: raw bytes in the body, its name in the query. It waits for the message that sends it.
		handle("POST /api/assistant/requests/{id}/attachments", func(w http.ResponseWriter, r *http.Request) error {
			if !requestExists(assistant, r.PathValue("id")) {
				return engine.ErrNotFound
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, maxUploadSize+1))
			if err != nil {
				return err
			}
			name := r.URL.Query().Get("name")
			if name == "" {
				name = "file"
			}
			ref, err := attachments.Save(name, data)
			if err != nil {
				return i18n.Wrap(engine.ErrInput, "%s", err)
			}
			writeJSON(w, http.StatusCreated, ref)
			return nil
		})
		handle("GET /api/assistant/requests/{id}/browser", func(w http.ResponseWriter, r *http.Request) error {
			frame, ok := assistant.BrowserFrame(r.PathValue("id"))
			if !ok {
				return engine.ErrNotFound
			}
			// A picture of a page is shown as a picture and nothing else, and is never kept by the browser.
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(frame)
			return nil
		})
		handle("GET /api/assistant/requests/{id}/attachments/{attachmentId}", func(w http.ResponseWriter, r *http.Request) error {
			if !requestExists(assistant, r.PathValue("id")) {
				return engine.ErrNotFound
			}
			ref, path, err := attachments.Open(r.PathValue("attachmentId"))
			if err != nil {
				return engine.ErrNotFound
			}
			// Only what is safe to show is shown: images as images, everything else a download the browser never runs.
			inline := ref.Kind == "image"
			contentType, disposition := "application/octet-stream", "attachment"
			if inline {
				contentType, disposition = ref.Mime, "inline"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(ref.Name))
			w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeFile(w, r, path)
			return nil
		})
	}
}

func requestExists(assistant Assistant, id string) bool {
	for _, request := range assistant.Snapshot().Requests {
		if request.ID == id {
			return true
		}
	}
	return false
}

func decodeBody(r *http.Request, into any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(into)
}

func accepted(w http.ResponseWriter, err error) error {
	if err == nil {
		w.WriteHeader(http.StatusAccepted)
	}
	return err
}

func noContent(w http.ResponseWriter, err error) error {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
	}
	return err
}

func writeEngineError(w http.ResponseWriter, lang i18n.Lang, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, engine.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, engine.ErrState), errors.Is(err, engine.ErrInactive):
		status = http.StatusConflict
	case errors.Is(err, engine.ErrInput):
		status = http.StatusBadRequest
	}
	message := i18n.ErrorText(lang, err)
	if status == http.StatusInternalServerError {
		message = i18n.T(lang, "Something went wrong")
	}
	writeJSON(w, status, map[string]string{"error": message})
}

// stream sends the full snapshot on connect and again after each change; bursts are sent as one.
func stream(assistant Assistant, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.WriteHeader(http.StatusOK)

	changes, unsubscribe := assistant.Subscribe()
	defer unsubscribe()
	last := ""
	send := func() {
		data, _ := json.Marshal(assistant.Snapshot())
		if string(data) == last {
			return
		}
		last = string(data)
		// Named, as the macOS bubbles expect (they ignore unnamed events): a change here silently hides every bubble.
		_, _ = fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
		flusher.Flush()
	}
	send()
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			time.Sleep(coalesce)
			select {
			case <-changes:
			default:
			}
			send()
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}
