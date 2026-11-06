package svchttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

func TestForbiddenAddresses(t *testing.T) {
	for _, text := range []string{"127.0.0.1", "127.8.9.1", "::1", "0.0.0.0", "::", "169.254.169.254", "169.254.0.1", "fe80::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "224.0.0.1", "fd00:ec2::254"} {
		if !Forbidden(netip.MustParseAddr(text)) {
			t.Errorf("%s must be forbidden", text)
		}
	}
	// Private networks stay allowed: an internal GitLab is the normal case.
	for _, text := range []string{"10.1.2.3", "192.168.1.20", "172.16.0.9", "8.8.8.8", "100.64.0.1", "2606:4700::1111", "fd12:3456::1"} {
		if Forbidden(netip.MustParseAddr(text)) {
			t.Errorf("%s must be allowed", text)
		}
	}
}

func TestGuardedClientDoesNotConnectToThisMac(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	var out map[string]any
	_, err := GetJSON(context.Background(), NewClient(), "GitLab", server.URL, nil, &out)
	if err == nil || called {
		t.Fatalf("a loopback server must be refused at connection time (called=%v, err=%v)", called, err)
	}
}

func TestRedirectToAnotherHostIsRefused(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the other host must never be called (headers %v)", r.Header)
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	defer server.Close()
	var out map[string]any
	header := http.Header{"Private-Token": {"secret-token-value"}}
	_, err := GetJSON(context.Background(), NewPublicClient(), "GitLab", server.URL, header, &out)
	if !errors.Is(err, ErrRedirect) {
		t.Fatalf("got %v", err)
	}
}

func TestSameHostRedirectIsFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var out struct{ OK bool }
	if _, err := GetJSON(context.Background(), NewPublicClient(), "X", server.URL+"/old", nil, &out); err != nil || !out.OK {
		t.Fatalf("got %+v, %v", out, err)
	}
}

func TestErrorsCarryNoBodyAndNoCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "your token secret-token-value is wrong, Authorization: "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	}))
	defer server.Close()
	var out map[string]any
	header := http.Header{"Authorization": {"Bearer secret-token-value"}}
	_, err := GetJSON(context.Background(), NewPublicClient(), "Asana", server.URL, header, &out)
	if Status(err) != 401 || strings.Contains(err.Error(), "secret-token-value") || strings.Contains(i18n.ErrorText(i18n.FR, err), "secret-token-value") {
		t.Fatalf("got %v", err)
	}
	if err.Error() != "Asana answered 401" || i18n.ErrorText(i18n.FR, err) != "Asana a répondu 401" {
		t.Fatalf("texts: %q / %q", err, i18n.ErrorText(i18n.FR, err))
	}
	if reason := Reason(err).Text(i18n.EN); reason != "access refused, check the token" {
		t.Fatalf("reason %q", reason)
	}
}

func TestOversizedAndMalformedAnswers(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("a", MaxBody) + `"}`))
	}))
	defer big.Close()
	var out map[string]any
	if _, err := GetJSON(context.Background(), NewPublicClient(), "Slite", big.URL, nil, &out); !errors.Is(err, ErrUnexpected) {
		t.Fatalf("oversized: %v", err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>login</html>`)) }))
	defer bad.Close()
	if _, err := GetJSON(context.Background(), NewPublicClient(), "Slite", bad.URL, nil, &out); !errors.Is(err, ErrUnexpected) {
		t.Fatalf("malformed: %v", err)
	}
}
