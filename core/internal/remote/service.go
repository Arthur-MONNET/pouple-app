package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

// DefaultPort is the remote listener's port when the user chose none. It is fixed, so a paired phone finds it again.
const DefaultPort = 47821

// externalRefresh is how often the public address is detected again while the external host is "auto".
const externalRefresh = 5 * time.Minute

// detectTimeout bounds one detection: the two servers share it.
const detectTimeout = 6 * time.Second

// tunnelTokenSecret names the ngrok authtoken in the secrets store (the Keychain on macOS). It is never in the
// configuration, a response or a log.
const tunnelTokenSecret = "ngrok-authtoken"

// addressCheck is how often the chosen interfaces' addresses are compared with the bound ones (a new DHCP lease).
const addressCheck = 30 * time.Second

var (
	// ErrDisabled is a pairing asked while remote access is off or not listening.
	ErrDisabled = i18n.NewError("Turn on access from the phone, on a network that is connected, before pairing one.")
	// ErrNoExternalAddress is the address update asked while no external address is known.
	ErrNoExternalAddress = i18n.NewError("Turn on access from outside and enter an address, or wait for Pouple to detect it, before sharing it with the phone.")
	// ErrInvalidSettings wraps a remote setting that cannot be saved.
	ErrInvalidSettings = errors.New("invalid remote settings")
)

