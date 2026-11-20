package slite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

func TestCreateNoteSendsExactApprovedContent(t *testing.T) {
	const markdown = "\n# Page\n\n    Code\n  \n"
	const link = "https://team.slite.com/api/s/created/Page"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/notes" || r.Header.Get("Authorization") != "Bearer key" ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 3 ||
			body["parentNoteId"] != "parent" || body["title"] != "Page" || body["markdown"] != markdown {
			t.Errorf("unexpected payload: %+v, %v", body, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "created", "parentNoteId": "parent", "url": link})
	}))
	defer server.Close()
	client := (&Connector{Base: server.URL, HTTP: server.Client()}).client("key")
	if _, err := client.CreateNote(context.Background(), "", "Page", markdown); err == nil || calls.Load() != 0 {
		t.Fatal("missing parent must be rejected before sending")
	}
	got, err := client.CreateNote(context.Background(), "parent", "Page", markdown)
	if err != nil || got != link || calls.Load() != 1 {
		t.Fatalf("link=%q error=%v calls=%d", got, err, calls.Load())
	}
}

func TestCreateNoteDoesNotRepeatAnUnconfirmedWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"invalid response", http.StatusOK, `{"title":"Page"}`},
		{"wrong parent", http.StatusOK, `{"id":"created","parentNoteId":"different","url":"https://team.slite.com/api/s/created"}`},
		{"server error", http.StatusBadGateway, ""},
		{"redirect", http.StatusTemporaryRedirect, ""},
		{"forbidden", http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/notes" {
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer server.Close()
			client := (&Connector{Base: server.URL, HTTP: server.Client()}).client("key")
			link, err := client.CreateNote(context.Background(), "parent", "Page", "Content")
			if tc.status == http.StatusForbidden {
				if svchttp.Status(err) != http.StatusForbidden || errors.Is(err, ErrCreationUnconfirmed) {
					t.Fatalf("expected a definite rejection, got %v", err)
				}
			} else if !errors.Is(err, ErrCreationUnconfirmed) {
				t.Fatalf("expected an unconfirmed creation, got %v", err)
			}
			if link != "" || calls.Load() != 1 {
				t.Fatalf("link=%q calls=%d", link, calls.Load())
			}
		})
	}
}
