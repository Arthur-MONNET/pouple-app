package remote

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

// --- connection caps -------------------------------------------------------------------------------------------

// queued puts connections that claim the given sources in a listener, and returns them.
func queued(t *testing.T, sources ...string) (*queueListener, []*addrConn) {
	t.Helper()
	list := &queueListener{conns: make(chan net.Conn, len(sources))}
	var all []*addrConn
	for _, source := range sources {
		client, server := net.Pipe()
		t.Cleanup(func() { client.Close() })
		conn := &addrConn{Conn: server, remote: &net.TCPAddr{IP: net.ParseIP(source), Port: 1000 + len(all)}, closed: make(chan struct{})}
		all = append(all, conn)
		list.conns <- conn
	}
	close(list.conns)
	return list, all
}

func isClosed(c *addrConn) bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func acceptAll(l net.Listener) []net.Conn {
	var out []net.Conn
	for {
		conn, err := l.Accept()
		if err != nil {
			return out
		}
		out = append(out, conn)
	}
}

func TestConnectionCapsDropTheOnesOverTheGlobalAndPerSourceLimits(t *testing.T) {
	// A has 3 tries (2 allowed), B and C one each: 4 slots in all, so D is over the global cap.
	list, all := queued(t, "203.0.113.1", "203.0.113.1", "203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4")
	limiter := newConnLimiter(4, 2, nil)
	accepted := acceptAll(&limitListener{Listener: list, limiter: limiter})
	if len(accepted) != 4 {
		t.Fatalf("accepted %d, want 4", len(accepted))
	}
	for i, wantDropped := range []bool{false, false, true, false, false, true} {
		if isClosed(all[i]) != wantDropped {
			t.Errorf("connection %d: dropped=%v, want %v", i, isClosed(all[i]), wantDropped)
		}
	}
	if total, sources := limiter.open(); total != 4 || sources != 3 {
		t.Fatalf("open = %d connections of %d sources, want 4 of 3", total, sources)
	}
	// Closing gives the slot back, once however many times it is closed.
	for range 3 {
		_ = accepted[0].Close()
	}
	if total, sources := limiter.open(); total != 3 || sources != 3 {
		t.Fatalf("after one close: %d of %d, want 3 of 3", total, sources)
	}
	_ = accepted[1].Close()
	if total, sources := limiter.open(); total != 2 || sources != 2 {
		t.Fatalf("after the source's last close: %d of %d, want 2 of 2", total, sources)
	}
	for _, conn := range accepted[2:] {
		_ = conn.Close()
	}
	if total, sources := limiter.open(); total != 0 || sources != 0 {
		t.Fatalf("everything closed: %d of %d, want none", total, sources)
	}
}

func TestConnectionCapsGroupIPv6By64AndIPv4MappedWithIPv4(t *testing.T) {
	list, all := queued(t, "2001:db8:1:2::1", "2001:db8:1:2:ffff::2", "2001:db8:1:3::1", "::ffff:203.0.113.9", "203.0.113.9")
	limiter := newConnLimiter(10, 1, nil)
	accepted := acceptAll(&limitListener{Listener: list, limiter: limiter})
	if len(accepted) != 3 {
		t.Fatalf("accepted %d, want 3", len(accepted))
	}
	for i, wantDropped := range []bool{false, true, false, false, true} {
		if isClosed(all[i]) != wantDropped {
			t.Errorf("connection %d: dropped=%v, want %v", i, isClosed(all[i]), wantDropped)
		}
	}
}

func TestConnectionCapsUseTheDefaultsAndDropAFloodingSource(t *testing.T) {
	limiter := newConnLimiter(0, 0, func(source netip.Addr) bool { return source == netip.MustParseAddr("203.0.113.66") })
	if limiter.max != 256 || limiter.perSource != 16 {
		t.Fatalf("defaults: %d and %d", limiter.max, limiter.perSource)
	}
	list, all := queued(t, "203.0.113.66", "203.0.113.67")
	accepted := acceptAll(&limitListener{Listener: list, limiter: limiter})
	if len(accepted) != 1 || !isClosed(all[0]) || isClosed(all[1]) {
		t.Fatalf("a flooding source must be dropped at accept time, and only it: %d accepted", len(accepted))
	}
}