var interfaceName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,15}$`)

// Options configure the remote access Service.
type Options struct {
	// Dir holds devices.json and the public certificate.
	Dir string
	// Secrets keeps the TLS private key (the Keychain on macOS).
	Secrets secrets.Store
	// Load and Save read and write the remote settings in the configuration.
	Load func() (config.RemoteSettings, error)
	Save func(config.RemoteSettings) error
	// Handler is the rest of the handler's options: what it serves and how it decides on step-ups.
	Handler HandlerOptions
	// Interfaces lists the network interfaces; nil reads the system's.
	Interfaces func() ([]Interface, error)
	// Hostname is the Mac's name; nil reads the system's.
	Hostname func() (string, error)
	Now      func() time.Time
	// PublicAddress finds the Mac's public IPv4 address while the external host is "auto"; nil asks the STUN servers.
	PublicAddress PublicAddress
	// Local says whether a source address is on a local network; nil is IsLocalAddress, plus the IPv6 networks the Mac
	// is attached to (a phone at home with a global IPv6 address is not an outside source).
	Local func(netip.Addr) bool
	// OnLink lists the IPv6 networks the Mac is attached to through the chosen interfaces; nil reads the system's.
	OnLink func(chosen []string) ([]netip.Prefix, error)
	// MaxConnections and MaxConnectionsPerSource cap the connections the listeners hold open, in all and per source
	// (an IPv4 address, an IPv6 /64); 0 is the default (256 and 16). Connections over a cap are dropped.
	MaxConnections          int
	MaxConnectionsPerSource int
	// Tunnel brings the public endpoint of the ngrok tunnel online; nil is ngrok's SDK (NgrokTunnel). Tests give a
	// local server in its place.
	Tunnel TunnelProvider
	// TunnelRetry is the wait before the tunnel is brought up again after a failure; 0 is 30 seconds, or 5 minutes for
	// a fault only the user can fix.
	TunnelRetry time.Duration
}

// Interface is a network interface that can be chosen, with its usable addresses.
type Interface struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Loopback  bool     `json:"loopback"`
}

// Service runs the remote listener as the settings say, and is what the loopback admin endpoints act on.
type Service struct {
	opts       Options
	devices    *Devices
	pairing    *Pairing
	nonces     *Nonces
	handler    *Handler
	interfaces func() ([]Interface, error)
	hostname   func() (string, error)
	now        func() time.Time
	local      func(netip.Addr) bool
	onLinkFn   func(chosen []string) ([]netip.Prefix, error)
	onLink     atomic.Pointer[[]netip.Prefix]
	publicAddr PublicAddress
	// limiter holds the caps on the connections of the LAN listeners and of the tunnel together: the caps are the Mac's.
	limiter *connLimiter
	// tunnel keeps the ngrok tunnel as the settings say, apart from the LAN listeners: an address change of the Mac must
	// not drop the tunnel's session.
	tunnel *tunnelRuntime
	// tunnelTiming lets a test shorten the pings and the idle limit of the tunnel's connections.
	tunnelTiming func(*tunnelGate)
	// tokenSet caches whether the ngrok authtoken is in the secrets store: 0 not read yet, 1 set, 2 not set.
	tokenSet atomic.Int32

	// life ends the detections in the background when the service shuts down; bg waits for them.
	life       context.Context
	lifeCancel context.CancelFunc
	bg         sync.WaitGroup

	// cfgMu serializes the changes of the settings and their application: loading the settings and applying them is
	// one step, so a stale Apply (the address ticker) never reopens a listener the owner just closed, and two updates
	// never overwrite each other. It is taken before mu.
	cfgMu sync.Mutex

	mu        sync.Mutex
	cert      *Certificate
	servers   []*http.Server
	cancel    context.CancelFunc
	listening []string
	// resolved are the chosen interfaces' addresses at the last Apply, bound or not: a change of them re-applies, a
	// port that stays busy does not.
	resolved []string
	problem  string
	stopped  bool
	// detected is the public IPv4 address found for the "auto" external host, and when; empty while unknown.
	detected   string
	detectedAt time.Time
}

// NewService opens the devices file and builds the handler; nothing listens before Apply.
func NewService(opts Options) (*Service, error) {
	devices, err := OpenDevices(filepath.Join(opts.Dir, "devices.json"), opts.Now)
	if err != nil {
		return nil, fmt.Errorf("remote devices: %w", err)
	}
	s := &Service{opts: opts, devices: devices, pairing: NewPairing(opts.Now), nonces: NewNonces(opts.Now), interfaces: opts.Interfaces, hostname: opts.Hostname,
		now: opts.Now, local: opts.Local, onLinkFn: opts.OnLink, publicAddr: opts.PublicAddress}
	s.life, s.lifeCancel = context.WithCancel(context.Background())
	if s.now == nil {
		s.now = time.Now
	}
	if s.local == nil {
		s.local = s.isLocalSource
	}
	if s.onLinkFn == nil {
		s.onLinkFn = systemOnLink
	}
	if s.publicAddr == nil {
		s.publicAddr = STUNPublicAddress(stunServers)
	}
	if s.interfaces == nil {
		s.interfaces = SystemInterfaces
	}
	if s.hostname == nil {
		s.hostname = os.Hostname
	}
	handlerOptions := opts.Handler
	handlerOptions.Devices, handlerOptions.Pairing, handlerOptions.Nonces = devices, s.pairing, s.nonces
	handlerOptions.Local, handlerOptions.Endpoint, handlerOptions.Now = s.local, s.currentEndpoint, opts.Now
	handlerOptions.Tunnel = s.currentTunnel
	s.handler = NewHandler(handlerOptions)
	s.limiter = newConnLimiter(opts.MaxConnections, opts.MaxConnectionsPerSource, s.handler.throttle.Flooding)
	provider := opts.Tunnel
	if provider == nil {
		provider = NgrokTunnel()
	}
	s.tunnel = newTunnelRuntime(s, provider, opts.TunnelRetry)
	s.bg.Go(func() { s.tunnel.loop(s.life) })
	return s, nil
}

// Handler is the remote API, as the listener serves it.
func (s *Service) Handler() http.Handler { return s.handler }

// SystemInterfaces lists the interfaces that are up, with their addresses: IPv4 and global IPv6 (a link-local IPv6
// needs a zone a phone cannot be given in a QR code).
func SystemInterfaces() ([]Interface, error) {
	system, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := []Interface{}
	for _, iface := range system {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		addresses := []string{}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLinkLocalUnicast() || ipNet.IP.IsMulticast() || ipNet.IP.IsUnspecified() {
				continue
			}
			addresses = append(addresses, ipNet.IP.String())
		}
		if len(addresses) > 0 {
			out = append(out, Interface{Name: iface.Name, Addresses: addresses, Loopback: iface.Flags&net.FlagLoopback != 0})
		}
	}
	return out, nil
}

// resolve is the interfaces the access listens on and their addresses to bind, sorted; never an unspecified address.
// Chosen interfaces are taken as they are, loopback included. With none chosen the choice is automatic: every interface
// that is up and has a private address (RFC 1918, or unique local for IPv6), never a loopback one.
func (s *Service) resolve(chosen []string) (names, addresses []string, err error) {
	available, err := s.interfaces()
	if err != nil {
		return nil, nil, err
	}
	automatic := len(chosen) == 0
	for _, iface := range available {
		if automatic {
			if iface.Loopback || !slices.ContainsFunc(iface.Addresses, isPrivateAddress) {
				continue
			}
		} else if !slices.Contains(chosen, iface.Name) {
			continue
		}
		names = append(names, iface.Name)
		for _, address := range iface.Addresses {
			if ip := net.ParseIP(address); ip != nil && !ip.IsUnspecified() {
				addresses = append(addresses, address)
			}
		}
	}
	slices.Sort(addresses)
	return names, slices.Compact(addresses), nil
}

// isPrivateAddress says whether an address is a private one: RFC 1918 or IPv6 unique local.
func isPrivateAddress(address string) bool {
	addr, err := netip.ParseAddr(address)
	return err == nil && addr.Unmap().IsPrivate()
}

func portOf(settings config.RemoteSettings) int {
	if settings.Port == 0 {
		return DefaultPort
	}
	return settings.Port
}

// Apply stops the listeners and starts them again as the stored settings say.
func (s *Service) Apply() error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.applyLocked()
}

// applyLocked is Apply with cfgMu held: the settings are read here, so they are the latest saved ones.
func (s *Service) applyLocked() error {
	settings, loadErr := s.opts.Load()
	// The tunnel follows the settings on its own, apart from the listeners below, and is only told here (never waited
	// for): settings that cannot be read close it like the rest.
	s.syncTunnel(settings, loadErr)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopLocked()
	s.problem, s.resolved = "", nil
	if loadErr != nil {
		// Settings that cannot be read leave the access closed, never as it was.
		s.problem = "the settings could not be read"
		s.pairing.Cancel()
		return loadErr
	}
	if !wantsDetection(settings) {
		s.detected, s.detectedAt = "", time.Time{}
	}
	if !settings.Enabled {
		s.pairing.Cancel()
		return nil
	}
	names, addresses, err := s.resolve(settings.Interfaces)
	if err != nil {
		s.problem = err.Error()
		return err
	}
	s.refreshOnLink(names)
	s.resolved = addresses
	if len(addresses) == 0 {
		s.problem = "none of the chosen networks has an address"
		if len(settings.Interfaces) == 0 {
			s.problem = "no network with a private address was found"
		}
		return nil
	}
	if err := s.loadCertLocked(); err != nil {
		s.problem = "the certificate could not be loaded: " + err.Error()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	tlsConfig := s.tlsConfig(*s.cert)
	port := strconv.Itoa(portOf(settings))
	// One limiter for all the listeners, and the tunnel: the caps hold for the Mac, not per address.
	limiter := s.limiter
	var problems []string
	for _, address := range addresses {
		listener, err := net.Listen("tcp", net.JoinHostPort(address, port))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		server := s.newServer(ctx, tlsConfig)
		s.servers = append(s.servers, server)
		s.listening = append(s.listening, listener.Addr().String())
		// Without the external access the listener only talks to local networks: any other source is dropped before the
		// TLS handshake. With it, a router forwards a port, so any source is accepted and the pinned certificate, the
		// tokens and the throttle are what protect the Mac.
		var accepting net.Listener = listener
		if !settings.ExternalEnabled {
			accepting = &sourceFilter{Listener: listener, allow: s.local}
		}
		// The limits come before the handshake too: a connection over a cap, or of a source that keeps hammering a
		// lockout, costs a reset and no TLS work. A lockout alone drops nothing here (a valid token behind the same
		// public address must still get through, see Handler.ServeHTTP): only a flood does. A connection that never
		// authenticates cannot idle: the handshake and the request headers have 10 seconds, and every refusal
		// answers "Connection: close".
		accepting = &limitListener{Listener: accepting, limiter: limiter}
		go func() {
			if err := server.Serve(tls.NewListener(accepting, tlsConfig)); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("remote access on %s: %v", listener.Addr(), err)
			}
		}()
	}
	s.problem = strings.Join(problems, "; ")
	if len(s.listening) > 0 {
		log.Printf("remote access listening on %s (external access %s)", strings.Join(s.listening, ", "), map[bool]string{true: "on", false: "off"}[settings.ExternalEnabled])
	}
	return nil
}

// loadCertLocked loads the certificate, or makes it the first time.
func (s *Service) loadCertLocked() error {
	if s.cert != nil {
		return nil
	}
	cert, err := LoadOrCreateCertificate(s.opts.Secrets, filepath.Join(s.opts.Dir, "cert.pem"))
	if err != nil {
		return err
	}
	s.cert = &cert
	return nil
}

// certificate is the certificate of the access, loaded when it is first needed.
func (s *Service) certificate() (Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadCertLocked(); err != nil {
		return Certificate{}, err
	}
	return *s.cert, nil
}

// tlsConfig is the TLS 1.3 server configuration of the LAN listeners and of the tunnel: the same certificate.
func (s *Service) tlsConfig(cert Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert.TLS}, MinVersion: tls.VersionTLS13}
}

// newServer is the HTTP server of the LAN listeners and of the tunnel: the same handler and the same timeouts.
func (s *Service) newServer(ctx context.Context, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Handler: s.handler, TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx },
		ErrorLog:    quietLog(),
	}
}

func quietLog() *log.Logger { return log.New(withoutHandshakeNoise{}, "", log.LstdFlags) }

// withoutHandshakeNoise drops the failed TLS handshakes (scanners on the network, a phone with a stale pin), which say
// nothing the user can act on, and keeps the server's other lines (a panic in a handler).
type withoutHandshakeNoise struct{}

func (withoutHandshakeNoise) Write(p []byte) (int, error) {
	if !strings.Contains(string(p), "TLS handshake error") {
		_, _ = log.Writer().Write(p)
	}
	return len(p), nil
}

// stopLocked closes the listeners: streams end with their base context, other requests get a moment to finish.
func (s *Service) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if len(s.servers) == 0 {
		s.listening = nil
		return
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, server := range s.servers {
		wg.Go(func() {
			if err := server.Shutdown(shutdown); err != nil {
				_ = server.Close()
			}
		})
	}
	wg.Wait()
	s.servers, s.listening = nil, nil
}

// Run applies the settings, then follows the chosen interfaces' addresses until ctx ends, and drains the listeners.
func (s *Service) Run(ctx context.Context) {
	if err := s.Apply(); err != nil {
		log.Printf("remote access: %v", err)
	}
	s.refreshSoon()
	ticker := time.NewTicker(addressCheck)
	defer ticker.Stop()
	detect := time.NewTicker(externalRefresh)
	defer detect.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Shutdown()
			return
		case <-detect.C:
			s.refreshSoon()
		case <-ticker.C:
			if s.addressesChanged() {
				if err := s.Apply(); err != nil {
					log.Printf("remote access: %v", err)
				}
			}
		}
	}
}

func (s *Service) addressesChanged() bool {
	settings, err := s.opts.Load()
	if err != nil || !settings.Enabled {
		return false
	}
	_, addresses, err := s.resolve(settings.Interfaces)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.stopped && !slices.Equal(s.resolved, addresses)
}

// Shutdown drains the listeners for good: a later Apply starts nothing.
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.stopped = true
	s.pairing.Cancel()
	s.stopLocked()
	s.mu.Unlock()
	s.lifeCancel()
	s.bg.Wait()
}

// SettingsView is what the settings show about remote access.
type SettingsView struct {
	Enabled    bool     `json:"enabled"`
	Interfaces []string `json:"interfaces"`
	Port       int      `json:"port"`
	// Available are the interfaces that can be chosen now.
	Available []Interface `json:"available"`
	// Listening are the addresses the listener is bound to now.
	Listening []string `json:"listening"`
	// Problem says why it does not listen as asked; empty when it does.
	Problem string `json:"problem"`
	// Fingerprint is the certificate's SHA-256, once it exists: the phone shows it after pairing.
	Fingerprint string `json:"fingerprint"`
	// ExternalEnabled, ExternalHost and ExternalPort are the access from outside the home network, as stored:
	// ExternalPort 0 means the same port as Port, and ExternalHost may be "auto".
	ExternalEnabled bool   `json:"externalEnabled"`
	ExternalHost    string `json:"externalHost"`
	ExternalPort    int    `json:"externalPort"`
	// ExternalDetected is the public IPv4 address found for the "auto" host, empty while unknown, and
	// ExternalDetectedAt when it was last confirmed (RFC 3339, empty while unknown).
	ExternalDetected   string `json:"externalDetected"`
	ExternalDetectedAt string `json:"externalDetectedAt"`
	// ExternalReady says that the Mac's side is in place: the external access is on, an address is known and the
	// listener is bound to a network address. It never tests the router or the internet: the Mac does not call any
	// outside service to check a port forward.
	ExternalReady bool `json:"externalReady"`
	// TunnelEnabled and TunnelDomain are the access through the user's ngrok static domain, as stored. The authtoken is
	// write-only: TunnelAuthtokenSet says that one is stored, and nothing returns it.
	TunnelEnabled      bool   `json:"tunnelEnabled"`
	TunnelDomain       string `json:"tunnelDomain"`
	TunnelAuthtokenSet bool   `json:"tunnelAuthtokenSet"`
	// TunnelStatus is "off", "starting", "running" or "error" (the connection to ngrok is lost, or refused), and
	// TunnelProblem says why in the interface language; empty when there is none.
	TunnelStatus  string `json:"tunnelStatus"`
	TunnelProblem string `json:"tunnelProblem"`
}

// SettingsUpdate changes the fields that are present.
type SettingsUpdate struct {
	Enabled    *bool     `json:"enabled"`
	Interfaces *[]string `json:"interfaces"`
	Port       *int      `json:"port"`
	// ExternalEnabled, ExternalHost and ExternalPort change the access from outside the home network.
	ExternalEnabled *bool   `json:"externalEnabled"`
	ExternalHost    *string `json:"externalHost"`
	ExternalPort    *int    `json:"externalPort"`
	// TunnelEnabled and TunnelDomain change the access through ngrok. TunnelAuthtoken is the ngrok authtoken: it goes
	// to the Keychain, "" removes it, and it is never echoed, returned or logged.
	TunnelEnabled   *bool   `json:"tunnelEnabled"`
	TunnelDomain    *string `json:"tunnelDomain"`
	TunnelAuthtoken *string `json:"tunnelAuthtoken"`
}

// Settings returns the stored settings and the listener's state.
func (s *Service) Settings() (SettingsView, error) {
	settings, err := s.opts.Load()
	if err != nil {
		return SettingsView{}, err
	}
	available, err := s.interfaces()
	if err != nil {
		available = []Interface{}
	}
	status, problem := s.tunnel.view(s.language())
	tokenSet := s.tokenIsSet()
	s.mu.Lock()
	defer s.mu.Unlock()
	view := SettingsView{
		Enabled: settings.Enabled, Interfaces: nonNil(settings.Interfaces), Port: portOf(settings), Available: available,
		Listening: nonNil(slices.Clone(s.listening)), Problem: s.problem,
		ExternalEnabled: settings.ExternalEnabled, ExternalHost: settings.ExternalHost, ExternalPort: settings.ExternalPort,
		ExternalDetected: s.detected,
		TunnelEnabled:    settings.TunnelEnabled, TunnelDomain: settings.TunnelDomain, TunnelAuthtokenSet: tokenSet,
		TunnelStatus: status, TunnelProblem: problem,
	}
	if s.cert != nil {
		view.Fingerprint = s.cert.Fingerprint
	}
	if !s.detectedAt.IsZero() {
		view.ExternalDetectedAt = s.detectedAt.UTC().Format(time.RFC3339)
	}
	view.ExternalReady = settings.Enabled && externalEndpoint(settings, s.detected) != "" &&
		slices.ContainsFunc(s.listening, func(address string) bool { return !isLoopback(address) })
	return view, nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// UpdateSettings checks and saves a change, then restarts the listener with it.
func (s *Service) UpdateSettings(update SettingsUpdate) (SettingsView, error) {
	if err := s.updateSettings(update); err != nil {
		return SettingsView{}, err
	}
	s.refreshSoon()
	return s.Settings()
}

// updateSettings loads, changes, saves and applies the settings as one step against the other updates and Applies.
func (s *Service) updateSettings(update SettingsUpdate) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	settings, err := s.opts.Load()
	if err != nil {
		return err
	}
	if update.Enabled != nil {
		settings.Enabled = *update.Enabled
	}
	if update.Interfaces != nil {
		chosen := []string{}
		for _, name := range *update.Interfaces {
			if !interfaceName.MatchString(name) {
				return i18n.Wrap(ErrInvalidSettings, "“%s” is not the name of a network interface.", name)
			}
			if !slices.Contains(chosen, name) {
				chosen = append(chosen, name)
			}
		}
		settings.Interfaces = chosen
	}
	if update.Port != nil {
		if *update.Port < 1024 || *update.Port > 65535 {
			return i18n.Wrap(ErrInvalidSettings, "The port must be between 1024 and 65535.")
		}
		settings.Port = *update.Port
	}
	if update.ExternalEnabled != nil {
		settings.ExternalEnabled = *update.ExternalEnabled
	}
	if update.ExternalHost != nil {
		host, err := ValidateExternalHost(*update.ExternalHost)
		if err != nil {
			return i18n.Wrap(ErrInvalidSettings, "“%s” is not a valid public name or address: use a host name such as home.example.net, or an IP address, with no http://, port, path or space.", shown(*update.ExternalHost))
		}
		settings.ExternalHost = host
	}
	if update.ExternalPort != nil {
		if !validExternalPort(*update.ExternalPort) {
			return i18n.Wrap(ErrInvalidSettings, "The external port must be 0 (the same as the port above) or between 1 and 65535.")
		}
		settings.ExternalPort = *update.ExternalPort
	}
	if update.TunnelEnabled != nil {
		settings.TunnelEnabled = *update.TunnelEnabled
	}
	if update.TunnelDomain != nil {
		domain, err := ValidateTunnelDomain(*update.TunnelDomain)
		if err != nil {
			return i18n.Wrap(ErrInvalidSettings, "“%s” is not a valid ngrok domain: use the name of your static domain, such as my-mac.ngrok-free.app, with no https://, port, path or space.", shown(*update.TunnelDomain))
		}
		settings.TunnelDomain = domain
	}
	// The authtoken is checked without ever being echoed: an error says what is wrong, never the value.
	var token *string
	if update.TunnelAuthtoken != nil {
		trimmed := strings.TrimSpace(*update.TunnelAuthtoken)
		if trimmed != "" && !validTunnelAuthtoken(trimmed) {
			return i18n.Wrap(ErrInvalidSettings, "That does not look like an ngrok authtoken: copy it again from your ngrok dashboard, without spaces.")
		}
		token = &trimmed
	}
	if settings.ExternalEnabled && settings.ExternalHost == "" {
		return i18n.Wrap(ErrInvalidSettings, "Enter the public name or address of your home network to reach the Mac from outside.")
	}
	haveToken := s.tokenIsSet()
	if token != nil {
		haveToken = *token != ""
	}
	if settings.TunnelEnabled && (settings.TunnelDomain == "" || !haveToken) {
		return i18n.Wrap(ErrInvalidSettings, "Enter your ngrok domain and authtoken to turn the tunnel on.")
	}
	if token != nil {
		if err := s.storeTunnelToken(*token); err != nil {
			return err
		}
	}
	if err := s.opts.Save(settings); err != nil {
		return err
	}
	if err := s.applyLocked(); err != nil {
		log.Printf("remote access: %v", err)
	}
	return nil
}

// PairingOffer is what the settings show to pair a phone: the QR code's content and when it stops working.
type PairingOffer struct {
	Payload   string    `json:"payload"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// OpenPairing opens a pairing window and returns the QR payload:
