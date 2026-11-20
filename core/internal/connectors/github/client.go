// Package github connects Pouple to GitHub, read-only: the user's pull requests, the ones awaiting their review, and a
// pull request's details, discussion, files and checks. Nothing here can write to GitHub.
package github

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

const (
	service = "GitHub"
	// listSize is how many pull requests one search returns.
	listSize = 20
	// maxDetails is how many listed pull requests get their branches completed, a few at a time.
	maxDetails  = 6
	concurrency = 3
	// apiVersion pins the REST API version the payloads below follow.
	apiVersion = "2022-11-28"
)

// Client reads one GitHub. The token is sent in the Authorization header, to its API only.
type Client struct {
	api   string
	web   string
	token string
	http  *http.Client
}

// NewClient builds a client for the address the user typed; the address is checked as ParseBaseURL does.
func NewClient(rawURL, token string) (*Client, error) {
	base, err := ParseBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &Client{api: base.API, web: base.Web, token: token, http: svchttp.NewClient()}, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	address := c.api + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	header := http.Header{
		"Authorization":        {"Bearer " + c.token},
		"X-Github-Api-Version": {apiVersion},
		"User-Agent":           {"Pouple"},
	}
	return svchttp.GetJSON(ctx, c.http, service, address, header, out)
}

// User is the account a token belongs to.
type User struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Type      string `json:"type"`
}

// User returns the owner of the token, and the headers GitHub says about the token with (scopes, SSO, expiry).
func (c *Client) User(ctx context.Context) (User, http.Header, error) {
	var user User
	header, err := c.get(ctx, "/user", nil, &user)
	if err == nil && user.Login == "" {
		return User{}, header, svchttp.UnexpectedError(service)
	}
	return user, header, err
}

// PRRef names one pull request: its repository and its number.
type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

func (r PRRef) String() string { return r.Owner + "/" + r.Repo + "#" + strconv.Itoa(r.Number) }

var (
	ownerName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	repoName  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	shortRef  = regexp.MustCompile(`^([^/\s#]+)/([^/\s#]+)#(\d{1,9})$`)
)

// valid accepts plain names only: nothing that could change the meaning of the address the reference becomes part of.
func (r PRRef) valid() bool {
	return ownerName.MatchString(r.Owner) && repoName.MatchString(r.Repo) && r.Repo != "." && r.Repo != ".." && r.Number > 0
}

func (r PRRef) repoPath(suffix string) (string, error) {
	if !r.valid() {
		return "", connector.Invalid("target", "This is not a pull request reference.")
	}
	return "/repos/" + r.Owner + "/" + r.Repo + suffix, nil
}

func (r PRRef) path(kind, suffix string) (string, error) {
	return r.repoPath("/" + kind + "/" + strconv.Itoa(r.Number) + suffix)
}

