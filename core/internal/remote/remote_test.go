package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a settable time for the tests.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeRoutes stands for the assistant routes: each allowlisted pattern, plus two the allowlist leaves out.
type fakeRoutes struct {
	mu     sync.Mutex
	calls  []string
	stream chan struct{}
}

func (f *fakeRoutes) mount(mux *http.ServeMux) {
	patterns := []string{"GET /api/assistant/style", "GET /api/settings", "GET /api/remote/devices"}
	for pattern := range AllowedRoutes() {
		patterns = append(patterns, pattern)
	}
	for _, pattern := range patterns {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.calls = append(f.calls, pattern+" "+string(body))
			f.mu.Unlock()
			if pattern == "GET /api/assistant/stream" {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				f.stream <- struct{}{}
				<-r.Context().Done()
				f.stream <- struct{}{}
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})
	}
}

func (f *fakeRoutes) served() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type fixture struct {
	t        *testing.T
	clock    *clock
	handler  *Handler
	routes   *fakeRoutes
	devices  *Devices
	pairing  *Pairing
	nonces   *Nonces
	granted  map[string]bool
	autoAll  bool
	deviceID string
	token    string
	key      *ecdsa.PrivateKey
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, clock: &clock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}, routes: &fakeRoutes{stream: make(chan struct{}, 2)}, granted: map[string]bool{}}
	devices, err := OpenDevices(filepath.Join(t.TempDir(), "devices.json"), f.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	f.devices, f.pairing, f.nonces = devices, NewPairing(f.clock.Now), NewNonces(f.clock.Now)
	f.handler = NewHandler(HandlerOptions{
		Devices: devices, Pairing: f.pairing, Nonces: f.nonces, Mount: f.routes.mount,
		Granted:        func(id string) bool { return f.granted[id] },
		AutoApproveAll: func() bool { return f.autoAll },
	})
	return f
}

func newKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key, base64.StdEncoding.EncodeToString(der)
}

func (f *fixture) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "https://192.168.1.20:47821"+path, strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

func (f *fixture) authed(method, path, body string, extra map[string]string) *httptest.ResponseRecorder {
	headers := map[string]string{"Authorization": "Bearer " + f.token}
	for name, value := range extra {
		headers[name] = value
	}
	return f.do(method, path, body, headers)
}

// pair opens a window and pairs a device with a fresh key, through the handler.
func (f *fixture) pair() {
	f.t.Helper()
	code, _, err := f.pairing.Open()
	if err != nil {
		f.t.Fatal(err)
	}
	key, public := newKey(f.t)
	body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel 8", "stepUpPublicKey": public})
	response := f.do("POST", "/remote/v1/pair", string(body), nil)
	if response.Code != http.StatusCreated {
		f.t.Fatalf("pairing: %d %s", response.Code, response.Body)
	}
	var paired struct {
		DeviceID string `json:"deviceId"`
		Token    string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &paired); err != nil || paired.DeviceID == "" || len(paired.Token) != 64 {
		f.t.Fatalf("pairing answer: %s", response.Body)
	}
	f.deviceID, f.token, f.key = paired.DeviceID, paired.Token, key
}