// pouple-pair:v1?hosts=<ip>,<name>.local&port=<port>&fp=<sha256 hex>&code=<code>&name=<mac name>[&ext=<host>:<port>][&tun=<domain>].
// ext comes after name, only while the access from outside is on and its address is known (an IPv6 literal is
// bracketed); tun comes last, only while the tunnel is on and configured.
func (s *Service) OpenPairing() (PairingOffer, error) {
	settings, err := s.opts.Load()
	if err != nil {
		return PairingOffer{}, err
	}
	s.mu.Lock()
	listening, cert := slices.Clone(s.listening), s.cert
	s.mu.Unlock()
	if !settings.Enabled || len(listening) == 0 || cert == nil {
		return PairingOffer{}, ErrDisabled
	}
	hosts := []string{}
	for _, address := range listening {
		host, _, err := net.SplitHostPort(address)
		if err == nil && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	name := "Mac"
	if hostname, err := s.hostname(); err == nil && hostname != "" {
		label, _, _ := strings.Cut(hostname, ".")
		if label != "" {
			name = label
			// The mDNS name reaches the Mac on the network, never on the loopback address alone.
			if slices.ContainsFunc(listening, func(address string) bool { return !isLoopback(address) }) {
				hosts = append(hosts, label+".local")
			}
		}
	}
	code, expires, err := s.pairing.Open()
	if err != nil {
		return PairingOffer{}, err
	}
	query := url.Values{}
	query.Set("hosts", strings.Join(hosts, ","))
	query.Set("port", strconv.Itoa(portOf(settings)))
	query.Set("fp", cert.Fingerprint)
	query.Set("code", code)
	query.Set("name", name)
	keys := []string{"hosts", "port", "fp", "code", "name"}
	if ext := s.endpointOf(settings); ext != "" {
		query.Set("ext", ext)
		keys = append(keys, "ext")
	}
	if tun := s.tunnelDomainOf(settings); tun != "" {
		query.Set("tun", tun)
		keys = append(keys, "tun")
	}
	return PairingOffer{Payload: "pouple-pair:v1?" + encodeOrdered(query, keys...), ExpiresAt: expires.UTC()}, nil
}

// AddressOffer is the payload that updates the address of a phone that is already paired.
type AddressOffer struct {
	Payload string `json:"payload"`
}

// AddressOffer returns pouple-address:v1?ext=<host>:<port>&fp=<sha256 hex>[&tun=<domain>]. It holds no secret and needs no pairing
// window: the phone only accepts the address if the certificate there has this fingerprint, so a wrong address can
// never connect it to anything else than this Mac.
func (s *Service) AddressOffer() (AddressOffer, error) {
	settings, err := s.opts.Load()
	if err != nil {
		return AddressOffer{}, err
	}
	s.mu.Lock()
	cert := s.cert
	s.mu.Unlock()
	if !settings.Enabled || cert == nil {
		return AddressOffer{}, ErrDisabled
	}
	ext := s.endpointOf(settings)
	if ext == "" {
		return AddressOffer{}, ErrNoExternalAddress
	}
	return AddressOffer{Payload: addressPayload(ext, cert.Fingerprint, s.tunnelDomainOf(settings))}, nil
}

// externalEndpoint is the "host:port" the phone uses from outside, or "" when the access is off or no address is known
// (an "auto" host that was not detected yet, or a stored host that does not validate).
func externalEndpoint(settings config.RemoteSettings, detected string) string {
	if !settings.ExternalEnabled {
		return ""
	}
	host := settings.ExternalHost
	if host == ExternalAuto {
		host = detected
	}
	if host == "" {
		return ""
	}
	if valid, err := ValidateExternalHost(host); err != nil || valid != host || host == ExternalAuto {
		return ""
	}
	port := settings.ExternalPort
	if port == 0 {
		port = portOf(settings)
	}
	return endpoint(host, port)
}

func (s *Service) endpointOf(settings config.RemoteSettings) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return externalEndpoint(settings, s.detected)
}

