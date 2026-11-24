package asana

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

var projectGID = regexp.MustCompile(`^[0-9]{1,25}$`)

var ErrCreationUnconfirmed = errors.New("Asana task creation is unconfirmed")

type Project struct {
	GID       string    `json:"gid"`
	Name      string    `json:"name"`
	Archived  bool      `json:"archived"`
	Workspace Workspace `json:"workspace"`
}

// ProjectGID accepts a project ID or an Asana project URL.
func ProjectGID(ref string) string {
	if projectGID.MatchString(ref) {
		return ref
	}
	link, err := url.Parse(ref)
	if err != nil || link.Scheme != "https" || link.User != nil || !strings.EqualFold(link.Host, "app.asana.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(link.EscapedPath(), "/"), "/")
	switch {
	case len(parts) >= 4 && parts[0] == "1" && parts[2] == "project" && projectGID.MatchString(parts[3]):
		return parts[3]
	case len(parts) >= 2 && parts[0] == "0" && projectGID.MatchString(parts[1]):
		return parts[1]
	}
	return ""
}

func (c *Client) Project(ctx context.Context, gid string) (Project, error) {
	if !projectGID.MatchString(gid) {
		return Project{}, connector.Invalid("project", "This is not an Asana project.")
	}
	var payload struct {
		Data Project `json:"data"`
	}
	err := c.get(ctx, "/projects/"+gid, url.Values{"opt_fields": {"name,archived,workspace.name"}}, &payload)
	if err != nil {
		return Project{}, err
	}
	if payload.Data.GID != gid || strings.TrimSpace(payload.Data.Name) == "" {
		return Project{}, svchttp.UnexpectedError(service)
	}
	if payload.Data.Archived {
		return Project{}, connector.Invalid("project", "The Asana project is archived.")
	}
	return payload.Data, nil
}

// FindProjects searches all connected workspaces, including every page. A partial search cannot establish uniqueness.
func (c *Client) FindProjects(ctx context.Context, name string) ([]Project, error) {
	if gid := ProjectGID(name); gid != "" {
		project, err := c.Project(ctx, gid)
		if err != nil {
			return nil, err
		}
		return []Project{project}, nil
	}
	me, err := c.Me(ctx)
	if err != nil {
		return nil, err
	}
	var found []Project
	seen := map[string]bool{}
	for _, workspace := range me.Workspaces {
		query := url.Values{"archived": {"false"}, "limit": {"100"}, "opt_fields": {"name"}}
		offsets := map[string]bool{}
		for {
			var page struct {
				Data []Project `json:"data"`
				Next *struct {
					Offset string `json:"offset"`
				} `json:"next_page"`
			}
			if err := c.get(ctx, "/workspaces/"+url.PathEscape(workspace.GID)+"/projects", query, &page); err != nil {
				return nil, err
			}
			for _, project := range page.Data {
				if projectGID.MatchString(project.GID) && !seen[project.GID] &&
					strings.Contains(strings.ToLower(project.Name), strings.ToLower(name)) {
					project.Workspace = workspace
					found = append(found, project)
					seen[project.GID] = true
				}
			}
			if page.Next == nil {
				break
			}
			if page.Next.Offset == "" || offsets[page.Next.Offset] {
				return nil, svchttp.UnexpectedError(service)
			}
			offsets[page.Next.Offset] = true
			query.Set("offset", page.Next.Offset)
		}
	}
	return found, nil
}

// CreateTask sends the approved content as plain text. An ambiguous response must never trigger a second POST.
func (c *Client) CreateTask(ctx context.Context, project, title, description string) (string, error) {
	if !projectGID.MatchString(project) || strings.TrimSpace(title) == "" {
		return "", connector.Invalid("task", "An Asana task needs a title and a project.")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	body := map[string]any{"data": map[string]any{"name": title, "notes": description, "projects": []string{project}}}
	var payload struct {
		Data taskPayload `json:"data"`
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	header := http.Header{"Authorization": {"Bearer " + c.token}}
	query := url.Values{"opt_fields": {"name,permalink_url"}}
	if _, err := svchttp.PostJSON(ctx, &client, service, c.base+"/tasks?"+query.Encode(), header, body, &payload); err != nil {
		switch svchttp.Status(err) {
		case 400, 401, 403, 404, 413, 422, 429:
			return "", err
		default:
			return "", errors.Join(ErrCreationUnconfirmed, err)
		}
	}
	link, err := url.Parse(payload.Data.PermalinkURL)
	if err != nil || link.Scheme != "https" || link.User != nil || !strings.EqualFold(link.Host, "app.asana.com") || !taskGID.MatchString(payload.Data.GID) {
		return "", ErrCreationUnconfirmed
	}
	return link.String(), nil
}
