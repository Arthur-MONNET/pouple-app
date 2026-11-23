package remote

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type memorySettings struct {
	mu       sync.Mutex
	settings config.RemoteSettings
}

func (m *memorySettings) load() (config.RemoteSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil
}

func (m *memorySettings) save(next config.RemoteSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = next
	return nil
}

func loopbackInterface(t *testing.T) Interface {
	t.Helper()
	interfaces, err := SystemInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Loopback {
			return iface
		}
	}
	t.Skip("no loopback interface")
	return Interface{}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func newTestService(t *testing.T, dir string, store secrets.Store, settings *memorySettings) *Service {
	t.Helper()
	service, err := NewService(Options{
		Dir: dir, Secrets: store, Load: settings.load, Save: settings.save,
		Hostname: func() (string, error) { return "studio.local", nil },
		Handler: HandlerOptions{Mount: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/assistant", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"requests":[]}`)
			})
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Shutdown)
	return service
}

// pinnedClient trusts only the certificate whose SHA-256 is fingerprint, as the phone does.
func pinnedClient(fingerprint string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true, // replaced by the pin below, as on the phone
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 || Fingerprint(state.PeerCertificates[0].Raw) != fingerprint {
				return errors.New("certificate does not match the pin")
			}
			return nil
		},
	}}}
}

func TestTLSHandshakeWithThePinnedCertificateReadsTheSnapshot(t *testing.T) {
	loopback := loopbackInterface(t)
	dir, store, settings := t.TempDir(), secrets.NewMemory(), &memorySettings{}
	service := newTestService(t, dir, store, settings)
	if _, err := service.OpenPairing(); !errors.Is(err, ErrDisabled) {
		t.Fatalf("pairing while off: %v", err)
	}
	enabled, port := true, freePort(t)
	view, err := service.UpdateSettings(SettingsUpdate{Enabled: &enabled, Interfaces: &[]string{loopback.Name}, Port: &port})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Listening) == 0 || view.Fingerprint == "" {
		t.Fatalf("not listening: %+v", view)
	}
	for _, address := range view.Listening {
		host, _, _ := net.SplitHostPort(address)
		if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() || !ip.IsLoopback() {
			t.Fatalf("bound outside the chosen interface: %s", address)
		}
	}
	if key, err := store.Get(tlsKeySecret); err != nil || !strings.Contains(key, "PRIVATE KEY") {
		t.Fatalf("the private key must be in the secrets store: %v", err)
	}

	offer, err := service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := strings.CutPrefix(offer.Payload, "pouple-pair:v1?")
	if !ok {
		t.Fatalf("payload: %s", offer.Payload)
	}
	query, err := url.ParseQuery(payload)
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("fp") != view.Fingerprint || query.Get("port") != strconv.Itoa(port) || query.Get("name") != "studio" || len(query.Get("code")) != 32 {
		t.Fatalf("payload fields: %v", query)
	}
	if strings.Contains(query.Get("hosts"), ".local") {
		t.Fatalf("a loopback-only listener has no network name: %v", query.Get("hosts"))
	}
	host := strings.Split(query.Get("hosts"), ",")[0]
	base := "https://" + net.JoinHostPort(host, query.Get("port"))

	client := pinnedClient(query.Get("fp"))
	_, public := newKey(t)
	body, _ := json.Marshal(map[string]string{"code": query.Get("code"), "deviceName": "Pixel 8", "stepUpPublicKey": public})
	response, err := client.Post(base+"/remote/v1/pair", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	var paired struct{ Token string }
	_ = json.NewDecoder(response.Body).Decode(&paired)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || paired.Token == "" {
		t.Fatalf("pairing over TLS: %d", response.StatusCode)
	}

	request, _ := http.NewRequest("GET", base+"/api/assistant", nil)
	request.Header.Set("Authorization", "Bearer "+paired.Token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(snapshot) != `{"requests":[]}` {
		t.Fatalf("snapshot over TLS: %d %s", response.StatusCode, snapshot)
	}
	if response.TLS == nil || response.TLS.Version != tls.VersionTLS13 {
		t.Fatal("the remote API must be served over TLS 1.3")
	}

	// Another pin is refused by the client: the phone never talks to another certificate.
	if _, err := pinnedClient(strings.Repeat("0", 64)).Get(base + "/api/assistant"); err == nil {
		t.Fatal("a wrong pin must fail the handshake")
	}

	// The same key and certificate come back after a restart: the pin of the paired phones stays valid.
	service.Shutdown()
	again := newTestService(t, dir, store, settings)
	if err := again.Apply(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := again.Settings()
	if reopened.Fingerprint != view.Fingerprint {
		t.Fatalf("the fingerprint changed across a restart: %s != %s", reopened.Fingerprint, view.Fingerprint)
	}

	// Turned off, nothing listens and pairing closes.
	disabled := false
	if view, err := again.UpdateSettings(SettingsUpdate{Enabled: &disabled}); err != nil || len(view.Listening) != 0 {
		t.Fatalf("turned off: %+v %v", view, err)
	}
	if _, err := client.Get(base + "/api/assistant"); err == nil {
		t.Fatal("the listener stayed open after remote access was turned off")
	}
}

func TestTheListenerFollowsAddressChangesButNotABusyPort(t *testing.T) {
	var mu sync.Mutex
	addresses := []string{"127.0.0.1"}
	settings := &memorySettings{settings: config.RemoteSettings{Enabled: true, Interfaces: []string{"lo0"}, Port: freePort(t)}}
	service, err := NewService(Options{
		Dir: t.TempDir(), Secrets: secrets.NewMemory(), Load: settings.load, Save: settings.save,
		Interfaces: func() ([]Interface, error) {
			mu.Lock()
			defer mu.Unlock()
			return []Interface{{Name: "lo0", Addresses: slices.Clone(addresses), Loopback: true}, {Name: "en9", Addresses: []string{"127.0.0.3"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Shutdown)
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	if view, _ := service.Settings(); len(view.Listening) != 1 || !strings.HasPrefix(view.Listening[0], "127.0.0.1:") {
		t.Fatalf("only the chosen interface is bound: %+v", view.Listening)
	}
	if service.addressesChanged() {
		t.Fatal("nothing changed")
	}
	mu.Lock()
	addresses = append(addresses, "192.0.2.1") // an address this machine does not have: binding it fails
	mu.Unlock()
	if !service.addressesChanged() {
		t.Fatal("a new address of the chosen interface must re-apply")
	}
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	if view, _ := service.Settings(); view.Problem == "" {
		t.Fatalf("an address that cannot be bound is reported: %+v", view)
	}
	if service.addressesChanged() {
		t.Fatal("an address that cannot be bound must not restart the listener every check")
	}
}

func TestRemoteSettingsAreChecked(t *testing.T) {
	service := newTestService(t, t.TempDir(), secrets.NewMemory(), &memorySettings{})
	bad := []SettingsUpdate{
		{Interfaces: &[]string{"en0; rm -rf"}},
		{Port: new(80)},
		{Port: new(70000)},
	}
	for _, update := range bad {
		if _, err := service.UpdateSettings(update); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("%+v: %v", update, err)
		}
	}
	view, err := service.Settings()
	if err != nil || view.Enabled || view.Port != DefaultPort || len(view.Listening) != 0 {
		t.Fatalf("remote access is off by default on the default port: %+v %v", view, err)
	}
}