func (f *fixture) nonce() string {
	f.t.Helper()
	response := f.authed("POST", "/remote/v1/challenge", "", nil)
	var challenge struct {
		Nonce string `json:"nonce"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &challenge) != nil || len(challenge.Nonce) != 64 {
		f.t.Fatalf("challenge: %d %s", response.Code, response.Body)
	}
	return challenge.Nonce
}

// sign builds the step-up headers the phone sends, for the canonical form written by hand as docs/REMOTE-API.md says.
func (f *fixture) sign(key *ecdsa.PrivateKey, nonce, route, requestID, planID, proposalID, actions, body string) map[string]string {
	f.t.Helper()
	bodySum := sha256.Sum256([]byte(body))
	canonical := `{"v":1,"nonce":"` + nonce + `","deviceId":"` + f.deviceID + `","route":"` + route + `","requestId":"` + requestID +
		`","planId":"` + planID + `","proposalId":"` + proposalID + `","actions":` + actions + `,"bodySha256":"` + hex.EncodeToString(bodySum[:]) + `"}`
	digest := sha256.Sum256([]byte(canonical))
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return map[string]string{HeaderNonce: nonce, HeaderSignature: base64.StdEncoding.EncodeToString(signature)}
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body apiError
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return body.Error
}

func TestPairingOutsideItsWindowOrReusedIsRefused(t *testing.T) {
	f := newFixture(t)
	_, public := newKey(t)
	pairBody := func(code string) string {
		body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel", "stepUpPublicKey": public})
		return string(body)
	}
	if response := f.do("POST", "/remote/v1/pair", pairBody(strings.Repeat("0", 32)), nil); response.Code != http.StatusForbidden || errorCode(t, response) != "pairing-closed" {
		t.Fatalf("pairing with no window: %d %s", response.Code, response.Body)
	}

	code, _, _ := f.pairing.Open()
	f.clock.advance(PairingWindow + time.Second)
	if response := f.do("POST", "/remote/v1/pair", pairBody(code), nil); response.Code != http.StatusForbidden || errorCode(t, response) != "pairing-closed" {
		t.Fatalf("pairing after the window: %d %s", response.Code, response.Body)
	}

	code, _, _ = f.pairing.Open()
	if response := f.do("POST", "/remote/v1/pair", pairBody(code), nil); response.Code != http.StatusCreated {
		t.Fatalf("pairing in the window: %d %s", response.Code, response.Body)
	}
	if response := f.do("POST", "/remote/v1/pair", pairBody(code), nil); response.Code != http.StatusForbidden {
		t.Fatalf("a code paired twice: %d %s", response.Code, response.Body)
	}

	code, _, _ = f.pairing.Open()
	for range maxPairingAttempts {
		if response := f.do("POST", "/remote/v1/pair", pairBody(strings.Repeat("f", 32)), nil); response.Code != http.StatusForbidden {
			t.Fatalf("a wrong code: %d", response.Code)
		}
	}
	if response := f.do("POST", "/remote/v1/pair", pairBody(code), nil); response.Code != http.StatusForbidden || errorCode(t, response) != "pairing-closed" {
		t.Fatalf("the window must close after %d wrong codes: %d %s", maxPairingAttempts, response.Code, response.Body)
	}

	code, _, _ = f.pairing.Open()
	f.pairing.Cancel()
	if response := f.do("POST", "/remote/v1/pair", pairBody(code), nil); response.Code != http.StatusForbidden {
		t.Fatalf("a cancelled window: %d", response.Code)
	}
}

func TestPairingRefusesAKeyThatIsNotP256(t *testing.T) {
	f := newFixture(t)
	code, _, _ := f.pairing.Open()
	for _, key := range []string{"", "bm90IGEga2V5", strings.Repeat("A", 600)} {
		body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel", "stepUpPublicKey": key})
		if response := f.do("POST", "/remote/v1/pair", string(body), nil); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("key %q: %d %s", key, response.Code, response.Body)
		}
	}
	body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "\x1b[31mPixel", "stepUpPublicKey": "x"})
	if response := f.do("POST", "/remote/v1/pair", string(body), nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a name with control characters: %d", response.Code)
	}
	// A refused key never spends the code.
	f.pair()
}

func TestBadRevokedOrExpiredTokenIs401(t *testing.T) {
	f := newFixture(t)
	f.pair()
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
		t.Fatalf("a paired device reads the snapshot: %d %s", response.Code, response.Body)
	}
	for _, header := range []string{"", "Bearer ", "Bearer " + strings.Repeat("a", 64), "Basic " + f.token, f.token} {
		response := f.do("GET", "/api/assistant", "", map[string]string{"Authorization": header})
		if response.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: %d", header, response.Code)
		}
	}
	if response := f.do("POST", "/remote/v1/challenge", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("a challenge without a token: %d", response.Code)
	}

	f.clock.advance(TokenTTL + time.Minute)
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("a token unused for %v must expire: %d", TokenTTL, response.Code)
	}

	f.pair()
	if err := f.devices.Revoke(f.deviceID); err != nil {
		t.Fatal(err)
	}
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked device: %d", response.Code)
	}
	// Revoked stays revoked across a restart.
	reopened, err := OpenDevices(f.devices.path, f.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Authenticate(f.token); err == nil {
		t.Fatal("a revoked device came back after a restart")
	}
}

