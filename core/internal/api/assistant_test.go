package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type fakeAssistant struct {
	changes chan struct{}
	calls   []string
	err     error
}

func (f *fakeAssistant) Snapshot() engine.Snapshot {
	return engine.Snapshot{Status: engine.StatusInfo{Active: true, Notices: []string{}}, Requests: []engine.Request{{ID: "r1", Questions: []engine.Question{}, Entries: []engine.Entry{}}}}
}
func (f *fakeAssistant) Subscribe() (<-chan struct{}, func()) { return f.changes, func() {} }
func (f *fakeAssistant) record(name string, args ...any) error {
	parts := []string{name}
	for _, arg := range args {
		parts = append(parts, fmt.Sprint(arg))
	}
	f.calls = append(f.calls, strings.Join(parts, " "))
	return f.err
}
func (f *fakeAssistant) Answer(id string, a []engine.Answer) error {
	return f.record("answer", id, len(a))
}
func (f *fakeAssistant) Message(id, text string, ids []string) error {
	return f.record("message", id, text, len(ids))
}
func (f *fakeAssistant) MessageFromLocalAPI(id, text string, ids []string) error {
	return f.record("local message", id, text, len(ids))
}
func (f *fakeAssistant) DecideProposal(id string, ok bool, proposal string) error {
	return f.record("decide", id, ok, proposal)
}
func (f *fakeAssistant) Ignore(id string) error { return f.record("ignore", id) }
func (f *fakeAssistant) Retry(id string) error  { return f.record("retry", id) }
func (f *fakeAssistant) Remove(id string) error { return f.record("remove", id) }
func (f *fakeAssistant) Stop(id string) error   { return f.record("stop", id) }

func assistantServer(t *testing.T) (*Server, *fakeAssistant) {
	t.Helper()
	fake := &fakeAssistant{changes: make(chan struct{}, 1)}
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: config.Open(t.TempDir() + "/c.json")})
	ui := fstest.MapFS{"index.html": {Data: []byte("<!--POUPLE_BOOT-->")}}
	server := New(Options{Registry: registry, Token: "secret-token", UI: ui, Mount: MountAssistant(fake, engine.NewAttachments(t.TempDir()))}, port)
	return server, fake
}

func TestAssistantRoutesCallTheEngineAndMapItsErrors(t *testing.T) {
	s, fake := assistantServer(t)
	cases := []struct {
		method, path, body string
		want               int
		call               string
	}{
		{"POST", "/api/assistant/requests/r1/stop", "", 202, "stop r1"},
		{"POST", "/api/assistant/requests/r1/retry", "", 202, "retry r1"},
		{"POST", "/api/assistant/requests/r1/ignore", "", 204, "ignore r1"},
		{"DELETE", "/api/assistant/requests/r1", "", 204, "remove r1"},
		{"POST", "/api/assistant/requests/r1/proposal", `{"decision":"approve"}`, 202, "decide r1 true "},
		{"POST", "/api/assistant/requests/r1/messages", `{"text":"hi","attachmentIds":["a"]}`, 202, "message r1 hi 1"},
		{"POST", "/api/assistant/requests/r1/answers", `{"answers":[{"questionId":"q1","optionId":"A"}]}`, 202, "answer r1 1"},
		{"POST", "/api/assistant/requests/r1/proposal", `{"decision":"maybe"}`, 400, ""},
		{"POST", "/api/assistant/requests/r1/answers", `{"answers":[]}`, 400, ""},
	}
	for _, c := range cases {
		fake.calls = nil
		if got := call(s, c.method, c.path, c.body, nil).Code; got != c.want {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, got, c.want)
		}
		if c.call != "" && (len(fake.calls) != 1 || fake.calls[0] != c.call) {
			t.Errorf("%s %s: calls %v, want %q", c.method, c.path, fake.calls, c.call)
		}
	}
	for err, want := range map[error]int{engine.ErrNotFound: 404, engine.ErrState: 409, engine.ErrInactive: 409, engine.ErrInput: 400, errors.New("secret detail"): 500} {
		fake.err = err
		response := call(s, "POST", "/api/assistant/requests/r1/stop", "", nil)
		if response.Code != want {
			t.Errorf("%v: status %d, want %d", err, response.Code, want)
		}
		if want == 500 && strings.Contains(response.Body.String(), "secret detail") {
			t.Error("an unexpected error must not leak its detail")
		}
	}
}