// TestConnectionSlotsAreReleasedOnEveryClosePath serves real TLS connections and ends them in every way a connection
// can end: a client that never speaks, garbage instead of a handshake, a request answered with "Connection: close", a
// keep-alive connection ended by the shutdown, and a handshake abandoned half way.
func TestConnectionSlotsAreReleasedOnEveryClosePath(t *testing.T) {
	cert, err := LoadOrCreateCertificate(secrets.NewMemory(), filepath.Join(t.TempDir(), "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert.TLS}, MinVersion: tls.VersionTLS13}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	limiter := newConnLimiter(8, 8, nil)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/close" {
				w.Header().Set("Connection", "close")
			}
			_, _ = io.WriteString(w, "ok")
		}),
		TLSConfig: tlsConfig, ReadHeaderTimeout: time.Second, ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() { _ = server.Serve(tls.NewListener(&limitListener{Listener: raw, limiter: limiter}, tlsConfig)) }()
	address := raw.Addr().String()
	noneOpen := func() bool { total, _ := limiter.open(); return total == 0 }
	client := pinnedClient(cert.Fingerprint)

	dial := func() net.Conn {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}

	silent := dial()
	waitFor(t, "the silent connection to be counted", func() bool { total, _ := limiter.open(); return total == 1 })
	_ = silent.Close()
	waitFor(t, "a silent client's slot to be released", noneOpen)

	garbage := dial()
	_, _ = garbage.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	waitFor(t, "a failed handshake's slot to be released", noneOpen)
	_ = garbage.Close()

	response, err := client.Get("https://" + address + "/close")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	waitFor(t, "a closed-by-the-server connection's slot to be released", noneOpen)

	response, err = client.Get("https://" + address + "/keepalive")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if total, _ := limiter.open(); total != 1 {
		t.Fatalf("a keep-alive connection holds its slot: %d", total)
	}
	half := dial() // a handshake abandoned: the server holds it until the client goes
	_ = half.Close()
	_ = server.Close()
	waitFor(t, "every slot to be released after the server closes", noneOpen)
}