func TestDevicesFileIsPrivateAndKeepsNoToken(t *testing.T) {
	f := newFixture(t)
	f.pair()
	info, err := os.Stat(f.devices.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("devices.json: %v %v", info, err)
	}
	data, _ := os.ReadFile(f.devices.path)
	if strings.Contains(string(data), f.token) {
		t.Fatal("the token is stored in clear")
	}
	for _, device := range f.devices.List() {
		encoded, _ := json.Marshal(device)
		if strings.Contains(string(encoded), "tokenHash") || strings.Contains(string(encoded), "stepUp") {
			t.Fatalf("the settings see secrets: %s", encoded)
		}
	}
}

func TestAnUnreadableDevicesFileIsSetAsideAndPairsNoOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := OpenDevices(path, nil)
	if err != nil {
		t.Fatalf("an unreadable file must not stop the app: %v", err)
	}
	if len(devices.List()) != 0 {
		t.Fatal("no device may be read from an unreadable file")
	}
	if _, err := os.Stat(path + ".unreadable"); err != nil {
		t.Fatalf("the unreadable file is kept aside: %v", err)
	}
}

func TestRouteOutsideTheAllowlistIs403(t *testing.T) {
	f := newFixture(t)
	f.pair()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/assistant/style"},
		{"GET", "/api/settings"},
		{"GET", "/api/remote/devices"},
		{"GET", "/api/connectors"},
		{"GET", "/api/unknown"},
		{"PUT", "/api/assistant"},
		{"GET", "/"},
		{"GET", "/index.html"},
		{"GET", "/api//assistant"},
		{"GET", "/api/assistant/../settings"},
	} {
		if response := f.authed(c.method, c.path, "", nil); response.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d, want 403", c.method, c.path, response.Code)
		}
	}
	for _, call := range f.routes.served() {
		t.Errorf("a refused route reached the assistant: %s", call)
	}
}

