package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

// DefaultPort is the remote listener's port when the user chose none. It is fixed, so a paired phone finds it again.
const DefaultPort = 47821

// addressCheck is how often the chosen interfaces' addresses are compared with the bound ones (a new DHCP lease).
const addressCheck = 30 * time.Second

var (
	// ErrDisabled is a pairing asked while remote access is off or not listening.
	ErrDisabled = i18n.NewError("Turn on access from the phone, on a network that is connected, before pairing one.")
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
}

// NewService opens the devices file and builds the handler; nothing listens before Apply.
func NewService(opts Options) (*Service, error) {
	devices, err := OpenDevices(filepath.Join(opts.Dir, "devices.json"), opts.Now)
	if err != nil {
		return nil, fmt.Errorf("remote devices: %w", err)
	}
	s := &Service{opts: opts, devices: devices, pairing: NewPairing(opts.Now), nonces: NewNonces(opts.Now), interfaces: opts.Interfaces, hostname: opts.Hostname}
	if s.interfaces == nil {
		s.interfaces = SystemInterfaces
	}
	if s.hostname == nil {
		s.hostname = os.Hostname
	}
	handlerOptions := opts.Handler
	handlerOptions.Devices, handlerOptions.Pairing, handlerOptions.Nonces = devices, s.pairing, s.nonces
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
	if !settings.Enabled {
		s.pairing.Cancel()
		return nil
	}
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
		go func() {
			if err := server.Serve(tls.NewListener(listener, tlsConfig)); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("remote access on %s: %v", listener.Addr(), err)
			}
		}()
	}
	s.problem = strings.Join(problems, "; ")
	if len(s.listening) > 0 {
		log.Printf("remote access listening on %s", strings.Join(s.listening, ", "))
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
	ticker := time.NewTicker(addressCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Shutdown()
			return
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
	defer s.mu.Unlock()
	s.stopped = true
	s.pairing.Cancel()
	s.stopLocked()
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
}

// SettingsUpdate changes the fields that are present.
type SettingsUpdate struct {
	Enabled    *bool     `json:"enabled"`
	Interfaces *[]string `json:"interfaces"`
	Port       *int      `json:"port"`
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
	}
	if s.cert != nil {
		view.Fingerprint = s.cert.Fingerprint
	}
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
	if settings.Enabled && len(settings.Interfaces) == 0 {
		return SettingsView{}, i18n.Wrap(ErrInvalidSettings, "Choose the network the phone uses: Pouple never listens on every network at once.")
	}
	if err := s.opts.Save(settings); err != nil {
		return SettingsView{}, err
	}
	if err := s.Apply(); err != nil {
		log.Printf("remote access: %v", err)
	}
	return s.Settings()
}

// PairingOffer is what the settings show to pair a phone: the QR code's content and when it stops working.
type PairingOffer struct {
	Payload   string    `json:"payload"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// OpenPairing opens a pairing window and returns the QR payload:
// pouple-pair:v1?hosts=<ip>,<name>.local&port=<port>&fp=<sha256 hex>&code=<code>&name=<mac name>.
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
	return PairingOffer{Payload: "pouple-pair:v1?" + encodeOrdered(query, "hosts", "port", "fp", "code", "name"), ExpiresAt: expires.UTC()}, nil
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
