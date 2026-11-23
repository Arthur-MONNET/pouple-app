package remote

import (
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func newTestThrottle(max int) (*Throttle, *clock) {
	c := &clock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	return NewThrottle(ThrottleOptions{Now: c.Now, MaxEntries: max}), c
}

var (
	external = netip.MustParseAddr("203.0.113.7")
	lan      = netip.MustParseAddr("192.168.1.50")
)

func failN(t *Throttle, source netip.Addr, n int) (last time.Duration) {
	for range n {
		last = t.Fail(source)
	}
	return last
}

func TestThrottleLocksAnExternalSourceAfterFewFailuresAndTheLockoutGrows(t *testing.T) {
	th, c := newTestThrottle(0)
	if failN(th, external, externalLimits.failures-1) != 0 {
		t.Fatal("locked out too early")
	}
	if _, blocked := th.Blocked(external); blocked {
		t.Fatal("blocked before the limit")
	}
	if lock := th.Fail(external); lock != externalLimits.baseLock {
		t.Fatalf("first lockout %s, want %s", lock, externalLimits.baseLock)
	}
	if wait, blocked := th.Blocked(external); !blocked || wait != externalLimits.baseLock {
		t.Fatalf("blocked=%v wait=%s", blocked, wait)
	}
	// A failure while locked out neither extends nor restarts the lockout.
	c.advance(10 * time.Second)
	if th.Fail(external) != 0 {
		t.Fatal("a failure during a lockout must not start another")
	}
	if wait, _ := th.Blocked(external); wait != externalLimits.baseLock-10*time.Second {
		t.Fatalf("remaining %s", wait)
	}
	// Expiry.
	c.advance(externalLimits.baseLock)
	if _, blocked := th.Blocked(external); blocked {
		t.Fatal("still blocked after the lockout")
	}
	// The next lockout doubles, up to the cap.
	want := externalLimits.baseLock
	for strike := 2; strike <= 10; strike++ {
		want = min(want*2, externalLimits.maxLock)
		if lock := failN(th, external, externalLimits.failures); lock != want {
			t.Fatalf("lockout %d is %s, want %s", strike, lock, want)
		}
		c.advance(want)
	}
	if want != externalLimits.maxLock {
		t.Fatalf("the lockout must reach its cap, got %s", want)
	}
}

func TestThrottleCountsFailuresInAWindowOnly(t *testing.T) {
	th, c := newTestThrottle(0)
	failN(th, external, externalLimits.failures-1)
	c.advance(externalLimits.window + time.Second)
	if failN(th, external, externalLimits.failures-1) != 0 {
		t.Fatal("old failures must not add up with new ones")
	}
	if _, blocked := th.Blocked(external); blocked {
		t.Fatal("blocked on failures spread over two windows")
	}
}

func TestThrottleIsGenerousOnTheLocalNetworkAndSourcesAreIndependent(t *testing.T) {
	th, _ := newTestThrottle(0)
	if failN(th, lan, localLimits.failures-1) != 0 {
		t.Fatal("a local source is locked out too early")
	}
	if _, blocked := th.Blocked(lan); blocked {
		t.Fatal("blocked")
	}
	if lock := th.Fail(lan); lock != localLimits.baseLock {
		t.Fatalf("local lockout %s", lock)
	}
	if _, blocked := th.Blocked(external); blocked {
		t.Fatal("one source's lockout reached another")
	}
	if localLimits.failures <= externalLimits.failures || localLimits.baseLock >= externalLimits.baseLock || localLimits.maxLock >= externalLimits.maxLock {
		t.Fatal("the local limits must be more generous than the external ones")
	}
}

func TestThrottleGroupsIPv6ByPrefixAndIPv4MappedWithIPv4(t *testing.T) {
	th, _ := newTestThrottle(0)
	for i := range externalLimits.failures {
		th.Fail(netip.MustParseAddr(fmt.Sprintf("2001:db8:1:2::%x", i+1))) // one host rotating its addresses
	}
	if _, blocked := th.Blocked(netip.MustParseAddr("2001:db8:1:2:ffff::9")); !blocked {
		t.Fatal("a /64 is one source")
	}
	if _, blocked := th.Blocked(netip.MustParseAddr("2001:db8:1:3::1")); blocked {
		t.Fatal("another /64 is another source")
	}
	failN(th, netip.MustParseAddr("::ffff:198.51.100.9"), externalLimits.failures)
	if _, blocked := th.Blocked(netip.MustParseAddr("198.51.100.9")); !blocked {
		t.Fatal("an IPv4-mapped address is the IPv4 address")
	}
}

func TestThrottleSucceedClearsFailuresButNotALockout(t *testing.T) {
	th, _ := newTestThrottle(0)
	failN(th, external, externalLimits.failures-1)
	th.Succeed(external)
	if failN(th, external, externalLimits.failures-1) != 0 {
		t.Fatal("a success must clear the failures")
	}
	th.Fail(external) // locked
	th.Succeed(external)
	if _, blocked := th.Blocked(external); !blocked {
		t.Fatal("a success must not lift a lockout")
	}
}

func TestThrottleForgetsOldSourcesAndStaysBounded(t *testing.T) {
	th, c := newTestThrottle(8)
	for i := range 100 {
		th.Fail(netip.MustParseAddr(fmt.Sprintf("198.51.100.%d", i)))
		if th.size() > 8 {
			t.Fatalf("the table holds %d sources, the bound is 8", th.size())
		}
	}
	// A locked-out source survives a flood of one-failure sources.
	failN(th, external, externalLimits.failures)
	for i := range 100 {
		th.Fail(netip.MustParseAddr(fmt.Sprintf("198.18.0.%d", i)))
	}
	if _, blocked := th.Blocked(external); !blocked {
		t.Fatal("a flood of new addresses washed a lockout away")
	}
	if th.size() > 8 {
		t.Fatalf("size %d", th.size())
	}
	// Long after, everything is forgotten: the next lockout starts from the shortest again.
	c.advance(forgetAfter + time.Hour)
	if lock := failN(th, external, externalLimits.failures); lock != externalLimits.baseLock {
		t.Fatalf("lockout after being forgotten is %s, want %s", lock, externalLimits.baseLock)
	}
	for i := range 20 {
		th.Fail(netip.MustParseAddr(fmt.Sprintf("192.0.2.%d", i)))
	}
	c.advance(forgetAfter + time.Hour)
	th.Fail(netip.MustParseAddr("192.0.2.200"))
	for i := range 20 {
		th.Fail(netip.MustParseAddr(fmt.Sprintf("192.0.2.%d", 100+i)))
	}
	if th.size() > 8 {
		t.Fatalf("size %d after expiry", th.size())
	}
}

func TestThrottleUnreadableSourceSharesOneExternalBucket(t *testing.T) {
	th, _ := newTestThrottle(0)
	failN(th, netip.Addr{}, externalLimits.failures)
	if _, blocked := th.Blocked(netip.Addr{}); !blocked {
		t.Fatal("an unreadable source must be throttled like an external one")
	}
}
