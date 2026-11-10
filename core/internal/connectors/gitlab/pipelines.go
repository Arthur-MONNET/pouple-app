package gitlab

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

const (
	// maxPushedProjects is how many recently pushed projects one look at the pipelines covers.
	maxPushedProjects = 8
	pipelinePageSize  = "20"
	maxFailedJobs     = 3
	// logTailBytes is how much of the end of a job log is kept; the cause of a failure is almost always there.
	logTailBytes = 12 << 10
	// LogTailLines and LogTailRunes bound what a failed job shows of its log.
	LogTailLines = 60
	LogTailRunes = 4000
)

// Finished pipeline statuses worth a look: the others (canceled, skipped, manual) are not an outcome.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// Pipeline is a finished pipeline of the account. Every text field comes from GitLab and from other people: data.
type Pipeline struct {
	ID        int
	ProjectID int
	// Project is the path of the project ("group/project"), "" when GitLab did not say.
	Project string
	Ref     string
	SHA     string
	Status  string
	WebURL  string
	// Source is what started it ("push", "merge_request_event", "schedule"...).
	Source     string
	FinishedAt time.Time
	// CommitTitle is the title of the commit it ran on, "" when unknown.
	CommitTitle string
	// MergeRequest is the open merge request of the branch, nil when there is none.
	MergeRequest *PipelineMergeRequest
}

// PipelineMergeRequest is the merge request a pipeline belongs to.
type PipelineMergeRequest struct {
	IID    int
	Title  string
	WebURL string
}

// FailedJob is a job that failed in a pipeline, with the end of its log.
type FailedJob struct {
	Name          string
	Stage         string
	FailureReason string
	// AllowedToFail jobs do not fail the pipeline: they are listed last.
	AllowedToFail bool
	WebURL        string
	// LogTail is the last lines of the log, without terminal colors; "" when it could not be read.
	LogTail string
}

type pushEvent struct {
	ProjectID int `json:"project_id"`
}

