package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
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
				case r.URL.Path == "/personal_access_tokens/self":
					w.WriteHeader(404)
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
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || posts != 1 {
		t.Fatalf("%v, %d posts", err, posts)
	}
}

func TestMRRefusalDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, want string
	}{
		{401, `{"message":"401 Unauthorized"}`, "authentication rejected"},
		{403, `{"error":"insufficient_scope"}`, "insufficient token scope"},
		{403, `{"message":"You are not allowed to create merge requests in this project."}`, "cannot create merge requests in this project"},
		{403, `{"message":"glpat-secret https://user:password@example.com?token=private email@example.com"}`, "did not establish"},
	} {
		for _, stage := range []string{"lookup", "post"} {
			t.Run(tc.want+"/"+stage, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stage == "post" && r.Method != "POST" {
						switch {
						case r.URL.Path == "/personal_access_tokens/self":
							w.WriteHeader(404)
						case strings.Contains(r.URL.Path, "/repository/branches/"):
							fmt.Fprint(w, `{"name":"fix/logo"}`)
						default:
							fmt.Fprint(w, `[]`)
						}
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				}))
				defer server.Close()
				c := &Client{api: server.URL, token: "glpat-secret", http: server.Client()}
				_, err := c.OpenMergeRequest(context.Background(), "acme/app", "fix/logo", "master", "Fix")
				if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), fmt.Sprint(tc.status)) {
					t.Fatalf("%v", err)
				}
				if svchttp.Status(err) != tc.status {
					t.Fatalf("lost HTTP status: %v", err)
				}
				for _, secret := range []string{"glpat-secret", "password", "email@example.com", "token=private"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatal("sensitive error")
					}
				}
			})
		}
	}
}

func TestMRScopePreflightAndResume(t *testing.T) {
	writable, created, posts := false, false, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/personal_access_tokens/self":
			scope := "read_api"
			if writable {
				scope = "api"
			}
			fmt.Fprintf(w, `{"scopes":[%q]}`, scope)
		case r.Method == "POST":
			posts++
			created = true
			fmt.Fprint(w, `{"web_url":"https://gitlab.example.com/mr/1"}`)
		case strings.Contains(r.URL.Path, "/repository/branches/"):
			fmt.Fprint(w, `{"name":"fix/logo"}`)
		default:
			if created {
				fmt.Fprint(w, `[{"source_branch":"fix/logo","target_branch":"master","web_url":"https://gitlab.example.com/mr/1"}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		}
	}))
	defer server.Close()
	c := &Client{api: server.URL, token: "secret", http: server.Client()}
	if _, err := c.OpenMergeRequest(context.Background(), "acme/app", "fix/logo", "master", "Fix"); err == nil || posts != 0 || !strings.Contains(err.Error(), "api scope") {
		t.Fatalf("%v; %d", err, posts)
	}
	writable = true
	for i := 0; i < 2; i++ {
		if link, err := c.OpenMergeRequest(context.Background(), "acme/app", "fix/logo", "master", "Fix"); err != nil || link == "" {
			t.Fatalf("%s %v", link, err)
		}
	}
	if posts != 1 {
		t.Fatalf("%d posts", posts)
	}
}
