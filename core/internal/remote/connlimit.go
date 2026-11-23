package remote

import (
	"net"
	"net/netip"
	"sync"
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
}

func newConnLimiter(maxTotal, maxPerSource int, flooding func(netip.Addr) bool) *connLimiter {
	if maxTotal <= 0 {
		maxTotal = defaultMaxConnections
	}
	if maxPerSource <= 0 {
		maxPerSource = defaultMaxConnectionsPerSource
	}
	return &connLimiter{max: maxTotal, perSource: maxPerSource, flooding: flooding, bySrc: map[netip.Addr]int{}}
}

// acquire takes a slot for a source, or says there is none.
func (l *connLimiter) acquire(key netip.Addr) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.max || l.bySrc[key] >= l.perSource {
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
}

func (c *limitedConn) Close() error {
	c.once.Do(func() { c.limiter.release(c.key) })
	return c.Conn.Close()
}

// dropConn closes a connection at once, with a reset: nothing is left waiting on this side.
func dropConn(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
