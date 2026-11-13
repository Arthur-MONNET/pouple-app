package screenshot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func taker(hosts ...string) *Taker {
	return &Taker{Hosts: func() []string { return hosts }, Forbidden: func() []int { return []int{8123} }}
}

func TestOnlyLocalhostAndListedSitesAreOpened(t *testing.T) {
	tk := taker("preprod.example.com")
	for _, ok := range []string{"http://localhost:8001/premium", "https://127.0.0.1:5173/", "https://PREPROD.example.com/a?b=1#frag", "http://[::1]:3000/"} {
		if _, err := tk.Check(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"file:///etc/passwd", "javascript:alert(1)", "ftp://localhost/", "http://evil.example.com/", "http://user:pw@localhost/",
		"http://localhost.evil.com/", "http://169.254.169.254/latest", "http://localhost:8123/api/assistant", "localhost:8001", "",
	} {
		if _, err := tk.Check(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if clean, _ := tk.Check("https://preprod.example.com/a#frag"); strings.Contains(clean, "frag") {
		t.Errorf("the fragment must go: %s", clean)
	}
}

// fakeBrowser stands for Chrome on the other end of the pipe: it answers the commands it receives (recording them), and,
// once the page is asked for, makes the requests a page would make, draws a frame and says the page loaded.
type fakeBrowser struct {
	mu   sync.Mutex
	seen []string
	// failed and continued are the ids of the requests the client refused and let through.
	failed, continued []string
}

func (f *fakeBrowser) connect(t *testing.T) *connection {
	t.Helper()
	fromBrowserRead, fromBrowserWrite := io.Pipe()
	toBrowserRead, toBrowserWrite := io.Pipe()
	t.Cleanup(func() { fromBrowserWrite.Close(); toBrowserWrite.Close() })
	send := func(v any) {
		data, _ := json.Marshal(v)
		_, _ = fromBrowserWrite.Write(append(data, 0))
	}
	go func() {
		reader := bufio.NewReader(toBrowserRead)
		for {
			data, err := reader.ReadBytes(0)
			if err != nil {
				return
			}
			var m struct {
				ID        int             `json:"id"`
				Method    string          `json:"method"`
				Params    json.RawMessage `json:"params"`
				SessionID string          `json:"sessionId"`
			}
			_ = json.Unmarshal(data[:len(data)-1], &m)
			var params struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(m.Params, &params)
			f.mu.Lock()
			f.seen = append(f.seen, m.Method)
			switch m.Method {
			case "Fetch.failRequest":
				f.failed = append(f.failed, params.RequestID)
			case "Fetch.continueRequest":
				f.continued = append(f.continued, params.RequestID)
			}
			f.mu.Unlock()
			result := map[string]any{}
			switch m.Method {
			case "Target.createTarget":
				result = map[string]any{"targetId": "T1"}
			case "Target.attachToTarget":
				result = map[string]any{"sessionId": "S1"}
			case "Page.captureScreenshot":
				result = map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("\x89PNG-the-page"))}
			}
			send(map[string]any{"id": m.ID, "sessionId": m.SessionID, "result": result})
			if m.Method == "Page.navigate" {
				paused := func(id, resource, address string) {
					send(map[string]any{"method": "Fetch.requestPaused", "sessionId": "S1", "params": map[string]any{"requestId": id, "resourceType": resource, "request": map[string]any{"url": address}}})
				}
				paused("page", "Document", "http://localhost:8001/premium")
				paused("redirect", "Document", "http://evil.example.com/steal")
				paused("beacon", "Fetch", "https://cdn.example.net/collect")
				paused("picture", "Image", "https://cdn.example.net/logo.png")
				paused("local-file", "Document", "file:///etc/passwd")
				paused("core", "Fetch", "http://localhost:8123/api/assistant")
				send(map[string]any{"method": "Page.screencastFrame", "sessionId": "S1", "params": map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("JPEG1")), "sessionId": 7}})
				send(map[string]any{"method": "Page.loadEventFired", "sessionId": "S1", "params": map[string]any{}})
			}
		}
	}()
	return newConnection(fromBrowserRead, toBrowserWrite)
}

