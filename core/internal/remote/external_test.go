package remote

import (
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestExternalHostValidation(t *testing.T) {
	long := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	cases := []struct {
		name, host, want string
		ok               bool
	}{
		{"empty is allowed (nothing entered yet)", "", "", true},
		{"spaces around are trimmed", "  home.example.net ", "home.example.net", true},
		{"dns name", "home.example.net", "home.example.net", true},
		{"dns name is lowercased", "Home.Example.NET", "home.example.net", true},
		{"ddns with digits and hyphens", "my-mac-2.duckdns.org", "my-mac-2.duckdns.org", true},
		{"punycode label", "xn--mnchen-3ya.example", "xn--mnchen-3ya.example", true},
		{"single label", "mymac", "mymac", true},
		{"auto", "auto", "auto", true},
		{"auto is case-insensitive", "AUTO", "auto", true},
		{"ipv4", "203.0.113.7", "203.0.113.7", true},
		{"ipv6", "2001:DB8::1", "2001:db8::1", true},
		{"ipv6 full form is canonical", "2001:0db8:0000:0000:0000:0000:0000:0001", "2001:db8::1", true},
		{"private address (a VPN)", "10.8.0.1", "10.8.0.1", true},
		{"max length", long, long, true},

		{"too long", long + "e", "", false},
		{"scheme", "https://home.example.net", "", false},
		{"path", "home.example.net/x", "", false},
		{"port", "home.example.net:47821", "", false},
		{"ipv4 with port", "203.0.113.7:47821", "", false},
		{"bracketed ipv6", "[2001:db8::1]", "", false},
		{"bracketed ipv6 with port", "[2001:db8::1]:47821", "", false},
		{"ipv6 zone", "fe80::1%en0", "", false},
		{"ipv4-mapped ipv6", "::ffff:203.0.113.7", "", false},
		{"inner space", "home example.net", "", false},
		{"query", "home.example.net?x=1", "", false},
		{"userinfo", "me@home.example.net", "", false},
		{"underscore", "my_mac.example.net", "", false},
		{"non-ascii", "mönchen.example", "", false},
		{"leading hyphen", "-home.example.net", "", false},
		{"trailing hyphen", "home-.example.net", "", false},
		{"empty label", "home..example.net", "", false},
		{"trailing dot", "home.example.net.", "", false},
		{"leading dot", ".example.net", "", false},
		{"label over 63", strings.Repeat("a", 64) + ".net", "", false},
		{"malformed ipv4 (out of range)", "999.1.1.1", "", false},
		{"malformed ipv4 (three parts)", "1.2.3", "", false},
		{"leading zero ipv4", "01.2.3.4", "", false},
		{"unspecified", "0.0.0.0", "", false},
		{"unspecified ipv6", "::", "", false},
		{"loopback", "127.0.0.1", "", false},
		{"loopback ipv6", "::1", "", false},
		{"link-local", "169.254.1.1", "", false},
		{"multicast", "224.0.0.1", "", false},
		{"control character", "home\n.example.net", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateExternalHost(tc.host)
			if (err == nil) != tc.ok || got != tc.want {
				t.Fatalf("ValidateExternalHost(%q) = %q, %v; want %q ok=%v", tc.host, got, err, tc.want, tc.ok)
			}
		})
	}
}

func TestExternalPortValidation(t *testing.T) {
	for port, want := range map[int]bool{-1: false, 0: true, 1: true, 443: true, 65535: true, 65536: false} {
		if validExternalPort(port) != want {
			t.Errorf("port %d: want %v", port, want)
		}
	}
}

func TestLocalSourceAddresses(t *testing.T) {
	cases := map[string]bool{
		"10.1.2.3": true, "172.16.0.1": true, "172.31.255.255": true, "192.168.1.20": true, "127.0.0.1": true, "127.5.5.5": true,
		"169.254.10.10": true, "::1": true, "fe80::1": true, "fd12:3456::1": true, "fc00::1": true,
		"::ffff:192.168.1.20": true, "::ffff:203.0.113.7": false,
		"172.15.0.1": false, "172.32.0.1": false, "192.169.1.1": false, "11.0.0.1": false, "8.8.8.8": false, "203.0.113.7": false,
		"2001:db8::1": false, "2a00:1450::1": false, "100.64.0.1": false, "fe00::1": false,
	}
	for text, want := range cases {
		if got := IsLocalAddress(netip.MustParseAddr(text)); got != want {
			t.Errorf("IsLocalAddress(%s) = %v, want %v", text, got, want)
		}
	}
	if IsLocalAddress(netip.Addr{}) {
		t.Error("an unreadable source is never local")
	}
}

