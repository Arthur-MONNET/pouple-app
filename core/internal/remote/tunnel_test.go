package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

const (
	testTunnelDomain = "pouple-test.ngrok-free.app"
	testTunnelToken  = "ngrok_test_0123456789abcdef"
	// testSource is a public address (TEST-NET-3) standing for the phone behind ngrok.
	testSource = "203.0.113.9"
)

// fakeTunnel stands for ngrok: a plain TCP listener on the loopback is the "public endpoint", and what the service
// serves on it is exactly what it serves behind the real one. No network, no ngrok.
type fakeTunnel struct {
	mu       sync.Mutex
	calls    []tunnelCall
	active   int
	overlap  bool
	err      error
	block    chan struct{}
	listener *trackedListener
	accepted chan<- struct{}
	report   TunnelReport
	closed   int
}

type tunnelCall struct{ Domain, Token string }

// trackedListener is the endpoint of the fake: like ngrok's, the connections it accepts carry the address of the client
// they stand for. A test client writes "@<address>\n" first (the stand-in for ngrok's proxy header); a connection that
// starts with anything else has the address of the loopback connection.
type trackedListener struct {
	net.Listener
	f     *fakeTunnel
	once  sync.Once
	ready chan net.Conn
	done  chan struct{}
}

func newTrackedListener(inner net.Listener, f *fakeTunnel) *trackedListener {
	l := &trackedListener{Listener: inner, f: f, ready: make(chan net.Conn), done: make(chan struct{})}
	go func() {
		for {
			conn, err := inner.Accept()
			if err != nil {
				l.close()
				return
			}
			go l.admit(conn)
		}
	}()
	return l
}

func (l *trackedListener) admit(conn net.Conn) {
	reader := bufio.NewReader(conn)
	remote := conn.RemoteAddr()
	first, err := reader.Peek(1)
	if err != nil {
		_ = conn.Close()
		return
	}
	if first[0] == '@' {
		line, _ := reader.ReadString('\n')
		if ip, err := netip.ParseAddr(strings.TrimSpace(line[1:])); err == nil {
			remote = net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip, 40000))
		}
	}
	select {
	case l.ready <- &proxiedConn{Conn: conn, reader: reader, remote: remote}:
	case <-l.done:
		_ = conn.Close()
	}
}

func (l *trackedListener) close() { l.once.Do(func() { close(l.done) }) }

func (l *trackedListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ready:
		select {
		case l.f.accepted <- struct{}{}:
		default:
		}
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *trackedListener) Close() error {
	first := false
	l.once.Do(func() { first = true; close(l.done) })
	if first {
		l.f.mu.Lock()
		l.f.active--
		l.f.closed++
		l.f.mu.Unlock()
	}
	return l.Listener.Close()
}

type proxiedConn struct {
	net.Conn
	reader *bufio.Reader
	remote net.Addr
}

func (c *proxiedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *proxiedConn) RemoteAddr() net.Addr       { return c.remote }

func (f *fakeTunnel) Listen(ctx context.Context, domain, token string, report TunnelReport) (net.Listener, error) {
	f.mu.Lock()
	f.calls = append(f.calls, tunnelCall{domain, token})
	err, block := f.err, f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	inner, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		return nil, listenErr
	}
	f.mu.Lock()
	f.active++
	if f.active > 1 {
		f.overlap = true
	}
	tracked := newTrackedListener(inner, f)
	f.listener, f.report = tracked, report
	f.mu.Unlock()
	report(true, nil)
	return tracked, nil
}

func (f *fakeTunnel) address() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listener.Addr().String()
}

func (f *fakeTunnel) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeTunnel) snapshot() (calls []tunnelCall, active int, overlap bool, closed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tunnelCall(nil), f.calls...), f.active, f.overlap, f.closed
}

func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type tunnelFixture struct {
	service  *Service
	settings *memorySettings
	store    *secrets.Memory
	fake     *fakeTunnel
	clock    *clock
	lang     atomic.Value
	// dialed counts the WebSockets the clients of tunnelClient opened.
	dialed atomic.Int32
}