// currentEndpoint is the external address as the Mac knows it now, for GET /remote/v1/endpoint.
func (s *Service) currentEndpoint() string {
	settings, err := s.opts.Load()
	if err != nil {
		return ""
	}
	return s.endpointOf(settings)
}

// tunnelDomainOf is the ngrok domain the phone uses, or "" while the tunnel is off or not configured (no valid domain or
// no authtoken stored).
func (s *Service) tunnelDomainOf(settings config.RemoteSettings) string {
	if !settings.Enabled || !settings.TunnelEnabled {
		return ""
	}
	if domain, err := ValidateTunnelDomain(settings.TunnelDomain); err != nil || domain == "" || domain != settings.TunnelDomain || !s.tokenIsSet() {
		return ""
	}
	return settings.TunnelDomain
}

// currentTunnel is the tunnel's domain as the Mac knows it now, for GET /remote/v1/endpoint.
func (s *Service) currentTunnel() string {
	settings, err := s.opts.Load()
	if err != nil {
		return ""
	}
	return s.tunnelDomainOf(settings)
}

// tokenIsSet says whether an ngrok authtoken is stored. The answer is kept: the Keychain is asked once, and after each
// change made here.
func (s *Service) tokenIsSet() bool {
	switch s.tokenSet.Load() {
	case 1:
		return true
	case 2:
		return false
	}
	if s.opts.Secrets == nil {
		return false
	}
	token, err := s.opts.Secrets.Get(tunnelTokenSecret)
	switch {
	case err == nil && token != "":
		s.tokenSet.Store(1)
		return true
	case err == nil || errors.Is(err, secrets.ErrNotFound):
		s.tokenSet.Store(2)
	}
	return false
}

