package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// maxBody is the largest JSON body the remote API reads (the assistant routes read no more).
	maxBody = 1 << 20
	// maxDeviceName is the longest device name kept, in characters.
	maxDeviceName = 64

	// HeaderNonce and HeaderSignature carry a step-up.
	HeaderNonce     = "X-Pouple-Nonce"
	HeaderSignature = "X-Pouple-Signature"
)

// HandlerOptions configure the remote API handler.
type HandlerOptions struct {
	Devices *Devices
	Pairing *Pairing
	Nonces  *Nonces
	// Mount adds the assistant routes the allowlist picks from (api.MountRemote): they are mounted on a mux of their
	// own, never the loopback API's.
	Mount func(mux *http.ServeMux)
	// Granted says whether a conversation has write approvals.
	Granted func(id string) bool
	// AutoApproveAll says whether every proposed change applies without asking.
	AutoApproveAll func() bool
	// Language is the interface language of the engine's error messages.
	Language func() string
	// Endpoint is the current external address as "host:port" (the pairing payload's ext), or "" when there is none or
	// it is not known: GET /remote/v1/endpoint.
	Endpoint func() string
	// Local says whether a source address is on a local network; nil is IsLocalAddress. It picks the limits of the
	// throttle and decides which sources are logged as external.
	Local func(netip.Addr) bool
	// Throttle slows down sources that keep failing to authenticate; nil makes one with the default limits.
	Throttle *Throttle
	// Now is the clock of the default throttle.
	Now func() time.Time
}

// Handler serves the remote API: pairing, step-up challenges, and the allowlisted assistant routes.
type Handler struct {
	opts     HandlerOptions
	mux      *http.ServeMux
	local    func(netip.Addr) bool
	throttle *Throttle

	seenMu sync.Mutex
	seen   map[netip.Addr]struct{} // external sources already logged
}

// maxSeenSources bounds the external sources remembered for the "first request" log line.
const maxSeenSources = 1024

func NewHandler(opts HandlerOptions) *Handler {
	mux := http.NewServeMux()
	if opts.Mount != nil {
		opts.Mount(mux)
	}
	h := &Handler{opts: opts, mux: mux, local: opts.Local, throttle: opts.Throttle, seen: map[netip.Addr]struct{}{}}
	if h.local == nil {
		h.local = IsLocalAddress
	}
	if h.throttle == nil {
		h.throttle = NewThrottle(ThrottleOptions{Now: opts.Now, Local: h.local})
	}
	return h
}

// noteExternal logs, once per source address, the first request an external source makes that got past the TLS
// handshake and authenticated or tried to pair. It writes no token and no content.
func (h *Handler) noteExternal(source netip.Addr) {
	if !source.IsValid() || h.local(source) {
		return
	}
	key := throttleKey(source)
	h.seenMu.Lock()
	_, known := h.seen[key]
	if !known {
		if len(h.seen) >= maxSeenSources {
			clear(h.seen)
		}
		h.seen[key] = struct{}{}
	}
	h.seenMu.Unlock()
	if !known {
		log.Printf("remote access: first request from the external address %s", source)
	}
}

// failed counts a failed authentication against the source, and logs the lockout it may start.
func (h *Handler) failed(source netip.Addr) {
	if lock := h.throttle.Fail(source); lock > 0 {
		log.Printf("remote access: %s failed to authenticate too many times, locked out for %s", source, lock)
	}
}

// tooManyAttempts answers a locked-out source.
func tooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
	refuse(w, http.StatusTooManyRequests, "too-many-attempts")
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func refuse(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, apiError{Error: code})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// The phone is not a browser: a request a web page made (any origin, any fetch metadata) is never served.
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		refuse(w, http.StatusForbidden, "forbidden-origin")
		return
	}
	if r.Context().Err() != nil {
		refuse(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	source := sourceOf(r.RemoteAddr)
	// A source that keeps failing waits, whatever it sends: a valid token does not get it past a lockout.
	if wait, blocked := h.throttle.Blocked(source); blocked {
		tooManyAttempts(w, wait)
		return
	}
	if r.URL.Path == "/remote/v1/pair" {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusMethodNotAllowed, "method-not-allowed")
			return
		}
		h.pair(w, r, source)
		return
	}
	device, err := h.authenticate(r)
	if err != nil {
		h.failed(source)
		refuse(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	h.noteExternal(source)
	switch r.URL.Path {
	case "/remote/v1/challenge":
		h.challenge(w, r, device)
		return
	case "/remote/v1/device":
		h.self(w, r, device)
		return
	case "/remote/v1/endpoint":
		h.endpoint(w, r)
		return
	}
	h.serveAllowed(w, r, device, source)
}

// endpoint is the external address as the Mac knows it now: GET /remote/v1/endpoint. The phone asks after each
// connection and keeps what it learns, so an address that changed needs no new pairing.
func (h *Handler) endpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		refuse(w, http.StatusMethodNotAllowed, "method-not-allowed")
		return
	}
	var ext *string
	if h.opts.Endpoint != nil {
		if current := h.opts.Endpoint(); current != "" {
			ext = &current
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Ext *string `json:"ext"`
	}{ext})
}

