package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// The access through a tunnel (docs/REMOTE-E2E.md): a public HTTPS endpoint of the user's ngrok static domain accepts a
// WebSocket on GET /tunnel/v1, and the binary messages of that WebSocket are the raw bytes of a TLS 1.3 session between
// the phone and this Mac. Each WebSocket becomes a net.Conn that is handed to the same TLS and HTTP stack as the LAN
// listener, so the pinned certificate, the device tokens, the step-up, the throttle and the connection caps are shared.
// ngrok only ever carries ciphertext.

const (
	// tunnelPath is the only path the tunnel serves.
	tunnelPath = "/tunnel/v1"
	// tunnelReadLimit is the largest WebSocket message read: the phone forwards TCP reads, which are far smaller.
	tunnelReadLimit = 1 << 20
	// tunnelPingEvery is how often a WebSocket is pinged, and tunnelPingTimeout how long its pong may take.
	tunnelPingEvery   = 20 * time.Second
	tunnelPingTimeout = 10 * time.Second
	// tunnelIdle closes a WebSocket that carried no data for this long (the pings do not count).
	tunnelIdle = 2 * time.Minute
	// The upgrade budget of a source is a token bucket: tunnelUpgradeBurst upgrades at once, then one more every
	// tunnelUpgradeRefill, and a connection that authenticates gives its upgrade back. maxUpgradeSources bounds the
	// sources it remembers.
	tunnelUpgradeBurst  = 30
	tunnelUpgradeRefill = 20 * time.Second
	maxUpgradeSources   = 4096
	// The server that reads the upgrade requests is not the LAN's: it never reads a body, so it has short timeouts.
	edgeReadHeaderTimeout = 10 * time.Second
	edgeReadTimeout       = 15 * time.Second
	edgeWriteTimeout      = 15 * time.Second
	edgeIdleTimeout       = 30 * time.Second
	edgeMaxHeaderBytes    = 16 << 10

	// Tunnel states, as the settings show them.
	TunnelStatusOff      = "off"
	TunnelStatusStarting = "starting"
	TunnelStatusRunning  = "running"
	TunnelStatusError    = "error"

	// retryTransient is the wait before the tunnel is brought up again after a failure the network explains;
	// retryUser after one that only the user can fix (a wrong token, a domain that is not theirs, a limit): ngrok is
	// not hammered with a request that cannot succeed, and the settings stay the place to change it.
	retryTransient = 30 * time.Second
	retryUser      = 5 * time.Minute
)

// --- the provider: what brings the public endpoint online ---------------------------------------------------------

// TunnelReport tells the supervisor that the endpoint went online (again), or lost its connection to the provider,
// which is reconnecting by itself.
type TunnelReport func(online bool, err error)

// TunnelProvider brings an HTTPS endpoint online on a domain and hands back the connections it receives, as plain HTTP
// (the provider terminated the outer TLS). Listen returns once the endpoint is online, after reporting it; later
// losses and recoveries of the connection are reported through report. Cancelling ctx, or closing the listener, ends
// everything. The ngrok SDK sits behind it (NgrokTunnel), so tests use a local server instead of the network.
type TunnelProvider interface {
	Listen(ctx context.Context, domain, authtoken string, report TunnelReport) (net.Listener, error)
}

// TunnelFault says why a tunnel is not up, as far as the user can act on it.
type TunnelFault int

const (
	// FaultOther is anything that is not classified.
	FaultOther TunnelFault = iota
	// FaultNetwork is no network, or ngrok unreachable: the provider keeps trying by itself.
	FaultNetwork
	// FaultToken is an authtoken ngrok refuses.
	FaultToken
	// FaultDomain is a domain that is not reserved by, or not allowed to, the ngrok account.
	FaultDomain
	// FaultLimit is a limit of the ngrok plan (sessions, endpoints) or an endpoint that is already online elsewhere.
	FaultLimit
	// FaultAccount is an ngrok account that cannot start a tunnel (suspended, unpaid, unverified).
	FaultAccount
	// faultStopped is a tunnel that ended without being asked to.
	faultStopped
	// faultKeychain is an authtoken that could not be read from the Keychain.
	faultKeychain
)

