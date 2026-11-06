package slite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const goodKey = "slite-key-0123456789"

func fakeSlite(t *testing.T, meBody string) *Connector {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-slite-api-key") != goodKey || r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.URL.String(), goodKey) {
			t.Errorf("key in the address: %s", r.URL)
		}
		switch r.URL.Path {
		case "/me":
			if meBody != "" {
				_, _ = w.Write([]byte(meBody))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"displayName": "Arthur Monnet", "email": "arthur@example.com", "organizationName": "Acme", "organizationDomain": "acme.com"})
		case "/search-notes":
			if r.URL.Query().Get("query") != "api conventions" || r.URL.Query().Get("hitsPerPage") != "3" {
				t.Errorf("unexpected query %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": []map[string]any{
				{"id": "n1", "title": "API conventions", "highlight": "Use <em>REST</em> routes", "parentNotes": []map[string]string{{"title": "Engineering"}}, "archivedAt": nil, "updatedAt": "2026-10-01T00:00:00Z"},
				{"id": "n2", "title": "Old", "archivedAt": "2025-01-01T00:00:00Z"},
				{"nope": true},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Connector{HTTP: server.Client(), Base: server.URL}
}

func TestValidateReturnsTheProfile(t *testing.T) {
	profile, err := fakeSlite(t, "").Validate(context.Background(), connector.Values{"token": goodKey})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Arthur Monnet" || len(profile.Facts) != 3 || profile.Facts[0] != (connector.Fact{Label: "Organization", Value: "Acme"}) || len(profile.Warnings) != 0 {
		t.Fatalf("unexpected profile %+v", profile)
	}
}

func TestBadKeyAndMalformedAnswer(t *testing.T) {
	_, err := fakeSlite(t, "").Validate(context.Background(), connector.Values{"token": "wrong-key-0123456789"})
	invalid, ok := err.(*connector.ValidationError)
	if !ok || invalid.Field != "token" || !strings.Contains(invalid.Message, "does not accept") {
		t.Fatalf("got %v", err)
	}
	for _, body := range []string{`<html></html>`, `{}`, `[]`} {
		if _, err := fakeSlite(t, body).Validate(context.Background(), connector.Values{"token": goodKey}); err == nil {
			t.Errorf("%s was accepted", body)
		}
	}
}

func TestSearchReturnsCleanHitsWithoutArchivedNotes(t *testing.T) {
	client := fakeSlite(t, "").client(goodKey)
	hits, err := client.Search(context.Background(), "api conventions", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := Hit{ID: "n1", Title: "API conventions", Excerpt: "Use REST routes", Channel: "Engineering", UpdatedAt: "2026-10-01T00:00:00Z"}
	if len(hits) != 1 || hits[0] != want {
		t.Fatalf("got %+v", hits)
	}
}

func TestEveryTextHasAFrenchTranslation(t *testing.T) {
	descriptor := New().Descriptor()
	keys := []string{descriptor.Tagline}
	for _, field := range descriptor.Fields {
		keys = append(keys, field.Label, field.Help, field.PatternHint)
	}
	for _, step := range descriptor.Guide {
		keys = append(keys, step.Title, step.Body)
		if step.Link != nil {
			keys = append(keys, step.Link.Label)
		}
	}
	_, err := fakeSlite(t, "").Validate(context.Background(), connector.Values{"token": "wrong-key-0123456789"})
	keys = append(keys, err.(*connector.ValidationError).Message)
	profile, _ := fakeSlite(t, "").Validate(context.Background(), connector.Values{"token": goodKey})
	for _, fact := range profile.Facts {
		keys = append(keys, fact.Label)
	}
	for _, key := range keys {
		if key != "" && !i18n.Has(key) {
			t.Errorf("no French for %q", key)
		}
	}
	if descriptor.Required || descriptor.Order != 12 || descriptor.ID != "slite" || descriptor.Guide[0].Link.URL != DocsURL {
		t.Fatalf("unexpected descriptor %+v", descriptor)
	}
}
