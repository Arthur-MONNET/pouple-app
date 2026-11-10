package asana

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

var (
	taskGID = regexp.MustCompile(`^\d{5,25}$`)
)

// ParseTaskRef reads a task from the address Asana shows for it, in either of its forms
// (https://app.asana.com/1/<workspace>/project/<project>/task/<task> and https://app.asana.com/0/<project>/<task>),
// or from the task's own number. Only app.asana.com is accepted: the token is never sent anywhere a message points to.
func ParseTaskRef(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if taskGID.MatchString(text) {
		return text, true
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || !strings.EqualFold(parsed.Host, "app.asana.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	for i, part := range parts {
		if part == "task" && i+1 < len(parts) && taskGID.MatchString(parts[i+1]) {
			return parts[i+1], true
		}
	}
	// The older form: /0/<project>/<task>, optionally followed by "/f" (the task opened in focus mode).
	if (len(parts) == 3 || (len(parts) == 4 && parts[3] == "f")) && parts[0] == "0" {
		if taskGID.MatchString(parts[1]) && taskGID.MatchString(parts[2]) {
			return parts[2], true
		}
	}
	return "", false
}

// TaskDetails is a task with what is needed to write about it. Text fields are data written by people.
type TaskDetails struct {
	Task
	Notes string
}

// Comment is something a person wrote on a task.
type Comment struct {
	Author    string
	Text      string
	CreatedAt string
}

func validTask(gid string) error {
	if !taskGID.MatchString(gid) {
		return connector.Invalid("target", "This is not an Asana task.")
	}
	return nil
}

// Task reads one task.
func (c *Client) Task(ctx context.Context, gid string) (TaskDetails, error) {
	if err := validTask(gid); err != nil {
		return TaskDetails{}, err
	}
	var payload struct {
		Data struct {
			taskPayload
			Notes string `json:"notes"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/tasks/"+url.PathEscape(gid), url.Values{"opt_fields": {taskFields + ",notes"}}, &payload); err != nil {
		return TaskDetails{}, err
	}
	task, ok := payload.Data.task()
	if !ok {
		return TaskDetails{}, svchttp.UnexpectedError(service)
	}
	return TaskDetails{Task: task, Notes: payload.Data.Notes}, nil
}

// Comments returns the last `limit` comments of a task, oldest first (the history entries Asana adds for every
// change are left out).
func (c *Client) Comments(ctx context.Context, gid string, limit int) ([]Comment, error) {
	if err := validTask(gid); err != nil {
		return nil, err
	}
	var payload struct {
		Data []struct {
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
			Subtype   string `json:"resource_subtype"`
			CreatedBy *struct {
				Name string `json:"name"`
			} `json:"created_by"`
		} `json:"data"`
	}
	query := url.Values{"opt_fields": {"text,created_at,resource_subtype,created_by.name"}, "limit": {"100"}}
	if err := c.get(ctx, "/tasks/"+url.PathEscape(gid)+"/stories", query, &payload); err != nil {
		return nil, err
	}
	var comments []Comment
	for _, story := range payload.Data {
		if story.Subtype != "comment_added" || strings.TrimSpace(story.Text) == "" {
			continue
		}
		comment := Comment{Text: story.Text, CreatedAt: story.CreatedAt}
		if story.CreatedBy != nil {
			comment.Author = story.CreatedBy.Name
		}
		comments = append(comments, comment)
	}
	return comments[max(0, len(comments)-limit):], nil
}

// MaxCommentRunes bounds a comment Pouple posts.
const MaxCommentRunes = 4000

// PostedComment is the comment Asana created.
type PostedComment struct {
	GID string
}

// AddComment posts a comment on a task, under the name of the token's owner. This is the only thing Pouple writes in
// Asana, and only for a user who asked for it in the app (the assistant's engine decides that, not this client).
func (c *Client) AddComment(ctx context.Context, gid, text string) (PostedComment, error) {
	if err := validTask(gid); err != nil {
		return PostedComment{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > MaxCommentRunes {
		return PostedComment{}, connector.Invalid("text", "The comment is empty or too long.")
	}
	var payload struct {
		Data struct {
			GID string `json:"gid"`
		} `json:"data"`
	}
	header := http.Header{"Authorization": {"Bearer " + c.token}}
	body := map[string]any{"data": map[string]string{"text": text}}
	if _, err := svchttp.PostJSON(ctx, c.http, service, c.base+"/tasks/"+url.PathEscape(gid)+"/stories", header, body, &payload); err != nil {
		return PostedComment{}, err
	}
	if payload.Data.GID == "" {
		return PostedComment{}, svchttp.UnexpectedError(service)
	}
	return PostedComment{GID: payload.Data.GID}, nil
}
