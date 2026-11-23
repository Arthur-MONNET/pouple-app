package remote

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type externalFixture struct {
	service  *Service
	settings *memorySettings
	port     int
	detect   *fakeDetector
}

// fakeDetector stands for the STUN client.
type fakeDetector struct {
	calls atomic.Int32
	addr  atomic.Value // netip.Addr
	fail  atomic.Bool
	block chan struct{}
}

func (d *fakeDetector) find(ctx context.Context) (netip.Addr, error) {
	d.calls.Add(1)
	if d.block != nil {
		select {
		case <-d.block:
		case <-ctx.Done():
			return netip.Addr{}, ctx.Err()
		}
	}
	if d.fail.Load() {
		return netip.Addr{}, errors.New("no answer")
	}
	return d.addr.Load().(netip.Addr), nil
}

func newExternalFixture(t *testing.T, local func(netip.Addr) bool) *externalFixture {
	t.Helper()
	loopback := loopbackInterface(t)
	settings := &memorySettings{settings: config.RemoteSettings{Enabled: true, Interfaces: []string{loopback.Name}, Port: freePort(t)}}
	f := &externalFixture{settings: settings, port: settings.settings.Port, detect: &fakeDetector{}}
	f.detect.addr.Store(netip.MustParseAddr("203.0.113.50"))
	service, err := NewService(Options{
		Dir: t.TempDir(), Secrets: secrets.NewMemory(), Load: settings.load, Save: settings.save,
		Hostname:      func() (string, error) { return "studio.local", nil },
		Now:           func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) },
		PublicAddress: f.detect.find, Local: local,
		Handler: HandlerOptions{Mount: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/assistant", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Shutdown)
	f.service = service
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	return f
}

func update(t *testing.T, s *Service, u SettingsUpdate) SettingsView {
	t.Helper()
	view, err := s.UpdateSettings(u)
	if err != nil {
		t.Fatalf("%+v: %v", u, err)
	}
	return view
}

func ptr[T any](v T) *T { return &v }

func payloadQuery(t *testing.T, payload, prefix string) (url.Values, string) {
	t.Helper()
	raw, ok := strings.CutPrefix(payload, prefix)
	if !ok {
		t.Fatalf("payload %q lacks %q", payload, prefix)
	}
	query, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return query, raw
}

func TestExternalSettingsRoundTripAndAreValidated(t *testing.T) {
	f := newExternalFixture(t, nil)
	view, _ := f.service.Settings()
	if view.ExternalEnabled || view.ExternalHost != "" || view.ExternalPort != 0 || view.ExternalDetected != "" || view.ExternalDetectedAt != "" || view.ExternalReady {
		t.Fatalf("the external access is off by default: %+v", view)
	}

	for _, bad := range []SettingsUpdate{
		{ExternalEnabled: ptr(true)}, // no host
		{ExternalEnabled: ptr(true), ExternalHost: ptr("  ")},
		{ExternalHost: ptr("https://home.example.net")},
		{ExternalHost: ptr("home.example.net:47821")},
		{ExternalHost: ptr("[2001:db8::1]")},
		{ExternalHost: ptr("home example.net")},
		{ExternalHost: ptr(strings.Repeat("a.", 130) + "net")},
		{ExternalPort: ptr(-1)},
		{ExternalPort: ptr(65536)},
	} {
		if _, err := f.service.UpdateSettings(bad); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if stored, _ := f.settings.load(); stored.ExternalEnabled || stored.ExternalHost != "" || stored.ExternalPort != 0 {
		t.Fatalf("a refused update stored something: %+v", stored)
	}

	view = update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr(" Home.Example.NET "), ExternalPort: ptr(8443)})
	if !view.ExternalEnabled || view.ExternalHost != "home.example.net" || view.ExternalPort != 8443 || !view.Enabled || view.Port != f.port {
		t.Fatalf("round trip: %+v", view)
	}
	// Only the fields that are present change.
	view = update(t, f.service, SettingsUpdate{ExternalPort: ptr(0)})
	if !view.ExternalEnabled || view.ExternalHost != "home.example.net" || view.ExternalPort != 0 {
		t.Fatalf("partial update: %+v", view)
	}
	view = update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(false)})
	if view.ExternalEnabled || view.ExternalHost != "home.example.net" {
		t.Fatalf("turning it off keeps the host: %+v", view)
	}
	// The host can be cleared while the access from outside is off.
	if view = update(t, f.service, SettingsUpdate{ExternalHost: ptr("")}); view.ExternalHost != "" {
		t.Fatalf("clearing the host: %+v", view)
	}
}

