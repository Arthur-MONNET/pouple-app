package remote

import (
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
)

const (
	// defaultMaxConnections is how many connections the remote listeners hold open at once, in all.
	defaultMaxConnections = 256
	// defaultMaxConnectionsPerSource is how many of them one source may hold (a whole /64 for IPv6, like the
	// throttle): a phone needs a handful (a stream and a few requests), a scanner opening hundreds is cut short.
	defaultMaxConnectionsPerSource = 16
)

// connLimiter counts the open connections, in all and per source, and refuses the ones over the caps. It is shared by
// the listeners of one Apply.
type connLimiter struct {
	max       int
	perSource int
	// flooding says whether a source's connections are dropped whatever the caps (see Throttle.Flooding); nil never.
	flooding func(netip.Addr) bool

	mu    sync.Mutex
	total int
	bySrc map[netip.Addr]int
	// authed counts, per source, the open connections that completed an authenticated request: a source that has one is
	// a paired phone (perhaps behind a carrier NAT shared with a scanner), and the per-source cap no longer applies to it.
	authed map[netip.Addr]int
}

func newConnLimiter(maxTotal, maxPerSource int, flooding func(netip.Addr) bool) *connLimiter {
	if maxTotal <= 0 {
		maxTotal = defaultMaxConnections
	}
	if maxPerSource <= 0 {
		maxPerSource = defaultMaxConnectionsPerSource
	}
	return &connLimiter{max: maxTotal, perSource: maxPerSource, flooding: flooding, bySrc: map[netip.Addr]int{}, authed: map[netip.Addr]int{}}
}

// acquire takes a slot for a source, or says there is none.
func (l *connLimiter) acquire(key netip.Addr) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.max || (l.bySrc[key] >= l.perSource && l.authed[key] == 0) {
		return false
	}
	l.total++
	l.bySrc[key]++
	return true
}

func (l *connLimiter) release(key netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total--
	if l.bySrc[key] <= 1 {
		delete(l.bySrc, key)
	} else {
		l.bySrc[key]--
	}
}

// setAuthenticated records that a connection of the source completed an authenticated request, or (with false) that
// such a connection ended.
func (l *connLimiter) setAuthenticated(key netip.Addr, on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case on:
		l.authed[key]++
	case l.authed[key] <= 1:
		delete(l.authed, key)
	default:
		l.authed[key]--
	}
}

// open is the number of connections held, and of sources holding them.
func (l *connLimiter) open() (connections, sources int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.total, len(l.bySrc)
}

// limitListener drops, before any TLS byte is read, the connections over the caps and the ones of a flooding source.
// A source that cannot be read shares one bucket, like in the throttle.
type limitListener struct {
	net.Listener
	limiter *connLimiter
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		var source netip.Addr
		if remote := conn.RemoteAddr(); remote != nil {
			source = sourceOf(remote.String())
		}
		if l.limiter.flooding != nil && l.limiter.flooding(source) {
			dropConn(conn)
			continue
		}
		key := throttleKey(source)
		if !l.limiter.acquire(key) {
			dropConn(conn)
			continue
		}
		return &limitedConn{Conn: conn, limiter: l.limiter, key: key}, nil
	}
}

// limitedConn gives its slot back when it is closed, once, whoever closes it (the server, the TLS layer, a failed
// handshake, a shutdown).
type limitedConn struct {
	net.Conn
	limiter *connLimiter
	key     netip.Addr
	once    sync.Once
	// authenticated is set when the connection completed a request with a valid device token.
	authenticated atomic.Bool
}

// Authenticated says that the connection completed an authenticated request: its source is exempt from the per-source
// cap while the connection is open, and the connection under it (a tunnel's) may give back what it cost.
func (c *limitedConn) Authenticated() {
	if c.authenticated.CompareAndSwap(false, true) {
		c.limiter.setAuthenticated(c.key, true)
	}
	if inner, ok := c.Conn.(interface{ Authenticated() }); ok {
		inner.Authenticated()
	}
}

func (c *limitedConn) Close() error {
	c.once.Do(func() {
		if c.authenticated.Load() {
			c.limiter.setAuthenticated(c.key, false)
		}
		c.limiter.release(c.key)
	})
	return c.Conn.Close()
}

// dropConn closes a connection at once, with a reset: nothing is left waiting on this side.
func dropConn(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