// storeTunnelToken writes the authtoken to the Keychain, or removes it when it is empty.
func (s *Service) storeTunnelToken(token string) error {
	if s.opts.Secrets == nil {
		return errors.New("no secrets store")
	}
	if token == "" {
		if err := s.opts.Secrets.Delete(tunnelTokenSecret); err != nil {
			return errors.New("the ngrok authtoken could not be removed from the Keychain")
		}
		s.tokenSet.Store(2)
		return nil
	}
	if err := s.opts.Secrets.Set(tunnelTokenSecret, token); err != nil {
		return errors.New("the ngrok authtoken could not be saved in the Keychain")
	}
	s.tokenSet.Store(1)
	return nil
}

// syncTunnel tells the tunnel what to run: it runs while the access and the tunnel are on, and a domain and its
// authtoken are stored. It reads the Keychain, never the network.
func (s *Service) syncTunnel(settings config.RemoteSettings, loadErr error) {
	var cfg tunnelConfig
	if domain, err := ValidateTunnelDomain(settings.TunnelDomain); loadErr == nil && settings.Enabled && settings.TunnelEnabled && err == nil && domain != "" && s.opts.Secrets != nil {
		token, err := s.opts.Secrets.Get(tunnelTokenSecret)
		switch {
		case err == nil && token != "":
			cfg = tunnelConfig{On: true, Domain: domain, Authtoken: token}
		case err != nil && !errors.Is(err, secrets.ErrNotFound):
			cfg.Unreadable = true
		}
	}
	s.tunnel.set(cfg)
}

