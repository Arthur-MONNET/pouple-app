package remote

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

func automaticFixture(t *testing.T, list *[]Interface, mu *sync.Mutex) (*Service, *memorySettings) {
	t.Helper()
	settings := &memorySettings{}
	service, err := NewService(Options{
		Dir: t.TempDir(), Secrets: secrets.NewMemory(), Load: settings.load, Save: settings.save,
		Hostname: func() (string, error) { return "studio.local", nil },
		Interfaces: func() ([]Interface, error) {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(*list), nil
		},
		Handler: HandlerOptions{Mount: func(mux *http.ServeMux) {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Shutdown)
	return service, settings
}

// homeNet is a home network interface: up, broadcast, not point-to-point.
func homeNet(name string, addresses ...string) Interface {
	return Interface{Name: name, Addresses: addresses, Broadcast: true}
}

func TestNoChosenInterfaceMeansTheBroadcastNetworksAndTheirPrivateAddressesOnly(t *testing.T) {
	list := []Interface{
		{Name: "lo0", Addresses: []string{"127.0.0.1", "::1"}, Loopback: true},
		homeNet("en0", "192.168.1.20", "2001:db8::20", "fd12:3456::20"), // the global IPv6 is not bound
		homeNet("en1", "203.0.113.5"),                                   // a public address only
		homeNet("en2", "172.16.4.1"),                                    // RFC 1918 (172.16/12)
		homeNet("en3", "10.8.0.2", "169.254.1.1"),                       // RFC 1918 (10/8); link-local is not private
		homeNet("en7", "2001:db8::7"),                                   // global IPv6 only
		homeNet("en9", "172.32.0.1"),                                    // just outside 172.16/12
		homeNet("bad0", "not-an-address", "0.0.0.0"),                    // never bound
		{Name: "en5", Addresses: []string{"192.168.5.5"}},               // not a broadcast network
		{Name: "en6", Addresses: []string{"192.168.6.6"}, Broadcast: true, PointToPoint: true},
		{Name: "lo1", Addresses: []string{"10.9.9.9"}, Broadcast: true, Loopback: true},
	}
	// Virtual and tunnel interfaces are skipped by name, even when they look like a home network.
	for _, name := range []string{"utun4", "utun0", "ipsec0", "ppp0", "bridge100", "vmnet8", "awdl0", "llw0", "anpi0"} {
		list = append(list, homeNet(name, "10.77.0.2"))
	}
	var mu sync.Mutex
	service, _ := automaticFixture(t, &list, &mu)

	names, addresses, err := service.resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"en0", "en2", "en3"}; !slices.Equal(names, want) {
		t.Fatalf("automatic interfaces %v, want %v", names, want)
	}
	if want := []string{"10.8.0.2", "172.16.4.1", "192.168.1.20", "fd12:3456::20"}; !slices.Equal(addresses, want) {
		t.Fatalf("automatic addresses %v, want %v", addresses, want)
	}

	// An explicit choice is taken as it is: loopback, a public or global address, a VPN, a bridge. An unknown name is
	// ignored.
	for chosen, want := range map[string][]string{
		"lo0": {"127.0.0.1", "::1"}, "en1": {"203.0.113.5"}, "en7": {"2001:db8::7"}, "utun4": {"10.77.0.2"}, "bridge100": {"10.77.0.2"},
		"en0": {"192.168.1.20", "2001:db8::20", "fd12:3456::20"}, "en5": {"192.168.5.5"}, "nope": nil,
	} {
		_, got, err := service.resolve([]string{chosen})
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: %v %v, want %v", chosen, got, err, want)
		}
	}
	if names, _, _ := service.resolve([]string{"en1", "lo0"}); !slices.Equal(names, []string{"lo0", "en1"}) {
		t.Fatalf("explicit names follow the system's order: %v", names)
	}
}

func TestSystemInterfacesCarryTheirFlags(t *testing.T) {
	interfaces, err := SystemInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Loopback && iface.Broadcast {
			t.Errorf("%s: a loopback interface is not a broadcast network", iface.Name)
		}
		if automaticInterface(iface) && (iface.Loopback || iface.PointToPoint || !iface.Broadcast) {
			t.Errorf("%s: %+v", iface.Name, iface)
		}
	}
	// The flags are not part of the settings' answer.
	raw, _ := json.Marshal(Interface{Name: "en0", Addresses: []string{"192.168.1.2"}, Broadcast: true, PointToPoint: true})
	if strings.Contains(string(raw), "roadcast") || strings.Contains(string(raw), "oint") {
		t.Fatalf("%s", raw)
	}
}

func TestEnabledWithNoInterfaceIsAcceptedAndFollowsTheNetwork(t *testing.T) {
	var mu sync.Mutex
	list := []Interface{{Name: "lo0", Addresses: []string{"127.0.0.1"}, Loopback: true}}
	service, settings := automaticFixture(t, &list, &mu)

	// The settings screen sends enabled with no interface: accepted, even with no network to listen on yet.
	view := update(t, service, SettingsUpdate{Enabled: ptr(true), Port: ptr(freePort(t))})
	if !view.Enabled || len(view.Interfaces) != 0 || len(view.Listening) != 0 || view.Problem == "" {
		t.Fatalf("automatic, nothing private to listen on: %+v", view)
	}
	if stored, _ := settings.load(); !stored.Enabled || len(stored.Interfaces) != 0 {
		t.Fatalf("stored: %+v", stored)
	}
	if service.addressesChanged() {
		t.Fatal("nothing changed")
	}

	// A network comes up (a Wi-Fi lease): the automatic choice sees it at the next check, like an explicit one would.
	mu.Lock()
	list = append(list, homeNet("en0", "192.0.2.1"), homeNet("en4", "10.200.200.2"))
	mu.Unlock()
	if !service.addressesChanged() {
		t.Fatal("a new private network must re-apply the automatic choice")
	}
	if err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	if service.addressesChanged() {
		t.Fatal("applied: no change left, even when the address cannot be bound here")
	}
	mu.Lock()
	list = list[:1]
	mu.Unlock()
	if !service.addressesChanged() {
		t.Fatal("the network went away")
	}

	// Explicit choices keep working as before.
	view = update(t, service, SettingsUpdate{Interfaces: &[]string{"lo0"}})
	if len(view.Interfaces) != 1 || len(view.Listening) == 0 {
		t.Fatalf("a chosen loopback interface listens: %+v", view)
	}
	view = update(t, service, SettingsUpdate{Interfaces: &[]string{}})
	if len(view.Interfaces) != 0 || len(view.Listening) != 0 {
		t.Fatalf("back to automatic: %+v", view)
	}
}
