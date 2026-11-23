package remote

import (
	"net"
	"os"
	"sync"
	"time"
)

// deadlineConn gives a WebSocket connection the read deadlines of an ordinary net.Conn.
//
// websocket.NetConn closes the whole connection when a read deadline expires during a read, which is not what
// net/http relies on: after every response the HTTP server interrupts its background read with a deadline in the past
// and then clears it, and the TLS layer treats a timeout as temporary. On a bare NetConn that closes the tunnel after
// each request. Here one goroutine reads the connection for good, never with a deadline, and Read takes what it has read
// or waits for it until its own deadline: a deadline ends a Read, never the connection. Writes and their deadlines are
// the connection's own.
type deadlineConn struct {
	net.Conn

	deadline deadline
	buf      []byte
	ready    chan pumped   // the pump hands what it read: one buffer at a time
	free     chan struct{} // the reader gives the buffer back

	closed chan struct{}
	once   sync.Once

	readMu  sync.Mutex
	pending []byte
	err     error
}

// pumped is the result of one read of the underlying connection.
type pumped struct {
	n   int
	err error
}

func newDeadlineConn(conn net.Conn) *deadlineConn {
	c := &deadlineConn{
		Conn: conn, deadline: newDeadline(), buf: make([]byte, 32<<10),
		ready: make(chan pumped), free: make(chan struct{}, 1), closed: make(chan struct{}),
	}
	go c.pump()
	return c
}

func (c *deadlineConn) pump() {
	for {
		n, err := c.Conn.Read(c.buf)
		select {
		case c.ready <- pumped{n, err}:
		case <-c.closed:
			return
		}
		if err != nil {
			return
		}
		select {
		case <-c.free:
		case <-c.closed:
			return
		}
	}
}

func (c *deadlineConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if len(c.pending) > 0 {
			n := copy(p, c.pending)
			c.pending = c.pending[n:]
			if len(c.pending) == 0 && c.err == nil {
				c.free <- struct{}{}
			}
			return n, nil
		}
		if c.err != nil {
			return 0, c.err
		}
		select {
		case <-c.closed:
			return 0, net.ErrClosed
		case <-c.deadline.wait():
			return 0, os.ErrDeadlineExceeded
		default:
		}
		select {
		case <-c.closed:
			return 0, net.ErrClosed
		case <-c.deadline.wait():
			return 0, os.ErrDeadlineExceeded
		case result := <-c.ready:
			c.pending, c.err = c.buf[:result.n], result.err
			if len(c.pending) == 0 && c.err == nil {
				c.free <- struct{}{}
			}
		}
	}
}

func (c *deadlineConn) SetReadDeadline(t time.Time) error {
	c.deadline.set(t)
	return nil
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	c.deadline.set(t)
	return c.Conn.SetWriteDeadline(t)
}

func (c *deadlineConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// deadline is a point in time that can be waited for and moved, the way net.Pipe keeps its deadlines: wait returns a
// channel that is closed once the deadline has passed.
type deadline struct {
	mu     sync.Mutex
	timer  *time.Timer
	cancel chan struct{}
}

func newDeadline() deadline { return deadline{cancel: make(chan struct{})} }

func (d *deadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil && !d.timer.Stop() {
		<-d.cancel // the timer fired: let it finish closing the channel
	}
	d.timer = nil
	expired := channelClosed(d.cancel)
	if t.IsZero() {
		if expired {
			d.cancel = make(chan struct{})
		}
		return
	}
	if wait := time.Until(t); wait > 0 {
		if expired {
			d.cancel = make(chan struct{})
		}
		cancel := d.cancel
		d.timer = time.AfterFunc(wait, func() { close(cancel) })
		return
	}
	if !expired {
		close(d.cancel)
	}
}

func (d *deadline) wait() chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cancel
}

func channelClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
