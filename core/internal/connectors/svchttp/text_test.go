package svchttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetTextTailKeepsOnlyTheEndOfALongAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("noise\n", 100000) + "the end"))
	}))
	defer server.Close()

	got, err := GetTextTail(context.Background(), server.Client(), "GitLab", server.URL, http.Header{"Private-Token": {"tok"}}, 100)
	if err != nil || len(got) != 100 || !strings.HasSuffix(got, "noise\nthe end") {
		t.Fatalf("got %d bytes %q, err %v", len(got), got, err)
	}
}

func TestGetTextTailReportsAnErrorStatusWithoutItsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("secret details"))
	}))
	defer server.Close()

	_, err := GetTextTail(context.Background(), server.Client(), "GitLab", server.URL, nil, 100)
	if Status(err) != http.StatusForbidden || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err %v", err)
	}
}