func TestLocalAssistantOnlyExposesConversationRoutesAndRequiresProposalID(t *testing.T) {
	fake := &fakeAssistant{}
	handler := LocalAssistant(fake, engine.NewAttachments(t.TempDir()), func() string { return "en" })
	for _, tc := range []struct {
		method, host, path, origin, body string
		code                             int
	}{
		{"GET", "localhost", "/api/assistant", "", "", 200},
		{"GET", "localhost", "/api/connectors", "", "", 404},
		{"GET", "localhost", "/api/settings", "", "", 404},
		{"GET", "example.test", "/api/assistant", "", "", 403},
		{"GET", "localhost", "/api/assistant", "https://example.test", "", 403},
		{"POST", "localhost", "/api/assistant/requests/r1/proposal", "", `{"decision":"approve"}`, 400},
		{"POST", "localhost", "/api/assistant/requests/r1/proposal", "", `{"decision":"approve","proposalId":"p1"}`, 202},
		{"POST", "localhost", "/api/assistant/requests/r1/messages", "", `{"text":"yes"}`, 202},
	} {
		req := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, strings.NewReader(tc.body))
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.code {
			t.Errorf("%s %s: got %d, want %d", tc.method, tc.path, res.Code, tc.code)
		}
	}
	if len(fake.calls) != 2 || fake.calls[0] != "decide r1 true p1" || fake.calls[1] != "local message r1 yes 0" {
		t.Fatalf("unexpected engine calls: %v", fake.calls)
	}
}

func TestAttachmentsRoundTripAndAreServedSafely(t *testing.T) {
	s, _ := assistantServer(t)
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00"
	upload := call(s, "POST", "/api/assistant/requests/r1/attachments?name=shot.png", png, nil)
	if upload.Code != 201 || !strings.Contains(upload.Body.String(), `"kind":"image"`) {
		t.Fatalf("upload %d %s", upload.Code, upload.Body)
	}
	id := strings.Split(strings.Split(upload.Body.String(), `"id":"`)[1], `"`)[0]
	got := call(s, "GET", "/api/assistant/requests/r1/attachments/"+id, "", nil)
	if got.Code != 200 || got.Header().Get("Content-Type") != "image/png" || !strings.Contains(got.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("download %d %v", got.Code, got.Header())
	}
	text := call(s, "POST", "/api/assistant/requests/r1/attachments?name=x.html", "<script>alert(1)</script>", nil)
	textID := strings.Split(strings.Split(text.Body.String(), `"id":"`)[1], `"`)[0]
	if h := call(s, "GET", "/api/assistant/requests/r1/attachments/"+textID, "", nil).Header(); h.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Fatalf("non-images must download, never render: %v", h)
	}
	if call(s, "POST", "/api/assistant/requests/nope/attachments", "x", nil).Code != 404 {
		t.Fatal("unknown request")
	}
}

func TestImagesCanLoadWithTheSessionCookieOnReadsOnly(t *testing.T) {
	s, _ := assistantServer(t)
	page := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "http://127.0.0.1:4310/", nil)
	s.ServeHTTP(page, request)
	cookies := page.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie %+v", cookies)
	}
	read := httptest.NewRequest("GET", "http://127.0.0.1:4310/api/assistant", nil)
	read.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, read)
	if rec.Code != 200 {
		t.Fatalf("a read with the cookie: %d", rec.Code)
	}
	write := httptest.NewRequest("POST", "http://127.0.0.1:4310/api/assistant/requests/r1/stop", nil)
	write.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, write)
	if rec.Code != 401 {
		t.Fatalf("a write must need the bearer token: %d", rec.Code)
	}
}

func TestStreamSendsTheSnapshotThenChanges(t *testing.T) {
	s, fake := assistantServer(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "127.0.0.1:4310"
		r.Header.Set("Authorization", "Bearer secret-token")
		s.ServeHTTP(w, r)
	}))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/assistant/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buffer := make([]byte, 4096)
	n, _ := response.Body.Read(buffer)
	if !strings.HasPrefix(string(buffer[:n]), "data: {") || !strings.Contains(string(buffer[:n]), `"r1"`) {
		t.Fatalf("first event %q", buffer[:n])
	}
	fake.changes <- struct{}{}
	time.Sleep(10 * time.Millisecond)
}
