package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const goodToken = "1/1234567890/abcdefabcdef:0123456789abcdef"

type fake struct {
	mu          sync.Mutex
	seen        []string
	meBody      string
	tasksStatus int
	searchCode  int // status of the search endpoint, 0 = 200
	workspaces  []map[string]string
}

func (f *fake) connector(t *testing.T) *Connector {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.URL.String())
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Not Authorized"}]}`))
			return
		}
		if strings.Contains(r.URL.String(), "1/1234567890") || strings.Contains(r.URL.RawQuery, "abcdefabcdef") {
			t.Errorf("token in the address: %s", r.URL)
		}
		write := func(body any) { _ = json.NewEncoder(w).Encode(map[string]any{"data": body}) }
		switch {
		case r.URL.Path == "/users/me":
			if f.meBody != "" {
				_, _ = w.Write([]byte(f.meBody))
				return
			}
			workspaces := f.workspaces
			if workspaces == nil {
				workspaces = []map[string]string{{"gid": "w1", "name": "Acme"}}
			}
			write(map[string]any{"gid": "u1", "name": "Arthur Monnet", "email": "arthur@example.com", "photo": map[string]string{"image_128x128": "https://photos.example.com/a.png"}, "workspaces": workspaces})
		case r.URL.Path == "/tasks":
			if f.tasksStatus != 0 {
				w.WriteHeader(f.tasksStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
				{"gid": "3", "name": "No date", "permalink_url": "https://app.asana.com/0/1/3"},
				{"gid": "2", "name": "Later", "due_on": "2026-12-01"},
				{"gid": "1", "name": "Soon", "due_on": "2026-11-06", "memberships": []map[string]any{{"section": map[string]string{"name": "Doing"}, "project": map[string]string{"name": "Web"}}}},
				{"gid": "4", "name": "Done", "completed": true},
			}, "next_page": map[string]string{"offset": "abc"}})
		case strings.HasSuffix(r.URL.Path, "/tasks/search"):
			if f.searchCode != 0 {
				w.WriteHeader(f.searchCode)
				return
			}
			write([]map[string]any{{"gid": "9", "name": "Login bug", "assignee": map[string]string{"name": "Morgan"}}})
		case strings.HasSuffix(r.URL.Path, "/typeahead"):
			write([]map[string]any{{"gid": "8", "name": "Login SSO"}, {"gid": "7", "name": "Closed", "completed": true}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Connector{HTTP: server.Client(), Base: server.URL}
}

func TestValidateReturnsTheProfile(t *testing.T) {
	f := &fake{}
	profile, err := f.connector(t).Validate(context.Background(), connector.Values{"token": goodToken})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Arthur Monnet" || profile.AvatarURL != "https://photos.example.com/a.png" || len(profile.Warnings) != 0 {
		t.Fatalf("unexpected profile %+v", profile)
	}
	want := map[string]string{"Workspaces": "Acme", "E-mail": "arthur@example.com", "Open tasks": "3+"}
	for _, fact := range profile.Facts {
		if want[fact.Label] != fact.Value {
			t.Errorf("fact %q = %q", fact.Label, fact.Value)
		}
		delete(want, fact.Label)
	}
	if len(want) != 0 {
		t.Fatalf("missing facts %v", want)
	}
	meCalls := 0
	for _, address := range f.seen {
		if strings.HasPrefix(address, "/users/me") {
			meCalls++
		}
	}
	if meCalls != 1 {
		t.Fatalf("the account is read once, got %d calls: %v", meCalls, f.seen)
	}
}

func TestOpenTasksCountFailureDoesNotStopTheConnection(t *testing.T) {
	profile, err := (&fake{tasksStatus: 500}).connector(t).Validate(context.Background(), connector.Values{"token": goodToken})
	if err != nil || profile.Name != "Arthur Monnet" {
		t.Fatalf("got %+v, %v", profile, err)
	}
	for _, fact := range profile.Facts {
		if fact.Label == "Open tasks" {
			t.Fatalf("no count was available: %+v", fact)
		}
	}
}

