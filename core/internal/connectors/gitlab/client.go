// Package gitlab connects Pouple to a GitLab server with a read-only personal access token. The connector shows
// whose account it is; the client lets the assistant read the user's merge requests as data.
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

const (
	service     = "GitLab"
	listSize    = "20"
	maxLookups  = 10
	concurrency = 5
)

// Client reads a GitLab server. The token is sent in the PRIVATE-TOKEN header, to that server only.
type Client struct {
	api   string
	token string
	http  *http.Client
}

// NewClient builds a client for the address the user typed; the address is checked as ParseBaseURL does.
func NewClient(rawURL, token string) (*Client, error) {
	base, err := ParseBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &Client{api: base.String() + "/api/v4", token: token, http: svchttp.NewClient()}, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	address := c.api + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	return svchttp.GetJSON(ctx, c.http, service, address, http.Header{"Private-Token": {c.token}}, out)
}

// User is the account a token belongs to.
type User struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
}

// User returns the owner of the token.
func (c *Client) User(ctx context.Context) (User, error) {
	var user User
	_, err := c.get(ctx, "/user", nil, &user)
	if err == nil && user.ID == 0 && user.Username == "" {
		return User{}, svchttp.UnexpectedError(service)
	}
	return user, err
}

// TokenInfo is what GitLab says about the token itself (GitLab 15.5 and later).
type TokenInfo struct {
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	Active    *bool    `json:"active"`
	Revoked   bool     `json:"revoked"`
}

// Token describes the token in use.
func (c *Client) Token(ctx context.Context) (TokenInfo, error) {
	var info TokenInfo
	_, err := c.get(ctx, "/personal_access_tokens/self", nil, &info)
	return info, err
}

type pipelinePayload struct {
	Status string `json:"status"`
}

type mergeRequestPayload struct {
	IID                 int                       `json:"iid"`
	ProjectID           int                       `json:"project_id"`
	Title               string                    `json:"title"`
	State               string                    `json:"state"`
	WebURL              string                    `json:"web_url"`
	Draft               bool                      `json:"draft"`
	WorkInProgress      bool                      `json:"work_in_progress"`
	SourceBranch        string                    `json:"source_branch"`
	TargetBranch        string                    `json:"target_branch"`
	DetailedMergeStatus string                    `json:"detailed_merge_status"`
	HasConflicts        bool                      `json:"has_conflicts"`
	UpdatedAt           string                    `json:"updated_at"`
	Author              struct{ Username string } `json:"author"`
	HeadPipeline        json.RawMessage           `json:"head_pipeline"`
	Pipeline            json.RawMessage           `json:"pipeline"`
	References          struct{ Full string }     `json:"references"`
}

// MergeRequest is one merge request, as the assistant reads it. Every text field comes from GitLab and from other
// people: it is data.
type MergeRequest struct {
	ProjectPath    string
	IID            int
	Title          string
	WebURL         string
	Author         string
	SourceBranch   string
	TargetBranch   string
	Draft          bool
	PipelineStatus string
	MergeStatus    string
	HasConflicts   bool
	UpdatedAt      string
}

func pipelineStatus(raw json.RawMessage) string {
	var pipeline pipelinePayload
	if len(raw) == 0 || json.Unmarshal(raw, &pipeline) != nil {
		return ""
	}
	return pipeline.Status
}

// ownLink keeps a link only when it points at the configured GitLab server: what the server says about other
// addresses is not something to hand to the assistant (it could copy a phishing link into a reply).
func (c *Client) ownLink(raw string) string {
	base, err := url.Parse(c.api)
	link, err2 := url.Parse(raw)
	if err != nil || err2 != nil || link.Scheme != "https" || !strings.EqualFold(link.Host, base.Host) {
		return ""
	}
	return raw
}

