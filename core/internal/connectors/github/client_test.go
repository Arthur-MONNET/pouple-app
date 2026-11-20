package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// pullsServer answers the pull request routes for octo/app#42 and a search of two results (one from another host).
func pullsServer(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+goodToken || r.Header.Get("X-Github-Api-Version") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/search/issues":
			write(map[string]any{"total_count": 3, "items": []any{
				map[string]any{"number": 42, "title": "Fix login", "html_url": "https://github.com/octo/app/pull/42", "repository_url": server.URL + "/repos/octo/app",
					"draft": true, "updated_at": "2026-11-20T08:00:00Z", "user": map[string]any{"login": "arthur"}, "pull_request": map[string]any{}},
				map[string]any{"number": 9, "title": "From elsewhere", "html_url": "https://evil.example/x", "repository_url": "https://evil.example/repos/a/b", "pull_request": map[string]any{}},
				map[string]any{"number": 10, "title": "An issue", "html_url": "https://github.com/octo/app/issues/10", "repository_url": server.URL + "/repos/octo/app"},
			}})
		case "/repos/octo/app/pulls/42":
			write(map[string]any{"number": 42, "title": "Fix login", "state": "open", "body": "Fixes it", "html_url": "https://github.com/octo/app/pull/42",
				"user": map[string]any{"login": "arthur"}, "head": map[string]any{"ref": "fix", "sha": strings.Repeat("a", 40)}, "base": map[string]any{"ref": "main"},
				"mergeable_state": "clean", "changed_files": 2, "labels": []any{map[string]any{"name": "bug"}}, "requested_reviewers": []any{map[string]any{"login": "gael"}}})
		case "/repos/octo/app/issues/42/comments":
			write([]any{
				map[string]any{"id": 1, "body": "old", "user": map[string]any{"login": "gael"}, "created_at": "2026-11-19T08:00:00Z"},
				map[string]any{"id": 2, "body": "Why this?", "user": map[string]any{"login": "gael"}, "created_at": "2026-11-20T08:10:00Z"},
				map[string]any{"id": 3, "body": "Coverage 80%", "user": map[string]any{"login": "codecov[bot]", "type": "Bot"}, "created_at": "2026-11-20T08:11:00Z"},
			})
		case "/repos/octo/app/pulls/42/comments":
			write([]any{map[string]any{"id": 1, "body": "Rename this", "path": "a.go", "user": map[string]any{"login": "thibaut"}, "created_at": "2026-11-20T08:05:00Z"}})
		case "/repos/octo/app/pulls/42/reviews":
			write([]any{
				map[string]any{"id": 5, "body": "", "state": "COMMENTED", "user": map[string]any{"login": "thibaut"}, "submitted_at": "2026-11-20T08:05:00Z"},
				map[string]any{"id": 6, "body": "", "state": "CHANGES_REQUESTED", "user": map[string]any{"login": "thibaut"}, "submitted_at": "2026-11-20T08:06:00Z"},
				map[string]any{"id": 7, "body": "draft", "state": "PENDING", "user": map[string]any{"login": "thibaut"}},
			})
		case "/repos/octo/app/pulls/42/files":
			write([]any{
				map[string]any{"filename": "a.go", "status": "modified", "patch": "@@ -1 +1 @@\n-a\n+b"},
				map[string]any{"filename": "logo.png", "status": "added"},
				map[string]any{"filename": "new.go", "previous_filename": "old.go", "status": "renamed", "patch": "@@"},
			})
		case "/repos/octo/app/commits/" + strings.Repeat("a", 40) + "/check-runs":
			write(map[string]any{"check_runs": []any{
				map[string]any{"name": "lint", "status": "completed", "conclusion": "failure", "completed_at": "2026-11-20T08:20:00Z"},
				map[string]any{"name": "test", "status": "completed", "conclusion": "success"},
				map[string]any{"name": "e2e", "status": "in_progress"},
			}})
		case "/repos/octo/app/commits/" + strings.Repeat("a", 40) + "/status":
			w.WriteHeader(http.StatusForbidden) // a fine-grained token without Commit statuses
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Client{api: server.URL, web: "github.com", token: goodToken, http: server.Client()}, &seen
}

func TestSearchKeepsOnlyPullRequestsOfThisGitHub(t *testing.T) {
	client, seen := pullsServer(t)
	result, err := client.SearchMine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 1 || result.Me != "arthur" {
		t.Fatalf("%+v", result)
	}
	pr := result.Items[0]
	if pr.Ref != (PRRef{"octo", "app", 42}) || !pr.Draft || pr.WebURL != "https://github.com/octo/app/pull/42" {
		t.Fatalf("%+v", pr)
	}
	if query := (*seen)[0]; !strings.Contains(query, "author%3A%40me") || !strings.Contains(query, "is%3Aopen") {
		t.Errorf("query %s", query)
	}
}

func TestTheDiscussionMergesCommentsCodeCommentsAndReviews(t *testing.T) {
	client, _ := pullsServer(t)
	ref := PRRef{"octo", "app", 42}
	since := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	comments, err := client.Discussion(context.Background(), ref, since, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range comments {
		got = append(got, c.Kind+"/"+c.Author+"/"+c.State+"/"+c.File)
	}
	want := "code comment/thibaut//a.go,review/thibaut/changes requested/,comment/gael//,comment/codecov[bot]//"
	if strings.Join(got, ",") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, ","), want)
	}
	if !comments[3].Bot {
		t.Error("a bot is marked as one")
	}
	latest, _ := client.Discussion(context.Background(), ref, time.Time{}, 2)
	if len(latest) != 2 || latest[1].Author != "codecov[bot]" {
		t.Errorf("the latest are kept: %+v", latest)
	}
}

func TestDetailsFilesAndChecks(t *testing.T) {
	client, _ := pullsServer(t)
	ref := PRRef{"octo", "app", 42}
	details, err := client.PullRequest(context.Background(), ref)
	if err != nil || details.HeadBranch != "fix" || details.BaseBranch != "main" || details.Reviewers[0] != "gael" || details.Labels[0] != "bug" {
		t.Fatalf("%+v %v", details, err)
	}
	files, err := client.Files(context.Background(), ref, 2)
	if err != nil || len(files.Files) != 2 || !files.More || files.Files[1].Status != "new" || files.Files[1].Patch != "" {
		t.Fatalf("%+v %v", files, err)
	}
	checks, err := client.Checks(context.Background(), ref, details.HeadSHA)
	if err != nil || !checks.Any || !checks.Pending || len(checks.Failed) != 1 || checks.Failed[0].Name != "lint" {
		t.Fatalf("one of the two check routes refused is not an error: %+v %v", checks, err)
	}
	if _, err := client.Checks(context.Background(), ref, "../../x"); err == nil {
		t.Error("a commit must be a commit hash")
	}
}

func TestTheOverviewCompletesBranchesFromTheDetails(t *testing.T) {
	client, seen := pullsServer(t)
	overview, err := client.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.MineTotal != 3 || overview.ToReviewTotal != 3 || len(overview.Mine) != 1 || len(overview.ToReview) != 1 {
		t.Fatalf("%+v", overview)
	}
	for _, pr := range append(overview.Mine, overview.ToReview...) {
		if pr.HeadBranch != "fix" || pr.BaseBranch != "main" {
			t.Errorf("branches not completed: %+v", pr)
		}
	}
	searches := 0
	for _, call := range *seen {
		if strings.HasPrefix(call, "/search/issues") {
			searches++
		}
	}
	if searches != 2 {
		t.Errorf("an overview is two searches, got %d", searches)
	}
}