func TestTheServiceDropsConnectionsOverThePerSourceCap(t *testing.T) {
	f := newExternalFixture(t, nil)
	f.service.limiter = newConnLimiter(0, 2, f.service.handler.throttle.Flooding)
	if err := f.service.Apply(); err != nil {
		t.Fatal(err)
	}
	view, _ := f.service.Settings()
	address := view.Listening[0]
	// dial returns nil when the server reset the connection before the dial even returned: that is a drop too (on a
	// busy runner the reset can win the race with the handshake of the dial).
	dial := func() net.Conn {
		conn, err := net.Dial("tcp", address)
		if errors.Is(err, syscall.ECONNRESET) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	closeConn := func(conn net.Conn) {
		if conn != nil {
			_ = conn.Close()
		}
	}
	// dropped says whether the server has closed the connection: an accepted one waits for a handshake, so a read
	// times out; a dropped one is reset or at its end at once.
	dropped := func(conn net.Conn) bool {
		if conn == nil {
			return true
		}
		_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		_, err := conn.Read(make([]byte, 1))
		var timeout net.Error
		return !(errors.As(err, &timeout) && timeout.Timeout())
	}
	first, second := dial(), dial()
	defer closeConn(first)
	third := dial()
	defer closeConn(third)
	if !dropped(third) {
		t.Fatal("the third connection of a source with a cap of 2 was accepted")
	}
	if dropped(second) {
		t.Fatal("the second connection was dropped")
	}
	closeConn(second)
	// The slot comes back when the server has seen the close.
	waitFor(t, "a slot to be free again", func() bool {
		conn := dial()
		defer closeConn(conn)
		return !dropped(conn)
	})
}

// --- throttle: flood -------------------------------------------------------------------------------------------

func TestOnlyASourceThatKeepsSendingRefusedRequestsIsFlooding(t *testing.T) {
	th, clk := newTestThrottle(0)
	if th.Flooding(external) {
		t.Fatal("unknown source flooding")
	}
	th.Refused(external) // not locked out: nothing is counted
	failN(th, external, externalLimits.failures)
	if _, blocked := th.Blocked(external); !blocked {
		t.Fatal("not locked out")
	}
	for range floodRefusals - 1 {
		th.Refused(external)
	}
	if th.Flooding(external) {
		t.Fatal("flooding before the threshold")
	}
	th.Refused(external)
	if !th.Flooding(external) || th.Flooding(lan) {
		t.Fatal("flooding at the threshold, and only this source")
	}
	clk.advance(externalLimits.baseLock + time.Second)
	if th.Flooding(external) {
		t.Fatal("a lockout that ended no longer drops anything")
	}
	// The next lockout starts the count again.
	failN(th, external, externalLimits.failures)
	if th.Flooding(external) {
		t.Fatal("the refusals of an earlier lockout carry over")
	}
}

// --- handler: lockout semantics, Connection: close -------------------------------------------------------------

// lockOut fails the source out with a wrong token, from f.source, and returns the bad token's headers.
func lockOut(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	bad := f.bearer(strings.Repeat("f", 64))
	for range externalLimits.failures {
		f.do("GET", "/api/assistant", "", bad)
	}
	if _, blocked := f.handler.throttle.Blocked(sourceOf(f.source)); !blocked {
		t.Fatal("the source is not locked out")
	}
	return bad
}

func TestAValidTokenIsServedWhileItsSourceIsLockedOut(t *testing.T) {
	f := newFixture(t)
	f.pair() // at home
	f.source = outside
	bad := lockOut(t, f)

	// The owner behind the same public address is served, on every route kind.
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
		t.Fatalf("valid token, locked-out source: %d", response.Code)
	}
	if response := f.authed("GET", "/remote/v1/device", "", nil); response.Code != http.StatusOK {
		t.Fatalf("device: %d", response.Code)
	}
	route := "DELETE /api/assistant/requests/{id}"
	if response := f.authed("DELETE", "/api/assistant/requests/r1", "", f.sign(f.key, f.nonce(), route, "r1", "", "", "null", "")); response.Code != http.StatusAccepted {
		t.Fatalf("a validly signed step-up from a locked-out source: %d %s", response.Code, response.Body)
	}
	// Everything that does not carry a valid token still waits: no token, a wrong one, pairing, a wrong signature.
	for name, headers := range map[string]map[string]string{"no token": nil, "bad token": bad} {
		if response := f.do("GET", "/api/assistant", "", headers); response.Code != http.StatusTooManyRequests {
			t.Errorf("%s: %d", name, response.Code)
		}
	}
	if response := f.do("POST", "/remote/v1/pair", `{}`, nil); response.Code != http.StatusTooManyRequests {
		t.Errorf("pairing: %d", response.Code)
	}
	wrong := map[string]string{HeaderNonce: strings.Repeat("a", 64), HeaderSignature: "AAAA"}
	if response := f.authed("DELETE", "/api/assistant/requests/r1", "", wrong); response.Code != http.StatusTooManyRequests {
		t.Errorf("a wrong signature from a locked-out source: %d", response.Code)
	}
	// A revoked token is not a valid one.
	if err := f.devices.Revoke(f.deviceID); err != nil {
		t.Fatal(err)
	}
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusTooManyRequests {
		t.Errorf("revoked token, locked-out source: %d", response.Code)
	}
}

func TestAValidTokenDoesNotWashFailuresAwayAndRevokedTokensStillCount(t *testing.T) {
	f := newFixture(t)
	f.pair()
	f.source = outside
	bad := f.bearer(strings.Repeat("f", 64))
	for range externalLimits.failures - 1 {
		f.do("GET", "/api/assistant", "", bad)
	}
	if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
		t.Fatalf("valid token: %d", response.Code)
	}
	f.do("GET", "/api/assistant", "", bad)
	if _, blocked := f.handler.throttle.Blocked(sourceOf(outside)); !blocked {
		t.Fatal("a valid request must not reset the failures of its source")
	}
	// A revoked token counts as a failure, once the source is free again.
	g := newFixture(t)
	g.pair()
	if err := g.devices.Revoke(g.deviceID); err != nil {
		t.Fatal(err)
	}
	g.source = outside
	for range externalLimits.failures {
		if response := g.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("revoked: %d", response.Code)
		}
	}
	if response := g.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d revoked-token requests: %d", externalLimits.failures, response.Code)
	}
}