// logTunnel writes why the tunnel stopped, without the authtoken.
func (s *Service) logTunnel(cfg tunnelConfig, err error) {
	text := err.Error()
	if cfg.Authtoken != "" {
		text = strings.ReplaceAll(text, cfg.Authtoken, "[authtoken]")
	}
	log.Printf("remote access: the tunnel on %s stopped: %s", cfg.Domain, text)
}

// language is the interface language of the messages the settings show.
func (s *Service) language() i18n.Lang {
	if s.opts.Handler.Language != nil {
		return i18n.Parse(s.opts.Handler.Language())
	}
	return i18n.FR
}

// shown cuts a value the user typed before it is echoed in a message.
func shown(value string) string {
	if runes := []rune(value); len(runes) > 64 {
		return string(runes[:64]) + "…"
	}
	return value
}

// wantsDetection says whether the public address must be looked up: only for an "auto" host, while the access and its
// external part are on. Nothing is sent to anyone otherwise.
func wantsDetection(settings config.RemoteSettings) bool {
	return settings.Enabled && settings.ExternalEnabled && settings.ExternalHost == ExternalAuto
}

// refreshSoon detects the public address in the background.
func (s *Service) refreshSoon() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.bg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.bg.Done()
		s.RefreshExternal(s.life)
	}()
}