func (p mergeRequestPayload) toMergeRequest() (MergeRequest, bool) {
	if p.IID == 0 || p.Title == "" || p.WebURL == "" {
		return MergeRequest{}, false
	}
	path := ""
	if parsed, err := url.Parse(p.WebURL); err == nil {
		path, _, _ = strings.Cut(strings.Trim(parsed.Path, "/"), "/-/merge_requests/")
	}
	if path == "" {
		path, _, _ = strings.Cut(p.References.Full, "!")
	}
	status := pipelineStatus(p.HeadPipeline)
	if status == "" {
		status = pipelineStatus(p.Pipeline)
	}
	return MergeRequest{
		ProjectPath: path, IID: p.IID, Title: p.Title, WebURL: p.WebURL, Author: p.Author.Username,
		SourceBranch: p.SourceBranch, TargetBranch: p.TargetBranch, Draft: p.Draft || p.WorkInProgress,
		PipelineStatus: status, MergeStatus: p.DetailedMergeStatus, HasConflicts: p.HasConflicts, UpdatedAt: p.UpdatedAt,
	}, true
}

// list reads one page of open merge requests, and how many there are in all when GitLab says it.
func (c *Client) list(ctx context.Context, query url.Values, size string) ([]mergeRequestPayload, int, error) {
	query.Set("state", "opened")
	query.Set("order_by", "updated_at")
	query.Set("per_page", size)
	var payloads []mergeRequestPayload
	header, err := c.get(ctx, "/merge_requests", query, &payloads)
	if err != nil {
		return nil, 0, err
	}
	total, convErr := strconv.Atoi(header.Get("X-Total"))
	if convErr != nil || total < len(payloads) {
		total = len(payloads)
	}
	return payloads, total, nil
}

// CountOpenMergeRequests counts the open merge requests the user wrote. It also proves the token may read them.
func (c *Client) CountOpenMergeRequests(ctx context.Context) (int, error) {
	_, total, err := c.list(ctx, url.Values{"scope": {"created_by_me"}}, "1")
	return total, err
}

// Overview is the user's open merge requests and the ones waiting for their review.
type Overview struct {
	Mine          []MergeRequest
	MineTotal     int
	ToReview      []MergeRequest
	ToReviewTotal int
}

// Overview reads both lists in parallel. Pipelines that the list left out are completed from the detail endpoint,
// a few at a time.
func (c *Client) Overview(ctx context.Context) (Overview, error) {
	var (
		wg                          sync.WaitGroup
		mine, review                []mergeRequestPayload
		mineTotal, reviewTotal      int
		mineErr, userErr, reviewErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		mine, mineTotal, mineErr = c.list(ctx, url.Values{"scope": {"created_by_me"}}, listSize)
	}()
	go func() {
		defer wg.Done()
		var user User
		if user, userErr = c.User(ctx); userErr != nil {
			return
		}
		review, reviewTotal, reviewErr = c.list(ctx, url.Values{"scope": {"all"}, "reviewer_id": {strconv.Itoa(user.ID)}}, listSize)
	}()
	wg.Wait()
	for _, err := range []error{mineErr, userErr, reviewErr} {
		if err != nil {
			return Overview{}, err
		}
	}
	c.completePipelines(ctx, mine, review)
	overview := Overview{MineTotal: mineTotal, ToReviewTotal: reviewTotal}
	for _, payload := range mine {
		if mr, ok := payload.toMergeRequest(); ok {
			mr.WebURL = c.ownLink(mr.WebURL)
			overview.Mine = append(overview.Mine, mr)
		}
	}
	for _, payload := range review {
		if mr, ok := payload.toMergeRequest(); ok {
			mr.WebURL = c.ownLink(mr.WebURL)
			overview.ToReview = append(overview.ToReview, mr)
		}
	}
	return overview, nil
}

// completePipelines fills the pipeline of merge requests whose list entry has none (some GitLab versions leave it
// out). A lookup that fails leaves the status unknown; it never fails the overview.
func (c *Client) completePipelines(ctx context.Context, lists ...[]mergeRequestPayload) {
	var missing []*mergeRequestPayload
	for _, list := range lists {
		for i := range list {
			if len(list[i].HeadPipeline) == 0 && len(list[i].Pipeline) == 0 && list[i].ProjectID > 0 && len(missing) < maxLookups {
				missing = append(missing, &list[i])
			}
		}
	}
	slots := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, payload := range missing {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			var detail mergeRequestPayload
			if _, err := c.get(ctx, "/projects/"+strconv.Itoa(payload.ProjectID)+"/merge_requests/"+strconv.Itoa(payload.IID), nil, &detail); err == nil {
				// Each goroutine owns a distinct payload.
				payload.HeadPipeline, payload.Pipeline = detail.HeadPipeline, detail.Pipeline
			}
		}()
	}
	wg.Wait()
}
