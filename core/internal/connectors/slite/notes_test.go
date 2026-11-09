package slite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func notesServer(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notes/app1/children":
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 3, "notes": []map[string]any{
				{"id": "c1", "title": "Release 1.2", "updatedAt": "2026-10-01T00:00:00Z"},
				{"id": "c2", "title": "Old", "archivedAt": "2025-01-01T00:00:00Z"},
				{"id": "c3", "title": "Release 1.3", "updatedAt": "2026-11-01T00:00:00Z"},
				{"title": "no id"},
			}})
		case "/notes/c1":
			if r.URL.Query().Get("format") != "md" {
				t.Errorf("the text is asked in Markdown: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "title": "Release 1.2", "updatedAt": "2026-10-01T00:00:00Z", "content": "  # Changes\n" + strings.Repeat("é", 50)})
		case "/ask":
			if r.URL.Query().Get("question") != "where are the app versions?" || r.URL.Query().Get("wait") != "true" {
				t.Errorf("unexpected ask %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"answer": "In the app folder.", "sources": []map[string]any{{"id": "app1", "title": "App releases"}, {"id": "x"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return (&Connector{HTTP: server.Client(), Base: server.URL}).client(goodKey)
}

func TestChildrenSkipsArchivedAndIncompleteNotesAndKeepsTheTotal(t *testing.T) {
	children, total, err := notesServer(t).Children(context.Background(), "app1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0] != (Child{ID: "c1", Title: "Release 1.2", UpdatedAt: "2026-10-01T00:00:00Z"}) || children[1].ID != "c3" || total != 3 {
		t.Fatalf("%+v of %d", children, total)
	}
	if limited, _, _ := notesServer(t).Children(context.Background(), "app1", 1); len(limited) != 1 {
		t.Errorf("the list is capped: %+v", limited)
	}
}

func TestReadReturnsTrimmedMarkdownCutToTheLimit(t *testing.T) {
	note, err := notesServer(t).Read(context.Background(), "c1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if note.Title != "Release 1.2" || !note.Truncated || len([]rune(note.Content)) != 20 || !strings.HasPrefix(note.Content, "# Changes") {
		t.Fatalf("%+v", note)
	}
	if full, _ := notesServer(t).Read(context.Background(), "c1", 1000); full.Truncated {
		t.Error("a short note is not cut")
	}
}

func TestAnIdThatIsNotANoteIdNeverReachesTheNetwork(t *testing.T) {
	client := notesServer(t)
	for _, id := range []string{"", "../me", "a/b", "c1?format=html", "c1 ", strings.Repeat("a", 65)} {
		if _, _, err := client.Children(context.Background(), id, 5); err == nil {
			t.Errorf("children accepted %q", id)
		}
		if _, err := client.Read(context.Background(), id, 5); err == nil {
			t.Errorf("read accepted %q", id)
		}
	}
}

func TestAskReturnsTheAnswerAndItsSources(t *testing.T) {
	answer, err := notesServer(t).Ask(context.Background(), "where are the app versions?")
	if err != nil {
		t.Fatal(err)
	}
	if answer.Text != "In the app folder." || len(answer.Sources) != 1 || answer.Sources[0] != (Source{ID: "app1", Title: "App releases"}) {
		t.Fatalf("%+v", answer)
	}
}
