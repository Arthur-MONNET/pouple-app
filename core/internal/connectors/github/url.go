package github

import (
	"net/netip"
	"net/url"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// DefaultURL is the API of github.com.
const DefaultURL = "https://api.github.com"

// enterprisePath is where a GitHub Enterprise Server serves its REST API.
const enterprisePath = "/api/v3"

// Base is where a GitHub lives: the API the token is sent to, and the web host its pull request links point at.
type Base struct {
	// API is the API root, without a trailing slash ("https://api.github.com", "https://git.example.com/api/v3").
	API string
	// Web is the host of the web pages ("github.com", "git.example.com").
	Web string
}

// ParseBaseURL checks the address the user typed and returns where the API and the web pages are. The token will be
// sent there, so the address is held to a strict standard: https, no credentials, no query, no path other than the
// API root of a GitHub Enterprise Server, and never this Mac itself or a link-local address (cloud metadata). Private
// network hosts are allowed: an internal GitHub Enterprise is the normal case for a company. The dial guard of the
// HTTP client checks again where the name really leads.
//
// The web address of github.com (or of a GitHub Enterprise) is accepted too and turned into its API address.
func ParseBaseURL(raw string) (Base, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return Base{}, connector.Invalid("url", "This is not a valid web address. Example: https://api.github.com")
	}
	if parsed.Scheme != "https" {
		return Base{}, connector.Invalid("url", "The address must start with https:// so the token travels encrypted.")
	}
	if parsed.User != nil {
		return Base{}, connector.Invalid("url", "The address must not contain a user name or a password. The token goes in the token field.")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (path != "" && path != enterprisePath) {
		return Base{}, connector.Invalid("url", "Keep https://api.github.com, or for GitHub Enterprise give its address without any other path (for example https://github.example.com/api/v3).")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return Base{}, connector.Invalid("url", "This address points to this Mac or to a link-local address. Pouple does not connect to those.")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if svchttp.Forbidden(ip) {
			return Base{}, connector.Invalid("url", "This address points to this Mac or to a link-local address. Pouple does not connect to those.")
		}
	} else if numericName(host) {
		// "2130706433" or "0x7f.1" are addresses the system resolver would accept: they are not host names.
		return Base{}, connector.Invalid("url", "This is not a valid web address. Example: https://api.github.com")
	}
	port := ""
	if p := parsed.Port(); p != "" {
		port = ":" + p
	}
	hostPort := host
	if strings.Contains(host, ":") {
		hostPort = "[" + host + "]"
	}
	switch {
	case host == "github.com" || host == "www.github.com" || host == "api.github.com":
		// github.com has its API on its own host and no /api/v3.
		if path != "" || port != "" {
			return Base{}, connector.Invalid("url", "Keep https://api.github.com, or for GitHub Enterprise give its address without any other path (for example https://github.example.com/api/v3).")
		}
		return Base{API: DefaultURL, Web: "github.com"}, nil
	case strings.HasSuffix(host, ".ghe.com"):
		// GitHub Enterprise Cloud with data residency: the API is on api.<name>.ghe.com, without /api/v3.
		if path != "" {
			return Base{}, connector.Invalid("url", "Keep https://api.github.com, or for GitHub Enterprise give its address without any other path (for example https://github.example.com/api/v3).")
		}
		web := strings.TrimPrefix(hostPort, "api.")
		return Base{API: "https://api." + web, Web: web}, nil
	default:
		return Base{API: "https://" + hostPort + port + enterprisePath, Web: hostPort + port}, nil
	}
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
