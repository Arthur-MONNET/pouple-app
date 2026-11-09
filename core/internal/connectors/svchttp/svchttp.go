// Package svchttp is the small HTTP layer the optional connectors (GitLab, Asana, Slite) share: bounded requests
// and answers, no redirect to another host, and a dial guard for servers whose address the user typed. Tokens
// travel in headers only; nothing here ever logs a request or puts a header value in an error.
package svchttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// MaxBody is the most one answer may weigh.
	MaxBody = 2 << 20
	// Timeout bounds a whole request, connection and body included.
	Timeout = 10 * time.Second
	// maxRedirects is how many same-host redirects are followed.
	maxRedirects = 3
)

// ErrUnexpected marks an answer that is not what the API documents (not JSON, too large): usually a wrong address.
var ErrUnexpected = errors.New("unexpected answer")

// ErrRedirect is returned when a server tries to send the request to another host.
var ErrRedirect = errors.New("redirect to another host refused")

// Forbidden reports addresses a user-supplied server must never resolve to: this Mac itself, the link-local range
// (cloud metadata services live at 169.254.169.254) and addresses that mean "anywhere".
func Forbidden(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip == awsIPv6Metadata || thisNetwork.Contains(ip)
}

// thisNetwork is 0.0.0.0/8, which some systems route to this machine.
var thisNetwork = netip.MustParsePrefix("0.0.0.0/8")

// ownAddress reports whether ip is one of this Mac's own network addresses (Wi-Fi, VPN, virtual machine bridge): a
// service typed at one of them would be a program on this Mac, not a server.
func ownAddress(ip netip.Addr) bool {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		if prefix, err := netip.ParsePrefix(address.String()); err == nil && prefix.Addr().Unmap() == ip {
			return true
		}
	}
	return false
}

var awsIPv6Metadata = netip.MustParseAddr("fd00:ec2::254")

// guard runs after the name is resolved and before the connection is made, so it also stops a host name that
// points at a forbidden address (and the odd literals the system resolver accepts, such as 2130706433).
func guard(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("address refused")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || Forbidden(ip) || ownAddress(ip.Unmap()) {
		return errors.New("address refused: this Mac itself and link-local addresses are not allowed")
	}
	return nil
}

// sameHost follows a redirect only when it stays on the same host and does not leave https.
func sameHost(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errors.New("too many redirects")
	}
	first := via[0].URL
	if req.URL.Host != first.Host || (first.Scheme == "https" && req.URL.Scheme != "https") {
		return ErrRedirect
	}
	return nil
}

// NewClient is the client for a server the user chose (self-hosted GitLab): internal hosts are fine, this Mac
// and link-local addresses are not.
func NewClient() *http.Client { return newClient(guard) }

// NewPublicClient is the client for a fixed public API (Asana, Slite): same limits, no dial guard needed.
func NewPublicClient() *http.Client { return newClient(nil) }

// NewPublicClientWithTimeout is NewPublicClient for a call that legitimately takes long (an answer written on request).
func NewPublicClientWithTimeout(timeout time.Duration) *http.Client {
	client := newClient(nil)
	client.Timeout = timeout
	client.Transport.(*http.Transport).ResponseHeaderTimeout = timeout
	return client
}

func newClient(control func(network, address string, c syscall.RawConn) error) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: control}
	return &http.Client{
		Timeout:       Timeout,
		CheckRedirect: sameHost,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: Timeout,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
		},
	}
}

// StatusError is an answer with an error status. It carries no body: third-party text never reaches a message.
type StatusError struct {
	Service string
	Status  int
	message i18n.Message
}

func (e *StatusError) Error() string                { return e.message.Text(i18n.EN) }
func (e *StatusError) ErrorText(l i18n.Lang) string { return e.message.Text(l) }

// Status returns the HTTP status of err when it is a StatusError, else 0.
func Status(err error) int {
	var status *StatusError
	if errors.As(err, &status) {
		return status.Status
	}
	return 0
}

// Reason is a short, fixed description of why a call failed, safe for a note handed to the assistant: it never
// contains an address, a header or any text from the service.
func Reason(err error) i18n.Message {
	switch status := Status(err); {
	case errors.Is(err, context.DeadlineExceeded) || timedOut(err):
		return i18n.NewMessage("timed out")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return i18n.NewMessage("access refused, check the token")
	case status == http.StatusTooManyRequests:
		return i18n.NewMessage("rate limit reached")
	case status != 0:
		return i18n.NewMessage("the service answered with an error")
	default:
		return i18n.NewMessage("could not be reached")
	}
}

func timedOut(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

// GetJSON performs a GET and decodes the answer, at most MaxBody bytes of it. The headers carry the credentials.
// It returns the response headers (pagination totals, granted scopes…).
func GetJSON(ctx context.Context, client *http.Client, service, address string, header http.Header, out any) (http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, i18n.Wrap(err, "%s could not be reached: %s", service, errors.New("invalid address"))
	}
	for key, values := range header {
		request.Header[key] = values
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		// url.Error repeats the address, which holds no credential (those are in headers) but may hold search words.
		return nil, i18n.Wrap(err, "%s could not be reached: %s", service, unwrapTransport(err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return response.Header, &StatusError{Service: service, Status: response.StatusCode, message: i18n.NewMessage("%s answered %d", service, response.StatusCode)}
	}
	limited := io.LimitReader(response.Body, MaxBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return response.Header, i18n.Wrap(err, "%s could not be reached: %s", service, unwrapTransport(err))
	}
	if len(data) > MaxBody {
		return response.Header, i18n.Wrap(ErrUnexpected, "%s answered with too much data", service)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return response.Header, i18n.Wrap(errors.Join(ErrUnexpected, err), "%s answered with something Pouple does not understand", service)
	}
	return response.Header, nil
}

// unwrapTransport drops the request address that net/http puts in front of the cause.
func unwrapTransport(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

// UnexpectedError is the error for a well-formed answer that does not say what the API should.
func UnexpectedError(service string) error {
	return i18n.Wrap(ErrUnexpected, "%s answered with something Pouple does not understand", service)
}
