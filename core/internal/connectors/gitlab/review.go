package gitlab

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// MRRef names one merge request: the project path ("group/subgroup/project") and its number in that project.
type MRRef struct {
	Project string
	IID     int
}

func (r MRRef) String() string { return r.Project + "!" + strconv.Itoa(r.IID) }

var (
	segment    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	shortRef   = regexp.MustCompile(`^([^!\s]+)!(\d{1,9})$`)
	mergeMarks = []string{"/-/merge_requests/", "/merge_requests/"}
)

const maxProjectDepth = 10

// validProject accepts a path of plain segments only: no empty segment, no dot segment, nothing that could change
// the meaning of the address the project becomes part of.
func validProject(path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) < 2 || len(parts) > maxProjectDepth {
		return false
	}
	for _, part := range parts {
		if part == "." || part == ".." || !segment.MatchString(part) {
			return false
		}
	}
	return true
}

// ParseMergeRequest reads a merge request reference: the address of a merge request on THIS server (the one the
// token belongs to; a link to another host is refused, so a message cannot aim the token elsewhere) or the short
// form "group/project!123".
func (c *Client) ParseMergeRequest(text string) (MRRef, bool) {
	text = strings.TrimSpace(text)
	if match := shortRef.FindStringSubmatch(text); match != nil && !strings.Contains(text, "://") {
		iid, _ := strconv.Atoi(match[2])
		if validProject(match[1]) && iid > 0 {
			return MRRef{Project: match[1], IID: iid}, true
		}
		return MRRef{}, false
	}
	base, err := url.Parse(c.api)
	if err != nil {
		return MRRef{}, false
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || !strings.EqualFold(parsed.Host, base.Host) {
		return MRRef{}, false
	}
	path := parsed.EscapedPath()
	for _, mark := range mergeMarks {
		head, tail, found := strings.Cut(path, mark)
		if !found {
			continue
		}
		digits := tail
		if end := strings.IndexByte(tail, '/'); end >= 0 {
			digits = tail[:end]
		}
		iid, err := strconv.Atoi(digits)
		project := strings.Trim(head, "/")
		if err != nil || iid <= 0 || iid > 999_999_999 || !validProject(project) {
			return MRRef{}, false
		}
		return MRRef{Project: project, IID: iid}, true
	}
	return MRRef{}, false
}

// path is the part of an API address that names the merge request. The project path is one address segment, so its
// slashes are encoded; every segment was checked by validProject.
func (r MRRef) path(suffix string) (string, error) {
	if !validProject(r.Project) || r.IID <= 0 {
		return "", connector.Invalid("target", "This is not a merge request reference.")
	}
	return "/projects/" + strings.ReplaceAll(url.PathEscape(r.Project), "/", "%2F") + "/merge_requests/" + strconv.Itoa(r.IID) + suffix, nil
}

// MergeRequestDetails is a merge request with what a review needs around the code. Every text field was written by
// people: it is data.
type MergeRequestDetails struct {
	MergeRequest
	State       string
	Description string
	Labels      []string
	Reviewers   []string
	// ChangedFiles is what GitLab says ("12", or "1000+" for a very large one); "" when it does not say.
	ChangedFiles string
}

// MergeRequest reads one merge request.
func (c *Client) MergeRequest(ctx context.Context, ref MRRef) (MergeRequestDetails, error) {
	path, err := ref.path("")
	if err != nil {
		return MergeRequestDetails{}, err
	}
	var payload struct {
		mergeRequestPayload
		Description  string `json:"description"`
		ChangesCount string `json:"changes_count"`
		Labels       []string
		Reviewers    []struct{ Username string }
	}
	if _, err := c.get(ctx, path, nil, &payload); err != nil {
		return MergeRequestDetails{}, err
	}
	if payload.IID == 0 || payload.Title == "" {
		return MergeRequestDetails{}, svchttp.UnexpectedError(service)
	}
	status := pipelineStatus(payload.HeadPipeline)
	if status == "" {
		status = pipelineStatus(payload.Pipeline)
	}
	merge := MergeRequest{
		ProjectPath: ref.Project, IID: payload.IID, Title: payload.Title, WebURL: c.ownLink(payload.WebURL), Author: payload.Author.Username,
		SourceBranch: payload.SourceBranch, TargetBranch: payload.TargetBranch, Draft: payload.Draft || payload.WorkInProgress,
		PipelineStatus: status, MergeStatus: payload.DetailedMergeStatus, HasConflicts: payload.HasConflicts, UpdatedAt: payload.UpdatedAt,
	}
	details := MergeRequestDetails{MergeRequest: merge, State: payload.State, Description: payload.Description, Labels: payload.Labels, ChangedFiles: payload.ChangesCount}
	for _, reviewer := range payload.Reviewers {
		if reviewer.Username != "" {
			details.Reviewers = append(details.Reviewers, reviewer.Username)
		}
	}
	return details, nil
}

// Comment is something a person wrote on a merge request (system notes such as "added 2 commits" are left out).
type Comment struct {
	Author    string
	Body      string
	CreatedAt string
	// Resolved is set for a review thread that was resolved.
	Resolved bool
	// File is the file a comment on the code is about; "" for a general comment.
	File string
}

// Comments returns the first `limit` comments, oldest first.
func (c *Client) Comments(ctx context.Context, ref MRRef, limit int) ([]Comment, error) {
	path, err := ref.path("/notes")
	if err != nil {
		return nil, err
	}
	var payload []struct {
		Body      string                    `json:"body"`
		System    bool                      `json:"system"`
		CreatedAt string                    `json:"created_at"`
		Resolved  bool                      `json:"resolved"`
		Author    struct{ Username string } `json:"author"`
		Position  struct {
			NewPath string `json:"new_path"`
			OldPath string `json:"old_path"`
		} `json:"position"`
	}
	query := url.Values{"sort": {"asc"}, "order_by": {"created_at"}, "per_page": {"50"}}
	if _, err := c.get(ctx, path, query, &payload); err != nil {
		return nil, err
	}
	var comments []Comment
	for _, note := range payload {
		if note.System || strings.TrimSpace(note.Body) == "" {
			continue
		}
		if len(comments) == limit {
			break
		}
		file := note.Position.NewPath
		if file == "" {
			file = note.Position.OldPath
		}
		comments = append(comments, Comment{Author: note.Author.Username, Body: note.Body, CreatedAt: note.CreatedAt, Resolved: note.Resolved, File: file})
	}
	return comments, nil
}

// FileDiff is what changed in one file. Diff is a unified diff, and empty when GitLab left it out (see Omitted).
type FileDiff struct {
	OldPath string
	NewPath string
	// Status is "new", "deleted", "renamed" or "modified".
	Status string
	Diff   string
	// Omitted says why GitLab gave no diff ("too large", "collapsed", "generated"); "" when it gave one.
	Omitted string
}

// Diffs is the changed files of a merge request: the first page of at most `limit` files, and whether there are more.
type Diffs struct {
	Files []FileDiff
	More  bool
}

type diffPayload struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	Diff        string `json:"diff"`
	TooLarge    bool   `json:"too_large"`
	Collapsed   bool   `json:"collapsed"`
	Generated   bool   `json:"generated_file"`
}

