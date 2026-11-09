package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func parsingClient() *Client {
	return &Client{api: "https://gitlab.example.com/api/v4", token: "t"}
}

func TestParseMergeRequestAcceptsThisServersLinksAndTheShortForm(t *testing.T) {
	client := parsingClient()
	for text, want := range map[string]MRRef{
		"https://gitlab.example.com/dating/vision/-/merge_requests/1724/diffs":      {"dating/vision", 1724},
		"https://gitlab.example.com/a/b/c/-/merge_requests/9":                       {"a/b/c", 9},
		"https://GITLAB.example.com/dating/vision/-/merge_requests/5?tab=changes#x": {"dating/vision", 5},
		"https://gitlab.example.com/dating/vision/merge_requests/12":                {"dating/vision", 12},
		"dating/vision!1724":     {"dating/vision", 1724},
		"  group/sub/project!3 ": {"group/sub/project", 3},
	} {
		got, ok := client.ParseMergeRequest(text)
		if !ok || got != want {
			t.Errorf("%q: got %+v %v, want %+v", text, got, ok, want)
		}
	}
}

func TestParseMergeRequestRefusesAnythingThatCouldAimTheTokenElsewhere(t *testing.T) {
	client := parsingClient()
	for _, text := range []string{
		"https://evil.example.com/dating/vision/-/merge_requests/1",
		"http://gitlab.example.com/dating/vision/-/merge_requests/1",
		"https://user@gitlab.example.com/dating/vision/-/merge_requests/1",
		"https://gitlab.example.com.evil.com/dating/vision/-/merge_requests/1",
		"https://gitlab.example.com/dating/../admin/-/merge_requests/1",
		"https://gitlab.example.com/dating/vision/-/merge_requests/0",
		"https://gitlab.example.com/dating/vision/-/merge_requests/abc",
		"https://gitlab.example.com/dating/vision/-/issues/1",
		"https://gitlab.example.com/vision/-/merge_requests/1",
		"../admin!1", "a/./b!1", "a/b!0", "a/b!x", "a b/c!1", "a//b!1", "a/b!99999999999", "",
		"https://gitlab.example.com/a%2Fb/c/-/merge_requests/1",
	} {
		if got, ok := client.ParseMergeRequest(text); ok {
			t.Errorf("%q was accepted as %+v", text, got)
		}
	}
}

