package remote

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	outside = "203.0.113.7:40000"
	home    = "192.168.1.50:51234"
)

// logCapture collects the log lines written while a test runs.
type logCapture struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines.Write(p)
}

func (l *logCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines.String()
}

func captureLog(t *testing.T) *logCapture {
	t.Helper()
	capture := &logCapture{}
	previous := log.Writer()
	log.SetOutput(capture)
	t.Cleanup(func() { log.SetOutput(previous) })
	return capture
}

func (f *fixture) bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestEndpointRouteIsAuthenticatedAndReportsTheCurrentAddress(t *testing.T) {
	f := newFixture(t)
	if response := f.do("GET", "/remote/v1/endpoint", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", response.Code)
	}
	if response := f.do("GET", "/remote/v1/endpoint", "", f.bearer(strings.Repeat("0", 64))); response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", response.Code)
	}
	f.pair()
	if response := f.authed("GET", "/remote/v1/endpoint", "", map[string]string{"Origin": "https://evil.example"}); response.Code != http.StatusForbidden || errorCode(t, response) != "forbidden-origin" {
		t.Fatalf("a browser request: %d %s", response.Code, response.Body)
	}
	if response := f.authed("GET", "/remote/v1/endpoint", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); response.Code != http.StatusForbidden {
		t.Fatalf("fetch metadata: %d", response.Code)
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		if response := f.authed(method, "/remote/v1/endpoint", "", nil); response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: %d", method, response.Code)
		}
	}
	for _, tc := range []struct{ endpoint, body string }{
		{"", `{"ext":null}`},
		{"home.example.net:47821", `{"ext":"home.example.net:47821"}`},
		{"[2001:db8::1]:8443", `{"ext":"[2001:db8::1]:8443"}`},
	} {
		f.endpoint = tc.endpoint
		response := f.authed("GET", "/remote/v1/endpoint", "", nil)
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != tc.body || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("endpoint %q: %d %s", tc.endpoint, response.Code, response.Body)
		}
	}
	if err := f.devices.Revoke(f.deviceID); err != nil {
		t.Fatal(err)
	}
	if response := f.authed("GET", "/remote/v1/endpoint", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked device: %d", response.Code)
	}
}

func TestBadTokensLockAnExternalSourceOutAndTheLockoutEnds(t *testing.T) {
	f := newFixture(t)
	f.pair() // from the home network
	f.source = outside
	bad := f.bearer(strings.Repeat("f", 64))
	for i := 1; i < externalLimits.failures; i++ {
		if response := f.do("GET", "/api/assistant", "", bad); response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i, response.Code)
		}
	}
	if response := f.do("GET", "/api/assistant", "", bad); response.Code != http.StatusUnauthorized {
		t.Fatalf("the failure that starts the lockout is still a 401: %d", response.Code)
	}
	response := f.do("GET", "/api/assistant", "", bad)
	if response.Code != http.StatusTooManyRequests || errorCode(t, response) != "too-many-attempts" || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("locked out: %d %s retry-after=%q", response.Code, response.Body, response.Header().Get("Retry-After"))
	}
	// Whatever the request, a locked-out source waits: a valid token does not get through, nor does pairing.
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("valid token during the lockout: %d", response.Code)
	}
	if response := f.do("POST", "/remote/v1/pair", `{}`, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("pairing during the lockout: %d", response.Code)
	}
	// The home network is another source.
	f.source = home
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
		t.Fatalf("a phone at home during someone else's lockout: %d", response.Code)
	}
	f.source = outside
	f.clock.advance(30 * time.Second)
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "30" {
		t.Fatalf("half way through the lockout, the remaining time is reported: %d %q", response.Code, response.Header().Get("Retry-After"))
	}
	f.clock.advance(31 * time.Second)
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
		t.Fatalf("after the lockout: %d", response.Code)
	}
	// Failing again locks for longer.
	for range externalLimits.failures {
		f.do("GET", "/api/assistant", "", bad)
	}
	if response := f.do("GET", "/api/assistant", "", bad); response.Header().Get("Retry-After") != "120" {
		t.Fatalf("the second lockout must be longer: %d retry-after=%q", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestTheHomeNetworkGetsGenerousLimits(t *testing.T) {
	f := newFixture(t)
	for i := range localLimits.failures {
		if response := f.do("GET", "/api/assistant", "", f.bearer(strings.Repeat("e", 64))); response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i+1, response.Code)
		}
	}
	if response := f.do("GET", "/api/assistant", "", nil); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "30" {
		t.Fatalf("a local source is locked out too after %d failures: %d %q", localLimits.failures, response.Code, response.Header().Get("Retry-After"))
	}
}