func TestOnlyRefusedRequestsOfALockedOutSourceMakeItAFlood(t *testing.T) {
	f := newFixture(t)
	f.pair()
	f.source = outside
	bad := lockOut(t, f)
	for range 3 * floodRefusals {
		if response := f.authed("GET", "/api/assistant", "", nil); response.Code != http.StatusAccepted {
			t.Fatalf("valid token: %d", response.Code)
		}
	}
	if f.handler.throttle.Flooding(sourceOf(outside)) {
		t.Fatal("the owner's valid requests made the source a flood")
	}
	for range floodRefusals {
		if response := f.do("GET", "/api/assistant", "", bad); response.Code != http.StatusTooManyRequests {
			t.Fatalf("locked out: %d", response.Code)
		}
	}
	if !f.handler.throttle.Flooding(sourceOf(outside)) {
		t.Fatal("a source that ignores its 429 answers is a flood")
	}
	// Dropping its connections at accept time ends with the lockout.
	f.clock.advance(externalLimits.baseLock + time.Second)
	if f.handler.throttle.Flooding(sourceOf(outside)) {
		t.Fatal("still flooding after the lockout")
	}
}

func TestEveryUnauthenticatedRefusalClosesTheConnection(t *testing.T) {
	f := newFixture(t)
	f.pair()
	_, public := newKey(t)
	pairBody := func(code string) string {
		b, _ := json.Marshal(map[string]string{"code": code, "deviceName": "Pixel", "stepUpPublicKey": public})
		return string(b)
	}
	bad := f.bearer(strings.Repeat("f", 64))
	closed := func(name string, response interface{ Header() http.Header }, code, want int) {
		t.Helper()
		if code != want {
			t.Errorf("%s: status %d, want %d", name, code, want)
		}
		if got := response.Header().Get("Connection"); got != "close" {
			t.Errorf("%s: Connection = %q, want close", name, got)
		}
	}
	r := f.do("GET", "/api/assistant", "", nil)
	closed("no token", r, r.Code, http.StatusUnauthorized)
	r = f.do("GET", "/api/assistant", "", bad)
	closed("bad token", r, r.Code, http.StatusUnauthorized)
	r = f.do("GET", "/api/assistant", "", map[string]string{"Origin": "https://example.com"})
	closed("forbidden origin", r, r.Code, http.StatusForbidden)
	r = f.do("GET", "/remote/v1/pair", "", nil)
	closed("pair method", r, r.Code, http.StatusMethodNotAllowed)
	r = f.do("POST", "/remote/v1/pair", `not json`, nil)
	closed("pair bad body", r, r.Code, http.StatusBadRequest)
	r = f.do("POST", "/remote/v1/pair", `{"code":"x","deviceName":"","stepUpPublicKey":""}`, nil)
	closed("pair bad name", r, r.Code, http.StatusUnprocessableEntity)
	r = f.do("POST", "/remote/v1/pair", pairBody("x"), nil)
	closed("pair with no window", r, r.Code, http.StatusForbidden)
	if _, _, err := f.pairing.Open(); err != nil {
		t.Fatal(err)
	}
	r = f.do("POST", "/remote/v1/pair", pairBody(strings.Repeat("0", 32)), nil)
	closed("pair wrong code", r, r.Code, http.StatusForbidden)
	r = f.do("POST", "/remote/v1/pair", `{"code":"x","deviceName":"P","stepUpPublicKey":"nope"}`, nil)
	closed("pair bad key", r, r.Code, http.StatusUnprocessableEntity)

	f.source = outside
	lockOut(t, f)
	r = f.do("GET", "/api/assistant", "", nil)
	closed("lockout", r, r.Code, http.StatusTooManyRequests)
	r = f.do("POST", "/remote/v1/pair", pairBody("x"), nil)
	closed("pair during the lockout", r, r.Code, http.StatusTooManyRequests)

	// An authenticated client keeps its connection, whatever it is told.
	f.source = home
	for name, response := range map[string]*httptest.ResponseRecorder{
		"served":          f.authed("GET", "/api/assistant", "", nil),
		"forbidden route": f.authed("GET", "/api/settings", "", nil),
		"step-up needed":  f.authed("DELETE", "/api/assistant/requests/r1", "", nil),
		"wrong method":    f.authed("GET", "/remote/v1/challenge", "", nil),
	} {
		if got := response.Header().Get("Connection"); got != "" {
			t.Errorf("%s (%d): Connection = %q, an authenticated client keeps its connection", name, response.Code, got)
		}
	}
}