// ParsePullRequest reads a pull request reference: the address of a pull request on THIS GitHub (a link to another
// host is refused, so a message cannot aim the token elsewhere) or the short form "owner/repo#123".
func (c *Client) ParsePullRequest(text string) (PRRef, bool) {
	text = strings.TrimSpace(text)
	if match := shortRef.FindStringSubmatch(text); match != nil && !strings.Contains(text, "://") {
		number, _ := strconv.Atoi(match[3])
		ref := PRRef{Owner: match[1], Repo: match[2], Number: number}
		return ref, ref.valid()
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || !strings.EqualFold(parsed.Host, c.web) {
		return PRRef{}, false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return PRRef{}, false
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number > 999_999_999 {
		return PRRef{}, false
	}
	ref := PRRef{Owner: parts[0], Repo: parts[1], Number: number}
	return ref, ref.valid()
}

// ownLink keeps a link only when it points at this GitHub's web pages: what the service says about other addresses is
// not something to hand to the assistant (it could copy a phishing link into a reply).
func (c *Client) ownLink(raw string) string {
	link, err := url.Parse(raw)
	if err != nil || link.Scheme != "https" || link.User != nil || !strings.EqualFold(link.Host, c.web) {
		return ""
	}
	return raw
}

// PullRequest is one pull request as a list shows it. Every text field comes from GitHub and from other people: it is
// data.
type PullRequest struct {
	Ref       PRRef
	Title     string
	WebURL    string
	Author    string
	Draft     bool
	UpdatedAt string
	// HeadBranch and BaseBranch are known only when the details were read ("" otherwise).
	HeadBranch string
	BaseBranch string
}

type userPayload struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func (u userPayload) bot() bool { return u.Type == "Bot" || strings.HasSuffix(u.Login, "[bot]") }

type searchItem struct {
	Number        int         `json:"number"`
	Title         string      `json:"title"`
	HTMLURL       string      `json:"html_url"`
	RepositoryURL string      `json:"repository_url"`
	Draft         bool        `json:"draft"`
	UpdatedAt     string      `json:"updated_at"`
	User          userPayload `json:"user"`
	PullRequest   *struct{}   `json:"pull_request"`
}

// SearchResult is one page of pull requests a search found, and how many it found in all.
type SearchResult struct {
	Total int
	Items []PullRequest
	// Me is the login of the user when the search was about their own pull requests and found one ("" otherwise).
	Me string
}

// search runs one search of open pull requests, the latest updated first. GitHub's search has its own, small rate
// limit (30 a minute): callers bound how often they use it.
func (c *Client) search(ctx context.Context, qualifier string, size int) (SearchResult, error) {
	query := url.Values{
		"q": {"is:pr is:open archived:false " + qualifier}, "sort": {"updated"}, "order": {"desc"},
		"per_page": {strconv.Itoa(size)},
	}
	var payload struct {
		TotalCount *int         `json:"total_count"`
		Items      []searchItem `json:"items"`
	}
	if _, err := c.get(ctx, "/search/issues", query, &payload); err != nil {
		return SearchResult{}, err
	}
	if payload.TotalCount == nil {
		return SearchResult{}, svchttp.UnexpectedError(service)
	}
	out := SearchResult{Total: max(*payload.TotalCount, len(payload.Items))}
	for _, item := range payload.Items {
		ref, ok := c.repoOf(item.RepositoryURL, item.Number)
		if !ok || item.PullRequest == nil || item.Title == "" {
			continue
		}
		out.Items = append(out.Items, PullRequest{
			Ref: ref, Title: item.Title, WebURL: c.ownLink(item.HTMLURL), Author: item.User.Login, Draft: item.Draft, UpdatedAt: item.UpdatedAt,
		})
	}
	return out, nil
}

// repoOf reads the repository of a search result from its API address, which must be on this GitHub's API.
func (c *Client) repoOf(repositoryURL string, number int) (PRRef, bool) {
	rest, found := strings.CutPrefix(repositoryURL, c.api+"/repos/")
	if !found {
		return PRRef{}, false
	}
	owner, repo, ok := strings.Cut(rest, "/")
	ref := PRRef{Owner: owner, Repo: repo, Number: number}
	return ref, ok && ref.valid()
}

// SearchMine lists the user's open pull requests.
func (c *Client) SearchMine(ctx context.Context) (SearchResult, error) {
	result, err := c.search(ctx, "author:@me", listSize)
	if err == nil && len(result.Items) > 0 {
		// Every result was written by the user: their login comes for free.
		result.Me = result.Items[0].Author
	}
	return result, err
}

// SearchReviewRequested lists the open pull requests waiting for the user's review.
func (c *Client) SearchReviewRequested(ctx context.Context) (SearchResult, error) {
	return c.search(ctx, "review-requested:@me", listSize)
}

// CountOpen counts the user's open pull requests and the ones waiting for their review, one search call each. It also
// proves the token may search. The headers of the last answer are returned (an SSO notice may be in them).
func (c *Client) CountOpen(ctx context.Context) (mine, toReview int, header http.Header, err error) {
	var payload struct {
		TotalCount *int `json:"total_count"`
	}
	count := func(qualifier string) (int, error) {
		payload.TotalCount = nil
		query := url.Values{"q": {"is:pr is:open archived:false " + qualifier}, "per_page": {"1"}}
		header, err = c.get(ctx, "/search/issues", query, &payload)
		if err == nil && payload.TotalCount == nil {
			err = svchttp.UnexpectedError(service)
		}
		if err != nil {
			return 0, err
		}
		return *payload.TotalCount, nil
	}
	if mine, err = count("author:@me"); err != nil {
		return 0, 0, header, err
	}
	toReview, err = count("review-requested:@me")
	return mine, toReview, header, err
}

// Overview is the user's open pull requests and the ones waiting for their review.
type Overview struct {
	Mine          []PullRequest
	MineTotal     int
	ToReview      []PullRequest
	ToReviewTotal int
}

// Overview runs both searches in parallel, then completes the branches of the first few pull requests from their
// details. A detail that cannot be read leaves the branches unknown; it never fails the overview.
func (c *Client) Overview(ctx context.Context) (Overview, error) {
	var (
		wg                sync.WaitGroup
		mine, review      SearchResult
		mineErr, reviewEr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); mine, mineErr = c.SearchMine(ctx) }()
	go func() { defer wg.Done(); review, reviewEr = c.SearchReviewRequested(ctx) }()
	wg.Wait()
	for _, err := range []error{mineErr, reviewEr} {
		if err != nil {
			return Overview{}, err
		}
	}
	var missing []*PullRequest
	for _, list := range [][]PullRequest{mine.Items, review.Items} {
		for i := range list {
			if len(missing) < maxDetails {
				missing = append(missing, &list[i])
			}
		}
	}
	slots := make(chan struct{}, concurrency)
	var details sync.WaitGroup
	for _, pr := range missing {
		details.Add(1)
		slots <- struct{}{}
		go func() {
			defer details.Done()
			defer func() { <-slots }()
			if full, err := c.PullRequest(ctx, pr.Ref); err == nil {
				// Each goroutine owns a distinct pull request.
				pr.HeadBranch, pr.BaseBranch = full.HeadBranch, full.BaseBranch
			}
		}()
	}
	details.Wait()
	return Overview{Mine: mine.Items, MineTotal: mine.Total, ToReview: review.Items, ToReviewTotal: review.Total}, nil
}

// PullRequestDetails is a pull request with what a review needs around the code. Every text field was written by
// people: it is data.
type PullRequestDetails struct {
	PullRequest
	State        string
	Merged       bool
	Description  string
	HeadSHA      string
	MergeState   string
	ChangedFiles int
	Labels       []string
	Reviewers    []string
}

// PullRequest reads one pull request.
func (c *Client) PullRequest(ctx context.Context, ref PRRef) (PullRequestDetails, error) {
	path, err := ref.path("pulls", "")
	if err != nil {
		return PullRequestDetails{}, err
	}
	var payload struct {
		Number    int         `json:"number"`
		Title     string      `json:"title"`
		State     string      `json:"state"`
		Merged    bool        `json:"merged"`
		Draft     bool        `json:"draft"`
		Body      string      `json:"body"`
		HTMLURL   string      `json:"html_url"`
		UpdatedAt string      `json:"updated_at"`
		User      userPayload `json:"user"`
		Head      struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		MergeableState     string                  `json:"mergeable_state"`
		ChangedFiles       int                     `json:"changed_files"`
		Labels             []struct{ Name string } `json:"labels"`
		RequestedReviewers []userPayload           `json:"requested_reviewers"`
	}
	if _, err := c.get(ctx, path, nil, &payload); err != nil {
		return PullRequestDetails{}, err
	}
	if payload.Number == 0 || payload.Title == "" {
		return PullRequestDetails{}, svchttp.UnexpectedError(service)
	}
	details := PullRequestDetails{
		PullRequest: PullRequest{
			Ref: ref, Title: payload.Title, WebURL: c.ownLink(payload.HTMLURL), Author: payload.User.Login, Draft: payload.Draft,
			UpdatedAt: payload.UpdatedAt, HeadBranch: payload.Head.Ref, BaseBranch: payload.Base.Ref,
		},
		State: payload.State, Merged: payload.Merged, Description: payload.Body, HeadSHA: payload.Head.SHA,
		MergeState: payload.MergeableState, ChangedFiles: payload.ChangedFiles,
	}
	for _, label := range payload.Labels {
		if label.Name != "" {
			details.Labels = append(details.Labels, label.Name)
		}
	}
	for _, reviewer := range payload.RequestedReviewers {
		if reviewer.Login != "" {
			details.Reviewers = append(details.Reviewers, reviewer.Login)
		}
	}
	return details, nil
}

// Comment is something a person wrote on a pull request: a comment on the conversation, a comment on the code, or a
// review. Every text field was written by people: it is data.
type Comment struct {
	// Kind is "comment", "code comment" or "review"; IDs are unique within a kind only.
	Kind   string
	ID     int64
	Author string
	// Bot is set for an account GitHub marks as a bot (CI reports, dependency updates).
	Bot  bool
	Body string
	// State is the verdict of a review ("approved", "changes requested", "commented"); "" for comments.
	State     string
	File      string
	CreatedAt string
}

func (c Comment) time() time.Time {
	parsed, _ := time.Parse(time.RFC3339, c.CreatedAt)
	return parsed
}

// Discussion reads the comments, code comments and reviews of a pull request written after since (all of them for a
// zero time), oldest first, at most limit of them (the latest are kept). Reviews that only hold code comments (no text
// and no verdict) are left out: their comments are listed on their own.
func (c *Client) Discussion(ctx context.Context, ref PRRef, since time.Time, limit int) ([]Comment, error) {
	issuePath, err := ref.path("issues", "/comments")
	if err != nil {
		return nil, err
	}
	reviewPath, _ := ref.path("pulls", "/reviews")
	codePath, _ := ref.path("pulls", "/comments")
	query := url.Values{"per_page": {"100"}}
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339))
	}
	type commentPayload struct {
		ID          int64       `json:"id"`
		Body        string      `json:"body"`
		User        userPayload `json:"user"`
		CreatedAt   string      `json:"created_at"`
		SubmittedAt string      `json:"submitted_at"`
		State       string      `json:"state"`
		Path        string      `json:"path"`
	}
	var (
		wait                         sync.WaitGroup
		issue, code, reviews         []commentPayload
		issueErr, codeErr, reviewErr error
	)
	wait.Add(3)
	go func() { defer wait.Done(); _, issueErr = c.get(ctx, issuePath, query, &issue) }()
	go func() { defer wait.Done(); _, codeErr = c.get(ctx, codePath, query, &code) }()
	go func() {
		defer wait.Done()
		_, reviewErr = c.get(ctx, reviewPath, url.Values{"per_page": {"100"}}, &reviews)
	}()
	wait.Wait()
	for _, err := range []error{issueErr, codeErr, reviewErr} {
		if err != nil {
			return nil, err
		}
	}
	var out []Comment
	add := func(kind string, payload commentPayload, created, state string) {
		comment := Comment{Kind: kind, ID: payload.ID, Author: payload.User.Login, Bot: payload.User.bot(), Body: payload.Body, State: state, File: payload.Path, CreatedAt: created}
		if payload.ID == 0 || (!since.IsZero() && !comment.time().After(since)) {
			return
		}
		out = append(out, comment)
	}
	for _, payload := range issue {
		if strings.TrimSpace(payload.Body) != "" {
			add("comment", payload, payload.CreatedAt, "")
		}
	}
	for _, payload := range code {
		if strings.TrimSpace(payload.Body) != "" {
			add("code comment", payload, payload.CreatedAt, "")
		}
	}
	for _, payload := range reviews {
		state := reviewState(payload.State)
		if state == "" || (state == "commented" && strings.TrimSpace(payload.Body) == "") {
			continue // pending, dismissed, or only the wrapper of code comments
		}
		add("review", payload, payload.SubmittedAt, state)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].time().Before(out[b].time()) })
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func reviewState(state string) string {
	switch state {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes requested"
	case "COMMENTED":
		return "commented"
	}
	return ""
}

