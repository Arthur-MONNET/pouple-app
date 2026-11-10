package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pipelineServer(t *testing.T) *Client {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		write := func(value any) { _ = json.NewEncoder(w).Encode(value) }
		switch path := r.URL.Path; {
		case path == "/api/v4/user":
			write(map[string]any{"id": 5, "username": "arthur"})
		case path == "/api/v4/users/5/events":
			if r.URL.Query().Get("action") != "pushed" {
				t.Errorf("only pushes name the projects: %s", r.URL.RawQuery)
			}
			write([]map[string]any{{"project_id": 11}, {"project_id": 11}, {"project_id": 12}})
		case path == "/api/v4/projects/11/pipelines":
			if r.URL.Query().Get("username") != "arthur" || r.URL.Query().Get("updated_after") == "" {
				t.Errorf("only the account's pipelines since the last look: %s", r.URL.RawQuery)
			}
			write([]map[string]any{
				{"id": 100, "project_id": 11, "status": "failed", "source": "push", "ref": "feat", "sha": "abcdef1234567890", "web_url": server.URL + "/dating/vision/-/pipelines/100", "updated_at": "2026-11-10T09:00:00Z"},
				{"id": 101, "project_id": 11, "status": "running", "ref": "feat", "updated_at": "2026-11-10T09:01:00Z"},
				{"id": 102, "project_id": 11, "status": "canceled", "ref": "feat", "updated_at": "2026-11-10T09:02:00Z"},
				{"id": 103, "project_id": 11, "status": "success", "source": "merge_request_event", "ref": "refs/merge-requests/7/head", "web_url": "https://elsewhere.example.com/x", "updated_at": "2026-11-10T09:03:00Z"},
			})
		case path == "/api/v4/projects/12/pipelines":
			w.WriteHeader(http.StatusForbidden)
		case path == "/api/v4/projects/11":
			write(map[string]any{"path_with_namespace": "dating/vision"})
		case path == "/api/v4/projects/11/repository/commits/abcdef1234567890":
			write(map[string]any{"title": "Fix the login"})
		case path == "/api/v4/projects/11/merge_requests":
			if r.URL.Query().Get("source_branch") != "feat" || r.URL.Query().Get("state") != "opened" {
				t.Errorf("a branch pipeline finds the open merge request of its branch: %s", r.URL.RawQuery)
			}
			write([]map[string]any{{"iid": 7, "title": "Add vision API", "web_url": server.URL + "/dating/vision/-/merge_requests/7"}})
		case path == "/api/v4/projects/11/merge_requests/7":
			write(map[string]any{"iid": 7, "title": "Add vision API", "web_url": server.URL + "/dating/vision/-/merge_requests/7"})
		case path == "/api/v4/projects/11/pipelines/100/jobs":
			write([]map[string]any{
				{"id": 1, "name": "lint", "stage": "test", "failure_reason": "script_failure", "allow_failure": true},
				{"id": 2, "name": "rspec", "stage": "test", "failure_reason": "script_failure", "web_url": server.URL + "/dating/vision/-/jobs/2"},
			})
		case path == "/api/v4/projects/11/jobs/2/trace":
			_, _ = w.Write([]byte("\x1b[31mexpected 1, got 2\x1b[0m\r\n\n\nFailure\n"))
		case path == "/api/v4/projects/11/jobs/1/trace":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Client{api: server.URL + "/api/v4", token: "tok", http: server.Client()}
}

func TestRecentPipelinesKeepsOnlyTheFinishedOnesOfTheProjectsPushedTo(t *testing.T) {
	client := pipelineServer(t)
	found, err := client.RecentPipelines(context.Background(), time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].ID != 100 || found[0].Status != StatusFailed || found[1].ID != 103 || found[1].Status != StatusSuccess {
		t.Fatalf("a project that cannot be read is skipped, running and canceled pipelines are not outcomes: %+v", found)
	}
	if !strings.HasSuffix(found[0].WebURL, "/dating/vision/-/pipelines/100") || found[1].WebURL != "" {
		t.Errorf("only links on this GitLab server are kept: %q %q", found[0].WebURL, found[1].WebURL)
	}
}

func TestDescribeNamesTheProjectTheCommitAndTheMergeRequest(t *testing.T) {
	client := pipelineServer(t)
	branch := client.Describe(context.Background(), Pipeline{ID: 100, ProjectID: 11, Ref: "feat", SHA: "abcdef1234567890"})
	if branch.Project != "dating/vision" || branch.CommitTitle != "Fix the login" || branch.MergeRequest == nil || branch.MergeRequest.IID != 7 {
		t.Fatalf("branch pipeline: %+v", branch)
	}
	mr := client.Describe(context.Background(), Pipeline{ID: 103, ProjectID: 11, Ref: "refs/merge-requests/7/head"})
	if mr.MergeRequest == nil || mr.MergeRequest.Title != "Add vision API" {
		t.Fatalf("merge request pipeline: %+v", mr)
	}
}

func TestFailedJobsListsTheOnesThatFailThePipelineFirstWithACleanLogTail(t *testing.T) {
	client := pipelineServer(t)
	jobs, err := client.FailedJobs(context.Background(), Pipeline{ID: 100, ProjectID: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].Name != "rspec" || jobs[1].Name != "lint" || !jobs[1].AllowedToFail {
		t.Fatalf("jobs: %+v", jobs)
	}
	if jobs[0].LogTail != "expected 1, got 2\nFailure" {
		t.Errorf("terminal colors, carriage returns and blank lines are dropped: %q", jobs[0].LogTail)
	}
	if jobs[1].LogTail != "" {
		t.Errorf("an unreadable log is left empty: %q", jobs[1].LogTail)
	}
}

func TestTailOfLogKeepsTheEndWithinTheLimits(t *testing.T) {
	log := strings.Repeat("line\n", 100) + "last error"
	if got := TailOfLog(log, 3, 100); got != "line\nline\nlast error" {
		t.Errorf("lines: %q", got)
	}
	got := TailOfLog("0123456789", 10, 5)
	if got != "…6789" {
		t.Errorf("runes: %q", got)
	}
	if got := TailOfLog("\xffok", 5, 50); got != "ok" {
		t.Errorf("invalid bytes from a cut: %q", got)
	}
}
