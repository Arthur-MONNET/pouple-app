package remote

import (
	"net/netip"
	"sync"
	"time"
)

// limits are how many failed authentications one source may make in a window before it is locked out, and how long
// the first lockout lasts; each new lockout of the same source doubles it, up to maxLock.
type limits struct {
	failures int
	window   time.Duration
	baseLock time.Duration
	maxLock  time.Duration
}

var (
	// localLimits are generous: a phone on the home network with a stale token, a mistyped code.
	localLimits = limits{failures: 30, window: 10 * time.Minute, baseLock: 30 * time.Second, maxLock: 10 * time.Minute}
	// externalLimits are tight: nobody on the internet has a reason to fail more than a handful of times.
	externalLimits = limits{failures: 6, window: 10 * time.Minute, baseLock: time.Minute, maxLock: time.Hour}
)

const (
	// defaultThrottleEntries bounds the memory of the throttle: one entry per source (per /64 for IPv6).
	defaultThrottleEntries = 4096
	// floodRefusals is how many requests a locked-out source must still send, and be refused, before its connections
	// are dropped at accept time (before the TLS handshake) until its lockout ends. A lockout alone never drops a
	// connection: the owner behind the same public address may hold a valid token, which is served whatever the
	// lockout says (see Handler.ServeHTTP). Only a source that ignores its 429 answers this many times is a flood.
	floodRefusals = 100
	// forgetAfter is how long a source that failed nothing since stays remembered: its lockouts start from the
	// shortest again after that.
	forgetAfter = 6 * time.Hour
)

type throttleEntry struct {
	failures    int
	windowStart time.Time
	strikes     int
	lockedUntil time.Time
	lastFail    time.Time
	// refused counts the unauthenticated requests refused during the current lockout.
	refused int
}

// ThrottleOptions configure a Throttle.
type ThrottleOptions struct {
	Now func() time.Time
	// Local says whether a source is on a local network (generous limits); nil is IsLocalAddress.
	Local func(netip.Addr) bool
	// MaxEntries bounds the sources remembered; 0 is the default.
	MaxEntries int
}

// Throttle slows down a source that keeps failing to authenticate (a wrong or revoked token, a wrong pairing code, a
// wrong step-up signature): after a number of failures in a window it is locked out, with a lockout that grows each
// time. Sources are counted by address (a whole /64 for IPv6, since one host holds billions of addresses), in a
// bounded table.
type Throttle struct {
	now   func() time.Time
	local func(netip.Addr) bool
	max   int

	mu      sync.Mutex
	entries map[netip.Addr]*throttleEntry
}

func NewThrottle(opts ThrottleOptions) *Throttle {
	t := &Throttle{now: opts.Now, local: opts.Local, max: opts.MaxEntries, entries: map[netip.Addr]*throttleEntry{}}
	if t.now == nil {
		t.now = time.Now
	}
	if t.local == nil {
		t.local = IsLocalAddress
	}
	if t.max <= 0 {
		t.max = defaultThrottleEntries
	}
	return t
}

// throttleKey groups the addresses of one source: an IPv4 address by itself, an IPv6 one by its /64.
func throttleKey(source netip.Addr) netip.Addr {
	source = source.Unmap().WithZone("")
	if source.Is6() {
		return netip.PrefixFrom(source, 64).Masked().Addr()
	}
	return source
}

func (t *Throttle) limitsOf(source netip.Addr) limits {
	if source.IsValid() && t.local(source) {
		return localLimits
	}
	return externalLimits
}

// Blocked says whether a source is locked out now, and for how much longer.
func (t *Throttle) Blocked(source netip.Addr) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.entries[throttleKey(source)]
	if entry == nil {
		return 0, false
	}
	if remaining := entry.lockedUntil.Sub(t.now()); remaining > 0 {
		return remaining, true
	}
	return 0, false
}

// Refused counts a request of a locked-out source that was refused without being served (the 429 answers): a source
// that keeps sending them is flooding.
func (t *Throttle) Refused(source netip.Addr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if entry := t.entries[throttleKey(source)]; entry != nil && t.now().Before(entry.lockedUntil) {
		entry.refused++
	}
}

// Flooding says whether a source is locked out now and has kept sending requests that were refused (floodRefusals of
// them): its connections can be dropped before the handshake. A source that merely failed to authenticate is not
// flooding, so a valid token behind a shared address still gets in.
func (t *Throttle) Flooding(source netip.Addr) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.entries[throttleKey(source)]
	return entry != nil && t.now().Before(entry.lockedUntil) && entry.refused >= floodRefusals
}

// Fail counts a failed authentication and returns the lockout it started, or 0.
func (t *Throttle) Fail(source netip.Addr) time.Duration {
	now, key, lim := t.now(), throttleKey(source), t.limitsOf(source)
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.entries[key]
	if entry != nil && now.Sub(entry.lastFail) > forgetAfter && !now.Before(entry.lockedUntil) {
		delete(t.entries, key)
		entry = nil
	}
	if entry == nil {
		t.makeRoom(now)
		entry = &throttleEntry{}
		t.entries[key] = entry
	}
	entry.lastFail = now
	if now.Before(entry.lockedUntil) {
		return 0 // already locked out: the lockout is not extended by what it refuses
	}
	if entry.failures == 0 || now.Sub(entry.windowStart) > lim.window {
		entry.failures, entry.windowStart = 0, now
	}
	entry.failures++
	if entry.failures < lim.failures {
		return 0
	}
	entry.strikes++
	lock := lim.baseLock
	for i := 1; i < entry.strikes && lock < lim.maxLock; i++ {
		lock *= 2
	}
	lock = min(lock, lim.maxLock)
	entry.lockedUntil = now.Add(lock)
	entry.failures, entry.refused = 0, 0
	return lock
}

// Succeed forgets the failures of a source that just authenticated, unless it is locked out. Its earlier lockouts
// still count: they decay with time only.
func (t *Throttle) Succeed(source netip.Addr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if entry := t.entries[throttleKey(source)]; entry != nil && !t.now().Before(entry.lockedUntil) {
		entry.failures = 0
	}
}

// size is the number of sources remembered.
func (t *Throttle) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// makeRoom frees one slot when the table is full: first the sources that are forgotten anyway, else the one that
// matters least (not locked out, oldest failure; failing that, the lockout that ends first). Sources locked out stay
// as long as another can go, so a flood of new addresses cannot wash a lockout away.
func (t *Throttle) makeRoom(now time.Time) {
	if len(t.entries) < t.max {
		return
	}
	var victim netip.Addr
	var victimEntry *throttleEntry
	worse := func(a, b *throttleEntry) bool { // a should go before b
		aLocked, bLocked := now.Before(a.lockedUntil), now.Before(b.lockedUntil)
		if aLocked != bLocked {
			return !aLocked
		}
		if aLocked {
			return a.lockedUntil.Before(b.lockedUntil)
		}
		return a.lastFail.Before(b.lastFail)
	}
	for key, entry := range t.entries {
		if now.Sub(entry.lastFail) > forgetAfter && !now.Before(entry.lockedUntil) {
			delete(t.entries, key)
			continue
		}
		if victimEntry == nil || worse(entry, victimEntry) {
			victim, victimEntry = key, entry
		}
	}
	if len(t.entries) >= t.max && victimEntry != nil {
		delete(t.entries, victim)
	}
}