// FileDiff is what changed in one file. Patch is a unified diff, empty when GitHub gives none (binary or too large).
type FileDiff struct {
	Path    string
	OldPath string
	Status  string
	Patch   string
	Changes int
}

// Files is the changed files of a pull request: the first page of at most limit files, and whether there are more.
type Files struct {
	Files []FileDiff
	More  bool
}

// Files reads the changes of a pull request.
func (c *Client) Files(ctx context.Context, ref PRRef, limit int) (Files, error) {
	path, err := ref.path("pulls", "/files")
	if err != nil {
		return Files{}, err
	}
	var payload []struct {
		Filename         string `json:"filename"`
		PreviousFilename string `json:"previous_filename"`
		Status           string `json:"status"`
		Patch            string `json:"patch"`
		Changes          int    `json:"changes"`
	}
	// One more than wanted tells whether there are more files.
	if _, err := c.get(ctx, path, url.Values{"per_page": {strconv.Itoa(min(limit+1, 100))}}, &payload); err != nil {
		return Files{}, err
	}
	if payload == nil {
		return Files{}, svchttp.UnexpectedError(service)
	}
	out := Files{More: len(payload) > limit}
	for _, file := range payload[:min(len(payload), limit)] {
		if file.Filename == "" {
			continue
		}
		out.Files = append(out.Files, FileDiff{Path: file.Filename, OldPath: file.PreviousFilename, Status: fileStatus(file.Status), Patch: file.Patch, Changes: file.Changes})
	}
	return out, nil
}