// userFixable says whether only a change of the user's settings or of their ngrok account can fix the fault. A limit
// is not one: it often is the previous session of this very Mac, which ngrok forgets after a moment.
func (f TunnelFault) userFixable() bool {
	return f == FaultToken || f == FaultDomain || f == FaultAccount || f == faultKeychain
}

// TunnelError is a failure of the provider with the fault it was classified as.
type TunnelError struct {
	Fault TunnelFault
	Err   error
}

func (e *TunnelError) Error() string {
	if e.Err == nil {
		return "tunnel failure"
	}
	return e.Err.Error()
}

func (e *TunnelError) Unwrap() error { return e.Err }

func faultOf(err error) TunnelFault {
	var tunnelErr *TunnelError
	if errors.As(err, &tunnelErr) {
		return tunnelErr.Fault
	}
	return FaultOther
}

// tunnelProblem is what the settings say about a fault, in the interface language.
func tunnelProblem(fault TunnelFault) i18n.Message {
	switch fault {
	case FaultNetwork:
		return i18n.NewMessage("Pouple cannot reach ngrok: check the Mac’s network connection. It keeps trying.")
	case FaultToken:
		return i18n.NewMessage("ngrok refused the authtoken: copy it again from your ngrok dashboard.")
	case FaultDomain:
		return i18n.NewMessage("ngrok refused the domain: it must be a domain reserved in your ngrok account.")
	case FaultLimit:
		return i18n.NewMessage("Your ngrok account’s limit is reached, or this domain is already online elsewhere: stop the other ngrok session using it.")
	case FaultAccount:
		return i18n.NewMessage("Your ngrok account cannot start a tunnel: check its status in the ngrok dashboard.")
	case faultStopped:
		return i18n.NewMessage("The tunnel stopped unexpectedly. Pouple will try again.")
	case faultKeychain:
		return i18n.NewMessage("The ngrok authtoken could not be read from the Keychain: enter it again.")
	}
	return i18n.NewMessage("The tunnel could not start. Pouple will try again.")
}

// --- the validated settings ----------------------------------------------------------------------------------------

// ValidateTunnelDomain checks the user's ngrok static domain and returns it in its canonical form: a lowercase DNS name
// of at least two labels, never an address. An empty domain is allowed (nothing entered yet). It is strict on purpose:
// no scheme, port, path, space or wildcard, because the value ends up in the pairing payload and in the phone's URL.
func ValidateTunnelDomain(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", nil
	}
	lower := strings.ToLower(domain)
	if len(lower) > maxHostLength || !strings.Contains(lower, ".") || !validDNSName(lower) {
		return "", errInvalidHost
	}
	return lower, nil
}

// validTunnelAuthtoken says whether a value looks like an ngrok authtoken: letters, digits, underscore, hyphen or dot,
// 8 to 256 of them. ngrok itself is the judge of whether it is a real one.
func validTunnelAuthtoken(token string) bool {
	if len(token) < 8 || len(token) > 256 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// --- the gate: GET /tunnel/v1 ------------------------------------------------------------------------------------

// tunnelGate is the HTTP handler behind the provider's endpoint. It serves one path, turns a WebSocket upgrade into a
// connection for the TLS server, and nothing else.
type tunnelGate struct {
	// ctx bounds the life of the connections it hands over.
	ctx      context.Context
	conns    *chanListener
	upgrades *upgradeLimiter
	flooding func(netip.Addr) bool

	pingEvery, pingTimeout, idle time.Duration
}

func (g *tunnelGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	// Every answer but the upgrade ends the connection: nothing that is not a phone keeps one open. The upgrade's own
	// "Connection: Upgrade" replaces this one.
	h.Set("Connection", "close")
	// Anything but the upgrade path, or a request that is no WebSocket upgrade, answers 404 with no body: the endpoint
	// reveals nothing to a scanner, and such requests cost no upgrade.
	if r.URL.Path != tunnelPath || r.Method != http.MethodGet || !isWebSocketUpgrade(r) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// The phone is not a browser: a request a web page made is refused.
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	// The source is the address ngrok reports for the connection (the SDK reads it from the proxy header of ngrok's own
	// stream, which a client cannot touch), never a header the client could have sent.
	source := sourceOf(r.RemoteAddr)
	// The upgrades are counted before they are accepted: each costs a WebSocket and a TLS handshake.
	if wait, ok := g.upgrades.allow(source); !ok {
		tooManyUpgrades(w, wait)
		return
	}
	if g.flooding != nil && g.flooding(source) {
		g.upgrades.refund(source)
		tooManyUpgrades(w, time.Minute)
		return
	}
	socket, err := websocket.Accept(hijackClearer{w}, r, &websocket.AcceptOptions{})
	if err != nil {
		g.upgrades.refund(source)
		return // Accept answered the request
	}
	// NetConn lifts the read limit: it is set again, after, to the limit of the tunnel.
	netConn := websocket.NetConn(g.ctx, socket, websocket.MessageBinary)
	socket.SetReadLimit(tunnelReadLimit)
	conn := newTunnelConn(g.ctx, newDeadlineConn(netConn), socket, source, remotePort(r.RemoteAddr), g.upgrades, g.pingEvery, g.pingTimeout, g.idle)
	if !g.conns.put(conn) {
		_ = conn.Close()
		return
	}
	go conn.watch()
}

// hijackClearer clears the deadlines of the edge server when the connection is taken over: they are the server's
// timeouts for reading a request, not those of the tunnel that follows.
type hijackClearer struct{ http.ResponseWriter }

func (w hijackClearer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.SetDeadline(time.Time{})
	}
	return conn, rw, err
}