// --- pairing window --------------------------------------------------------------------------------------------

func TestExternalWrongCodesCannotCloseTheOwnersWindow(t *testing.T) {
	f := newFixture(t)
	code, _, err := f.pairing.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, public := newKey(t)
	body := func(c string) string {
		b, _ := json.Marshal(map[string]string{"code": c, "deviceName": "Pixel", "stepUpPublicKey": public})
		return string(b)
	}
	f.source = outside
	for i := range externalLimits.failures {
		if response := f.do("POST", "/remote/v1/pair", body(strings.Repeat("0", 32)), nil); errorCode(t, response) != "pairing-refused" {
			t.Fatalf("flood %d: %d %s", i+1, response.Code, response.Body)
		}
	}
	// The throttle still locks the flooding source...
	if response := f.do("POST", "/remote/v1/pair", body(code), nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("the flooding source must be locked out: %d", response.Code)
	}
	// ...and the owner's window is still open for the owner.
	f.source = home
	if response := f.do("POST", "/remote/v1/pair", body(code), nil); response.Code != http.StatusCreated {
		t.Fatalf("the window was closed by outside guesses: %d %s", response.Code, response.Body)
	}
}

func TestLocalWrongCodesStillCloseTheWindow(t *testing.T) {
	f := newFixture(t)
	code, _, _ := f.pairing.Open()
	_, public := newKey(t)
	body := func(c string) string {
		b, _ := json.Marshal(map[string]string{"code": c, "deviceName": "Pixel", "stepUpPublicKey": public})
		return string(b)
	}
	for i := range maxPairingAttempts {
		if response := f.do("POST", "/remote/v1/pair", body(strings.Repeat("0", 32)), nil); errorCode(t, response) != "pairing-refused" {
			t.Fatalf("attempt %d: %s", i+1, response.Body)
		}
	}
	if response := f.do("POST", "/remote/v1/pair", body(code), nil); errorCode(t, response) != "pairing-closed" {
		t.Fatalf("after %d wrong local codes the window is closed: %d %s", maxPairingAttempts, response.Code, response.Body)
	}
}

func TestPairingConsumeFromCountsOnlyLocalAttempts(t *testing.T) {
	clk := &clock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	p := NewPairing(clk.Now)
	code, _, _ := p.Open()
	for range 50 * maxPairingAttempts {
		if err := p.ConsumeFrom("wrong", false); !errors.Is(err, ErrPairingRefused) {
			t.Fatalf("outside guess: %v", err)
		}
	}
	if err := p.ConsumeFrom(code, false); err != nil {
		t.Fatalf("the right code from outside still pairs while the window is open: %v", err)
	}
	code, _, _ = p.Open()
	for range maxPairingAttempts {
		_ = p.ConsumeFrom("wrong", true)
	}
	if err := p.ConsumeFrom(code, true); !errors.Is(err, ErrPairingClosed) {
		t.Fatalf("local attempts close the window: %v", err)
	}
}

// --- settings: serialized load and apply -----------------------------------------------------------------------

