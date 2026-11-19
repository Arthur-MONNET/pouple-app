package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenMRReconcilesRetriesWithoutDuplicates(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprint(lost), func(t *testing.T) {
			created, posts := false, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Private-Token") != "secret" {
					t.Error("missing authentication")
				}
				switch {
				case r.Method == "POST":
					posts++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["source_branch"] != "fix/logo" || body["target_branch"] != "master" || len(body) != 3 {
						t.Errorf("unexpected write: %v", body)
					}
					created = true
					if lost {
						w.WriteHeader(500)
						return
					}
					fmt.Fprint(w, `{"web_url":"https://gitlab.example.com/acme/app/-/merge_requests/1"}`)
				case strings.Contains(r.URL.Path, "/repository/branches/"):
					fmt.Fprint(w, `{"name":"fix/logo"}`)
				default:
					if r.URL.Query().Get("source_branch") != "fix/logo" || r.URL.Query().Get("target_branch") != "master" {
						t.Error("unscoped lookup")
					}
					if created {
						fmt.Fprint(w, `[{"source_branch":"fix/logo","target_branch":"master","web_url":"https://gitlab.example.com/acme/app/-/merge_requests/1"}]`)
					} else {
						fmt.Fprint(w, `[]`)
					}
				}
			}))
			defer server.Close()
			c := &Client{api: server.URL, token: "secret", http: server.Client()}
			for i := 0; i < 2; i++ {
				link, err := c.OpenMergeRequest(context.Background(), "acme/app", "fix/logo", "master", "Fix logo")
				if err != nil || !strings.HasSuffix(link, "/1") {
					t.Fatalf("%s %v", link, err)
				}
			}
			if posts != 1 {
				t.Fatalf("%d posts", posts)
			}
		})
	}
}

func TestOpenMRDoesNotWriteWithoutRemoteBranch(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			t.Error("write before branch push")
		}
		if strings.Contains(r.URL.Path, "/repository/branches/") {
			w.WriteHeader(404)
		} else {
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	c := &Client{api: server.URL, token: "secret", http: server.Client()}
	if _, err := c.OpenMergeRequest(context.Background(), "acme/app", "fix/logo", "master", "Fix logo"); err == nil || posts != 0 {
		t.Fatal("missing branch accepted")
	}
}

func TestReadOnlyTokenReportsCreationRefusalWithoutAnotherPost(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			posts++
			w.WriteHeader(http.StatusForbidden)
		case strings.Contains(r.URL.Path, "/repository/branches/"):
			fmt.Fprint(w, `{"name":"fix/logo"}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	c := &Client{api: server.URL, token: "read-only", http: server.Client()}
	_, err := c.OpenMergeRequest(context.Background(), "acme/app", "fix/logo", "master", "Fix logo")
	if err == nil || !strings.Contains(err.Error(), "api") || posts != 1 {
		t.Fatalf("%v, %d posts", err, posts)
	}
}