func reviewServer(t *testing.T, diffsStatus int) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(r.URL.EscapedPath(), "/api/v4/projects/dating%2Fvision/merge_requests/1724") {
			t.Errorf("the project is one encoded segment: %s", r.URL.EscapedPath())
		}
		switch strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4/projects/dating%2Fvision/merge_requests/1724") {
		case "":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"iid": 1724, "title": "Add vision API", "description": "Adds the endpoint", "state": "opened", "draft": true,
				"source_branch": "feat", "target_branch": "main", "detailed_merge_status": "mergeable", "changes_count": "3",
				"author": map[string]string{"username": "samuel"}, "reviewers": []map[string]string{{"username": "arthur"}, {"username": ""}},
				"labels": []string{"vision"}, "head_pipeline": map[string]string{"status": "success"},
				"web_url": "https://elsewhere.example.com/phish", "updated_at": "2026-11-09T10:00:00Z",
			})
		case "/notes":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"body": "added 2 commits", "system": true, "author": map[string]string{"username": "samuel"}},
				{"body": "Why not reuse the client?", "created_at": "2026-11-09T10:05:00Z", "author": map[string]string{"username": "jocelyn"}, "resolved": true, "position": map[string]string{"new_path": "api.go"}},
				{"body": "  ", "author": map[string]string{"username": "x"}},
				{"body": "LGTM overall", "author": map[string]string{"username": "arthur"}},
			})
		case "/diffs":
			if diffsStatus != 0 {
				w.WriteHeader(diffsStatus)
				return
			}
			if per := r.URL.Query().Get("per_page"); per != "3" && per != "4" {
				t.Errorf("one more file than wanted is asked for: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"old_path": "api.go", "new_path": "api.go", "diff": "@@ -1 +1 @@\n-a\n+b\n"},
				{"old_path": "new.go", "new_path": "new.go", "new_file": true, "diff": "@@ -0,0 +1 @@\n+x\n"},
				{"old_path": "big.json", "new_path": "big.json", "too_large": true},
			})
		case "/changes":
			_ = json.NewEncoder(w).Encode(map[string]any{"changes": []map[string]any{{"old_path": "old.go", "new_path": "renamed.go", "renamed_file": true, "diff": "@@"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &Client{api: server.URL + "/api/v4", token: "tok", http: server.Client()}
}

func TestMergeRequestReturnsWhatAReviewNeeds(t *testing.T) {
	details, err := reviewServer(t, 0).MergeRequest(context.Background(), MRRef{"dating/vision", 1724})
	if err != nil {
		t.Fatal(err)
	}
	if details.Title != "Add vision API" || details.Description != "Adds the endpoint" || !details.Draft || details.Author != "samuel" ||
		details.SourceBranch != "feat" || details.TargetBranch != "main" || details.PipelineStatus != "success" || details.ChangedFiles != "3" ||
		details.MergeStatus != "mergeable" || details.ProjectPath != "dating/vision" {
		t.Fatalf("%+v", details)
	}
	if len(details.Reviewers) != 1 || details.Reviewers[0] != "arthur" || len(details.Labels) != 1 {
		t.Errorf("reviewers %v labels %v", details.Reviewers, details.Labels)
	}
	if details.WebURL != "" {
		t.Errorf("a link to another host is not handed on: %q", details.WebURL)
	}
}

func TestCommentsLeaveSystemNotesAndEmptyOnesOut(t *testing.T) {
	comments, err := reviewServer(t, 0).Comments(context.Background(), MRRef{"dating/vision", 1724}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].Author != "jocelyn" || !comments[0].Resolved || comments[0].File != "api.go" || comments[1].Body != "LGTM overall" {
		t.Fatalf("%+v", comments)
	}
	if one, _ := reviewServer(t, 0).Comments(context.Background(), MRRef{"dating/vision", 1724}, 1); len(one) != 1 {
		t.Errorf("capped: %+v", one)
	}
}

func TestDiffsListEveryFileWithItsStatusAndWhyADiffIsMissing(t *testing.T) {
	diffs, err := reviewServer(t, 0).Diffs(context.Background(), MRRef{"dating/vision", 1724}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !diffs.More || len(diffs.Files) != 2 {
		t.Fatalf("%+v", diffs)
	}
	if diffs.Files[0].Status != "modified" || !strings.Contains(diffs.Files[0].Diff, "+b") || diffs.Files[1].Status != "new" {
		t.Errorf("%+v", diffs.Files)
	}
	all, _ := reviewServer(t, 0).Diffs(context.Background(), MRRef{"dating/vision", 1724}, 3)
	if all.More || len(all.Files) != 3 || all.Files[2].Omitted != "too large" || all.Files[2].Diff != "" {
		t.Errorf("%+v", all)
	}
}

func TestDiffsFallBackToTheOlderRouteOnAServerWithoutIt(t *testing.T) {
	diffs, err := reviewServer(t, http.StatusNotFound).Diffs(context.Background(), MRRef{"dating/vision", 1724}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs.Files) != 1 || diffs.Files[0].Status != "renamed" || diffs.Files[0].NewPath != "renamed.go" {
		t.Fatalf("%+v", diffs)
	}
}

func TestAReferenceThatIsNotValidNeverReachesTheNetwork(t *testing.T) {
	client := reviewServer(t, 0)
	for _, ref := range []MRRef{{"", 1}, {"a/../b", 1}, {"a/b", 0}, {"only-one", 1}} {
		if _, err := client.MergeRequest(context.Background(), ref); err == nil {
			t.Errorf("%+v was accepted", ref)
		}
	}
}
