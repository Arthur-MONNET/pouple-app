// Package asana connects Pouple to Asana with a personal access token. The connector shows whose account it is;
// the client lets the assistant read the user's open tasks and search tasks as data. The one thing it writes is a
// comment on a task, when the user asked for it (see taskwork.go).
package asana

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

const (
	service       = "Asana"
	apiBase       = "https://app.asana.com/api/1.0"
	pageSize      = 100
	searchSize    = 10
	maxWorkspaces = 3
	maxSearchTerm = 2
	taskFields    = "name,due_on,completed,permalink_url,assignee.name,memberships.section.name,memberships.project.name"
)

// Client reads Asana and posts comments. The token is sent in the Authorization header, to Asana only.
type Client struct {
	token string
	base  string
	http  *http.Client

	mu         sync.Mutex
	workspaces []Workspace
	loaded     bool
}

func NewClient(token string) *Client {
	return &Client{token: token, base: apiBase, http: svchttp.NewPublicClient()}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	address := c.base + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	_, err := svchttp.GetJSON(ctx, c.http, service, address, http.Header{"Authorization": {"Bearer " + c.token}}, out)
	return err
}

// Workspace is an Asana workspace or organization.
type Workspace struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

// Me is the account a token belongs to.
type Me struct {
	GID        string
	Name       string
	Email      string
	PhotoURL   string
	Workspaces []Workspace
}

type mePayload struct {
	Data struct {
		GID        string            `json:"gid"`
		Name       string            `json:"name"`
		Email      string            `json:"email"`
		Photo      map[string]string `json:"photo"`
		Workspaces []Workspace       `json:"workspaces"`
	} `json:"data"`
}

// Me returns the owner of the token.
func (c *Client) Me(ctx context.Context) (Me, error) {
	var payload mePayload
	err := c.get(ctx, "/users/me", url.Values{"opt_fields": {"name,email,photo.image_128x128,workspaces.name"}}, &payload)
	if err != nil {
		return Me{}, err
	}
	if payload.Data.GID == "" {
		return Me{}, svchttp.UnexpectedError(service)
	}
	me := Me{GID: payload.Data.GID, Name: payload.Data.Name, Email: payload.Data.Email, Workspaces: payload.Data.Workspaces}
	me.PhotoURL = payload.Data.Photo["image_128x128"]
	return me, nil
}

// workspaceList is the user's workspaces (the first few), read once per client.
func (c *Client) workspaceList(ctx context.Context) ([]Workspace, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		me, err := c.Me(ctx)
		if err != nil {
			return nil, err
		}
		c.workspaces, c.loaded = me.Workspaces[:min(len(me.Workspaces), maxWorkspaces)], true
	}
	return c.workspaces, nil
}

// Task is one task, as the assistant reads it. Every text field comes from Asana and from other people: it is data.
type Task struct {
	GID          string
	Name         string
	DueOn        string
	Completed    bool
	PermalinkURL string
	Assignee     string
	Section      string
	Project      string
}

type taskPayload struct {
	GID          string `json:"gid"`
	Name         string `json:"name"`
	DueOn        string `json:"due_on"`
	Completed    bool   `json:"completed"`
	PermalinkURL string `json:"permalink_url"`
	Assignee     *struct {
		Name string `json:"name"`
	} `json:"assignee"`
	Memberships []struct {
		Section *struct {
			Name string `json:"name"`
		} `json:"section"`
		Project *struct {
			Name string `json:"name"`
		} `json:"project"`
	} `json:"memberships"`
}

func (p taskPayload) task() (Task, bool) {
	if p.GID == "" || p.Name == "" {
		return Task{}, false
	}
	task := Task{GID: p.GID, Name: p.Name, DueOn: p.DueOn, Completed: p.Completed}
	// Only Asana's own address is kept: the assistant must not receive a link to somewhere else.
	if link, err := url.Parse(p.PermalinkURL); err == nil && link.Scheme == "https" && strings.EqualFold(link.Host, "app.asana.com") {
		task.PermalinkURL = p.PermalinkURL
	}
	if p.Assignee != nil {
		task.Assignee = p.Assignee.Name
	}
	for _, membership := range p.Memberships {
		if membership.Section != nil && task.Section == "" {
			task.Section = membership.Section.Name
		}
		if membership.Project != nil && task.Project == "" {
			task.Project = membership.Project.Name
		}
	}
	return task, true
}