// isWebSocketUpgrade says whether a request asks for a WebSocket (RFC 6455, version 13).
func isWebSocketUpgrade(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") && headerHasToken(r.Header, "Upgrade", "websocket") &&
		r.Header.Get("Sec-WebSocket-Key") != "" && r.Header.Get("Sec-WebSocket-Version") == "13"
}

func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func tooManyUpgrades(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
	w.WriteHeader(http.StatusTooManyRequests)
}

func remotePort(remote string) int {
	if ap, err := netip.ParseAddrPort(remote); err == nil {
		return int(ap.Port())
	}
	return 0
}

// upgradeLimiter is the upgrade budget of each source (a whole /64 for IPv6): a token bucket in a bounded table. A
// source that is only unlucky (a carrier NAT shared with a scanner) gets its budget back one upgrade at a time, and
// refund gives one back as soon as a connection proves to be a paired phone.
type upgradeLimiter struct {
	now    func() time.Time
	burst  float64
	refill time.Duration
	max    int

	mu      sync.Mutex
	entries map[netip.Addr]*upgradeEntry
}

type upgradeEntry struct {
	tokens float64
	last   time.Time
}

func newUpgradeLimiter(now func() time.Time) *upgradeLimiter {
	if now == nil {
		now = time.Now
	}
	return &upgradeLimiter{now: now, burst: tunnelUpgradeBurst, refill: tunnelUpgradeRefill, max: maxUpgradeSources, entries: map[netip.Addr]*upgradeEntry{}}
}

// top brings an entry up to date: tokens come back with time, up to the burst.
func (l *upgradeLimiter) top(e *upgradeEntry, now time.Time) {
	if elapsed := now.Sub(e.last); elapsed > 0 {
		e.tokens = min(l.burst, e.tokens+float64(elapsed)/float64(l.refill))
		e.last = now
	}
}

// allow spends an upgrade, or says how long the source must wait for the next one.
func (l *upgradeLimiter) allow(source netip.Addr) (time.Duration, bool) {
	key, now := throttleKey(source), l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry == nil {
		l.makeRoom(now)
		entry = &upgradeEntry{tokens: l.burst, last: now}
		l.entries[key] = entry
	}
	l.top(entry, now)
	if entry.tokens >= 1 {
		entry.tokens--
		return 0, true
	}
	return time.Duration((1 - entry.tokens) * float64(l.refill)), false
}

// refund gives back one upgrade: the connection it was spent on authenticated.
func (l *upgradeLimiter) refund(source netip.Addr) {
	key, now := throttleKey(source), l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry := l.entries[key]; entry != nil {
		l.top(entry, now)
		entry.tokens = min(l.burst, entry.tokens+1)
	}
}