func newTunnelFixture(t *testing.T, adjust ...func(*Options)) *tunnelFixture {
	t.Helper()
	loopback := loopbackInterface(t)
	f := &tunnelFixture{
		settings: &memorySettings{settings: config.RemoteSettings{Enabled: true, Interfaces: []string{loopback.Name}, Port: freePort(t)}},
		store:    secrets.NewMemory(),
		fake:     &fakeTunnel{},
		clock:    &clock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)},
	}
	f.lang.Store("en")
	opts := Options{
		Dir: t.TempDir(), Secrets: f.store, Load: f.settings.load, Save: f.settings.save,
		Hostname: func() (string, error) { return "studio.local", nil }, Now: f.clock.Now,
		Tunnel: f.fake, TunnelRetry: 20 * time.Millisecond,
		Handler: HandlerOptions{
			Language: func() string { return f.lang.Load().(string) },
			Mount: func(mux *http.ServeMux) {
				mux.HandleFunc("GET /api/assistant", func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"requests":[]}`)
				})
			},
		},
	}
	for _, change := range adjust {
		change(&opts)
	}
	service, err := NewService(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Shutdown)
	f.service = service
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	return f
}

// turnOn enables the tunnel as the settings screen does, and waits until it is running.
func (f *tunnelFixture) turnOn(t *testing.T) {
	t.Helper()
	update(t, f.service, SettingsUpdate{TunnelEnabled: ptr(true), TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(testTunnelToken)})
	f.waitStatus(t, TunnelStatusRunning)
}

func (f *tunnelFixture) status() string {
	view, _ := f.service.Settings()
	return view.TunnelStatus
}

func (f *tunnelFixture) waitStatus(t *testing.T, want string) {
	t.Helper()
	eventually(t, "tunnel status "+want, func() bool { return f.status() == want })
}

// dial opens the WebSocket of the tunnel, as the phone's forwarder does, as a client whose address (as ngrok would
// report it) is source; "" is a client with no such header, which has the loopback address.
func (f *tunnelFixture) dial(ctx context.Context, source string, header http.Header) (*websocket.Conn, *http.Response, error) {
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err == nil && source != "" {
			_, err = io.WriteString(conn, "@"+source+"\n")
		}
		return conn, err
	}}}
	return websocket.Dial(ctx, "ws://"+f.fake.address()+tunnelPath, &websocket.DialOptions{HTTPClient: client, HTTPHeader: header})
}

// tunnelClient is the pinned HTTP client of the phone: every connection it makes is a WebSocket to the tunnel, and TLS
// runs inside it, with the certificate checked against the pin only.
func (f *tunnelFixture) tunnelClient(t *testing.T, fingerprint, source string) *http.Client {
	t.Helper()
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			f.dialed.Add(1)
			return f.dialTLS(ctx, fingerprint, source)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

// dialTLS opens a WebSocket and runs TLS inside it, checking the certificate against the pin only.
func (f *tunnelFixture) dialTLS(ctx context.Context, fingerprint, source string) (net.Conn, error) {
	socket, response, err := f.dial(ctx, source, nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	conn := websocket.NetConn(context.Background(), socket, websocket.MessageBinary)
	secure := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, // the pin below replaces the chain check
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 || Fingerprint(state.PeerCertificates[0].Raw) != fingerprint {
				return errors.New("certificate does not match the pin")
			}
			return nil
		},
	})
	if err := secure.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return secure, nil
}

// pair opens a pairing window and returns the pin and the pairing code.
func (f *tunnelFixture) pair(t *testing.T) (fingerprint, code string, payload url.Values) {
	t.Helper()
	offer, err := f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	query, _ := payloadQuery(t, offer.Payload, "pouple-pair:v1?")
	return query.Get("fp"), query.Get("code"), query
}

const tunnelBase = "https://pouple.invalid"

func pairOver(t *testing.T, client *http.Client, code string) string {
	t.Helper()
	_, public := newKey(t)
	body, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel 8", "stepUpPublicKey": public})
	response, err := client.Post(tunnelBase+"/remote/v1/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var paired struct{ Token string }
	_ = json.NewDecoder(response.Body).Decode(&paired)
	if response.StatusCode != http.StatusCreated || paired.Token == "" {
		t.Fatalf("pairing through the tunnel: %d", response.StatusCode)
	}
	return paired.Token
}

func get(t *testing.T, client *http.Client, path, token string) (*http.Response, string) {
	t.Helper()
	request, _ := http.NewRequest("GET", tunnelBase+path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response, string(body)
}

// --- TLS inside the WebSocket --------------------------------------------------------------------------------------

func TestTunnelCarriesTheLANTLSSessionAndServesTheSnapshot(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	fingerprint, code, _ := f.pair(t)
	client := f.tunnelClient(t, fingerprint, testSource)

	token := pairOver(t, client, code)
	response, body := get(t, client, "/api/assistant", token)
	if response.StatusCode != http.StatusOK || body != `{"requests":[]}` {
		t.Fatalf("snapshot through the tunnel: %d %s", response.StatusCode, body)
	}
	if response.TLS == nil || response.TLS.Version != tls.VersionTLS13 {
		t.Fatal("the tunnel must carry a TLS 1.3 session with the Mac itself")
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("the handler is the same as the LAN's")
	}
	// The same certificate answers on the LAN listener and in the tunnel, and the same token works on both.
	view, _ := f.service.Settings()
	if view.Fingerprint != fingerprint {
		t.Fatalf("the tunnel must serve the pinned certificate: %s != %s", view.Fingerprint, fingerprint)
	}
	lan := pinnedClient(fingerprint)
	request, _ := http.NewRequest("GET", "https://"+view.Listening[0]+"/api/assistant", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	lanResponse, err := lan.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	lanResponse.Body.Close()
	if lanResponse.StatusCode != http.StatusOK {
		t.Fatalf("the token of the tunnel on the LAN listener: %d", lanResponse.StatusCode)
	}
}

func TestTunnelRefusesAnotherCertificateThanThePinnedOne(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	client := f.tunnelClient(t, strings.Repeat("0", 64), testSource)
	if _, err := client.Get(tunnelBase + "/api/assistant"); err == nil {
		t.Fatal("a wrong pin must fail the handshake inside the tunnel")
	}
}

// What is served on the tunnel is the LAN's handler: the allowlist, the tokens, the origin refusal.
func TestTunnelServesTheSameHandlerAsTheLAN(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	fingerprint, _, _ := f.pair(t)
	client := f.tunnelClient(t, fingerprint, testSource)
	if response, _ := get(t, client, "/api/assistant", ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", response.StatusCode)
	}
	request, _ := http.NewRequest("GET", tunnelBase+"/api/assistant", nil)
	request.Header.Set("Origin", "https://evil.example")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("an Origin inside the TLS session is refused too: %d", response.StatusCode)
	}
}

// --- the gate: paths, origin, source, throttle, limits ---------------------------------------------------------------

func TestTunnelServesOnlyTheUpgradePathAndAnswers404WithNoBody(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	base := "http://" + f.fake.address()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/"}, {"GET", "/remote/v1/device"}, {"GET", "/api/assistant"}, {"POST", tunnelPath}, {"GET", tunnelPath + "/"},
		{"GET", tunnelPath + "/x"}, {"GET", "/tunnel/v2"}, {"HEAD", tunnelPath}, {"DELETE", tunnelPath},
	} {
		request, _ := http.NewRequest(tc.method, base+tc.path, nil)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound || len(body) != 0 {
			t.Errorf("%s %s: %d with %d bytes of body", tc.method, tc.path, response.StatusCode, len(body))
		}
	}
}

// A GET of the path that is not a WebSocket upgrade (no Upgrade, Connection, key or version header) is a 404 with no body
// that closes the connection, and costs no upgrade of the budget of its source.
func TestTunnelAnswers404ToAGetThatIsNotAnUpgradeAndChargesNothing(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	base := "http://" + f.fake.address()
	for _, header := range []http.Header{
		nil,
		{"Upgrade": {"websocket"}},
		{"Connection": {"Upgrade"}},
		{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}},
		{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}, "Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}},
		{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}, "Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}, "Sec-WebSocket-Version": {"8"}},
		{"Upgrade": {"h2c"}, "Connection": {"Upgrade"}, "Sec-WebSocket-Key": {"x"}, "Sec-WebSocket-Version": {"13"}},
	} {
		for range 40 { // more than the budget of one source
			request, _ := http.NewRequest("GET", base+tunnelPath, nil)
			for name, values := range header {
				request.Header[name] = values
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound || len(body) != 0 || !response.Close {
				t.Fatalf("%v: %d, %d bytes, close=%v", header, response.StatusCode, len(body), response.Close)
			}
		}
	}
	// The same source (the loopback address, no header) still has its whole budget for real upgrades.
	for i := range tunnelUpgradeBurst {
		socket, _, err := f.dial(context.Background(), "", nil)
		if err != nil {
			t.Fatalf("upgrade %d after the plain requests: %v", i+1, err)
		}
		_ = socket.CloseNow()
	}
}

func TestTunnelRefusesRequestsThatCarryAnOrigin(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	for _, header := range []http.Header{{"Origin": {"https://evil.example"}}, {"Origin": {"null"}}, {"Sec-Fetch-Site": {"cross-site"}}} {
		_, response, err := f.dial(context.Background(), testSource, header)
		if err == nil {
			t.Fatalf("%v: a browser must not get a tunnel", header)
		}
		if response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("%v: got %v", header, response)
		}
		response.Body.Close()
	}
	socket, _, err := f.dial(context.Background(), testSource, nil)
	if err != nil {
		t.Fatalf("a phone is accepted: %v", err)
	}
	_ = socket.CloseNow()
}

// badToken is a request with a token nobody was given: it fails to authenticate, which the throttle counts.
func badToken(t *testing.T, client *http.Client) int {
	t.Helper()
	response, _ := get(t, client, "/api/assistant", "wrong-token")
	return response.StatusCode
}

// The source of a tunnel client is the address ngrok reports for its connection. A client can send any
// X-Forwarded-For it likes: it changes nothing.
func TestTunnelSourceIsTheAddressNgrokReportsNotAHeaderTheClientSent(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	fingerprint, _, _ := f.pair(t)
	source, forged := netip.MustParseAddr(testSource), netip.MustParseAddr("198.51.100.77")
	transport := &http.Transport{DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		socket, _, err := f.dial(ctx, testSource, http.Header{"X-Forwarded-For": {forged.String(), "10.0.0.1"}, "Forwarded": {"for=192.0.2.60"}, "X-Real-Ip": {"192.0.2.61"}})
		if err != nil {
			return nil, err
		}
		conn := websocket.NetConn(context.Background(), socket, websocket.MessageBinary)
		secure := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, VerifyConnection: func(tls.ConnectionState) error { return nil }})
		return secure, secure.HandshakeContext(ctx)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	_ = fingerprint
	for range 6 {
		if status := badToken(t, client); status != http.StatusUnauthorized && status != http.StatusTooManyRequests {
			t.Fatalf("wrong token: %d", status)
		}
	}
	throttle := f.service.handler.throttle
	if _, blocked := throttle.Blocked(source); !blocked {
		t.Fatal("the failures of the phone behind the tunnel must count against the address ngrok reported")
	}
	for _, innocent := range []string{forged.String(), "10.0.0.1", "192.0.2.60", "192.0.2.61", "127.0.0.1"} {
		if _, blocked := throttle.Blocked(netip.MustParseAddr(innocent)); blocked {
			t.Fatalf("%s did nothing: no header the client sends, nor the tunnel's own address, is a source", innocent)
		}
	}
	// The next request of that source gets the lockout's answer through the same TLS session.
	if status := badToken(t, client); status != http.StatusTooManyRequests {
		t.Fatalf("a locked-out source: %d", status)
	}
	// Another source behind the same tunnel is not affected.
	fresh := f.tunnelClient(t, fingerprint, "192.0.2.200")
	if status := badToken(t, fresh); status != http.StatusUnauthorized {
		t.Fatalf("another source: %d", status)
	}
}

// A source that comes through the tunnel is never local, whatever its address and whatever the Mac's own networks are:
// it gets the external limits (6 failures, not 30) and its wrong pairing codes never close the pairing window.
func TestTunnelSourcesAreNeverLocal(t *testing.T) {
	f := newTunnelFixture(t, func(o *Options) { o.Local = func(netip.Addr) bool { return true } }) // even if the Mac believed so
	f.turnOn(t)
	fingerprint, code, _ := f.pair(t)
	for _, source := range []string{"192.168.1.50", "10.1.2.3", "fd00::7", "2001:db8:aaaa:bbbb::5", "169.254.4.4"} {
		client := f.tunnelClient(t, fingerprint, source)
		for range 6 {
			badToken(t, client)
		}
		if _, blocked := f.service.handler.throttle.Blocked(netip.MustParseAddr(source)); !blocked {
			t.Errorf("%s: a tunnel source must get the external limits, locked out after 6 failures", source)
		}
	}
	// Wrong pairing codes through the tunnel do not count toward the 5 that close the window.
	client := f.tunnelClient(t, fingerprint, "192.168.1.60")
	_, public := newKey(t)
	for range maxPairingAttempts {
		body, _ := json.Marshal(map[string]string{"code": strings.Repeat("0", 32), "deviceName": "Pixel", "stepUpPublicKey": public})
		response, err := client.Post(tunnelBase+"/remote/v1/pair", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("a wrong code: %d", response.StatusCode)
		}
	}
	if token := pairOver(t, client, code); token == "" {
		t.Fatal("the window must still be open")
	}
}

func TestTunnelUpgradesFollowATokenBucketPerSource(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	ctx := context.Background()
	open := func(source string) (int, string) {
		socket, response, err := f.dial(ctx, source, nil)
		if err == nil {
			_ = socket.CloseNow()
			return http.StatusSwitchingProtocols, ""
		}
		defer response.Body.Close()
		return response.StatusCode, response.Header.Get("Retry-After")
	}
	for i := range tunnelUpgradeBurst {
		if status, _ := open(testSource); status != http.StatusSwitchingProtocols {
			t.Fatalf("upgrade %d: %d", i+1, status)
		}
	}
	status, retry := open(testSource)
	if status != http.StatusTooManyRequests {
		t.Fatalf("upgrade %d: %d, want 429", tunnelUpgradeBurst+1, status)
	}
	if seconds, err := strconv.Atoi(retry); err != nil || seconds < 1 || seconds > int(tunnelUpgradeRefill/time.Second) {
		t.Fatalf("Retry-After %q", retry)
	}
	if status, _ := open("198.51.100.77"); status != http.StatusSwitchingProtocols {
		t.Fatalf("another source is counted on its own: %d", status)
	}
	// An IPv6 source counts by its /64.
	for range tunnelUpgradeBurst {
		_, _ = open("2001:db8:1:2::1")
	}
	if status, _ := open("2001:db8:1:2:ffff::7"); status != http.StatusTooManyRequests {
		t.Fatalf("the same /64: %d", status)
	}
	// The budget comes back one upgrade at a time, not all at once after a window.
	f.clock.advance(tunnelUpgradeRefill / 2)
	if status, _ := open(testSource); status != http.StatusTooManyRequests {
		t.Fatalf("half a refill: %d", status)
	}
	f.clock.advance(tunnelUpgradeRefill / 2)
	if status, _ := open(testSource); status != http.StatusSwitchingProtocols {
		t.Fatalf("one refill: %d", status)
	}
	if status, _ := open(testSource); status != http.StatusTooManyRequests {
		t.Fatalf("only one upgrade came back: %d", status)
	}
	f.clock.advance(time.Duration(tunnelUpgradeBurst) * tunnelUpgradeRefill)
	for i := range tunnelUpgradeBurst {
		if status, _ := open(testSource); status != http.StatusSwitchingProtocols {
			t.Fatalf("a full budget again, upgrade %d: %d", i+1, status)
		}
	}
}

// A connection that proves to be a paired phone gives its upgrade back: a scanner behind the same carrier address that
// empties the budget cannot lock the owner out for long.
func TestAuthenticatedConnectionsGiveTheirUpgradeBack(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	ctx := context.Background()
	fingerprint, code, _ := f.pair(t)
	scan := func() int {
		socket, response, err := f.dial(ctx, testSource, nil)
		if err != nil {
			response.Body.Close()
			return response.StatusCode
		}
		_ = socket.CloseNow()
		return http.StatusSwitchingProtocols
	}
	for range tunnelUpgradeBurst - 1 {
		if scan() != http.StatusSwitchingProtocols {
			t.Fatal("within the burst")
		}
	}
	// The last upgrade of the budget is the phone's: pairing authenticates, and gives it back.
	client := f.tunnelClient(t, fingerprint, testSource)
	token := pairOver(t, client, code)
	if response, _ := get(t, client, "/api/assistant", token); response.StatusCode != http.StatusOK {
		t.Fatal("authenticated request")
	}
	if got := f.dialed.Load(); got != 1 {
		t.Fatalf("one WebSocket for the phone: %d", got)
	}
	if scan() != http.StatusSwitchingProtocols {
		t.Fatal("the upgrade of the authenticated connection must have been given back")
	}
	if scan() != http.StatusTooManyRequests {
		t.Fatal("and only that one")
	}
	// A second authenticated request on the same connection gives nothing more back.
	if response, _ := get(t, client, "/api/assistant", token); response.StatusCode != http.StatusOK {
		t.Fatal("authenticated request")
	}
	if scan() != http.StatusTooManyRequests {
		t.Fatal("a connection gives back its upgrade once")
	}
}

func TestUpgradeLimiterIsBounded(t *testing.T) {
	l := newUpgradeLimiter(nil)
	l.max = 8
	for i := range 100 {
		if _, ok := l.allow(netip.AddrFrom4([4]byte{198, 51, byte(i / 250), byte(i % 250)})); !ok {
			t.Fatal("one upgrade of a new source is allowed")
		}
		if len(l.entries) > l.max {
			t.Fatalf("%d sources remembered, bound %d", len(l.entries), l.max)
		}
	}
}

// The gate on its own, with no TLS: what it hands the server is a connection with the address it came from,
// whose messages are limited to 1 MiB and whose bytes go both ways.
func TestTunnelGateHandsOverConnectionsWithTheSourceAndTheReadLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conns := newChanListener(&net.TCPAddr{})
	gate := &tunnelGate{ctx: ctx, conns: conns, upgrades: newUpgradeLimiter(nil), pingEvery: time.Hour, pingTimeout: time.Second, idle: time.Hour}
	server := httptest.NewServer(gate)
	defer server.Close()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+tunnelPath, &websocket.DialOptions{HTTPHeader: http.Header{"X-Forwarded-For": {"198.51.100.77, " + testSource}}})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := conns.Accept(); err == nil {
			accepted <- conn
		}
	}()
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-ctx.Done():
		t.Fatal("the upgraded connection was never handed over")
	}
	defer conn.Close()
	// The address of the connection is the one the server saw (ngrok's report, in production), whatever the client sent.
	if got := sourceOf(conn.RemoteAddr().String()); got != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("the connection's address is the connection's: %v", conn.RemoteAddr())
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Bytes go both ways, as binary messages.
	if err := socket.Write(ctx, websocket.MessageBinary, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	hello := make([]byte, 5)
	if _, err := io.ReadFull(conn, hello); err != nil || string(hello) != "hello" {
		t.Fatalf("%q %v", hello, err)
	}
	if _, err := conn.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	if kind, data, err := socket.Read(ctx); err != nil || kind != websocket.MessageBinary || string(data) != "world" {
		t.Fatalf("%v %q %v", kind, data, err)
	}

	// A message of exactly the limit goes through; a longer one closes the connection. The writes run beside the reads: a megabyte
	// does not fit in the buffers of the sockets.
	go func() { _ = socket.Write(ctx, websocket.MessageBinary, make([]byte, tunnelReadLimit)) }()
	if _, err := io.ReadFull(conn, make([]byte, tunnelReadLimit)); err != nil {
		t.Fatalf("a message of 1 MiB: %v", err)
	}
	go func() { _ = socket.Write(ctx, websocket.MessageBinary, make([]byte, tunnelReadLimit+(64<<10))) }()
	received := 0
	for {
		n, err := conn.Read(make([]byte, 64<<10))
		received += n
		if err != nil {
			break
		}
	}
	// The library reads one byte past the limit to tell a message that ends there from a longer one.
	if received > tunnelReadLimit+1 {
		t.Fatalf("%d bytes of a message over the limit were delivered", received)
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("the connection must be closed after a message over the limit")
	}
}

// net/http interrupts its background read with a deadline in the past and then clears it after every response: that must
// end a Read, not the tunnel. A client that keeps its connection alive uses one WebSocket for all its requests.
func TestTunnelKeepsOneWebSocketAcrossRequestsAndStreams(t *testing.T) {
	f := newTunnelFixture(t, func(o *Options) {
		mount := o.Handler.Mount
		o.Handler.Mount = func(mux *http.ServeMux) {
			mount(mux)
			mux.HandleFunc("GET /api/assistant/stream", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for i := range 3 {
					_, _ = io.WriteString(w, "event: snapshot\ndata: "+strconv.Itoa(i)+"\n\n")
					w.(http.Flusher).Flush()
					time.Sleep(30 * time.Millisecond)
				}
			})
		}
	})
	f.turnOn(t)
	fingerprint, code, _ := f.pair(t)
	client := f.tunnelClient(t, fingerprint, testSource)
	token := pairOver(t, client, code)
	for range 3 {
		if response, body := get(t, client, "/api/assistant", token); response.StatusCode != http.StatusOK || body != `{"requests":[]}` {
			t.Fatalf("%d %s", response.StatusCode, body)
		}
	}
	response, body := get(t, client, "/api/assistant/stream", token)
	if response.StatusCode != http.StatusOK || strings.Count(body, "event: snapshot") != 3 || !strings.Contains(body, "data: 2") {
		t.Fatalf("stream: %d %q", response.StatusCode, body)
	}
	if response, _ := get(t, client, "/api/assistant", token); response.StatusCode != http.StatusOK {
		t.Fatalf("after the stream: %d", response.StatusCode)
	}
	if dials := f.dialed.Load(); dials != 1 {
		t.Fatalf("%d WebSockets were opened for the whole conversation, want 1: each request would otherwise cost an upgrade and a handshake", dials)
	}
}

func TestDeadlineConnEndsReadsNotTheConnection(t *testing.T) {
	near, far := net.Pipe()
	conn := newDeadlineConn(near)
	defer conn.Close()
	defer far.Close()
	buf := make([]byte, 3)

	// A deadline in the past ends a Read at once, as a timeout; clearing it lets the connection go on.
	_ = conn.SetReadDeadline(time.Now().Add(-time.Second))
	_, err := conn.Read(buf)
	var netErr net.Error
	if !errors.Is(err, os.ErrDeadlineExceeded) || !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("an expired deadline: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	go func() { _, _ = far.Write([]byte("hello")) }()
	if n, err := conn.Read(buf); err != nil || string(buf[:n]) != "hel" {
		t.Fatalf("%q %v", buf[:n], err)
	}
	if n, err := conn.Read(buf); err != nil || string(buf[:n]) != "lo" {
		t.Fatalf("the rest of a chunk: %q %v", buf[:n], err)
	}

	// A deadline that passes during a Read ends it, and the data that comes after is read.
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := conn.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("a deadline during a read: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	go func() { _, _ = far.Write([]byte("abc")) }()
	if n, err := conn.Read(buf); err != nil || string(buf[:n]) != "abc" {
		t.Fatalf("%q %v", buf[:n], err)
	}
	_ = conn.SetDeadline(time.Time{})

	// Writes go through, and the end of the peer is the end of the reads.
	go func() { _, _ = conn.Write([]byte("out")) }()
	out := make([]byte, 3)
	if _, err := io.ReadFull(far, out); err != nil || string(out) != "out" {
		t.Fatalf("%q %v", out, err)
	}
	_ = far.Close()
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("a closed peer ends the reads")
	}
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("and stays ended")
	}
	_ = conn.Close()
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("a closed connection reads nothing")
	}
}

func TestTunnelWebSocketsCountInTheConnectionCaps(t *testing.T) {
	f := newTunnelFixture(t, func(o *Options) { o.MaxConnectionsPerSource = 1 })
	f.turnOn(t)
	fingerprint, code, _ := f.pair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A connection that has not authenticated holds the slot of its source.
	idle, err := f.dialTLS(ctx, fingerprint, testSource)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	if total, sources := f.service.limiter.open(); total != 1 || sources != 1 {
		t.Fatalf("a tunnel connection counts in the caps: %d connections, %d sources", total, sources)
	}
	// The same external source cannot hold a second one: it is reset before any TLS work. Another source has its own share.
	if _, err := f.dialTLS(ctx, fingerprint, testSource); err == nil {
		t.Fatal("a second connection of the same source must be refused by the per-source cap")
	}
	other, err := f.dialTLS(ctx, fingerprint, "198.51.100.77")
	if err != nil {
		t.Fatalf("another source: %v", err)
	}
	defer other.Close()
	_ = idle.Close()
	eventually(t, "the slot of the idle connection", func() bool { total, _ := f.service.limiter.open(); return total == 1 })

	// A source that has an authenticated connection open is a paired phone, perhaps behind a carrier NAT shared with a
	// scanner: the per-source cap no longer applies to it, while that connection is open.
	phone := f.tunnelClient(t, fingerprint, testSource)
	token := pairOver(t, phone, code)
	if response, _ := get(t, phone, "/api/assistant", token); response.StatusCode != http.StatusOK {
		t.Fatalf("the phone: %d", response.StatusCode)
	}
	second := f.tunnelClient(t, fingerprint, testSource)
	if response, _ := get(t, second, "/api/assistant", token); response.StatusCode != http.StatusOK {
		t.Fatalf("a second connection of a source with an authenticated one: %d", response.StatusCode)
	}
	if total, _ := f.service.limiter.open(); total != 3 {
		t.Fatalf("%d connections", total)
	}
	// The exemption ends with the authenticated connections, and the total cap still holds for everybody.
	phone.CloseIdleConnections()
	second.CloseIdleConnections()
	eventually(t, "the authenticated connections to end", func() bool {
		f.service.limiter.mu.Lock()
		defer f.service.limiter.mu.Unlock()
		return len(f.service.limiter.authed) == 0
	})
	idle2, err := f.dialTLS(ctx, fingerprint, testSource)
	if err != nil {
		t.Fatal(err)
	}
	defer idle2.Close()
	if _, err := f.dialTLS(ctx, fingerprint, testSource); err == nil {
		t.Fatal("the cap applies again to a source with no authenticated connection")
	}
}

func TestConnectionLimiterExemptsAuthenticatedSourcesFromTheSourceCapOnly(t *testing.T) {
	l := newConnLimiter(3, 1, nil)
	phone, scanner := netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("198.51.100.1")
	if !l.acquire(phone) || l.acquire(phone) {
		t.Fatal("the per-source cap applies before any authentication")
	}
	l.setAuthenticated(phone, true)
	if !l.acquire(phone) {
		t.Fatal("exempt while an authenticated connection is open")
	}
	if !l.acquire(scanner) || l.acquire(scanner) {
		t.Fatal("other sources keep the cap")
	}
	if l.acquire(phone) {
		t.Fatal("the total cap holds for everybody")
	}
	l.setAuthenticated(phone, false)
	l.release(phone)
	if l.acquire(phone) {
		t.Fatal("the exemption ended")
	}
}

func TestTunnelClosesIdleWebSockets(t *testing.T) {
	f := newTunnelFixture(t)
	f.service.tunnelTiming = func(g *tunnelGate) {
		g.pingEvery, g.pingTimeout, g.idle = 20*time.Millisecond, time.Second, 150*time.Millisecond
	}
	f.turnOn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socket, _, err := f.dial(ctx, testSource, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	// The client only reads, which answers the pings; no data goes either way, so the Mac closes it as idle.
	if _, _, err := socket.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("an idle WebSocket must be closed by the Mac: %v", err)
	}
	eventually(t, "the slot of the idle connection", func() bool { total, _ := f.service.limiter.open(); return total == 0 })
}

// --- the supervisor and the settings ---------------------------------------------------------------------------------

func TestTunnelSettingsRoundTripAndTheTokenIsWriteOnly(t *testing.T) {
	f := newTunnelFixture(t)
	view, _ := f.service.Settings()
	if view.TunnelEnabled || view.TunnelDomain != "" || view.TunnelAuthtokenSet || view.TunnelStatus != TunnelStatusOff || view.TunnelProblem != "" {
		t.Fatalf("the tunnel is off by default: %+v", view)
	}
	// On needs a domain and a token, in the same request or before.
	for _, bad := range []SettingsUpdate{
		{TunnelEnabled: ptr(true)},
		{TunnelEnabled: ptr(true), TunnelDomain: ptr(testTunnelDomain)},
		{TunnelEnabled: ptr(true), TunnelAuthtoken: ptr(testTunnelToken)},
	} {
		if _, err := f.service.UpdateSettings(bad); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if view, _ := f.service.Settings(); view.TunnelAuthtokenSet || view.TunnelEnabled {
		t.Fatalf("a refused update stores nothing: %+v", view)
	}

	view = update(t, f.service, SettingsUpdate{TunnelDomain: ptr("  Pouple-Test.NGROK-free.app "), TunnelAuthtoken: ptr("  " + testTunnelToken + " ")})
	if view.TunnelDomain != testTunnelDomain || !view.TunnelAuthtokenSet || view.TunnelEnabled || view.TunnelStatus != TunnelStatusOff {
		t.Fatalf("domain and token stored, tunnel still off: %+v", view)
	}
	if stored, err := f.store.Get(tunnelTokenSecret); err != nil || stored != testTunnelToken {
		t.Fatalf("the token belongs in the secrets store, trimmed: %q %v", stored, err)
	}
	if raw, _ := json.Marshal(f.settings.settings); strings.Contains(string(raw), testTunnelToken) {
		t.Fatal("the token must never be in the configuration")
	}

	view = update(t, f.service, SettingsUpdate{TunnelEnabled: ptr(true)})
	f.waitStatus(t, TunnelStatusRunning)
	view, _ = f.service.Settings()
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), testTunnelToken) {
		t.Fatalf("the token is never returned: %s", raw)
	}
	for _, field := range []string{`"tunnelEnabled":true`, `"tunnelDomain":"` + testTunnelDomain + `"`, `"tunnelAuthtokenSet":true`, `"tunnelStatus":"running"`, `"tunnelProblem":""`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("%s missing from %s", field, raw)
		}
	}
	if strings.Contains(strings.ToLower(string(raw)), "tunnelauthtoken\"") {
		t.Fatalf("no authtoken field is returned: %s", raw)
	}

	// Turning it off stops it; the token stays stored for the next time.
	view = update(t, f.service, SettingsUpdate{TunnelEnabled: ptr(false)})
	f.waitStatus(t, TunnelStatusOff)
	if _, active, _, _ := f.fake.snapshot(); active != 0 {
		t.Fatalf("the endpoint must be closed when the tunnel is turned off: %d", active)
	}
	if view, _ = f.service.Settings(); !view.TunnelAuthtokenSet {
		t.Fatal("the token stays stored")
	}
	// An empty token removes it, and the tunnel cannot be on without one.
	update(t, f.service, SettingsUpdate{TunnelAuthtoken: ptr("")})
	if _, err := f.store.Get(tunnelTokenSecret); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("the token must be removed from the Keychain: %v", err)
	}
	if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelEnabled: ptr(true)}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("on without a token: %v", err)
	}
}

func TestTunnelDomainIsValidatedStrictly(t *testing.T) {
	good := map[string]string{
		"pouple-test.ngrok-free.app": "pouple-test.ngrok-free.app",
		"  MY-Mac.ngrok.app  ":       "my-mac.ngrok.app",
		"a.b":                        "a.b",
		"":                           "",
	}
	for in, want := range good {
		if got, err := ValidateTunnelDomain(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	bad := []string{
		"https://pouple.ngrok-free.app", "pouple.ngrok-free.app:443", "pouple.ngrok-free.app/tunnel/v1", "pouple ngrok.app",
		"localhost", "pouple", "203.0.113.7", "2001:db8::1", "[2001:db8::1]", "*.ngrok-free.app", "-a.ngrok.app", "a-.ngrok.app",
		"a..ngrok.app", ".ngrok.app", "ngrok.app.", "pou_ple.ngrok.app", "pouple.ngrok.app?x=1", "pouple.ngrok.app#x", "user@pouple.ngrok.app",
		"pouple.ngrok.123", "é.ngrok.app", strings.Repeat("a", 64) + ".ngrok.app", strings.Repeat("a.", 130) + "app", "auto",
		"pou\x00ple.ngrok.app", "pou\nple.ngrok.app",
	}
	for _, in := range bad {
		if got, err := ValidateTunnelDomain(in); err == nil {
			t.Errorf("%q must be refused, got %q", in, got)
		}
	}
	// A trailing newline is trimmed like any space, never kept inside the value.
	if got, err := ValidateTunnelDomain("pouple.ngrok.app\n"); err != nil || got != "pouple.ngrok.app" {
		t.Errorf("trailing newline: %q %v", got, err)
	}
	f := newTunnelFixture(t)
	for _, in := range bad {
		if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelDomain: ptr(in)}); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("update %q: %v", in, err)
		}
	}
}

func TestTunnelTokenIsCheckedAndNeverEchoed(t *testing.T) {
	f := newTunnelFixture(t)
	secret := "secret value with spaces and ' quotes"
	for _, token := range []string{secret, "short", strings.Repeat("a", 300), "tok\nen_0123456789", "é_0123456789ab", "a b_0123456789"} {
		_, err := f.service.UpdateSettings(SettingsUpdate{TunnelAuthtoken: ptr(token)})
		if !errors.Is(err, ErrInvalidSettings) {
			t.Fatalf("%q: %v", token, err)
		}
		if strings.Contains(err.Error(), token) || strings.Contains(strings.ToLower(err.Error()), "secret value") {
			t.Fatalf("an error must never echo the token: %v", err)
		}
	}
	if view, _ := f.service.Settings(); view.TunnelAuthtokenSet {
		t.Fatal("a refused token is not stored")
	}
	// The failures of the provider and the log never carry it either.
	f.fake.setErr(&TunnelError{Fault: FaultToken, Err: errors.New("authentication failed using " + testTunnelToken)})
	update(t, f.service, SettingsUpdate{TunnelEnabled: ptr(true), TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(testTunnelToken)})
	f.waitStatus(t, TunnelStatusError)
	view, _ := f.service.Settings()
	if raw, _ := json.Marshal(view); strings.Contains(string(raw), testTunnelToken) {
		t.Fatalf("the problem must not hold the token: %s", raw)
	}
}

func TestTunnelRestartsWhenItsSettingsChangeAndNeverOverlaps(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	// An unrelated change (the port, the external address) leaves the tunnel's session alone.
	update(t, f.service, SettingsUpdate{Port: ptr(freePort(t)), ExternalHost: ptr("home.example.net")})
	if calls, active, _, closed := f.fake.snapshot(); len(calls) != 1 || active != 1 || closed != 0 {
		t.Fatalf("the LAN listeners restarting must not restart the tunnel: %d calls, %d active, %d closed", len(calls), active, closed)
	}
	// A new domain replaces the old endpoint, which is closed before the new one binds.
	update(t, f.service, SettingsUpdate{TunnelDomain: ptr("second.ngrok-free.app")})
	eventually(t, "the second endpoint", func() bool { calls, active, _, _ := f.fake.snapshot(); return len(calls) == 2 && active == 1 })
	f.waitStatus(t, TunnelStatusRunning)
	// A new token does too.
	update(t, f.service, SettingsUpdate{TunnelAuthtoken: ptr("ngrok_other_0123456789abcdef")})
	eventually(t, "the third endpoint", func() bool { calls, active, _, _ := f.fake.snapshot(); return len(calls) == 3 && active == 1 })
	calls, _, overlap, _ := f.fake.snapshot()
	if calls[0] != (tunnelCall{testTunnelDomain, testTunnelToken}) || calls[1].Domain != "second.ngrok-free.app" || calls[2].Token != "ngrok_other_0123456789abcdef" {
		t.Fatalf("calls: %+v", calls)
	}
	if overlap {
		t.Fatal("two sessions on the same domain at once would be refused by ngrok: the old one must close first")
	}
	// The access itself turned off stops the tunnel too, and on again brings it back.
	update(t, f.service, SettingsUpdate{Enabled: ptr(false)})
	f.waitStatus(t, TunnelStatusOff)
	if _, active, _, _ := f.fake.snapshot(); active != 0 {
		t.Fatalf("the tunnel must stop with the access: %d", active)
	}
	update(t, f.service, SettingsUpdate{Enabled: ptr(true)})
	f.waitStatus(t, TunnelStatusRunning)
}

func TestTunnelStartsWithPoupleAndStopsAtShutdown(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	f.service.Shutdown()
	if _, active, _, closed := f.fake.snapshot(); active != 0 || closed != 1 {
		t.Fatalf("the endpoint must be closed at shutdown: %d active, %d closed", active, closed)
	}
	// A new Pouple with the same settings starts it from the stored configuration alone, as Run does.
	again := newTestService2(t, f)
	eventually(t, "the tunnel to start with the service", func() bool {
		view, _ := again.Settings()
		return view.TunnelStatus == TunnelStatusRunning
	})
}

// newTestService2 is a second service on the settings and the secrets of the fixture, started with Apply.
func newTestService2(t *testing.T, f *tunnelFixture) *Service {
	t.Helper()
	service, err := NewService(Options{
		Dir: t.TempDir(), Secrets: f.store, Load: f.settings.load, Save: f.settings.save,
		Tunnel: f.fake, TunnelRetry: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Shutdown)
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestTunnelNeverBlocksTheSettingsOnTheNetwork(t *testing.T) {
	f := newTunnelFixture(t)
	f.fake.block = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelEnabled: ptr(true), TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(testTunnelToken)}); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("an update waited for the network: the settings lock was held while connecting")
	}
	f.waitStatus(t, TunnelStatusStarting)
	// While ngrok does not answer, the settings read and change, and the LAN listener serves.
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range 20 {
			if _, err := f.service.Settings(); err != nil {
				t.Error(err)
			}
		}
		for _, change := range []SettingsUpdate{{ExternalHost: ptr("home.example.net")}, {TunnelEnabled: ptr(false)}} {
			if _, err := f.service.UpdateSettings(change); err != nil {
				t.Error(err)
			}
		}
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the settings were blocked while the tunnel was connecting")
	}
	f.waitStatus(t, TunnelStatusOff)
}

func TestTunnelFaultsAreShownTranslatedAndRetried(t *testing.T) {
	f := newTunnelFixture(t)
	f.fake.setErr(&TunnelError{Fault: FaultDomain, Err: errors.New("ERR_NGROK_320")})
	update(t, f.service, SettingsUpdate{TunnelEnabled: ptr(true), TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(testTunnelToken)})
	f.waitStatus(t, TunnelStatusError)
	view, _ := f.service.Settings()
	if !strings.Contains(view.TunnelProblem, "refused the domain") {
		t.Fatalf("english: %q", view.TunnelProblem)
	}
	f.lang.Store("fr")
	if view, _ = f.service.Settings(); !strings.Contains(view.TunnelProblem, "a refusé le domaine") {
		t.Fatalf("french: %q", view.TunnelProblem)
	}
	f.lang.Store("en")

	for fault, want := range map[TunnelFault]string{
		FaultToken: "refused the authtoken", FaultNetwork: "cannot reach ngrok", FaultLimit: "limit is reached", FaultAccount: "cannot start a tunnel", FaultOther: "could not start",
	} {
		f.fake.setErr(&TunnelError{Fault: fault, Err: errors.New("x")})
		eventually(t, "the problem for fault "+strconv.Itoa(int(fault)), func() bool {
			view, _ := f.service.Settings()
			return view.TunnelStatus == TunnelStatusError && strings.Contains(view.TunnelProblem, want)
		})
	}
	// The fault is fixed: the next attempt brings it up, and the problem goes away.
	f.fake.setErr(nil)
	f.waitStatus(t, TunnelStatusRunning)
	if view, _ = f.service.Settings(); view.TunnelProblem != "" {
		t.Fatalf("a running tunnel has no problem: %q", view.TunnelProblem)
	}
	if calls, _, _, _ := f.fake.snapshot(); len(calls) < 3 {
		t.Fatalf("the tunnel must be retried: %d calls", len(calls))
	}
}

func TestTunnelReconnectionIsShownInTheStatus(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	f.fake.mu.Lock()
	report := f.fake.report
	f.fake.mu.Unlock()
	report(false, &TunnelError{Fault: FaultNetwork, Err: errors.New("dial tcp: no route to host")})
	view, _ := f.service.Settings()
	if view.TunnelStatus != TunnelStatusError || !strings.Contains(view.TunnelProblem, "cannot reach ngrok") {
		t.Fatalf("a lost connection: %+v", view)
	}
	report(true, nil)
	if view, _ = f.service.Settings(); view.TunnelStatus != TunnelStatusRunning || view.TunnelProblem != "" {
		t.Fatalf("a recovered connection: %+v", view)
	}
	// The same listener served all along: the provider reconnected under it.
	if calls, _, _, _ := f.fake.snapshot(); len(calls) != 1 {
		t.Fatalf("a reconnection by the provider is not a restart: %d calls", len(calls))
	}
}

func TestTunnelRestartsAfterItsEndpointCloses(t *testing.T) {
	f := newTunnelFixture(t)
	f.turnOn(t)
	f.fake.mu.Lock()
	listener := f.fake.listener
	f.fake.mu.Unlock()
	_ = listener.Close() // the provider gave up
	eventually(t, "a second endpoint after the first one closed", func() bool {
		calls, active, _, _ := f.fake.snapshot()
		return len(calls) == 2 && active == 1
	})
	f.waitStatus(t, TunnelStatusRunning)
}

func TestTunnelNeedsTheAccessToBeOn(t *testing.T) {
	f := newTunnelFixture(t)
	update(t, f.service, SettingsUpdate{Enabled: ptr(false), TunnelEnabled: ptr(true), TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(testTunnelToken)})
	time.Sleep(50 * time.Millisecond)
	if calls, _, _, _ := f.fake.snapshot(); len(calls) != 0 || f.status() != TunnelStatusOff {
		t.Fatalf("no tunnel while the access is off: %+v %s", calls, f.status())
	}
}

func TestAnUnreadableTokenIsAProblemNotAPanic(t *testing.T) {
	f := newTunnelFixture(t, func(o *Options) { o.Secrets = failingSecrets{secrets.NewMemory()} })
	_ = f.settings.save(config.RemoteSettings{Enabled: true, Interfaces: f.settings.settings.Interfaces, Port: f.settings.settings.Port, TunnelEnabled: true, TunnelDomain: testTunnelDomain})
	if err := f.service.Apply(); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(t, TunnelStatusError)
	if view, _ := f.service.Settings(); !strings.Contains(view.TunnelProblem, "Keychain") {
		t.Fatalf("problem: %q", view.TunnelProblem)
	}
}

// failingSecrets is a Keychain that refuses to give the ngrok authtoken (locked).
type failingSecrets struct{ *secrets.Memory }

func (f failingSecrets) Get(name string) (string, error) {
	if name == tunnelTokenSecret {
		return "", errors.New("keychain locked")
	}
	return f.Memory.Get(name)
}

// --- payloads ---------------------------------------------------------------------------------------------------------

func TestPairingPayloadCarriesTheTunnelDomainLastAndOnlyWhenConfigured(t *testing.T) {
	f := newTunnelFixture(t)
	offer, err := f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(offer.Payload, "tun=") {
		t.Fatalf("no tunnel, no tun: %s", offer.Payload)
	}
	update(t, f.service, SettingsUpdate{TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(testTunnelToken)})
	if offer, _ = f.service.OpenPairing(); strings.Contains(offer.Payload, "tun=") {
		t.Fatalf("a tunnel that is off adds nothing: %s", offer.Payload)
	}
	f.turnOn(t)
	offer, err = f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	query, raw := payloadQuery(t, offer.Payload, "pouple-pair:v1?")
	if query.Get("tun") != testTunnelDomain || !strings.HasSuffix(raw, "&tun="+url.QueryEscape(testTunnelDomain)) {
		t.Fatalf("tun must come last: %s", raw)
	}
	order := []string{"hosts=", "port=", "fp=", "code=", "name=", "tun="}
	last := -1
	for _, key := range order {
		at := strings.Index(raw, key)
		if at <= last {
			t.Fatalf("%s out of order in %s", key, raw)
		}
		last = at
	}

	// With the external address too, tun follows ext.
	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("home.example.net"), ExternalPort: ptr(8443)})
	offer, _ = f.service.OpenPairing()
	if _, raw = payloadQuery(t, offer.Payload, "pouple-pair:v1?"); !strings.HasSuffix(raw, "&ext=home.example.net%3A8443&tun="+testTunnelDomain) {
		t.Fatalf("ext then tun: %s", raw)
	}

	// The address update payload accepts tun after fp.
	address, err := f.service.AddressOffer()
	if err != nil {
		t.Fatal(err)
	}
	fp := f.service.cert.Fingerprint
	if want := "pouple-address:v1?ext=home.example.net%3A8443&fp=" + fp + "&tun=" + testTunnelDomain; address.Payload != want {
		t.Fatalf("address payload %s, want %s", address.Payload, want)
	}
	if got := addressPayload("home.example.net:8443", fp, ""); strings.Contains(got, "tun") {
		t.Fatalf("no tun without a tunnel: %s", got)
	}

	// Without the token (removed behind the settings' back) it is not configured.
	_ = f.store.Delete(tunnelTokenSecret)
	f.service.tokenSet.Store(0)
	if offer, _ = f.service.OpenPairing(); strings.Contains(offer.Payload, "tun=") {
		t.Fatalf("no token, no tun: %s", offer.Payload)
	}
}

func TestEndpointRouteReturnsExtAndTun(t *testing.T) {
	f := newTunnelFixture(t)
	_, public := newKey(t)
	_, token, err := f.service.devices.Add("Pixel", public)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := func() string {
		t.Helper()
		request, _ := http.NewRequest("GET", "https://x/remote/v1/endpoint", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.RemoteAddr = "127.0.0.1:1234"
		recorder := httptest.NewRecorder()
		f.service.Handler().ServeHTTP(recorder, request)
		return strings.TrimSpace(recorder.Body.String())
	}
	if got := endpoint(); got != `{"ext":null,"tun":null}` {
		t.Fatalf("nothing configured: %s", got)
	}
	f.turnOn(t)
	if got := endpoint(); got != `{"ext":null,"tun":"`+testTunnelDomain+`"}` {
		t.Fatalf("tunnel only: %s", got)
	}
	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("home.example.net"), ExternalPort: ptr(8443)})
	if got := endpoint(); got != `{"ext":"home.example.net:8443","tun":"`+testTunnelDomain+`"}` {
		t.Fatalf("both: %s", got)
	}
}

// --- the stored configuration ---------------------------------------------------------------------------------------

func TestOldConfigurationsReadTheTunnelAsOff(t *testing.T) {
	var settings config.RemoteSettings
	if err := json.Unmarshal([]byte(`{"enabled":true,"interfaces":["en0"],"port":0,"externalEnabled":false,"externalHost":"","externalPort":0}`), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.TunnelEnabled || settings.TunnelDomain != "" {
		t.Fatalf("%+v", settings)
	}
	raw, _ := json.Marshal(config.RemoteSettings{TunnelEnabled: true, TunnelDomain: "a.ngrok.app"})
	if !strings.Contains(string(raw), `"tunnelEnabled":true`) || !strings.Contains(string(raw), `"tunnelDomain":"a.ngrok.app"`) || strings.Contains(strings.ToLower(string(raw)), "token") {
		t.Fatalf("%s", raw)
	}
}

// --- the server that reads the upgrade requests ---------------------------------------------------------------------

func TestEdgeServerHasShortTimeoutsAndSmallHeaders(t *testing.T) {
	edge := newEdgeServer(context.Background(), &tunnelGate{}, &edgeListener{limiter: newConnLimiter(0, 0, nil)})
	if edge.ReadHeaderTimeout <= 0 || edge.ReadHeaderTimeout > 10*time.Second || edge.ReadTimeout <= 0 || edge.ReadTimeout > 15*time.Second ||
		edge.WriteTimeout <= 0 || edge.WriteTimeout > 15*time.Second || edge.IdleTimeout <= 0 || edge.IdleTimeout > 30*time.Second {
		t.Fatalf("timeouts %v %v %v %v", edge.ReadHeaderTimeout, edge.ReadTimeout, edge.WriteTimeout, edge.IdleTimeout)
	}
	if edge.MaxHeaderBytes <= 0 || edge.MaxHeaderBytes > 16<<10 {
		t.Fatalf("header limit %d", edge.MaxHeaderBytes)
	}
}

// The timeouts of the server that reads the request must not follow the connection into the WebSocket: they are cleared
// when it is taken over.
func TestUpgradedConnectionsOutliveTheTimeoutsOfTheEdgeServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conns := newChanListener(&net.TCPAddr{})
	gate := &tunnelGate{ctx: ctx, conns: conns, upgrades: newUpgradeLimiter(nil), pingEvery: time.Hour, pingTimeout: time.Second, idle: time.Hour}
	server := httptest.NewUnstartedServer(gate)
	server.Config.ReadTimeout, server.Config.WriteTimeout, server.Config.IdleTimeout = 100*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond
	server.Start()
	defer server.Close()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+tunnelPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := conns.Accept(); err == nil {
			accepted <- conn
		}
	}()
	conn := <-accepted
	defer conn.Close()
	time.Sleep(400 * time.Millisecond) // several times every timeout of the edge server
	if err := socket.Write(ctx, websocket.MessageBinary, []byte("still here")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len("still here"))
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "still here" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := conn.Write([]byte("and here")); err != nil {
		t.Fatalf("a write after the write timeout of the edge server: %v", err)
	}
	if _, data, err := socket.Read(ctx); err != nil || string(data) != "and here" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestEdgeCapsTheConnectionsThatHaveNotUpgradedAndGivesTheSlotBackOnUpgrade(t *testing.T) {
	accepted := make(chan struct{}, 1)
	f := newTunnelFixture(t)
	f.fake.accepted = accepted
	f.turnOn(t)
	address := f.fake.address()
	hold := func(source string) net.Conn {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(conn, "@"+source+"\n"); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	// The fake admits peers concurrently. Wait for each handoff so the probe arrives after the holders.
	admissionDeadline := time.NewTimer(5 * time.Second)
	defer admissionDeadline.Stop()
	var holders []net.Conn
	defer func() {
		for _, conn := range holders {
			conn.Close()
		}
	}()
	for range defaultMaxConnectionsPerSource {
		holders = append(holders, hold(testSource))
		select {
		case <-accepted:
		case <-admissionDeadline.C:
			t.Fatal("the fake tunnel did not hand off a holding connection")
		}
	}
	extra := hold(testSource)
	defer extra.Close()
	if err := extra.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if size, err := extra.Read(make([]byte, 1)); size != 0 || err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("extra connection: read %d bytes, %v; want it closed without a response", size, err)
	}
	// Another source is not affected, and the ones that hold their slot are still there.
	other := hold("198.51.100.77")
	defer other.Close()
	_, _ = io.WriteString(other, "GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	_ = other.SetReadDeadline(time.Now().Add(5 * time.Second))
	if line, err := bufio.NewReader(other).ReadString('\n'); err != nil || !strings.Contains(line, "404") {
		t.Fatalf("another source: %q %v", line, err)
	}
	// A connection that upgrades gives its slot back: more upgrades than the cap of one source go through in turn (and
	// fewer than its upgrade budget).
	for _, conn := range holders {
		conn.Close()
	}
	holders = nil
	for i := range defaultMaxConnectionsPerSource + 8 {
		socket, _, err := f.dial(context.Background(), "192.0.2.9", nil)
		if err != nil {
			t.Fatalf("upgrade %d: %v", i+1, err)
		}
		_ = socket.CloseNow()
	}
}

// --- the authtoken and the configuration are saved as one step ---------------------------------------------------------

// refusingSecrets is a Keychain whose writes can be made to fail.
type refusingSecrets struct {
	*secrets.Memory
	mu                      sync.Mutex
	refuseSet, refuseDelete bool
}

func (r *refusingSecrets) Set(name, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuseSet {
		return errors.New("keychain refused the write of " + value)
	}
	return r.Memory.Set(name, value)
}

func (r *refusingSecrets) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuseDelete {
		return errors.New("keychain refused")
	}
	return r.Memory.Delete(name)
}

func (r *refusingSecrets) refuse(set, del bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuseSet, r.refuseDelete = set, del
}

func TestAFailedSaveLeavesTheKeychainAsItWas(t *testing.T) {
	keychain := &refusingSecrets{Memory: secrets.NewMemory()}
	f := newTunnelFixture(t, func(o *Options) { o.Secrets = keychain })
	const first, second = "ngrok_first_0123456789abcd", "ngrok_second_0123456789abc"
	update(t, f.service, SettingsUpdate{TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(first)})

	// The configuration cannot be saved: the new token must not stay in the Keychain, nor must the old one be lost.
	f.settings.mu.Lock()
	f.settings.saveErr = errors.New("disk full")
	f.settings.mu.Unlock()
	if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelDomain: ptr("other.ngrok-free.app"), TunnelAuthtoken: ptr(second)}); err == nil {
		t.Fatal("the save failed: so must the update")
	}
	if stored, err := keychain.Get(tunnelTokenSecret); err != nil || stored != first {
		t.Fatalf("the previous token must be back: %q %v", stored, err)
	}
	if view, _ := f.service.Settings(); view.TunnelDomain != testTunnelDomain || !view.TunnelAuthtokenSet {
		t.Fatalf("nothing changed: %+v", view)
	}
	// Same when there was no token before: nothing is left behind.
	_ = keychain.Delete(tunnelTokenSecret)
	f.service.tokenSet.Store(0)
	if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelAuthtoken: ptr(second)}); err == nil {
		t.Fatal("the save failed: so must the update")
	}
	if _, err := keychain.Get(tunnelTokenSecret); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("a token saved by an update that failed must not stay: %v", err)
	}
	if view, _ := f.service.Settings(); view.TunnelAuthtokenSet {
		t.Fatal("and the settings must say so")
	}
	// Removing the token fails the same way.
	f.settings.mu.Lock()
	f.settings.saveErr = nil
	f.settings.mu.Unlock()
	update(t, f.service, SettingsUpdate{TunnelAuthtoken: ptr(first)})
	f.settings.mu.Lock()
	f.settings.saveErr = errors.New("disk full")
	f.settings.mu.Unlock()
	if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelAuthtoken: ptr("")}); err == nil {
		t.Fatal("the save failed: so must the update")
	}
	if stored, err := keychain.Get(tunnelTokenSecret); err != nil || stored != first {
		t.Fatalf("the removed token must be back: %q %v", stored, err)
	}
	if view, _ := f.service.Settings(); !view.TunnelAuthtokenSet {
		t.Fatal("still set")
	}
}

func TestAKeychainThatRefusesTheTokenLeavesEverythingAsItWas(t *testing.T) {
	keychain := &refusingSecrets{Memory: secrets.NewMemory()}
	f := newTunnelFixture(t, func(o *Options) { o.Secrets = keychain })
	const first, second = "ngrok_first_0123456789abcd", "ngrok_second_0123456789abc"
	update(t, f.service, SettingsUpdate{TunnelDomain: ptr(testTunnelDomain), TunnelAuthtoken: ptr(first)})

	keychain.refuse(true, false)
	_, err := f.service.UpdateSettings(SettingsUpdate{TunnelDomain: ptr("other.ngrok-free.app"), TunnelAuthtoken: ptr(second)})
	if err == nil || strings.Contains(err.Error(), second) {
		t.Fatalf("an error that never repeats the token: %v", err)
	}
	if stored, _ := keychain.Get(tunnelTokenSecret); stored != first {
		t.Fatalf("the previous token stays: %q", stored)
	}
	if stored, _ := f.settings.load(); stored.TunnelDomain != testTunnelDomain {
		t.Fatalf("the configuration was not saved either: %+v", stored)
	}
	keychain.refuse(false, true)
	if _, err := f.service.UpdateSettings(SettingsUpdate{TunnelAuthtoken: ptr("")}); err == nil {
		t.Fatal("a token that cannot be removed")
	}
	if view, _ := f.service.Settings(); !view.TunnelAuthtokenSet {
		t.Fatal("it is still there, and the settings say so")
	}
}
