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
	// OnLink lists the IPv6 networks the Mac is attached to; nil reads the system's.
	OnLink func() ([]netip.Prefix, error)
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
	onLinkFn   func() ([]netip.Prefix, error)
	onLink     atomic.Pointer[[]netip.Prefix]
	publicAddr PublicAddress

	// life ends the detections in the background when the service shuts down; bg waits for them.
	life       context.Context
	lifeCancel context.CancelFunc
	bg         sync.WaitGroup

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
	s.handler = NewHandler(handlerOptions)
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

// addressesOf are the addresses to bind for the chosen interfaces, sorted; never an unspecified address.
func (s *Service) addressesOf(chosen []string) ([]string, error) {
	available, err := s.interfaces()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, iface := range available {
		if !slices.Contains(chosen, iface.Name) {
			continue
		}
		for _, address := range iface.Addresses {
			if ip := net.ParseIP(address); ip != nil && !ip.IsUnspecified() {
				out = append(out, address)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func portOf(settings config.RemoteSettings) int {
	if settings.Port == 0 {
		return DefaultPort
	}
	return settings.Port
}

// Apply stops the listeners and starts them again as the stored settings say.
func (s *Service) Apply() error {
	settings, loadErr := s.opts.Load()
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
	s.refreshOnLink()
	addresses, err := s.addressesOf(settings.Interfaces)
	if err != nil {
		s.problem = err.Error()
		return err
	}
	s.resolved = addresses
	if len(addresses) == 0 {
		s.problem = "none of the chosen networks has an address"
		return nil
	}
	if s.cert == nil {
		cert, err := LoadOrCreateCertificate(s.opts.Secrets, filepath.Join(s.opts.Dir, "cert.pem"))
		if err != nil {
			s.problem = "the certificate could not be loaded: " + err.Error()
			return err
		}
		s.cert = &cert
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{s.cert.TLS}, MinVersion: tls.VersionTLS13}
	port := strconv.Itoa(portOf(settings))
	var problems []string
	for _, address := range addresses {
		listener, err := net.Listen("tcp", net.JoinHostPort(address, port))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		server := &http.Server{
			Handler: s.handler, TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10,
			BaseContext: func(net.Listener) context.Context { return ctx },
			ErrorLog:    log.New(withoutHandshakeNoise{}, "", log.LstdFlags),
		}
		s.servers = append(s.servers, server)
		s.listening = append(s.listening, listener.Addr().String())
		// Without the external access the listener only talks to local networks: any other source is dropped before the
		// TLS handshake. With it, a router forwards a port, so any source is accepted and the pinned certificate, the
		// tokens and the throttle are what protect the Mac.
		var accepting net.Listener = listener
		if !settings.ExternalEnabled {
			accepting = &sourceFilter{Listener: listener, allow: s.local}
		}
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
	addresses, err := s.addressesOf(settings.Interfaces)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	view := SettingsView{
		Enabled: settings.Enabled, Interfaces: nonNil(settings.Interfaces), Port: portOf(settings), Available: available,
		Listening: nonNil(slices.Clone(s.listening)), Problem: s.problem,
		ExternalEnabled: settings.ExternalEnabled, ExternalHost: settings.ExternalHost, ExternalPort: settings.ExternalPort,
		ExternalDetected: s.detected,
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
	settings, err := s.opts.Load()
	if err != nil {
		return SettingsView{}, err
	}
	if update.Enabled != nil {
		settings.Enabled = *update.Enabled
	}
	if update.Interfaces != nil {
		chosen := []string{}
		for _, name := range *update.Interfaces {
			if !interfaceName.MatchString(name) {
				return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "“%s” is not the name of a network interface.", name)
			}
			if !slices.Contains(chosen, name) {
				chosen = append(chosen, name)
			}
		}
		settings.Interfaces = chosen
	}
	if update.Port != nil {
		if *update.Port < 1024 || *update.Port > 65535 {
			return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "The port must be between 1024 and 65535.")
		}
		settings.Port = *update.Port
	}
	if update.ExternalEnabled != nil {
		settings.ExternalEnabled = *update.ExternalEnabled
	}
	if update.ExternalHost != nil {
		host, err := ValidateExternalHost(*update.ExternalHost)
		if err != nil {
			return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "“%s” is not a valid public name or address: use a host name such as home.example.net, or an IP address, with no http://, port, path or space.", shown(*update.ExternalHost))
		}
		settings.ExternalHost = host
	}
	if update.ExternalPort != nil {
		if !validExternalPort(*update.ExternalPort) {
			return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "The external port must be 0 (the same as the port above) or between 1 and 65535.")
		}
		settings.ExternalPort = *update.ExternalPort
	}
	if settings.Enabled && len(settings.Interfaces) == 0 {
		return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "Choose the network the phone uses: Pouple never listens on every network at once.")
	}
	if settings.ExternalEnabled && settings.ExternalHost == "" {
		return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "Enter the public name or address of your home network to reach the Mac from outside.")
	}
	if err := s.opts.Save(settings); err != nil {
		return SettingsView{}, err
	}
	if err := s.Apply(); err != nil {
		log.Printf("remote access: %v", err)
	}
	s.refreshSoon()
	return s.Settings()
}

// PairingOffer is what the settings show to pair a phone: the QR code's content and when it stops working.
type PairingOffer struct {
	Payload   string    `json:"payload"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// OpenPairing opens a pairing window and returns the QR payload:
// pouple-pair:v1?hosts=<ip>,<name>.local&port=<port>&fp=<sha256 hex>&code=<code>&name=<mac name>[&ext=<host>:<port>].
// ext comes last, only while the access from outside is on and its address is known (an IPv6 literal is bracketed).
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
	return PairingOffer{Payload: "pouple-pair:v1?" + encodeOrdered(query, keys...), ExpiresAt: expires.UTC()}, nil
}

// AddressOffer is the payload that updates the address of a phone that is already paired.
type AddressOffer struct {
	Payload string `json:"payload"`
}

// AddressOffer returns pouple-address:v1?ext=<host>:<port>&fp=<sha256 hex>. It holds no secret and needs no pairing
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
	return AddressOffer{Payload: addressPayload(ext, cert.Fingerprint)}, nil
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

// refreshOnLink reads the IPv6 networks the Mac is attached to.
func (s *Service) refreshOnLink() {
	prefixes, err := s.onLinkFn()
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

// systemOnLink lists the global IPv6 networks of the interfaces that are up.
func systemOnLink() ([]netip.Prefix, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		prefix, err := netip.ParsePrefix(ipNet.String())
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || !prefix.Addr().IsGlobalUnicast() || prefix.Addr().IsPrivate() {
			continue
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
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
