package asana

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestParseTaskRefReadsBothAddressFormsAndTheNumber(t *testing.T) {
	for text, want := range map[string]string{
		"https://app.asana.com/1/1101206445191474/project/1201969473608964/task/1220044008196354?focus=true": "1220044008196354",
		"https://app.asana.com/1/12345/project/67890/task/1220044008196354/f":                                "1220044008196354",
		"https://app.asana.com/0/1201969473608964/1220044008196354":                                          "1220044008196354",
		"https://app.asana.com/0/1201969473608964/1220044008196354/f":                                        "1220044008196354",
		"1220044008196354":    "1220044008196354",
		"  1220044008196354 ": "1220044008196354",
	} {
		if got, ok := ParseTaskRef(text); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", text, got, ok, want)
		}
	}
}

func TestParseTaskRefRefusesAnythingThatIsNotAnAsanaTask(t *testing.T) {
	for _, text := range []string{
		"", "abc", "123", "https://evil.example.com/1/1/project/2/task/1220044008196354",
		"http://app.asana.com/0/1201969473608964/1220044008196354",
		"https://user@app.asana.com/0/1201969473608964/1220044008196354",
		"https://app.asana.com.evil.com/0/1201969473608964/1220044008196354",
		"https://app.asana.com/1/1/project/2/task/../../users/me",
		"https://app.asana.com/1/1/project/2/task/12", "1220044008196354/../x", "1220044008196354?x=1",
		"https://app.asana.com/1/1/inbox/0", "12345678901234567890123456789",
	} {
		if got, ok := ParseTaskRef(text); ok {
			t.Errorf("%q was accepted as %q", text, got)
		}
	}
}

type asanaFake struct {
	mu     sync.Mutex
	posted []string
	paths  []string
}

func (f *asanaFake) client(t *testing.T, postStatus int) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/1220044008196354/stories":
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.posted = append(f.posted, string(body)+"|"+r.Header.Get("Content-Type"))
			f.mu.Unlock()
			if postStatus != 0 {
				w.WriteHeader(postStatus)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gid": "9988776655"}})
		case r.URL.Path == "/tasks/1220044008196354":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"gid": "1220044008196354", "name": "AB test 179", "notes": "Premium layer", "due_on": "2026-11-12",
				"permalink_url": "https://app.asana.com/0/1/1220044008196354", "assignee": map[string]string{"name": "Thibaut"},
			}})
		case r.URL.Path == "/tasks/1220044008196354/stories":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
				{"text": "Thibaut assigned to you", "resource_subtype": "assigned"},
				{"text": "First", "resource_subtype": "comment_added", "created_at": "2026-11-01T10:00:00Z", "created_by": map[string]string{"name": "Nina"}},
				{"text": "Second", "resource_subtype": "comment_added"},
				{"text": "Third", "resource_subtype": "comment_added"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Client{token: "tok", base: server.URL, http: server.Client()}
}

func TestTaskAndCommentsReturnWhatIsNeededToWriteAboutIt(t *testing.T) {
	client := (&asanaFake{}).client(t, 0)
	task, err := client.Task(context.Background(), "1220044008196354")
	if err != nil || task.Name != "AB test 179" || task.Notes != "Premium layer" || task.Assignee != "Thibaut" || task.DueOn != "2026-11-12" {
		t.Fatalf("%+v %v", task, err)
	}
	comments, err := client.Comments(context.Background(), "1220044008196354", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].Text != "Second" || comments[1].Text != "Third" {
		t.Fatalf("the last comments, history entries left out: %+v", comments)
	}
	if all, _ := client.Comments(context.Background(), "1220044008196354", 10); len(all) != 3 || all[0].Author != "Nina" {
		t.Errorf("%+v", all)
	}
}

func TestAddCommentPostsTheTextAsJSONOnTheTaskOnly(t *testing.T) {
	fake := &asanaFake{}
	posted, err := fake.client(t, 0).AddComment(context.Background(), "1220044008196354", "  Déployé en preprod.  ")
	if err != nil || posted.GID != "9988776655" {
		t.Fatalf("%+v %v", posted, err)
	}
	if len(fake.posted) != 1 || fake.posted[0] != `{"data":{"text":"Déployé en preprod."}}|application/json` {
		t.Fatalf("%q", fake.posted)
	}
	if len(fake.paths) != 1 || fake.paths[0] != "POST /tasks/1220044008196354/stories" {
		t.Errorf("nothing else is called: %v", fake.paths)
	}
}

func TestAddCommentRefusesWhatIsNotACommentOnATaskWithoutACall(t *testing.T) {
	fake := &asanaFake{}
	client := fake.client(t, 0)
	for _, tc := range []struct{ gid, text string }{
		{"../users/me", "x"}, {"", "x"}, {"1220044008196354", "   "}, {"1220044008196354", strings.Repeat("x", MaxCommentRunes+1)},
	} {
		if _, err := client.AddComment(context.Background(), tc.gid, tc.text); err == nil {
			t.Errorf("%q / %d chars was accepted", tc.gid, len(tc.text))
		}
	}
	if len(fake.paths) != 0 {
		t.Errorf("no request was made: %v", fake.paths)
	}
}

func TestAnAsanaRefusalIsAnErrorNotASilentSuccess(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests} {
		if _, err := (&asanaFake{}).client(t, status).AddComment(context.Background(), "1220044008196354", "x"); err == nil {
			t.Errorf("status %d was taken for a success", status)
		}
	}
}
