package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/alerts"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/remote"
)

// remoteFake is the engine as the phone's API sees it: the conversations, starting one, and the alerts.
type remoteFake struct{ *fakeAssistant }

func (f remoteFake) CreateAlert(title, note, at string) (alerts.Alert, error) {
	return alerts.Alert{}, f.record("create-alert", title)
}
func (f remoteFake) UpdateAlert(id string, _ engine.AlertEdit) (alerts.Alert, error) {
	return alerts.Alert{}, f.record("update-alert", id)
}
func (f remoteFake) DeleteAlert(id string) error { return f.record("delete-alert", id) }

// samplePath turns a route pattern into a request path.
func samplePath(pattern string) (string, string) {
	method, path, _ := strings.Cut(pattern, " ")
	path = strings.ReplaceAll(path, "{id}", "r1")
	path = strings.ReplaceAll(path, "{attachmentId}", "a1")
	return method, path
}

// The allowlist names routes as the mux registers them: a renamed route would otherwise be refused (or, worse, a new
// route reachable under an old name) without any test noticing.
func TestEveryAllowlistedRouteIsMountedForThePhone(t *testing.T) {
	mux := http.NewServeMux()
	MountRemote(remoteFake{&fakeAssistant{}}, engine.NewAttachments(t.TempDir()))(mux)
	for pattern := range remote.AllowedRoutes() {
		method, path := samplePath(pattern)
		if _, matched := mux.Handler(httptest.NewRequest(method, path, nil)); matched != pattern {
			t.Errorf("%s is allowlisted but the mounted route is %q", pattern, matched)
		}
	}
	for _, never := range []string{"GET /api/assistant/style", "DELETE /api/assistant/style", "POST /api/assistant/requests/{id}/memories"} {
		if _, allowed := remote.AllowedRoutes()[never]; allowed {
			t.Errorf("%s must never be reachable from a phone", never)
		}
	}
}

type phone struct {
	t       *testing.T
	handler http.Handler
	id      string
	token   string
	key     *ecdsa.PrivateKey
}