func TestEndpointFormat(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"home.example.net", 47821, "home.example.net:47821"},
		{"203.0.113.7", 8443, "203.0.113.7:8443"},
		{"2001:db8::1", 47821, "[2001:db8::1]:47821"},
	} {
		if got := endpoint(tc.host, tc.port); got != tc.want {
			t.Errorf("endpoint(%s, %d) = %s, want %s", tc.host, tc.port, got, tc.want)
		}
	}
	fp := strings.Repeat("ab", 32)
	if got, want := addressPayload("[2001:db8::1]:47821", fp), "pouple-address:v1?ext=%5B2001%3Adb8%3A%3A1%5D%3A47821&fp="+fp; got != want {
		t.Errorf("address payload:\n got %s\nwant %s", got, want)
	}
	if got, want := addressPayload("home.example.net:8443", fp), "pouple-address:v1?ext=home.example.net%3A8443&fp="+fp; got != want {
		t.Errorf("address payload:\n got %s\nwant %s", got, want)
	}
}

// addrConn is a connection that claims a source address.
type addrConn struct {
	net.Conn
	remote net.Addr
	closed chan struct{}
}

func (c *addrConn) RemoteAddr() net.Addr { return c.remote }
func (c *addrConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return c.Conn.Close()
}

// queueListener hands out the connections it was given, then fails like a closed listener.
type queueListener struct {
	conns chan net.Conn
}

func (l *queueListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}
func (l *queueListener) Close() error { return nil }
func (l *queueListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 47821}
}

func TestSourceFilterDropsOutsideSourcesBeforeAnythingIsRead(t *testing.T) {
	sources := []struct {
		remote net.Addr
		kept   bool
	}{
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.50"), Port: 1}, true},
		{&net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 2}, false},
		{&net.TCPAddr{IP: net.ParseIP("10.0.0.9"), Port: 3}, true},
		{&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 4}, false},
		{&net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 5}, true},
		{&net.TCPAddr{IP: net.ParseIP("::1"), Port: 6}, true},
		{&net.UnixAddr{Name: "x", Net: "unix"}, false}, // a source that cannot be read is dropped
	}
	list := &queueListener{conns: make(chan net.Conn, len(sources))}
	var all []*addrConn
	for _, source := range sources {
		client, server := net.Pipe()
		defer client.Close()
		conn := &addrConn{Conn: server, remote: source.remote, closed: make(chan struct{})}
		all = append(all, conn)
		list.conns <- conn
	}
	close(list.conns)

	filtered := &sourceFilter{Listener: list, allow: IsLocalAddress}
	var accepted []net.Addr
	for {
		conn, err := filtered.Accept()
		if err != nil {
			break
		}
		accepted = append(accepted, conn.RemoteAddr())
	}
	wantKept := 0
	for i, source := range sources {
		select {
		case <-all[i].closed:
			if source.kept {
				t.Errorf("%s was dropped", source.remote)
			}
		default:
			if !source.kept {
				t.Errorf("%s was accepted", source.remote)
			}
			wantKept++
		}
	}
	if len(accepted) != wantKept {
		t.Fatalf("accepted %d connections, want %d", len(accepted), wantKept)
	}
}

func TestServiceTreatsOnLinkIPv6AsLocal(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8:abcd:12::/64")
	s := &Service{}
	s.onLinkFn = func([]string) ([]netip.Prefix, error) { return []netip.Prefix{prefix}, nil }
	if s.isLocalSource(netip.MustParseAddr("2001:db8:abcd:12::77")) {
		t.Fatal("nothing is on-link before the networks are read")
	}
	s.refreshOnLink(nil)
	for text, want := range map[string]bool{
		"2001:db8:abcd:12::77": true, "2001:db8:abcd:13::77": false, "192.168.1.5": true, "203.0.113.7": false, "2001:db8::1": false,
	} {
		if got := s.isLocalSource(netip.MustParseAddr(text)); got != want {
			t.Errorf("isLocalSource(%s) = %v, want %v", text, got, want)
		}
	}
	if _, err := systemOnLink(nil); err != nil {
		t.Fatal(err)
	}
}