type pipelineListItem struct {
	ID        int    `json:"id"`
	ProjectID int    `json:"project_id"`
	Status    string `json:"status"`
	Source    string `json:"source"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	WebURL    string `json:"web_url"`
	UpdatedAt string `json:"updated_at"`
}

// RecentPipelines lists the pipelines of this account that finished, as a success or a failure, after since. The
// projects looked at are those the account pushed to in the last two days: GitLab has no list of "my pipelines".
// A project that cannot be read is skipped; the call fails only when the account itself cannot be read.
func (c *Client) RecentPipelines(ctx context.Context, since time.Time) ([]Pipeline, error) {
	user, err := c.User(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := c.pushedProjects(ctx, user.ID, since)
	if err != nil {
		return nil, err
	}
	var found []Pipeline
	for _, projectID := range projects {
		var items []pipelineListItem
		query := url.Values{"username": {user.Username}, "updated_after": {since.UTC().Format(time.RFC3339)}, "order_by": {"updated_at"}, "sort": {"desc"}, "per_page": {pipelinePageSize}}
		if _, err := c.get(ctx, "/projects/"+strconv.Itoa(projectID)+"/pipelines", query, &items); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		for _, item := range items {
			if pipeline, ok := item.pipeline(c); ok {
				found = append(found, pipeline)
			}
		}
	}
	return found, nil
}

// pushedProjects is the ids of the projects the account pushed to since the day before since, newest first.
func (c *Client) pushedProjects(ctx context.Context, userID int, since time.Time) ([]int, error) {
	var events []pushEvent
	query := url.Values{"action": {"pushed"}, "after": {since.UTC().AddDate(0, 0, -1).Format("2006-01-02")}, "per_page": {"50"}}
	if _, err := c.get(ctx, "/users/"+strconv.Itoa(userID)+"/events", query, &events); err != nil {
		return nil, err
	}
	var ids []int
	seen := map[int]bool{}
	for _, event := range events {
		if event.ProjectID > 0 && !seen[event.ProjectID] && len(ids) < maxPushedProjects {
			seen[event.ProjectID] = true
			ids = append(ids, event.ProjectID)
		}
	}
	return ids, nil
}

func (item pipelineListItem) pipeline(c *Client) (Pipeline, bool) {
	finished, err := time.Parse(time.RFC3339, item.UpdatedAt)
	if item.ID == 0 || item.ProjectID == 0 || err != nil || (item.Status != StatusSuccess && item.Status != StatusFailed) {
		return Pipeline{}, false
	}
	return Pipeline{
		ID: item.ID, ProjectID: item.ProjectID, Ref: item.Ref, SHA: item.SHA, Status: item.Status,
		WebURL: c.ownLink(item.WebURL), Source: item.Source, FinishedAt: finished,
	}, true
}

var mergeRequestRefRe = regexp.MustCompile(`^refs/merge-requests/(\d+)/(?:head|merge)$`)

// Describe fills what makes a pipeline understandable: the project path, the commit title and the merge request of
// its branch. Whatever GitLab does not answer stays empty: it never fails.
func (c *Client) Describe(ctx context.Context, pipeline Pipeline) Pipeline {
	base := "/projects/" + strconv.Itoa(pipeline.ProjectID)
	var project struct {
		Path string `json:"path_with_namespace"`
	}
	if _, err := c.get(ctx, base, url.Values{"simple": {"true"}}, &project); err == nil && validProject(project.Path) {
		pipeline.Project = project.Path
	}
	var commit struct {
		Title string `json:"title"`
	}
	if pipeline.SHA != "" {
		if _, err := c.get(ctx, base+"/repository/commits/"+url.PathEscape(pipeline.SHA), nil, &commit); err == nil {
			pipeline.CommitTitle = commit.Title
		}
	}
	pipeline.MergeRequest = c.pipelineMergeRequest(ctx, base, pipeline.Ref)
	return pipeline
}

// pipelineMergeRequest finds the open merge request a ref belongs to: a merge request pipeline names it, a branch
// pipeline is matched by its source branch.
func (c *Client) pipelineMergeRequest(ctx context.Context, base, ref string) *PipelineMergeRequest {
	var path string
	query := url.Values{"per_page": {"1"}}
	if match := mergeRequestRefRe.FindStringSubmatch(ref); match != nil {
		path = base + "/merge_requests/" + match[1]
		query = nil
	} else if ref != "" {
		path = base + "/merge_requests"
		query.Set("source_branch", ref)
		query.Set("state", "opened")
	} else {
		return nil
	}
	var payloads []mergeRequestPayload
	if query == nil {
		var single mergeRequestPayload
		if _, err := c.get(ctx, path, nil, &single); err != nil {
			return nil
		}
		payloads = []mergeRequestPayload{single}
	} else if _, err := c.get(ctx, path, query, &payloads); err != nil {
		return nil
	}
	if len(payloads) == 0 || payloads[0].IID == 0 || payloads[0].Title == "" {
		return nil
	}
	return &PipelineMergeRequest{IID: payloads[0].IID, Title: payloads[0].Title, WebURL: c.ownLink(payloads[0].WebURL)}
}

// FailedJobs lists the jobs that failed in a pipeline, the ones that fail it first, each with the end of its log.
// A log that cannot be read leaves the job without one.
func (c *Client) FailedJobs(ctx context.Context, pipeline Pipeline) ([]FailedJob, error) {
	base := "/projects/" + strconv.Itoa(pipeline.ProjectID)
	var payloads []struct {
		ID            int    `json:"id"`
		Name          string `json:"name"`
		Stage         string `json:"stage"`
		FailureReason string `json:"failure_reason"`
		AllowFailure  bool   `json:"allow_failure"`
		WebURL        string `json:"web_url"`
	}
	query := url.Values{"scope[]": {"failed"}, "per_page": {"20"}}
	if _, err := c.get(ctx, base+"/pipelines/"+strconv.Itoa(pipeline.ID)+"/jobs", query, &payloads); err != nil {
		return nil, err
	}
	var jobs []FailedJob
	for _, allowed := range []bool{false, true} {
		for _, payload := range payloads {
			if payload.AllowFailure != allowed || payload.ID == 0 || len(jobs) >= maxFailedJobs {
				continue
			}
			job := FailedJob{Name: payload.Name, Stage: payload.Stage, FailureReason: payload.FailureReason, AllowedToFail: payload.AllowFailure, WebURL: c.ownLink(payload.WebURL)}
			job.LogTail = c.logTail(ctx, base+"/jobs/"+strconv.Itoa(payload.ID)+"/trace")
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (c *Client) logTail(ctx context.Context, path string) string {
	raw, err := svchttp.GetTextTail(ctx, c.http, service, c.api+path, http.Header{"Private-Token": {c.token}}, logTailBytes)
	if err != nil {
		return ""
	}
	return TailOfLog(raw, LogTailLines, LogTailRunes)
}

var terminalCodeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07|\r`)

// TailOfLog cleans a job log (terminal colors, carriage returns, blank lines) and keeps its last lines, at most
// maxRunes of them.
func TailOfLog(raw string, maxLines, maxRunes int) string {
	clean := terminalCodeRe.ReplaceAllString(strings.ToValidUTF8(raw, ""), "")
	var lines []string
	for _, line := range strings.Split(clean, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " \t"))
		}
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	text := strings.Join(lines, "\n")
	if runes := []rune(text); len(runes) > maxRunes {
		text = "…" + string(runes[len(runes)-maxRunes+1:])
	}
	return text
}
