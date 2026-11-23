package remote

import (
	"net/http"
	"slices"
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

func TestNoChosenInterfaceMeansEveryUpInterfaceWithAPrivateAddress(t *testing.T) {
	list := []Interface{
		{Name: "lo0", Addresses: []string{"127.0.0.1", "::1"}, Loopback: true},
		{Name: "en0", Addresses: []string{"192.168.1.20", "2001:db8::20", "fd12:3456::20"}},
		{Name: "en1", Addresses: []string{"203.0.113.5"}},                // a public address only
		{Name: "bridge100", Addresses: []string{"172.16.4.1"}},           // RFC 1918 (172.16/12)
		{Name: "utun4", Addresses: []string{"10.8.0.2"}},                 // RFC 1918 (10/8)
		{Name: "en7", Addresses: []string{"2001:db8::7"}},                // global IPv6 only
		{Name: "gif0", Addresses: []string{"169.254.1.1"}},               // link-local is not private
		{Name: "lo1", Addresses: []string{"10.9.9.9"}, Loopback: true},   // a loopback interface is never automatic
		{Name: "bad0", Addresses: []string{"not-an-address", "0.0.0.0"}}, // never bound
		{Name: "en9", Addresses: []string{"172.32.0.1"}},                 // just outside 172.16/12
	}
	var mu sync.Mutex
	service, _ := automaticFixture(t, &list, &mu)

	names, addresses, err := service.resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"en0", "bridge100", "utun4"}; !slices.Equal(names, want) {
		t.Fatalf("automatic interfaces %v, want %v", names, want)
	}
	if want := []string{"10.8.0.2", "172.16.4.1", "192.168.1.20", "2001:db8::20", "fd12:3456::20"}; !slices.Equal(addresses, want) {
		t.Fatalf("automatic addresses %v, want %v", addresses, want)
	}

	// An explicit choice is taken as it is: loopback when chosen, a public interface, an unknown name ignored.
	for chosen, want := range map[string][]string{"lo0": {"127.0.0.1", "::1"}, "en1": {"203.0.113.5"}, "nope": nil} {
		_, got, err := service.resolve([]string{chosen})
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: %v %v, want %v", chosen, got, err, want)
		}
	}
	if names, _, _ := service.resolve([]string{"en1", "lo0"}); !slices.Equal(names, []string{"lo0", "en1"}) {
		t.Fatalf("explicit names follow the system's order: %v", names)
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
	list = append(list, Interface{Name: "en0", Addresses: []string{"192.0.2.1"}}, Interface{Name: "en0b", Addresses: []string{"10.200.200.2"}})
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
