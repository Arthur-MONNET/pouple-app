package asana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestFindProjectsIncludesAmbiguousMatchesAcrossPagesAndWorkspaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me":
			fmt.Fprint(w, `{"data":{"gid":"100","workspaces":[{"gid":"101","name":"Produit"},{"gid":"102","name":"Support"}]}}`)
		case "/workspaces/101/projects":
			if r.URL.Query().Get("offset") == "page-two" {
				fmt.Fprint(w, `{"data":[{"gid":"12345","name":"Spiice"}],"next_page":null}`)
			} else {
				fmt.Fprint(w, `{"data":[{"gid":"98765","name":"Autre"}],"next_page":{"offset":"page-two"}}`)
			}
		case "/workspaces/102/projects":
			fmt.Fprint(w, `{"data":[{"gid":"23456","name":"Spiice"}],"next_page":null}`)
		case "/projects/23456":
			fmt.Fprint(w, `{"data":{"gid":"23456","name":"Spiice","workspace":{"gid":"102","name":"Support"}}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := (&Connector{Base: server.URL, HTTP: server.Client()}).Client("fake")
	projects, err := client.FindProjects(context.Background(), "spiice")
	if err != nil || !slices.Equal(projects, []Project{
		{GID: "12345", Name: "Spiice", Workspace: Workspace{GID: "101", Name: "Produit"}},
		{GID: "23456", Name: "Spiice", Workspace: Workspace{GID: "102", Name: "Support"}},
	}) {
		t.Fatalf("projects=%+v, err=%v", projects, err)
	}
	resolved, err := client.FindProjects(context.Background(), "https://app.asana.com/1/102/project/23456/list")
	if err != nil || !slices.Equal(resolved, projects[1:]) {
		t.Fatalf("resolved=%+v, err=%v", resolved, err)
	}
}

func TestCreateTaskDoesNotFollowRedirectOrAcceptUnsafePermalink(t *testing.T) {
	for _, redirect := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/tasks" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if redirect {
				http.Redirect(w, r, "/repeat", http.StatusTemporaryRedirect)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{
				"gid": "12345", "permalink_url": "https://another.example/task/12345",
			}})
		}))
		client := (&Connector{Base: server.URL, HTTP: server.Client()}).Client("fake")
		link, err := client.CreateTask(context.Background(), "12345", "Title", "Description")
		server.Close()
		if link != "" || !errors.Is(err, ErrCreationUnconfirmed) {
			t.Fatalf("link=%q, err=%v", link, err)
		}
	}
}