func pairedPhone(t *testing.T, assistant RemoteAssistant) *phone {
	t.Helper()
	devices, err := remote.OpenDevices(filepath.Join(t.TempDir(), "devices.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	pairing := remote.NewPairing(nil)
	handler := remote.NewHandler(remote.HandlerOptions{
		Devices: devices, Pairing: pairing, Nonces: remote.NewNonces(nil),
		Mount: MountRemote(assistant, engine.NewAttachments(t.TempDir())), Language: func() string { return "en" },
	})
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	code, _, _ := pairing.Open()
	body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel", "stepUpPublicKey": base64.StdEncoding.EncodeToString(der)})
	p := &phone{t: t, handler: handler, key: key}
	response := p.call("POST", "/remote/v1/pair", string(body), nil)
	var paired struct{ DeviceID, Token string }
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &paired) != nil {
		t.Fatalf("pairing: %d %s", response.Code, response.Body)
	}
	p.id, p.token = paired.DeviceID, paired.Token
	return p
}

func (p *phone) call(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "https://192.168.1.20:47821"+path, strings.NewReader(body))
	if p.token != "" {
		request.Header.Set("Authorization", "Bearer "+p.token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	p.handler.ServeHTTP(response, request)
	return response
}

// signed makes a step-up for a proposal decision, as docs/REMOTE-API.md describes it.
func (p *phone) signed(route, requestID, proposalID, body string) map[string]string {
	p.t.Helper()
	var challenge struct{ Nonce string }
	if response := p.call("POST", "/remote/v1/challenge", "", nil); json.Unmarshal(response.Body.Bytes(), &challenge) != nil {
		p.t.Fatalf("challenge: %s", response.Body)
	}
	sum := sha256.Sum256([]byte(body))
	canonical := `{"v":1,"nonce":"` + challenge.Nonce + `","deviceId":"` + p.id + `","route":"` + route + `","requestId":"` + requestID +
		`","planId":"","proposalId":"` + proposalID + `","actions":null,"bodySha256":"` + hex.EncodeToString(sum[:]) + `"}`
	digest := sha256.Sum256([]byte(canonical))
	signature, _ := ecdsa.SignASN1(rand.Reader, p.key, digest[:])
	return map[string]string{remote.HeaderNonce: challenge.Nonce, remote.HeaderSignature: base64.StdEncoding.EncodeToString(signature)}
}

func TestATypedYesFromThePhoneNeverApproves(t *testing.T) {
	fake := &fakeAssistant{}
	p := pairedPhone(t, remoteFake{fake})
	if response := p.call("POST", "/api/assistant/requests/r1/messages", `{"text":"yes"}`, nil); response.Code != http.StatusAccepted {
		t.Fatalf("message: %d %s", response.Code, response.Body)
	}
	// The local API's message: the engine refuses to read a "yes" as a decision (engine test
	// TestTwoApprovalsStartOneWriteAndAStaleOneIsRefused), and the phone's message never reaches the approving one.
	if len(fake.calls) != 1 || fake.calls[0] != "local message r1 yes 0" {
		t.Fatalf("the phone's message must use the local API's semantics: %v", fake.calls)
	}
	// An approval, even signed, must name the proposal that was read.
	route, body := "POST /api/assistant/requests/{id}/proposal", `{"decision":"approve"}`
	if response := p.call("POST", "/api/assistant/requests/r1/proposal", body, p.signed(route, "r1", "", body)); response.Code != http.StatusBadRequest {
		t.Fatalf("an approval without proposalId: %d %s", response.Code, response.Body)
	}
	body = `{"decision":"approve","proposalId":"x1"}`
	if response := p.call("POST", "/api/assistant/requests/r1/proposal", body, p.signed(route, "r1", "x1", body)); response.Code != http.StatusAccepted {
		t.Fatalf("a signed approval: %d %s", response.Code, response.Body)
	}
	if last := fake.calls[len(fake.calls)-1]; last != "decide r1 true x1" {
		t.Fatalf("approval call: %v", fake.calls)
	}
}

func TestThePhoneNeverReachesTheAdminOrTheWholeAPI(t *testing.T) {
	fake := &fakeAssistant{}
	p := pairedPhone(t, remoteFake{fake})
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/remote/devices"},
		{"POST", "/api/remote/pairing"},
		{"PUT", "/api/remote/settings"},
		{"GET", "/api/assistant/style"},
		{"DELETE", "/api/assistant/style"},
		{"POST", "/api/assistant/requests/r1/memories"},
		{"GET", "/api/settings"},
		{"GET", "/api/connectors"},
		{"GET", "/api/terminal"},
	} {
		if response := p.call(c.method, c.path, `{}`, nil); response.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d, want 403", c.method, c.path, response.Code)
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("a refused route reached the engine: %v", fake.calls)
	}
	if response := p.call("POST", "/api/alerts", `{"title":"call"}`, nil); response.Code != http.StatusCreated {
		t.Fatalf("an alert from the phone: %d %s", response.Code, response.Body)
	}
}

type fakeRemoteAdmin struct {
	enabled bool
	revoked []string
	update  remote.SettingsUpdate
}

func (f *fakeRemoteAdmin) OpenPairing() (remote.PairingOffer, error) {
	if !f.enabled {
		return remote.PairingOffer{}, remote.ErrDisabled
	}
	return remote.PairingOffer{Payload: "pouple-pair:v1?hosts=192.168.1.20", ExpiresAt: time.Now().Add(remote.PairingWindow)}, nil
}
func (f *fakeRemoteAdmin) AddressOffer() (remote.AddressOffer, error) {
	if !f.enabled {
		return remote.AddressOffer{}, remote.ErrDisabled
	}
	if f.update.ExternalHost == nil {
		return remote.AddressOffer{}, remote.ErrNoExternalAddress
	}
	return remote.AddressOffer{Payload: "pouple-address:v1?ext=home.example.net%3A47821&fp=" + strings.Repeat("a", 64)}, nil
}
func (f *fakeRemoteAdmin) CancelPairing() {}
func (f *fakeRemoteAdmin) Devices() []remote.DeviceInfo {
	return []remote.DeviceInfo{{ID: "d1", Name: "Pixel"}}
}
func (f *fakeRemoteAdmin) RevokeDevice(id string) error {
	if id != "d1" {
		return remote.ErrUnknownDevice
	}
	f.revoked = append(f.revoked, id)
	return nil
}
func (f *fakeRemoteAdmin) Settings() (remote.SettingsView, error) {
	view := remote.SettingsView{Enabled: f.enabled, Port: remote.DefaultPort, TunnelStatus: remote.TunnelStatusOff}
	if f.update.TunnelEnabled != nil {
		view.TunnelEnabled = *f.update.TunnelEnabled
	}
	if f.update.TunnelDomain != nil {
		view.TunnelDomain = *f.update.TunnelDomain
	}
	if f.update.TunnelAuthtoken != nil {
		view.TunnelAuthtokenSet = *f.update.TunnelAuthtoken != ""
	}
	return view, nil
}
func (f *fakeRemoteAdmin) UpdateSettings(update remote.SettingsUpdate) (remote.SettingsView, error) {
	if update.Port != nil && *update.Port < 1024 {
		return remote.SettingsView{}, remote.ErrInvalidSettings
	}
	f.update = update
	if update.Enabled != nil {
		f.enabled = *update.Enabled
	}
	return f.Settings()
}

func TestRemoteAdminIsOnTheLoopbackAPIBehindItsToken(t *testing.T) {
	admin := &fakeRemoteAdmin{}
	registryServer, _, _ := newServer(t, false)
	server := New(Options{Registry: registryServer.opts.Registry, Token: "secret-token", UI: registryServer.opts.UI, Mount: MountRemoteAdmin(admin)}, port)
	if response := call(server, "GET", "/api/remote/devices", "", map[string]string{"Authorization": ""}); response.Code != http.StatusUnauthorized {
		t.Fatalf("admin without the token: %d", response.Code)
	}
	if response := call(server, "POST", "/api/remote/pairing", "", nil); response.Code != http.StatusConflict {
		t.Fatalf("pairing while off: %d %s", response.Code, response.Body)
	}
	if response := call(server, "PUT", "/api/remote/settings", `{"port":80}`, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a bad port: %d", response.Code)
	}
	if response := call(server, "PUT", "/api/remote/settings", `{"enabled":true,"interfaces":["en0"]}`, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatalf("turning it on: %d %s", response.Code, response.Body)
	}
	if response := call(server, "POST", "/api/remote/pairing", "", nil); response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "pouple-pair:v1?") {
		t.Fatalf("pairing: %d %s", response.Code, response.Body)
	}
	if response := call(server, "GET", "/api/remote/address", "", map[string]string{"Authorization": ""}); response.Code != http.StatusUnauthorized {
		t.Fatalf("the address payload without the token: %d", response.Code)
	}
	if response := call(server, "GET", "/api/remote/address", "", nil); response.Code != http.StatusConflict {
		t.Fatalf("no external address yet: %d %s", response.Code, response.Body)
	}
	if response := call(server, "PUT", "/api/remote/settings", `{"externalEnabled":true,"externalHost":"home.example.net"}`, nil); response.Code != http.StatusOK {
		t.Fatalf("external settings: %d %s", response.Code, response.Body)
	}
	if response := call(server, "GET", "/api/remote/address", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"payload":"pouple-address:v1?ext=`) {
		t.Fatalf("the address payload: %d %s", response.Code, response.Body)
	}
	if response := call(server, "GET", "/api/remote/devices", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"d1"`) {
		t.Fatalf("devices: %d %s", response.Code, response.Body)
	}
	if response := call(server, "DELETE", "/api/remote/devices/d1", "", nil); response.Code != http.StatusNoContent || len(admin.revoked) != 1 {
		t.Fatalf("revoke: %d", response.Code)
	}
	if response := call(server, "DELETE", "/api/remote/devices/nope", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("revoke an unknown device: %d", response.Code)
	}
	if response := call(server, "DELETE", "/api/remote/pairing", "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("cancel pairing: %d", response.Code)
	}
}

func TestRemoteAdminCarriesTheTunnelSettingsAndNeverTheToken(t *testing.T) {
	admin := &fakeRemoteAdmin{}
	registryServer, _, _ := newServer(t, false)
	server := New(Options{Registry: registryServer.opts.Registry, Token: "secret-token", UI: registryServer.opts.UI, Mount: MountRemoteAdmin(admin)}, port)
	const secret = "ngrok_secret_0123456789abcdef"
	body := `{"tunnelEnabled":true,"tunnelDomain":"my-mac.ngrok-free.app","tunnelAuthtoken":"` + secret + `"}`
	response := call(server, "PUT", "/api/remote/settings", body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
	if admin.update.TunnelEnabled == nil || !*admin.update.TunnelEnabled || *admin.update.TunnelDomain != "my-mac.ngrok-free.app" || *admin.update.TunnelAuthtoken != secret {
		t.Fatalf("the update did not reach the service: %+v", admin.update)
	}
	for _, field := range []string{`"tunnelEnabled":true`, `"tunnelDomain":"my-mac.ngrok-free.app"`, `"tunnelAuthtokenSet":true`, `"tunnelStatus":"off"`, `"tunnelProblem":""`} {
		if !strings.Contains(response.Body.String(), field) {
			t.Errorf("%s missing from %s", field, response.Body)
		}
	}
	for _, answer := range []*httptest.ResponseRecorder{response, call(server, "GET", "/api/remote/settings", "", nil)} {
		if strings.Contains(answer.Body.String(), secret) || strings.Contains(strings.ToLower(answer.Body.String()), `"tunnelauthtoken"`) {
			t.Fatalf("the authtoken must never be returned: %s", answer.Body)
		}
	}
	// A body that is not JSON does not echo what was sent (it may hold the token).
	bad := call(server, "PUT", "/api/remote/settings", `{"tunnelAuthtoken":"`+secret+`"`, nil)
	if bad.Code != http.StatusBadRequest || strings.Contains(bad.Body.String(), secret) {
		t.Fatalf("%d %s", bad.Code, bad.Body)
	}
}