// makeRoom frees a slot when the table is full: a source whose budget is whole again is the same as an unknown one, else
// the one that was seen first goes.
func (l *upgradeLimiter) makeRoom(now time.Time) {
	if len(l.entries) < l.max {
		return
	}
	var oldest netip.Addr
	var oldestSeen time.Time
	for key, entry := range l.entries {
		l.top(entry, now)
		if entry.tokens >= l.burst {
			delete(l.entries, key)
			continue
		}
		if oldestSeen.IsZero() || entry.last.Before(oldestSeen) {
			oldest, oldestSeen = key, entry.last
		}
	}
	if len(l.entries) >= l.max {
		delete(l.entries, oldest)
	}
}

// tunnelConn is a WebSocket seen as the connection of a client: its address is the external source, and it closes
// when it is idle or its peer stops answering pings.
type tunnelConn struct {
	net.Conn
	socket *websocket.Conn
	remote net.Addr
	ctx    context.Context

	source   netip.Addr
	upgrades *upgradeLimiter
	refunded atomic.Bool

	pingEvery, pingTimeout, idle time.Duration
	lastData                     atomic.Int64 // unix nanoseconds of the last byte read or written
	done                         chan struct{}
	once                         sync.Once
}

func newTunnelConn(ctx context.Context, conn net.Conn, socket *websocket.Conn, source netip.Addr, port int, upgrades *upgradeLimiter, pingEvery, pingTimeout, idle time.Duration) *tunnelConn {
	c := &tunnelConn{Conn: conn, socket: socket, ctx: ctx, source: source, upgrades: upgrades, pingEvery: pingEvery, pingTimeout: pingTimeout, idle: idle, done: make(chan struct{})}
	if source.IsValid() {
		c.remote = net.TCPAddrFromAddrPort(netip.AddrPortFrom(source, uint16(port)))
	} else {
		c.remote = conn.RemoteAddr()
	}
	c.touch()
	return c
}

// Authenticated gives back the upgrade this connection cost its source: it proved to be a paired phone, so it must not
// eat the budget that a scanner behind the same address is draining.
func (c *tunnelConn) Authenticated() {
	if c.refunded.CompareAndSwap(false, true) {
		c.upgrades.refund(c.source)
	}
}

func (c *tunnelConn) touch() { c.lastData.Store(time.Now().UnixNano()) }

func (c *tunnelConn) RemoteAddr() net.Addr { return c.remote }

func (c *tunnelConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

func (c *tunnelConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

// Close drops the WebSocket without the closing handshake of the protocol: the TLS layer has already said goodbye by
// then, and waiting for a peer that may be gone would hold the connection for seconds (the library waits up to 5 s).
func (c *tunnelConn) Close() error {
	c.once.Do(func() { close(c.done) })
	_ = c.socket.CloseNow()
	return c.Conn.Close()
}

// watch pings the peer, which keeps the path through the provider alive and finds a peer that is gone, and closes the
// connection after tunnelIdle without data. The pong is read by the connection's own reader.
func (c *tunnelConn) watch() {
	ticker := time.NewTicker(c.pingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-c.ctx.Done():
			_ = c.Close()
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, c.lastData.Load())) > c.idle {
				_ = c.Close()
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, c.pingTimeout)
			err := c.socket.Ping(ctx)
			cancel()
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}
}

// newEdgeServer is the server that reads the upgrade requests of the provider's endpoint: short timeouts, small headers,
// and the slots of the connections that have not upgraded yet given back when they do.
func newEdgeServer(ctx context.Context, gate *tunnelGate, pending *edgeListener) *http.Server {
	return &http.Server{
		Handler: gate, ReadHeaderTimeout: edgeReadHeaderTimeout, ReadTimeout: edgeReadTimeout, WriteTimeout: edgeWriteTimeout,
		IdleTimeout: edgeIdleTimeout, MaxHeaderBytes: edgeMaxHeaderBytes,
		BaseContext: func(net.Listener) context.Context { return ctx },
		ConnState:   pending.state,
		ErrorLog:    quietLog(),
	}
}