func TestCaptureOpensThePageChecksEveryRequestAndFollowsItLive(t *testing.T) {
	browser := &fakeBrowser{}
	conn := browser.connect(t)
	var steps []string
	var frames [][]byte
	foreign := 0
	observer := &Observer{Step: func(s string) { steps = append(steps, s) }, Frame: func(f []byte) { frames = append(frames, f) }, Foreign: func() { foreign++ }}
	tk := taker("preprod.example.com")
	data, err := capture(context.Background(), conn, "http://localhost:8001/premium", viewports["mobile"], observer, tk.allow)
	if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("picture: %q, %v", data, err)
	}
	if len(frames) != 1 || string(frames[0]) != "JPEG1" {
		t.Fatalf("frames: %q", frames)
	}
	if got := strings.Join(steps, ","); got != "Opening the page,Waiting for the page to settle,Taking the picture" {
		t.Fatalf("steps: %s", got)
	}
	// Give the pump the moment it needs to answer the last request before reading what the browser was told.
	time.Sleep(100 * time.Millisecond)
	browser.mu.Lock()
	defer browser.mu.Unlock()
	// The page itself and a picture from a CDN go through; a redirect elsewhere, a script's call to a stranger, a local
	// file and Pouple's own port do not.
	if !slices.Equal(browser.continued, []string{"page", "picture"}) || !slices.Equal(browser.failed, []string{"redirect", "beacon", "local-file", "core"}) {
		t.Fatalf("continued %v failed %v", browser.continued, browser.failed)
	}
	if foreign != 1 {
		t.Errorf("the CDN picture goes beyond this computer: %d", foreign)
	}
	order := strings.Join(browser.seen, " ")
	for _, want := range []string{"Browser.setDownloadBehavior", "Fetch.enable", "Emulation.setUserAgentOverride", "Page.screencastFrameAck", "Page.stopScreencast"} {
		if !strings.Contains(order, want) {
			t.Errorf("missing %s in %s", want, order)
		}
	}
	// Nothing leaves before every request is looked at, the screen is set before the page opens, and the stream stops
	// before the picture.
	if strings.Index(order, "Fetch.enable") > strings.Index(order, "Page.navigate") || strings.Index(order, "setDeviceMetricsOverride") > strings.Index(order, "Page.navigate") || strings.Index(order, "Page.stopScreencast") > strings.Index(order, "Page.captureScreenshot") {
		t.Errorf("wrong order: %s", order)
	}
}

func TestTakeRefusesBeforeAnyBrowserRuns(t *testing.T) {
	tk := taker()
	tk.Browsers = []string{"/nonexistent/chrome"}
	if _, err := tk.Take(context.Background(), "http://evil.example.com/", "", nil); err == nil || strings.Contains(err.Error(), "Chromium") {
		t.Fatalf("a site that is not listed must be refused first: %v", err)
	}
	if _, err := tk.Take(context.Background(), "http://localhost:8001/", "tablet", nil); err == nil {
		t.Error("an unknown screen size must be refused")
	}
	if _, err := tk.Take(context.Background(), "http://localhost:8001/", "", nil); err == nil || !strings.Contains(err.Error(), "Chromium") {
		t.Fatalf("without a browser the error says so: %v", err)
	}
}

// With a real browser on the Mac, the whole path works: a local page, a real picture.
func TestARealBrowserTakesARealPicture(t *testing.T) {
	tk := &Taker{}
	if _, err := tk.browser(); err != nil {
		t.Skip("no Chromium-based browser on this machine")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<h1 style='color:#e0245e'>hello</h1>"))
	}))
	defer page.Close()
	frames := 0
	data, err := tk.Take(context.Background(), page.URL, "mobile", &Observer{Frame: func([]byte) { frames++ }})
	if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("picture: %d bytes, %v", len(data), err)
	}
	if frames == 0 {
		t.Error("the browser showed nothing live")
	}
}