func (p diffPayload) file() FileDiff {
	file := FileDiff{OldPath: p.OldPath, NewPath: p.NewPath, Diff: p.Diff, Status: "modified"}
	switch {
	case p.NewFile:
		file.Status = "new"
	case p.DeletedFile:
		file.Status = "deleted"
	case p.RenamedFile:
		file.Status = "renamed"
	}
	switch {
	case p.TooLarge:
		file.Omitted = "too large"
	case p.Generated:
		file.Omitted = "generated"
	case p.Collapsed:
		file.Omitted = "collapsed"
	}
	return file
}

// Diffs reads the changes of a merge request. The /diffs route (GitLab 15.7 and later) is tried first, the older
// /changes route when the server does not have it.
func (c *Client) Diffs(ctx context.Context, ref MRRef, limit int) (Diffs, error) {
	path, err := ref.path("/diffs")
	if err != nil {
		return Diffs{}, err
	}
	var files []diffPayload
	// One more than wanted tells whether there are more files.
	_, err = c.get(ctx, path, url.Values{"per_page": {strconv.Itoa(limit + 1)}}, &files)
	if svchttp.Status(err) == 404 {
		var legacy struct {
			Changes []diffPayload `json:"changes"`
		}
		oldPath, pathErr := ref.path("/changes")
		if pathErr != nil {
			return Diffs{}, pathErr
		}
		if _, err = c.get(ctx, oldPath, nil, &legacy); err == nil {
			files = legacy.Changes
		}
	}
	if err != nil {
		return Diffs{}, err
	}
	if files == nil {
		return Diffs{}, svchttp.UnexpectedError(service)
	}
	out := Diffs{More: len(files) > limit}
	for _, file := range files[:min(len(files), limit)] {
		if file.NewPath == "" && file.OldPath == "" {
			continue
		}
		out.Files = append(out.Files, file.file())
	}
	return out, nil
}