// edgeListener caps the connections of the provider's endpoint that have not upgraded yet, in all and per source (the
// address ngrok reports): a connection that says nothing, or never finishes its request, holds a slot until the
// server's timeouts end it, and one that upgrades gives its slot back, since it is then counted by the caps of the TLS
// server.
type edgeListener struct {
	net.Listener
	limiter *connLimiter
}

func (l *edgeListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		key := throttleKey(sourceOf(conn.RemoteAddr().String()))
		if !l.limiter.acquire(key) {
			dropConn(conn)
			continue
		}
		return &edgeConn{Conn: conn, limiter: l.limiter, key: key}, nil
	}
}

// state gives the slot back when a connection is taken over by the WebSocket, or ends.
func (l *edgeListener) state(conn net.Conn, state http.ConnState) {
	if state == http.StateHijacked || state == http.StateClosed {
		if edge, ok := conn.(*edgeConn); ok {
			edge.release()
		}
	}
}

type edgeConn struct {
	net.Conn
	limiter *connLimiter
	key     netip.Addr
	once    sync.Once
}

func (c *edgeConn) release() { c.once.Do(func() { c.limiter.release(c.key) }) }

func (c *edgeConn) Close() error {
	c.release()
	return c.Conn.Close()
}

// chanListener is a net.Listener fed by a channel: the connections the gate upgrades are its accepted connections.
type chanListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	addr  net.Addr
}

func newChanListener(addr net.Addr) *chanListener {
	return &chanListener{conns: make(chan net.Conn), done: make(chan struct{}), addr: addr}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }

// put hands a connection to Accept, or says the listener is closed.
func (l *chanListener) put(conn net.Conn) bool {
	select {
	case l.conns <- conn:
		return true
	case <-l.done:
		return false
	}
}

// --- the supervisor ----------------------------------------------------------------------------------------------

// tunnelConfig is what the tunnel runs with: nothing, or a domain and its authtoken. Unreadable says that the
// authtoken is in the settings' mind but could not be read from the Keychain.
type tunnelConfig struct {
	On         bool
	Domain     string
	Authtoken  string
	Unreadable bool
}

// tunnelRuntime keeps the tunnel as the settings say: one goroutine runs the tunnels one after the other, so the old
// session is closed before a new one binds the same domain. Changing what it should run never waits for the network,
// so it can be done with the settings lock held.
type tunnelRuntime struct {
	s        *Service
	provider TunnelProvider
	// retry is the wait before a new attempt; zero is retryTransient and retryUser.
	retry time.Duration
	wake  chan struct{}

	mu        sync.Mutex
	desired   tunnelConfig
	cancelRun context.CancelFunc
	status    string
	problem   i18n.Message
	fault     TunnelFault
}

func newTunnelRuntime(s *Service, provider TunnelProvider, retry time.Duration) *tunnelRuntime {
	return &tunnelRuntime{s: s, provider: provider, retry: retry, wake: make(chan struct{}, 1), status: TunnelStatusOff}
}

// set changes what the tunnel should run; the supervisor stops the current one and starts the new one.
func (t *tunnelRuntime) set(cfg tunnelConfig) {
	t.mu.Lock()
	if cfg == t.desired {
		t.mu.Unlock()
		return
	}
	t.desired = cfg
	if t.cancelRun != nil {
		t.cancelRun()
	}
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// view is the state the settings show, with its problem in the interface language.
func (t *tunnelRuntime) view(lang i18n.Lang) (status, problem string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status, t.problem.Text(lang)
}

// setState records the state of the run that is current: a run that was stopped says nothing more.
func (t *tunnelRuntime) setState(run context.Context, status string, problem i18n.Message, fault TunnelFault) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if run != nil && run.Err() != nil {
		return
	}
	t.status, t.problem, t.fault = status, problem, fault
}

