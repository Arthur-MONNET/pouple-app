package slackinbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const testToken = "xoxp-secret-token"

// call is one request the fake Slack received.
type call struct {
	method string
	form   url.Values
}

// reply is a custom answer, for what is not a plain JSON 200.
type reply struct {
	status int
	header map[string]string
	body   string
}

// fakeSlack is a local Slack Web API. Every request is checked for the token rules: the token is in the
// Authorization header and nowhere in the address.
type fakeSlack struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	calls    []call
	handlers map[string]func(call) any
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{t: t, handlers: map[string]func(call) any{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.t.Errorf("Slack call used %s, want POST", r.Method)
	}
	if r.URL.RawQuery != "" {
		f.t.Errorf("Slack call carries a query string: %q", r.URL.RawQuery)
	}
	if strings.Contains(r.URL.String(), testToken) {
		f.t.Errorf("the token leaked into the address %q", r.URL)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		f.t.Errorf("Authorization = %q", got)
	}
	if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		f.t.Errorf("Content-Type = %q", got)
	}
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("parse form: %v", err)
	}
	for key, values := range r.PostForm {
		for _, value := range values {
			if strings.Contains(value, testToken) {
				f.t.Errorf("the token leaked into the form field %q", key)
			}
		}
	}
	c := call{method: strings.TrimPrefix(r.URL.Path, "/"), form: r.PostForm}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	handler := f.handlers[c.method]
	f.mu.Unlock()
	if handler == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch answer := handler(c).(type) {
	case reply:
		for key, value := range answer.header {
			w.Header().Set(key, value)
		}
		if answer.status != 0 {
			w.WriteHeader(answer.status)
		}
		_, _ = w.Write([]byte(answer.body))
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(answer)
	}
}

func (f *fakeSlack) on(method string, handler func(call) any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[method] = handler
}

// respond answers a method with the same payload every time.
func (f *fakeSlack) respond(method string, body any) {
	f.on(method, func(call) any { return body })
}

func (f *fakeSlack) callsTo(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matching []call
	for _, c := range f.calls {
		if c.method == method {
			matching = append(matching, c)
		}
	}
	return matching
}

func (f *fakeSlack) client() *Client {
	return NewClient(testToken, f.server.Client(), f.server.URL)
}

func ok(fields object) object {
	body := object{"ok": true}
	for key, value := range fields {
		body[key] = value
	}
	return body
}

func slackError(code string, extra ...string) object {
	body := object{"ok": false, "error": code}
	if len(extra) > 0 {
		body["needed"] = extra[0]
	}
	return body
}