func (h *Handler) authenticate(r *http.Request) (Device, error) {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return Device{}, ErrUnauthorized
	}
	return h.opts.Devices.Authenticate(token)
}

// pair turns a pairing code into a device: POST /remote/v1/pair {code, deviceName, stepUpPublicKey}.
func (h *Handler) pair(w http.ResponseWriter, r *http.Request, source netip.Addr) {
	var body struct {
		Code            string `json:"code"`
		DeviceName      string `json:"deviceName"`
		StepUpPublicKey string `json:"stepUpPublicKey"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil {
		refuse(w, http.StatusBadRequest, "bad-request")
		return
	}
	name, ok := deviceName(body.DeviceName)
	if !ok {
		refuse(w, http.StatusUnprocessableEntity, "bad-device-name")
		return
	}
	if _, err := ParsePublicKey(body.StepUpPublicKey); err != nil {
		refuse(w, http.StatusUnprocessableEntity, "bad-key")
		return
	}
	switch err := h.opts.Pairing.Consume(body.Code); {
	case errors.Is(err, ErrPairingClosed):
		h.failed(source)
		refuse(w, http.StatusForbidden, "pairing-closed")
		return
	case err != nil:
		h.failed(source)
		refuse(w, http.StatusForbidden, "pairing-refused")
		return
	}
	device, token, err := h.opts.Devices.Add(name, body.StepUpPublicKey)
	switch {
	case errors.Is(err, ErrTooManyDevices):
		refuse(w, http.StatusConflict, "too-many-devices")
		return
	case err != nil:
		log.Printf("remote access: a device could not be paired: %v", err)
		refuse(w, http.StatusInternalServerError, "internal")
		return
	}
	h.throttle.Succeed(source)
	if h.local(source) {
		log.Printf("remote access: paired %q (%s)", device.Name, device.ID)
	} else {
		log.Printf("remote access: paired %q (%s) from the external address %s", device.Name, device.ID, source)
		h.noteExternal(source)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"deviceId": device.ID, "token": token})
}

// deviceName is the name the phone gave, trimmed; letters, marks, numbers, punctuation and spaces only.
func deviceName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxDeviceName {
		return "", false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || unicode.IsPunct(r) || r == ' ') {
			return "", false
		}
	}
	return name, true
}

// challenge issues a step-up nonce: POST /remote/v1/challenge.
func (h *Handler) challenge(w http.ResponseWriter, r *http.Request, device Device) {
	if r.Method != http.MethodPost {
		refuse(w, http.StatusMethodNotAllowed, "method-not-allowed")
		return
	}
	nonce, expires, err := h.opts.Nonces.Issue(device.ID)
	if err != nil {
		refuse(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"nonce": nonce, "expiresAt": expires.UTC().Format(time.RFC3339)})
}

// self is the device's own record: GET reads it, DELETE unpairs it.
func (h *Handler) self(w http.ResponseWriter, r *http.Request, device Device) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]string{"id": device.ID, "name": device.Name})
	case http.MethodDelete:
		if err := h.opts.Devices.Revoke(device.ID); err != nil {
			refuse(w, http.StatusInternalServerError, "internal")
			return
		}
		log.Printf("remote access: %q (%s) unpaired itself", device.Name, device.ID)
		w.WriteHeader(http.StatusNoContent)
	default:
		refuse(w, http.StatusMethodNotAllowed, "method-not-allowed")
	}
}

// stepUpFields are the body fields a step-up covers, decoded as the assistant routes decode them.
type stepUpFields struct {
	Decision   string `json:"decision"`
	PlanID     string `json:"planId"`
	ProposalID string `json:"proposalId"`
	Actions    *[]int `json:"actions"`
}

// serveAllowed serves an assistant route when the allowlist has it, after its step-up when it needs one.
func (h *Handler) serveAllowed(w http.ResponseWriter, r *http.Request, device Device, source netip.Addr) {
	// Only a path in its canonical form is matched: the mux would otherwise answer with a redirect of its own.
	if r.URL.Path == "" || path.Clean(r.URL.Path) != r.URL.Path || !strings.HasPrefix(r.URL.Path, "/api/") {
		refuse(w, http.StatusForbidden, "forbidden-route")
		return
	}
	_, pattern := h.mux.Handler(r)
	rule, allowed := policy[pattern]
	if !allowed {
		refuse(w, http.StatusForbidden, "forbidden-route")
		return
	}
	var body []byte
	if !rule.Upload && r.Method != http.MethodGet && r.Method != http.MethodHead {
		read, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			refuse(w, http.StatusBadRequest, "bad-request")
			return
		}
		if len(read) > maxBody {
			refuse(w, http.StatusRequestEntityTooLarge, "body-too-large")
			return
		}
		body = read
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	if rule.Scope == ScopeStepUp || rule.StepUpWhen != nil {
		var fields stepUpFields
		if len(body) > 0 {
			if err := json.NewDecoder(bytes.NewReader(body)).Decode(&fields); err != nil {
				refuse(w, http.StatusBadRequest, "bad-request")
				return
			}
		}
		requestID := pathValue(pattern, r.URL.EscapedPath(), "id")
		facts := Facts{Decision: fields.Decision}
		if rule.StepUpWhen != nil {
			if requestID != "" && strings.HasPrefix(patternPath(pattern), "/api/assistant/requests/{id}") && h.opts.Granted != nil {
				facts.Granted = h.opts.Granted(requestID)
			}
			facts.AutoApproveAll = h.opts.AutoApproveAll != nil && h.opts.AutoApproveAll()
		}
		if rule.needsStepUp(facts) {
			if code, ok := h.verifyStepUp(r, device, pattern, requestID, fields, body); !ok {
				if code == "step-up-refused" {
					h.failed(source)
				}
				refuse(w, http.StatusForbidden, code)
				return
			}
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	untrack, active := h.opts.Devices.track(device.ID, cancel)
	if !active {
		refuse(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	defer untrack()
	lang := i18n.FR
	if h.opts.Language != nil {
		lang = i18n.Parse(h.opts.Language())
	}
	h.mux.ServeHTTP(w, r.WithContext(i18n.WithLanguage(ctx, lang)))
}

func (h *Handler) verifyStepUp(r *http.Request, device Device, pattern, requestID string, fields stepUpFields, body []byte) (string, bool) {
	nonce, signature := r.Header.Get(HeaderNonce), r.Header.Get(HeaderSignature)
	if nonce == "" || signature == "" {
		return "step-up-required", false
	}
	sum := sha256.Sum256(body)
	payload := StepUp{
		Nonce: nonce, DeviceID: device.ID, Route: pattern, RequestID: requestID,
		PlanID: fields.PlanID, ProposalID: fields.ProposalID, Actions: fields.Actions, BodySHA256: hex.EncodeToString(sum[:]),
	}
	if err := h.opts.Nonces.Verify(device.StepUpPublicKey, payload, signature); err != nil {
		log.Printf("remote access: a step-up of %q (%s) on %s was refused", device.Name, device.ID, pattern)
		return "step-up-refused", false
	}
	return "", true
}

// patternPath is the path part of a "METHOD /path" pattern.
func patternPath(pattern string) string {
	if _, after, ok := strings.Cut(pattern, " "); ok {
		return after
	}
	return pattern
}

// pathValue reads a wildcard of a matched pattern from the request's escaped path. The allowlist has no "..."
// wildcard, so the pattern and the path have the same segments.
func pathValue(pattern, escapedPath, name string) string {
	patternSegments := strings.Split(patternPath(pattern), "/")
	pathSegments := strings.Split(escapedPath, "/")
	if len(patternSegments) != len(pathSegments) {
		return ""
	}
	for i, segment := range patternSegments {
		if segment == "{"+name+"}" {
			value, err := url.PathUnescape(pathSegments[i])
			if err != nil {
				return ""
			}
			return value
		}
	}
	return ""
}