// RefreshExternal detects the public IPv4 address when the settings ask for it. A failed detection keeps the last
// address (the phone checks the certificate at the address anyway) and its date.
func (s *Service) RefreshExternal(ctx context.Context) {
	settings, err := s.opts.Load()
	if err != nil || !wantsDetection(settings) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	found, err := s.publicAddr(ctx)
	if err != nil {
		if s.life.Err() == nil {
			log.Printf("remote access: the public address could not be detected: %v", err)
		}
		return
	}
	// The settings may have changed while it was asked: a stale answer is dropped.
	if again, err := s.opts.Load(); err != nil || !wantsDetection(again) {
		return
	}
	s.mu.Lock()
	changed := s.detected != found.String()
	s.detected, s.detectedAt = found.String(), s.now()
	s.mu.Unlock()
	if changed {
		log.Printf("remote access: the public address is %s", found)
	}
}

// refreshOnLink reads the IPv6 networks the Mac is attached to through the chosen interfaces.
func (s *Service) refreshOnLink(chosen []string) {
	prefixes, err := s.onLinkFn(chosen)
	if err != nil {
		return
	}
	s.onLink.Store(&prefixes)
}

// isLocalSource says whether a source address is on a local network: a private, loopback or link-local address, or a
// global IPv6 address inside a network the Mac is attached to (a phone on the home Wi-Fi with an IPv6 address).
func (s *Service) isLocalSource(addr netip.Addr) bool {
	if IsLocalAddress(addr) {
		return true
	}
	addr = addr.Unmap().WithZone("")
	if !addr.Is6() {
		return false
	}
	prefixes := s.onLink.Load()
	return prefixes != nil && slices.ContainsFunc(*prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(addr) })
}