func TestWrongPairingCodesAndWrongStepUpsCountAgainstTheSource(t *testing.T) {
	f := newFixture(t)
	f.source = outside
	if _, _, err := f.pairing.Open(); err != nil {
		t.Fatal(err)
	}
	_, public := newKey(t)
	wrong, _ := json.Marshal(map[string]string{"code": strings.Repeat("0", 32), "deviceName": "Pixel", "stepUpPublicKey": public})
	for i := 0; i < externalLimits.failures; i++ {
		response := f.do("POST", "/remote/v1/pair", string(wrong), nil)
		// Five wrong codes close the window; what follows is "pairing-closed", and counts as well.
		want := "pairing-refused"
		if i >= maxPairingAttempts {
			want = "pairing-closed"
		}
		if response.Code != http.StatusForbidden || errorCode(t, response) != want {
			t.Fatalf("attempt %d: %d %s", i+1, response.Code, response.Body)
		}
	}
	if response := f.do("POST", "/remote/v1/pair", string(wrong), nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("pairing after %d failures: %d", externalLimits.failures, response.Code)
	}
	// Malformed requests are not guesses at the code: they are not counted.
	g := newFixture(t)
	g.source = outside
	for range 3 * externalLimits.failures {
		if response := g.do("POST", "/remote/v1/pair", `not json`, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("malformed pairing: %d", response.Code)
		}
	}

	// A wrong step-up signature from a device that holds a valid token counts too.
	h := newFixture(t)
	h.source = outside
	h.pair()
	for i := 0; i < externalLimits.failures; i++ {
		headers := map[string]string{HeaderNonce: strings.Repeat("a", 64), HeaderSignature: "AAAA"}
		if response := h.authed("DELETE", "/api/assistant/requests/r1", "", headers); response.Code != http.StatusForbidden || errorCode(t, response) != "step-up-refused" {
			t.Fatalf("step-up %d: %d %s", i+1, response.Code, response.Body)
		}
	}
	if response := h.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong signatures: %d", externalLimits.failures, response.Code)
	}
	// A missing step-up is an honest client's mistake, not an attack.
	k := newFixture(t)
	k.source = outside
	k.pair()
	for range 3 * externalLimits.failures {
		if response := k.authed("DELETE", "/api/assistant/requests/r1", "", nil); errorCode(t, response) != "step-up-required" {
			t.Fatalf("missing step-up: %d %s", response.Code, response.Body)
		}
	}
}

func TestAGoodPairingClearsTheFailuresOfItsSource(t *testing.T) {
	f := newFixture(t)
	f.source = outside
	code, _, err := f.pairing.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, public := newKey(t)
	body := func(c string) string {
		b, _ := json.Marshal(map[string]string{"code": c, "deviceName": "Pixel", "stepUpPublicKey": public})
		return string(b)
	}
	for range 3 {
		f.do("POST", "/remote/v1/pair", body(strings.Repeat("0", 32)), nil)
	}
	if response := f.do("POST", "/remote/v1/pair", body(code), nil); response.Code != http.StatusCreated {
		t.Fatalf("pairing after a few typos: %d %s", response.Code, response.Body)
	}
	if _, blocked := f.handler.throttle.Blocked(sourceOf(outside)); blocked {
		t.Fatal("blocked after pairing")
	}
	if f.handler.throttle.entries[throttleKey(sourceOf(outside))].failures != 0 {
		t.Fatal("the failures were not cleared by a good pairing")
	}
}

func TestExternalSourcesAreLoggedOnceWithoutSecrets(t *testing.T) {
	capture := captureLog(t)
	f := newFixture(t)
	f.source = outside
	code, _, _ := f.pairing.Open()
	_, public := newKey(t)
	body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel 8", "stepUpPublicKey": public})
	response := f.do("POST", "/remote/v1/pair", string(body), nil)
	var paired struct{ Token string }
	_ = json.Unmarshal(response.Body.Bytes(), &paired)
	f.token = paired.Token
	for range 3 {
		if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
			t.Fatalf("external request: %d", response.Code)
		}
	}
	f.source = "203.0.113.99:1"
	f.authed("GET", "/api/assistant", "", nil)
	f.source = home
	f.authed("GET", "/api/assistant", "", nil)
	f.authed("GET", "/api/assistant", "", nil)

	out := capture.String()
	if got := strings.Count(out, "paired \"Pixel 8\""); got != 1 || !strings.Contains(out, "from the external address 203.0.113.7") {
		t.Errorf("the external pairing is logged once with its source:\n%s", out)
	}
	if got := strings.Count(out, "first request from the external address 203.0.113.7"); got != 1 {
		t.Errorf("the first request of 203.0.113.7 is logged %d times:\n%s", got, out)
	}
	if got := strings.Count(out, "first request from the external address 203.0.113.99"); got != 1 {
		t.Errorf("a new external source is logged once:\n%s", out)
	}
	if strings.Contains(out, "192.168.1.50") {
		t.Errorf("a source on the home network is not logged as external:\n%s", out)
	}
	for _, secret := range []string{paired.Token, code, "/api/assistant", "Bearer"} {
		if secret != "" && strings.Contains(out, secret) {
			t.Errorf("the log holds %q:\n%s", secret, out)
		}
	}
}