func TestOldRemoteSettingsKeepWorkingWithTheExternalAccessOff(t *testing.T) {
	f := newExternalFixture(t, nil) // Enabled, interfaces, port: what a configuration written before had
	view, err := f.service.Settings()
	if err != nil || !view.Enabled || len(view.Listening) == 0 || view.ExternalEnabled {
		t.Fatalf("old settings: %+v %v", view, err)
	}
	offer, err := f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	query, raw := payloadQuery(t, offer.Payload, "pouple-pair:v1?")
	if query.Has("ext") || strings.Contains(raw, "ext=") {
		t.Fatalf("no ext while the external access is off: %s", offer.Payload)
	}
	if _, err := f.service.AddressOffer(); !errors.Is(err, ErrNoExternalAddress) {
		t.Fatalf("address payload with no external address: %v", err)
	}
	if f.service.currentEndpoint() != "" {
		t.Fatal("no endpoint while the external access is off")
	}
}

func TestPairingPayloadCarriesExtLastOnlyWhenTheExternalAddressIsKnown(t *testing.T) {
	f := newExternalFixture(t, nil)
	order := func(raw string) []string {
		var keys []string
		for _, part := range strings.Split(raw, "&") {
			key, _, _ := strings.Cut(part, "=")
			keys = append(keys, key)
		}
		return keys
	}
	for _, tc := range []struct {
		name        string
		update      SettingsUpdate
		ext         string // decoded
		rawExt      string // as written in the payload
		extEndpoint string
	}{
		{"dns name, port of the listener", SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("home.example.net"), ExternalPort: ptr(0)},
			"home.example.net:" + strconv.Itoa(f.port), "ext=home.example.net%3A" + strconv.Itoa(f.port), ""},
		{"dns name, other external port", SettingsUpdate{ExternalPort: ptr(8443)}, "home.example.net:8443", "ext=home.example.net%3A8443", ""},
		{"ipv4", SettingsUpdate{ExternalHost: ptr("203.0.113.7")}, "203.0.113.7:8443", "ext=203.0.113.7%3A8443", ""},
		{"ipv6 is bracketed", SettingsUpdate{ExternalHost: ptr("2001:DB8::1")}, "[2001:db8::1]:8443", "ext=%5B2001%3Adb8%3A%3A1%5D%3A8443", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			update(t, f.service, tc.update)
			offer, err := f.service.OpenPairing()
			if err != nil {
				t.Fatal(err)
			}
			query, raw := payloadQuery(t, offer.Payload, "pouple-pair:v1?")
			if query.Get("ext") != tc.ext || !strings.HasSuffix(raw, "&"+tc.rawExt) {
				t.Fatalf("ext %q, want %q at the end of %s", query.Get("ext"), tc.ext, offer.Payload)
			}
			if got := strings.Join(order(raw), ","); got != "hosts,port,fp,code,name,ext" {
				t.Fatalf("parameter order: %s", got)
			}
			if query.Get("port") != strconv.Itoa(f.port) || query.Get("name") != "studio" || len(query.Get("code")) != 32 || len(query.Get("fp")) != 64 {
				t.Fatalf("the existing parameters keep their meaning: %v", query)
			}
			if f.service.currentEndpoint() != tc.ext {
				t.Fatalf("endpoint %q, want %q", f.service.currentEndpoint(), tc.ext)
			}
		})
	}
	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(false)})
	offer, err := f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	if _, raw := payloadQuery(t, offer.Payload, "pouple-pair:v1?"); strings.Contains(raw, "ext") {
		t.Fatalf("ext must disappear when the external access is off: %s", offer.Payload)
	}
}

