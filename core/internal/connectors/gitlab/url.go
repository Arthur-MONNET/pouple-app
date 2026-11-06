package gitlab

import (
	"net/netip"
	"net/url"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// DefaultURL is the public GitLab.
const DefaultURL = "https://gitlab.com"

// ParseBaseURL checks the address of a GitLab server the user typed and returns its canonical form (scheme and
// host only). The token will be sent there, so the address is held to a strict standard: https, no credentials, no
// path, and never this Mac itself or a link-local address (cloud metadata). Private network hosts are allowed: an
// internal GitLab is the normal case for a company. The dial guard of the HTTP client checks again where the name
// really leads.
func ParseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return nil, connector.Invalid("url", "This is not a valid web address. Example: https://gitlab.example.com")
	}
	if parsed.Scheme != "https" {
		return nil, connector.Invalid("url", "The address must start with https:// so the token travels encrypted.")
	}
	if parsed.User != nil {
		return nil, connector.Invalid("url", "The address must not contain a user name or a password. The token goes in the token field.")
	}
	if strings.Trim(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, connector.Invalid("url", "Give the address of the GitLab server itself, without a path (for example https://gitlab.example.com).")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, connector.Invalid("url", "This address points to this Mac or to a link-local address. Pouple does not connect to those.")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if svchttp.Forbidden(ip) {
			return nil, connector.Invalid("url", "This address points to this Mac or to a link-local address. Pouple does not connect to those.")
		}
	} else if numericName(host) {
		// "2130706433" or "0x7f.1" are addresses the system resolver would accept: they are not host names.
		return nil, connector.Invalid("url", "This is not a valid web address. Example: https://gitlab.example.com")
	}
	hostPort := host
	if strings.Contains(host, ":") {
		hostPort = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" {
		hostPort += ":" + port
	}
	return &url.URL{Scheme: "https", Host: hostPort}, nil
}

// numericName reports a name whose last label is a number (decimal or 0x…): real top-level domains never are.
func numericName(host string) bool {
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if strings.HasPrefix(last, "0x") {
		return true
	}
	for _, char := range last {
		if char < '0' || char > '9' {
			return false
		}
	}
	return last != ""
}