// minOnLinkPrefix is the shortest IPv6 prefix that counts as "the network the Mac is attached to": normally a /64.
// A shorter one (a /32 or a /48 announced on a point of presence, a mistaken /0) would make a whole provider's
// customers look local, which weakens the throttle and lets them past the source filter.
const minOnLinkPrefix = 48

// linkAddrs is an interface with its addresses, as the system gives them.
type linkAddrs struct {
	Name  string
	Flags net.Flags
	Addrs []net.Addr
}

// systemOnLink lists the global IPv6 networks of the chosen interfaces, the ones the phone access is bound to.
func systemOnLink(chosen []string) ([]netip.Prefix, error) {
	system, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var links []linkAddrs
	for _, iface := range system {
		if !slices.Contains(chosen, iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		links = append(links, linkAddrs{Name: iface.Name, Flags: iface.Flags, Addrs: addrs})
	}
	return onLinkPrefixes(links, chosen), nil
}

// onLinkPrefixes keeps the global IPv6 prefixes (at least minOnLinkPrefix long) of the chosen interfaces that are up
// and are not point-to-point (a tunnel or a VPN is not a network the phone is attached to).
func onLinkPrefixes(links []linkAddrs, chosen []string) []netip.Prefix {
	var out []netip.Prefix
	for _, link := range links {
		if !slices.Contains(chosen, link.Name) || link.Flags&net.FlagUp == 0 || link.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		for _, addr := range link.Addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			prefix, err := netip.ParsePrefix(ipNet.String())
			if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || !prefix.Addr().IsGlobalUnicast() ||
				prefix.Addr().IsPrivate() || prefix.Bits() < minOnLinkPrefix {
				continue
			}
			out = append(out, prefix.Masked())
		}
	}
	return out
}

func isLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// encodeOrdered writes the query in the documented order (url.Values.Encode sorts the keys).
func encodeOrdered(values url.Values, keys ...string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+url.QueryEscape(values.Get(key)))
	}
	return strings.Join(parts, "&")
}

// CancelPairing closes the pairing window.
func (s *Service) CancelPairing() { s.pairing.Cancel() }

// Devices lists the paired devices.
func (s *Service) Devices() []DeviceInfo { return s.devices.List() }

// RevokeDevice ends a device's access and closes its open requests.
func (s *Service) RevokeDevice(id string) error {
	if err := s.devices.Revoke(id); err != nil {
		return err
	}
	log.Printf("remote access: revoked device %s", id)
	return nil
}