func TestBadTokenAndMalformedAnswer(t *testing.T) {
	_, err := (&fake{}).connector(t).Validate(context.Background(), connector.Values{"token": "0/wrongwrongwrongwrong"})
	invalid, ok := err.(*connector.ValidationError)
	if !ok || invalid.Field != "token" || !strings.Contains(invalid.Message, "does not accept") {
		t.Fatalf("got %v", err)
	}
	for _, body := range []string{`<html></html>`, `{"data":{}}`, `[]`} {
		_, err = (&fake{meBody: body}).connector(t).Validate(context.Background(), connector.Values{"token": goodToken})
		if err == nil {
			t.Errorf("%s was accepted", body)
		}
	}
}

func TestNoWorkspaceIsAWarning(t *testing.T) {
	profile, err := (&fake{workspaces: []map[string]string{}}).connector(t).Validate(context.Background(), connector.Values{"token": goodToken})
	if err != nil || len(profile.Warnings) != 1 || profile.Warnings[0].Title != "No workspace" {
		t.Fatalf("got %+v, %v", profile, err)
	}
}

func TestOpenTasksAreSortedAndCompletedOnesLeftOut(t *testing.T) {
	f := &fake{}
	client := f.connector(t).client(goodToken)
	open, err := client.OpenTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, task := range open.Tasks {
		names = append(names, task.Name)
	}
	if strings.Join(names, ",") != "Soon,Later,No date" || !open.More {
		t.Fatalf("got %v more=%v", names, open.More)
	}
	if open.Tasks[0].Section != "Doing" || open.Tasks[0].Project != "Web" {
		t.Fatalf("unexpected task %+v", open.Tasks[0])
	}
}

func TestSearchFallsBackToTheTypeaheadOnAFreePlan(t *testing.T) {
	f := &fake{}
	client := f.connector(t).client(goodToken)
	found, err := client.SearchTasks(context.Background(), []string{"login", "sso"})
	if err != nil || len(found) != 1 || found[0].Name != "Login bug" || found[0].Assignee != "Morgan" {
		t.Fatalf("search: %+v, %v", found, err)
	}
	f.searchCode = http.StatusPaymentRequired
	found, err = client.SearchTasks(context.Background(), []string{"login", "sso"})
	if err != nil || len(found) != 1 || found[0].Name != "Login SSO" {
		t.Fatalf("typeahead: %+v, %v", found, err)
	}
}

func TestEveryTextHasAFrenchTranslation(t *testing.T) {
	descriptor := (&Connector{}).Descriptor()
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
	_, err := (&fake{}).connector(t).Validate(context.Background(), connector.Values{"token": "0/wrongwrongwrongwrong"})
	keys = append(keys, err.(*connector.ValidationError).Message)
	profile, _ := (&fake{workspaces: []map[string]string{}}).connector(t).Validate(context.Background(), connector.Values{"token": goodToken})
	for _, warning := range profile.Warnings {
		keys = append(keys, warning.Title, warning.Detail)
	}
	profile, _ = (&fake{}).connector(t).Validate(context.Background(), connector.Values{"token": goodToken})
	for _, fact := range profile.Facts {
		keys = append(keys, fact.Label)
	}
	for _, key := range keys {
		if key != "" && !i18n.Has(key) {
			t.Errorf("no French for %q", key)
		}
	}
}

func TestDescriptorIsOptional(t *testing.T) {
	descriptor := New().Descriptor()
	if descriptor.Required || descriptor.Order != 11 || descriptor.ID != "asana" || len(descriptor.Fields) != 1 || !descriptor.Fields[0].Secret {
		t.Fatalf("unexpected descriptor %+v", descriptor)
	}
	if descriptor.Guide[0].Link.URL != "https://app.asana.com/0/my-apps" {
		t.Fatalf("unexpected link %+v", descriptor.Guide[0].Link)
	}
}