func TestAddressPayloadHoldsTheAddressAndTheFingerprintButNoSecret(t *testing.T) {
	f := newExternalFixture(t, nil)
	view := update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("home.example.net"), ExternalPort: ptr(8443)})
	offer, err := f.service.AddressOffer()
	if err != nil {
		t.Fatal(err)
	}
	want := "pouple-address:v1?ext=home.example.net%3A8443&fp=" + view.Fingerprint
	if offer.Payload != want || len(view.Fingerprint) != 64 {
		t.Fatalf("address payload:\n got %s\nwant %s", offer.Payload, want)
	}
	query, _ := payloadQuery(t, offer.Payload, "pouple-address:v1?")
	if len(query) != 2 || query.Has("code") || query.Has("hosts") {
		t.Fatalf("the address payload holds only ext and fp: %v", query)
	}
	// No pairing window is needed, and none is opened.
	if f.service.pairing.code != "" {
		t.Fatal("the address payload must not open a pairing window")
	}
	// Off, nothing is shared.
	update(t, f.service, SettingsUpdate{Enabled: ptr(false)})
	if _, err := f.service.AddressOffer(); !errors.Is(err, ErrDisabled) {
		t.Fatalf("access off: %v", err)
	}
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAutoHostDetectsThePublicAddressAndUsesItInThePayload(t *testing.T) {
	f := newExternalFixture(t, nil)
	view := update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("AUTO")})
	if view.ExternalHost != "auto" {
		t.Fatalf("auto is stored lowercase: %q", view.ExternalHost)
	}
	waitFor(t, "the detection after the settings change", func() bool {
		v, _ := f.service.Settings()
		return v.ExternalDetected == "203.0.113.50"
	})
	view, _ = f.service.Settings()
	if view.ExternalDetectedAt != "2026-11-20T10:00:00Z" {
		t.Fatalf("detected at / ready: %+v", view)
	}
	if f.service.currentEndpoint() != "203.0.113.50:"+strconv.Itoa(f.port) {
		t.Fatalf("endpoint: %q", f.service.currentEndpoint())
	}
	offer, err := f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	if query, _ := payloadQuery(t, offer.Payload, "pouple-pair:v1?"); query.Get("ext") != "203.0.113.50:"+strconv.Itoa(f.port) {
		t.Fatalf("ext follows the detected address: %s", offer.Payload)
	}

	// The address changes: the next detection updates what the payload and the endpoint say.
	f.detect.addr.Store(netip.MustParseAddr("198.51.100.4"))
	f.service.RefreshExternal(context.Background())
	if f.service.currentEndpoint() != "198.51.100.4:"+strconv.Itoa(f.port) {
		t.Fatalf("endpoint after the change: %q", f.service.currentEndpoint())
	}
	address, err := f.service.AddressOffer()
	if err != nil || !strings.HasPrefix(address.Payload, "pouple-address:v1?ext=198.51.100.4%3A") {
		t.Fatalf("address payload after the change: %v %v", address, err)
	}

	// A failed detection keeps the last address.
	f.detect.fail.Store(true)
	f.service.RefreshExternal(context.Background())
	if v, _ := f.service.Settings(); v.ExternalDetected != "198.51.100.4" {
		t.Fatalf("a failed detection dropped the address: %+v", v)
	}

	// Another host, or the external access off: no detection, and what was found is forgotten.
	f.detect.fail.Store(false)
	update(t, f.service, SettingsUpdate{ExternalHost: ptr("home.example.net")})
	if v, _ := f.service.Settings(); v.ExternalDetected != "" || v.ExternalDetectedAt != "" {
		t.Fatalf("a detection survived a change of host: %+v", v)
	}
	before := f.detect.calls.Load()
	f.service.RefreshExternal(context.Background())
	update(t, f.service, SettingsUpdate{ExternalHost: ptr("auto"), ExternalEnabled: ptr(false)})
	f.service.RefreshExternal(context.Background())
	update(t, f.service, SettingsUpdate{Enabled: ptr(false), ExternalEnabled: ptr(true)})
	f.service.RefreshExternal(context.Background())
	if f.detect.calls.Load() != before {
		t.Fatalf("the public address was looked up while nobody needs it (%d calls)", f.detect.calls.Load()-before)
	}
}

