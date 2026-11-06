package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	store := NewMemory()
	if _, err := store.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_ = store.Set("a", "1")
	if got, _ := store.Get("a"); got != "1" {
		t.Fatalf("got %q", got)
	}
	_ = store.Delete("a")
	if _, err := store.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted secret still there")
	}
}

func TestKeychainKeepsTheValueOutOfArguments(t *testing.T) {
	var gotStdin string
	var gotArgs []string
	k := &Keychain{Service: "app.pouple", run: func(stdin string, args ...string) (string, error) {
		gotStdin, gotArgs = stdin, args
		return "", nil
	}}
	token := `xoxp-1 "quoted" ; rm -rf`
	if err := k.Set("slack.token", token); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(gotArgs, " "), "xoxp") {
		t.Fatalf("secret leaked into arguments: %v", gotArgs)
	}
	if strings.Contains(gotStdin, token) || strings.Contains(gotStdin, "rm -rf") {
		t.Fatalf("secret is not encoded: %q", gotStdin)
	}
	if !strings.HasPrefix(gotStdin, "add-generic-password -U -s app.pouple -a slack.token -w ") {
		t.Fatalf("unexpected command %q", gotStdin)
	}
}

func TestKeychainRoundTripAndMissing(t *testing.T) {
	var stored string
	k := &Keychain{Service: "app.pouple", run: func(stdin string, args ...string) (string, error) {
		if args[0] == "-i" {
			stored = stdin[strings.LastIndex(stdin, "-w ")+3:]
			return "", nil
		}
		if stored == "" {
			return "", errors.New("exit status 44: The specified item could not be found in the keychain.")
		}
		return strings.TrimSpace(stored) + "\n", nil
	}}
	if _, err := k.Get("slack.token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_ = k.Set("slack.token", "xoxp-abc")
	if got, err := k.Get("slack.token"); err != nil || got != "xoxp-abc" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := k.Set("bad name", "x"); err == nil {
		t.Fatal("a name with a space must be refused")
	}
}