func TestConcurrentUpdatesAndAppliesEndWithTheListenerOfTheLastSavedSettings(t *testing.T) {
	loopback := loopbackInterface(t)
	settings := &memorySettings{settings: config.RemoteSettings{Enabled: true, Interfaces: []string{loopback.Name}, Port: freePort(t)}}
	service := newTestService(t, t.TempDir(), secrets.NewMemory(), settings)
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	ports := []int{freePort(t), freePort(t), freePort(t)}
	var updaters, appliers sync.WaitGroup
	stop := make(chan struct{})
	for range 3 {
		appliers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = service.Apply()
				}
			}
		})
	}
	// Two writers change different fields: neither may lose the other's update.
	var lastPort, lastHost string
	var lastMu sync.Mutex
	updaters.Go(func() {
		for i := range 12 {
			port := ports[i%len(ports)]
			if _, err := service.UpdateSettings(SettingsUpdate{Port: &port}); err != nil {
				t.Error(err)
			}
			lastMu.Lock()
			lastPort = strconv.Itoa(port)
			lastMu.Unlock()
		}
	})
	updaters.Go(func() {
		for i := range 12 {
			host := "host" + strconv.Itoa(i) + ".example.net"
			if _, err := service.UpdateSettings(SettingsUpdate{ExternalHost: &host}); err != nil {
				t.Error(err)
			}
			lastMu.Lock()
			lastHost = host
			lastMu.Unlock()
		}
	})
	updaters.Wait()
	// The owner turns the access off last, while Applies keep ticking.
	off := false
	if _, err := service.UpdateSettings(SettingsUpdate{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	close(stop)
	appliers.Wait()

	stored, _ := settings.load()
	lastMu.Lock()
	defer lastMu.Unlock()
	if strconv.Itoa(stored.Port) != lastPort || stored.ExternalHost != lastHost || stored.Enabled {
		t.Fatalf("an update was lost: stored port %d host %q enabled %v, want port %s host %q off", stored.Port, stored.ExternalHost, stored.Enabled, lastPort, lastHost)
	}
	view, err := service.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Listening) != 0 {
		t.Fatalf("the listener was reopened with stale settings after the access was turned off: %v", view.Listening)
	}
	// And back on: it matches the stored port.
	on := true
	view, err = service.UpdateSettings(SettingsUpdate{Enabled: &on})
	if err != nil || len(view.Listening) == 0 {
		t.Fatalf("on again: %v %v", err, view.Listening)
	}
	for _, address := range view.Listening {
		if _, port, _ := net.SplitHostPort(address); port != lastPort {
			t.Fatalf("listening on %s, want port %s", address, lastPort)
		}
	}
}

// --- on-link IPv6 ----------------------------------------------------------------------------------------------

func TestOnLinkPrefixesUseOnlyTheChosenUsableInterfaces(t *testing.T) {
	prefix := func(text string) net.Addr {
		ip, network, err := net.ParseCIDR(text)
		if err != nil {
			t.Fatal(err)
		}
		network.IP = ip // an interface address: the host address with its mask
		return network
	}
	links := []linkAddrs{
		{Name: "en0", Flags: net.FlagUp, Addrs: []net.Addr{prefix("2001:db8:1:2::5/64"), prefix("192.168.1.5/24"), prefix("fe80::1/64"), prefix("fd00::5/64")}},
		{Name: "en1", Flags: net.FlagUp, Addrs: []net.Addr{prefix("2001:db8:9:9::5/64")}},                                          // not chosen
		{Name: "en2", Flags: 0, Addrs: []net.Addr{prefix("2001:db8:3:3::5/64")}},                                                   // down
		{Name: "utun3", Flags: net.FlagUp | net.FlagPointToPoint, Addrs: []net.Addr{prefix("2001:db8:4:4::5/64")}},                 // a tunnel
		{Name: "en3", Flags: net.FlagUp, Addrs: []net.Addr{prefix("2001:db8::5/32"), prefix("2001:db8:5::5/47"), prefix("::5/0")}}, // too wide
		{Name: "en4", Flags: net.FlagUp, Addrs: []net.Addr{prefix("2001:db8:6::5/48"), prefix("2001:db8:7:7::5/60")}},
	}
	got := onLinkPrefixes(links, []string{"en0", "en2", "utun3", "en3", "en4"})
	want := []netip.Prefix{netip.MustParsePrefix("2001:db8:1:2::/64"), netip.MustParsePrefix("2001:db8:6::/48"), netip.MustParsePrefix("2001:db8:7::/60")}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("prefix %d: got %v, want %v", i, got[i], want[i])
		}
	}
	if got := onLinkPrefixes(links, nil); len(got) != 0 {
		t.Fatalf("no chosen interface, no on-link network: %v", got)
	}
	// The service reads the chosen interfaces from the settings it applies.
	var asked [][]string
	s := &Service{}
	s.onLinkFn = func(chosen []string) ([]netip.Prefix, error) {
		asked = append(asked, chosen)
		return nil, nil
	}
	s.refreshOnLink([]string{"en7"})
	if len(asked) != 1 || len(asked[0]) != 1 || asked[0][0] != "en7" {
		t.Fatalf("asked %v", asked)
	}
}