func (t *tunnelRuntime) loop(life context.Context) {
	for {
		select { // a token left by a change that this iteration already reads would only repeat it
		case <-t.wake:
		default:
		}
		t.mu.Lock()
		cfg := t.desired
		var run context.Context
		if cfg.On {
			var cancel context.CancelFunc
			run, cancel = context.WithCancel(life)
			t.cancelRun = cancel
			t.status, t.problem, t.fault = TunnelStatusStarting, i18n.Message{}, FaultOther
		} else if cfg.Unreadable {
			t.status, t.problem, t.fault = TunnelStatusError, tunnelProblem(faultKeychain), faultKeychain
		} else {
			t.status, t.problem, t.fault = TunnelStatusOff, i18n.Message{}, FaultOther
		}
		t.mu.Unlock()

		if !cfg.On {
			select {
			case <-t.wake:
				continue
			case <-life.Done():
				return
			}
		}
		err := t.run(run, cfg)
		t.mu.Lock()
		superseded := run.Err() != nil // stopped by a change of the settings, or by the end of the service
		if t.cancelRun != nil {
			t.cancelRun()
			t.cancelRun = nil
		}
		t.mu.Unlock()
		if life.Err() != nil {
			return
		}
		if superseded {
			continue // the settings changed: run the new ones at once
		}
		// The run ended by itself: a fault the provider already explained to the user stays, else it is the error of
		// the run that is shown.
		fault := faultOf(err)
		t.mu.Lock()
		if t.status != TunnelStatusError || !t.fault.userFixable() {
			t.status, t.problem, t.fault = TunnelStatusError, tunnelProblem(fault), fault
		}
		wait := t.waitFor(t.fault)
		t.mu.Unlock()
		if err != nil {
			t.s.logTunnel(cfg, err)
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-t.wake:
		case <-life.Done():
			timer.Stop()
			return
		}
		timer.Stop()
	}
}

func (t *tunnelRuntime) waitFor(fault TunnelFault) time.Duration {
	if t.retry > 0 {
		return t.retry
	}
	if fault.userFixable() {
		return retryUser
	}
	return retryTransient
}

// run brings the endpoint online and serves it until ctx ends or the endpoint fails. Its connections go to the TLS and
// HTTP servers built like the LAN listeners', behind the same caps.
func (t *tunnelRuntime) run(ctx context.Context, cfg tunnelConfig) error {
	s := t.s
	cert, err := s.certificate()
	if err != nil {
		return &TunnelError{Fault: FaultOther, Err: err}
	}
	report := func(online bool, err error) {
		if online {
			t.setState(ctx, TunnelStatusRunning, i18n.Message{}, FaultOther)
			return
		}
		fault := faultOf(err)
		t.setState(ctx, TunnelStatusError, tunnelProblem(fault), fault)
	}
	listener, err := t.provider.Listen(ctx, cfg.Domain, cfg.Authtoken, report)
	if err != nil {
		return err
	}
	closeListener := sync.OnceFunc(func() { _ = listener.Close() })
	defer closeListener()

	serving, stop := context.WithCancel(ctx)
	defer stop()
	conns := newChanListener(listener.Addr())
	gate := &tunnelGate{
		ctx: serving, conns: conns, upgrades: newUpgradeLimiter(s.now), flooding: s.handler.throttle.Flooding,
		pingEvery: tunnelPingEvery, pingTimeout: tunnelPingTimeout, idle: tunnelIdle,
	}
	if s.tunnelTiming != nil {
		s.tunnelTiming(gate)
	}
	tlsConfig := s.tlsConfig(cert)
	secure := s.newServer(serving, tlsConfig, true)
	// The server that reads the upgrade requests has no caps of the LAN's behind it yet: it has its own, on the
	// connections that did not upgrade, and short timeouts.
	pending := &edgeListener{Listener: listener, limiter: newConnLimiter(0, 0, nil)}
	edge := newEdgeServer(serving, gate, pending)
	failed := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		// The caps apply to the tunnel like to a LAN listener, before any TLS byte is read.
		failed <- secure.Serve(tls.NewListener(&limitListener{Listener: conns, limiter: s.limiter}, tlsConfig))
	})
	wg.Go(func() { failed <- edge.Serve(pending) })

	var ended error
	select {
	case <-ctx.Done():
	case ended = <-failed:
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, server := range []*http.Server{edge, secure} {
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}
	closeListener()
	wg.Wait()
	if ctx.Err() != nil || errors.Is(ended, http.ErrServerClosed) {
		return nil
	}
	return &TunnelError{Fault: faultStopped, Err: ended}
}