func TestAutoHostWithoutADetectedAddressOmitsExt(t *testing.T) {
	f := newExternalFixture(t, nil)
	f.detect.fail.Store(true)
	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("auto")})
	waitFor(t, "the failed detection", func() bool { return f.detect.calls.Load() >= 1 })
	offer, err := f.service.OpenPairing()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(offer.Payload, "ext=") {
		t.Fatalf("ext must be omitted while the address is unknown: %s", offer.Payload)
	}
	if view, _ := f.service.Settings(); view.ExternalReady || view.ExternalDetected != "" {
		t.Fatalf("not ready without an address: %+v", view)
	}
	if _, err := f.service.AddressOffer(); !errors.Is(err, ErrNoExternalAddress) {
		t.Fatalf("address payload without an address: %v", err)
	}
	if f.service.currentEndpoint() != "" {
		t.Fatal("no endpoint without an address")
	}
}

func TestShutdownEndsADetectionInProgress(t *testing.T) {
	f := newExternalFixture(t, nil)
	f.detect.block = make(chan struct{}) // never released: only the end of the service stops it
	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("auto")})
	waitFor(t, "the detection to start", func() bool { return f.detect.calls.Load() >= 1 })
	done := make(chan struct{})
	go func() { f.service.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown waits for a detection that was never going to end")
	}
}

func TestTheListenerDropsOutsideSourcesUnlessTheExternalAccessIsOn(t *testing.T) {
	outsideOnly := func(addr netip.Addr) bool { return false } // every source looks like it comes from the internet
	f := newExternalFixture(t, outsideOnly)
	view, _ := f.service.Settings()
	if len(view.Listening) == 0 {
		t.Fatalf("not listening: %+v", view)
	}
	client := pinnedClient(view.Fingerprint)
	url := "https://" + view.Listening[0] + "/api/assistant"
	if response, err := client.Get(url); err == nil {
		response.Body.Close()
		t.Fatalf("an outside source got through with the external access off: %d", response.StatusCode)
	}

	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("home.example.net")})
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("with the external access on any source is accepted: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated outside request: %d", response.StatusCode)
	}

	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(false)})
	if response, err := client.Get(url); err == nil {
		response.Body.Close()
		t.Fatalf("turning the external access off must close the listener to outside sources again: %d", response.StatusCode)
	}
}

func TestTheListenerAcceptsLocalSourcesWithTheExternalAccessOff(t *testing.T) {
	f := newExternalFixture(t, nil) // the real classification: loopback is local
	view, _ := f.service.Settings()
	response, err := pinnedClient(view.Fingerprint).Get("https://" + view.Listening[0] + "/api/assistant")
	if err != nil {
		t.Fatalf("a local source must be accepted: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", response.StatusCode)
	}
}

func TestExternalReadyNeedsAListenerOnARealNetworkAddress(t *testing.T) {
	f := newExternalFixture(t, nil)
	view := update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(true), ExternalHost: ptr("home.example.net")})
	if view.ExternalReady {
		t.Fatalf("a loopback-only listener cannot receive a forwarded port: %+v", view)
	}
	// Bound to a network address, with an address known, the Mac's side is in place (the router is not tested).
	f.service.mu.Lock()
	f.service.listening = append(f.service.listening, "192.168.1.20:"+strconv.Itoa(f.port))
	f.service.mu.Unlock()
	if view, _ := f.service.Settings(); !view.ExternalReady {
		t.Fatalf("ready: %+v", view)
	}
	update(t, f.service, SettingsUpdate{ExternalEnabled: ptr(false)})
	f.service.mu.Lock()
	f.service.listening = append(f.service.listening, "192.168.1.20:"+strconv.Itoa(f.port))
	f.service.mu.Unlock()
	if view, _ := f.service.Settings(); view.ExternalReady {
		t.Fatalf("never ready while the external access is off: %+v", view)
	}
}