func fileStatus(status string) string {
	switch status {
	case "added":
		return "new"
	case "removed":
		return "deleted"
	case "renamed", "copied":
		return status
	}
	return "modified"
}

// Check is one check or commit status of a commit.
type Check struct {
	Name string
	// CompletedAt is when it finished; "" while it runs.
	CompletedAt string
}

// Checks is what the checks of a commit say.
type Checks struct {
	Failed []Check
	// Pending is set while a check still runs or waits.
	Pending bool
	// Any is set when the commit has at least one check or status.
	Any bool
}

// Checks reads the check runs and the commit statuses of a commit of a pull request's repository. One of the two
// unreadable (a fine-grained token without the Checks or Commit statuses permission) is not an error; both are.
func (c *Client) Checks(ctx context.Context, ref PRRef, sha string) (Checks, error) {
	if !shaPattern.MatchString(sha) {
		return Checks{}, connector.Invalid("target", "This is not a commit.")
	}
	runsPath, err := ref.repoPath("/commits/" + sha + "/check-runs")
	if err != nil {
		return Checks{}, err
	}
	statusPath, _ := ref.repoPath("/commits/" + sha + "/status")
	var (
		wait              sync.WaitGroup
		runsErr, statusEr error
		runs              struct {
			CheckRuns []struct {
				Name        string `json:"name"`
				Status      string `json:"status"`
				Conclusion  string `json:"conclusion"`
				CompletedAt string `json:"completed_at"`
			} `json:"check_runs"`
		}
		status struct {
			Statuses []struct {
				Context   string `json:"context"`
				State     string `json:"state"`
				UpdatedAt string `json:"updated_at"`
			} `json:"statuses"`
		}
	)
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, runsErr = c.get(ctx, runsPath, url.Values{"filter": {"latest"}, "per_page": {"100"}}, &runs)
	}()
	go func() {
		defer wait.Done()
		_, statusEr = c.get(ctx, statusPath, url.Values{"per_page": {"100"}}, &status)
	}()
	wait.Wait()
	if runsErr != nil && statusEr != nil {
		return Checks{}, runsErr
	}
	var out Checks
	for _, run := range runs.CheckRuns {
		out.Any = true
		if run.Status != "completed" {
			out.Pending = true
			continue
		}
		switch run.Conclusion {
		case "failure", "timed_out", "startup_failure", "action_required":
			out.Failed = append(out.Failed, Check{Name: run.Name, CompletedAt: run.CompletedAt})
		}
	}
	for _, one := range status.Statuses {
		out.Any = true
		switch one.State {
		case "pending":
			out.Pending = true
		case "failure", "error":
			out.Failed = append(out.Failed, Check{Name: one.Context, CompletedAt: one.UpdatedAt})
		}
	}
	return out, nil
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