type taskList struct {
	Data     []taskPayload `json:"data"`
	NextPage *struct {
		Offset string `json:"offset"`
	} `json:"next_page"`
}

func (l taskList) tasks() []Task {
	var tasks []Task
	for _, payload := range l.Data {
		if task, ok := payload.task(); ok {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// Open is the user's incomplete tasks, soonest due date first, tasks without a date last.
type Open struct {
	Tasks []Task
	// More is true when a workspace holds more tasks than were read: Total is then a lower bound.
	More bool
}

func (c *Client) openIn(ctx context.Context, workspace string) ([]Task, bool, error) {
	var list taskList
	err := c.get(ctx, "/tasks", url.Values{
		"assignee": {"me"}, "workspace": {workspace}, "completed_since": {"now"},
		"limit": {strconv.Itoa(pageSize)}, "opt_fields": {taskFields},
	}, &list)
	if err != nil {
		return nil, false, err
	}
	var open []Task
	for _, task := range list.tasks() {
		if !task.Completed {
			open = append(open, task)
		}
	}
	return open, list.NextPage != nil && list.NextPage.Offset != "", nil
}

// OpenTasks reads the user's open tasks in each of their workspaces (the first few), in parallel.
func (c *Client) OpenTasks(ctx context.Context) (Open, error) {
	workspaces, err := c.workspaceList(ctx)
	if err != nil {
		return Open{}, err
	}
	type result struct {
		tasks []Task
		more  bool
		err   error
	}
	results := make([]result, len(workspaces))
	var wg sync.WaitGroup
	for i, workspace := range workspaces {
		wg.Go(func() { results[i].tasks, results[i].more, results[i].err = c.openIn(ctx, workspace.GID) })
	}
	wg.Wait()
	var open Open
	for _, r := range results {
		if r.err != nil {
			return Open{}, r.err
		}
		open.Tasks = append(open.Tasks, r.tasks...)
		open.More = open.More || r.more
	}
	slices.SortStableFunc(open.Tasks, func(a, b Task) int {
		return strings.Compare(dueKey(a), dueKey(b))
	})
	return open, nil
}

func dueKey(task Task) string {
	if task.DueOn == "" {
		return "9999-99-99"
	}
	return task.DueOn
}

// SearchTasks finds open tasks whose text matches the words. The search endpoint needs a paid Asana plan: on a free
// one the typeahead (task names) answers instead.
func (c *Client) SearchTasks(ctx context.Context, words []string) ([]Task, error) {
	workspaces, err := c.workspaceList(ctx)
	if err != nil {
		return nil, err
	}
	type result struct {
		tasks []Task
		err   error
	}
	results := make([]result, len(workspaces))
	var wg sync.WaitGroup
	for i, workspace := range workspaces {
		wg.Go(func() { results[i].tasks, results[i].err = c.searchIn(ctx, workspace.GID, words) })
	}
	wg.Wait()
	var found []Task
	seen := map[string]bool{}
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			firstErr = r.err
			continue
		}
		for _, task := range r.tasks {
			if !seen[task.GID] && !task.Completed {
				seen[task.GID] = true
				found = append(found, task)
			}
		}
	}
	if len(found) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return found[:min(len(found), searchSize)], nil
}

func (c *Client) searchIn(ctx context.Context, workspace string, words []string) ([]Task, error) {
	var list taskList
	err := c.get(ctx, "/workspaces/"+url.PathEscape(workspace)+"/tasks/search", url.Values{
		"text": {strings.Join(words, " ")}, "completed": {"false"}, "sort_by": {"modified_at"},
		"limit": {strconv.Itoa(searchSize)}, "opt_fields": {taskFields},
	}, &list)
	if err == nil {
		return list.tasks(), nil
	}
	switch svchttp.Status(err) {
	case http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound, http.StatusBadRequest:
	default:
		return nil, err
	}
	// Free plan: look up the longest words one by one in the task names.
	longest := slices.Clone(words)
	slices.SortStableFunc(longest, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	longest = longest[:min(len(longest), maxSearchTerm)]
	var found []Task
	for _, word := range longest {
		var typeahead taskList
		if err := c.get(ctx, "/workspaces/"+url.PathEscape(workspace)+"/typeahead", url.Values{
			"resource_type": {"task"}, "query": {word}, "count": {strconv.Itoa(searchSize)}, "opt_fields": {taskFields},
		}, &typeahead); err != nil {
			return found, nil
		}
		found = append(found, typeahead.tasks()...)
	}
	return found, nil
}