func TestBrowserRequestsAre403(t *testing.T) {
	f := newFixture(t)
	f.pair()
	for _, header := range []map[string]string{{"Origin": "https://evil.example"}, {"Origin": "null"}, {"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "none"}} {
		if response := f.authed("GET", "/api/assistant", "", header); response.Code != http.StatusForbidden || errorCode(t, response) != "forbidden-origin" {
			t.Errorf("%v: %d", header, response.Code)
		}
	}
	if response := f.do("POST", "/remote/v1/pair", "{}", map[string]string{"Origin": "https://evil.example"}); response.Code != http.StatusForbidden {
		t.Errorf("pairing from a page: %d", response.Code)
	}
}

const planRoute = "POST /api/assistant/requests/{id}/plan"

func TestApprovalNeedsAValidFreshSignature(t *testing.T) {
	f := newFixture(t)
	f.pair()
	path := "/api/assistant/requests/r1/plan"
	body := `{"decision":"approve","planId":"p1","actions":[0,2]}`

	if response := f.authed("POST", path, body, nil); response.Code != http.StatusForbidden || errorCode(t, response) != "step-up-required" {
		t.Fatalf("approval without step-up: %d %s", response.Code, response.Body)
	}
	// Signed by another key.
	other, _ := newKey(t)
	if response := f.authed("POST", path, body, f.sign(other, f.nonce(), planRoute, "r1", "p1", "", "[0,2]", body)); response.Code != http.StatusForbidden || errorCode(t, response) != "step-up-refused" {
		t.Fatalf("approval signed by another key: %d %s", response.Code, response.Body)
	}
	// Signed for other actions than the ones sent.
	if response := f.authed("POST", path, body, f.sign(f.key, f.nonce(), planRoute, "r1", "p1", "", "[0]", body)); response.Code != http.StatusForbidden {
		t.Fatalf("approval signed for other actions: %d", response.Code)
	}
	// Signed for another conversation.
	if response := f.authed("POST", path, body, f.sign(f.key, f.nonce(), planRoute, "r2", "p1", "", "[0,2]", body)); response.Code != http.StatusForbidden {
		t.Fatalf("approval signed for another conversation: %d", response.Code)
	}
	// A nonce never issued.
	if response := f.authed("POST", path, body, f.sign(f.key, strings.Repeat("ab", 32), planRoute, "r1", "p1", "", "[0,2]", body)); response.Code != http.StatusForbidden {
		t.Fatalf("approval with an unknown nonce: %d", response.Code)
	}
	if calls := f.routes.served(); len(calls) != 0 {
		t.Fatalf("a refused approval reached the assistant: %v", calls)
	}

	nonce := f.nonce()
	headers := f.sign(f.key, nonce, planRoute, "r1", "p1", "", "[0,2]", body)
	if response := f.authed("POST", path, body, headers); response.Code != http.StatusAccepted {
		t.Fatalf("a valid approval: %d %s", response.Code, response.Body)
	}
	if calls := f.routes.served(); len(calls) != 1 || calls[0] != planRoute+" "+body {
		t.Fatalf("the approval must reach the route with its body: %v", calls)
	}
	if response := f.authed("POST", path, body, headers); response.Code != http.StatusForbidden || errorCode(t, response) != "step-up-refused" {
		t.Fatalf("a reused nonce: %d %s", response.Code, response.Body)
	}

	expiring := f.nonce()
	f.clock.advance(NonceTTL + time.Second)
	if response := f.authed("POST", path, body, f.sign(f.key, expiring, planRoute, "r1", "p1", "", "[0,2]", body)); response.Code != http.StatusForbidden {
		t.Fatalf("an expired nonce: %d", response.Code)
	}
}

func TestANonceIsSpentByAWrongSignature(t *testing.T) {
	f := newFixture(t)
	f.pair()
	path, body := "/api/assistant/requests/r1/proposal", `{"decision":"approve","proposalId":"x1"}`
	route := "POST /api/assistant/requests/{id}/proposal"
	nonce := f.nonce()
	bad := f.sign(f.key, nonce, route, "r1", "", "other", "null", body)
	if response := f.authed("POST", path, body, bad); response.Code != http.StatusForbidden {
		t.Fatalf("a wrong signature: %d", response.Code)
	}
	if response := f.authed("POST", path, body, f.sign(f.key, nonce, route, "r1", "", "x1", "null", body)); response.Code != http.StatusForbidden {
		t.Fatalf("a nonce must not survive a failed attempt: %d", response.Code)
	}
	if response := f.authed("POST", path, body, f.sign(f.key, f.nonce(), route, "r1", "", "x1", "null", body)); response.Code != http.StatusAccepted {
		t.Fatalf("a valid proposal approval: %d %s", response.Code, response.Body)
	}
}

func TestANonceBelongsToTheDeviceThatAskedForIt(t *testing.T) {
	f := newFixture(t)
	f.pair()
	stolen := f.nonce()
	f.pair() // a second device
	body := `{"decision":"approve","planId":"p1"}`
	if response := f.authed("POST", "/api/assistant/requests/r1/plan", body, f.sign(f.key, stolen, planRoute, "r1", "p1", "", "null", body)); response.Code != http.StatusForbidden {
		t.Fatalf("a nonce of another device: %d", response.Code)
	}
}

func TestDecliningAndReadingNeedNoStepUpButDeletingDoes(t *testing.T) {
	f := newFixture(t)
	f.pair()
	if response := f.authed("POST", "/api/assistant/requests/r1/plan", `{"decision":"decline","planId":"p1"}`, nil); response.Code != http.StatusAccepted {
		t.Fatalf("a decline: %d %s", response.Code, response.Body)
	}
	if response := f.authed("POST", "/api/assistant/requests/r1/proposal", `{"decision":"maybe"}`, nil); response.Code != http.StatusForbidden {
		t.Fatalf("anything but a decline needs the fingerprint: %d", response.Code)
	}
	if response := f.authed("POST", "/api/assistant/requests/r1/proposal", `not json`, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an unreadable decision: %d", response.Code)
	}
	if response := f.authed("DELETE", "/api/assistant/requests/r1", "", nil); response.Code != http.StatusForbidden {
		t.Fatalf("a delete without step-up: %d", response.Code)
	}
	route := "DELETE /api/assistant/requests/{id}"
	if response := f.authed("DELETE", "/api/assistant/requests/r1", "", f.sign(f.key, f.nonce(), route, "r1", "", "", "null", "")); response.Code != http.StatusAccepted {
		t.Fatalf("a signed delete: %d %s", response.Code, response.Body)
	}
	for _, path := range []string{"/api/assistant", "/api/assistant/requests/r1/browser", "/api/assistant/requests/r1/attachments/a1"} {
		if response := f.authed("GET", path, "", nil); response.Code != http.StatusAccepted {
			t.Errorf("GET %s: %d", path, response.Code)
		}
	}
}

func TestMessageToAGrantedConversationNeedsAStepUp(t *testing.T) {
	f := newFixture(t)
	f.pair()
	body := `{"text":"go on"}`
	if response := f.authed("POST", "/api/assistant/requests/r1/messages", body, nil); response.Code != http.StatusAccepted {
		t.Fatalf("a message to a conversation without approvals: %d", response.Code)
	}
	f.granted["r1"] = true
	for _, path := range []string{"/api/assistant/requests/r1/messages", "/api/assistant/requests/r1/answers"} {
		if response := f.authed("POST", path, body, nil); response.Code != http.StatusForbidden || errorCode(t, response) != "step-up-required" {
			t.Fatalf("%s in a granted conversation without step-up: %d %s", path, response.Code, response.Body)
		}
	}
	route := "POST /api/assistant/requests/{id}/messages"
	if response := f.authed("POST", "/api/assistant/requests/r1/messages", body, f.sign(f.key, f.nonce(), route, "r1", "", "", "null", body)); response.Code != http.StatusAccepted {
		t.Fatalf("a signed message: %d %s", response.Code, response.Body)
	}
	// The signature covers the text: another text is refused.
	if response := f.authed("POST", "/api/assistant/requests/r1/messages", `{"text":"delete it all"}`, f.sign(f.key, f.nonce(), route, "r1", "", "", "null", body)); response.Code != http.StatusForbidden {
		t.Fatalf("a message whose text was not signed: %d", response.Code)
	}
}

func TestEveryChangeApplyingUnreadMakesTextNeedAStepUp(t *testing.T) {
	f := newFixture(t)
	f.pair()
	f.autoAll = true
	for _, path := range []string{"/api/assistant/requests", "/api/assistant/requests/r1/messages"} {
		if response := f.authed("POST", path, `{"text":"hi"}`, nil); response.Code != http.StatusForbidden {
			t.Errorf("%s while every change applies unread: %d", path, response.Code)
		}
	}
	f.autoAll = false
	if response := f.authed("POST", "/api/assistant/requests", `{"text":"hi"}`, nil); response.Code != http.StatusAccepted {
		t.Fatalf("a new conversation: %d", response.Code)
	}
}

func TestRevokingADeviceClosesItsStreams(t *testing.T) {
	f := newFixture(t)
	f.pair()
	server := httptest.NewServer(f.handler)
	defer server.Close()
	request, _ := http.NewRequest("GET", server.URL+"/api/assistant/stream", nil)
	request.Header.Set("Authorization", "Bearer "+f.token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	select {
	case <-f.routes.stream:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not open")
	}
	if err := f.devices.Revoke(f.deviceID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.routes.stream:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream stayed open after the device was revoked")
	}
}

func TestCanonicalJSONIsTheDocumentedOne(t *testing.T) {
	actions := []int{0, 2}
	got, err := StepUp{Nonce: "n1", DeviceID: "d1", Route: planRoute, RequestID: "r1", PlanID: "p1", Actions: &actions, BodySHA256: "ab"}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"nonce":"n1","deviceId":"d1","route":"POST /api/assistant/requests/{id}/plan","requestId":"r1","planId":"p1","proposalId":"","actions":[0,2],"bodySha256":"ab"}`
	if string(got) != want {
		t.Fatalf("canonical form:\n got %s\nwant %s", got, want)
	}
	if _, err := (StepUp{Nonce: "n1", DeviceID: "d1", Route: planRoute, RequestID: `r1","planId":"x`}).Canonical(); err == nil {
		t.Fatal("a value that needs escaping must be refused")
	}
}

func TestPathValueReadsTheMatchedWildcard(t *testing.T) {
	if got := pathValue(planRoute, "/api/assistant/requests/abc/plan", "id"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := pathValue(planRoute, "/api/assistant/requests/a%2Fb/plan", "id"); got != "a/b" {
		t.Fatalf("an escaped id: %q", got)
	}
}
