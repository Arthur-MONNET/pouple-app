package remote

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// ExternalAuto is the external host that asks the core to detect its own public IPv4 address (see stun.go). It is
// reserved: a host name of that exact spelling cannot be used.
const ExternalAuto = "auto"

// maxHostLength is the longest DNS name (RFC 1035), without the trailing dot.
const maxHostLength = 253

// errInvalidHost is a public host that is neither a DNS name nor an acceptable IP address.
var errInvalidHost = errors.New("invalid external host")

// ValidateExternalHost checks the public name or address the phone uses from outside and returns it in its canonical
// form: a lowercase DNS name, an IPv4 address, an IPv6 address without brackets, or ExternalAuto. An empty host is
// allowed (nothing entered yet). It is strict on purpose: no scheme, path, port, space or zone, because the value ends
// up in the pairing payload and in the phone's connection.
func ValidateExternalHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", nil
	}
	if len(host) > maxHostLength {
		return "", errInvalidHost
	}
	lower := strings.ToLower(host)
	if lower == ExternalAuto {
		return ExternalAuto, nil
	}
	if strings.Contains(host, ":") {
		// Only a bare IPv6 literal may hold a colon: a port, a scheme or a bracket does not parse as one.
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is6() || addr.Zone() != "" || addr.Is4In6() {
			return "", errInvalidHost
		}
		if !usableExternalAddr(addr) {
			return "", errInvalidHost
		}
		return addr.String(), nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !addr.Is4() || !usableExternalAddr(addr) {
			return "", errInvalidHost
		}
		return addr.String(), nil
	}
	if !validDNSName(lower) {
		return "", errInvalidHost
	}
	return lower, nil
}

// usableExternalAddr refuses an address nobody can reach from outside the Mac: unspecified, loopback, link-local and
// multicast. A private address is allowed (a VPN, or a test from another room).
func usableExternalAddr(addr netip.Addr) bool {
	return !(addr.IsUnspecified() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsLinkLocalMulticast())
}

// validDNSName is a host name of letters, digits and hyphens, in labels of 1 to 63 characters that neither start nor
// end with a hyphen. A last label of digits only is refused: it is a malformed IPv4 address, not a name.
func validDNSName(name string) bool {
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	last := labels[len(labels)-1]
	return strings.Trim(last, "0123456789") != ""
}

// validExternalPort is 0 (the listening port) or a TCP port.
func validExternalPort(port int) bool { return port >= 0 && port <= 65535 }

// endpoint is "host:port" for the pairing payload and the endpoint route: an IPv6 literal is bracketed.
func endpoint(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// addressPayload is the lightweight payload that updates the address of a phone that is already paired:
// pouple-address:v1?ext=<host>:<port>&fp=<certificate sha256 hex>. It holds no secret.
func addressPayload(ext, fingerprint string) string {
	query := url.Values{}
	query.Set("ext", ext)
	query.Set("fp", fingerprint)
	return "pouple-address:v1?" + encodeOrdered(query, "ext", "fp")
}

// IsLocalAddress says whether a source address is on a local network: loopback, private (RFC 1918 and the IPv6 unique
// local range fc00::/7) or link-local (169.254.0.0/16, fe80::/10).
func IsLocalAddress(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	return addr.IsValid() && (addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast())
}

// sourceOf is the address a request or a connection comes from; the zero Addr when it cannot be read.
func sourceOf(remote string) netip.Addr {
	if ap, err := netip.ParseAddrPort(remote); err == nil {
		return ap.Addr().Unmap().WithZone("")
	}
	if addr, err := netip.ParseAddr(remote); err == nil {
		return addr.Unmap().WithZone("")
	}
	return netip.Addr{}
}

// sourceFilter is a listener that drops, before any TLS byte is read, the connections whose source address is not
// allowed. A source that cannot be read is dropped too.
type sourceFilter struct {
	net.Listener
	allow func(netip.Addr) bool
}

func (l *sourceFilter) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if source := sourceOf(conn.RemoteAddr().String()); source.IsValid() && l.allow(source) {
			return conn, nil
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0) // reset at once: nothing is left waiting on this side
		}
		_ = conn.Close()
	}
}
