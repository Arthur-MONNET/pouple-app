package slite

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// noteID is what a Slite note id looks like. The assistant names the notes it wants to open, so an id is checked
// before it becomes part of an address: it can never reach another path or add a query.
var noteID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidNoteID reports whether text can be the id of a note.
func ValidNoteID(text string) bool { return noteID.MatchString(text) }

// Child is a note found inside another one.
type Child struct {
	ID        string
	Title     string
	UpdatedAt string
}

// Children lists what a note holds, at most limit of them, and how many there are in all. Archived notes are left out.
func (c *Client) Children(ctx context.Context, id string, limit int) ([]Child, int, error) {
	if !ValidNoteID(id) {
		return nil, 0, connector.Invalid("id", "This is not a Slite note id.")
	}
	var payload struct {
		Notes []struct {
			ID         string  `json:"id"`
			Title      string  `json:"title"`
			UpdatedAt  string  `json:"updatedAt"`
			ArchivedAt *string `json:"archivedAt"`
		} `json:"notes"`
		Total int `json:"total"`
	}
	if err := c.get(ctx, "/notes/"+url.PathEscape(id)+"/children", url.Values{"orderBy": {"title"}}, &payload); err != nil {
		return nil, 0, err
	}
	var children []Child
	for _, note := range payload.Notes {
		if note.ID == "" || note.Title == "" || note.ArchivedAt != nil {
			continue
		}
		if len(children) == limit {
			break
		}
		children = append(children, Child{ID: note.ID, Title: note.Title, UpdatedAt: note.UpdatedAt})
	}
	return children, max(payload.Total, len(children)), nil
}

// Note is a note with its text, in Markdown. Title and Content were written by colleagues: they are data.
type Note struct {
	ID        string
	Title     string
	UpdatedAt string
	Content   string
	// Truncated is set when the text was cut to maxRunes.
	Truncated bool
}

// Read returns a note's text, cut to at most maxRunes characters.
func (c *Client) Read(ctx context.Context, id string, maxRunes int) (Note, error) {
	if !ValidNoteID(id) {
		return Note{}, connector.Invalid("id", "This is not a Slite note id.")
	}
	var payload struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		UpdatedAt string `json:"updatedAt"`
		Content   string `json:"content"`
	}
	if err := c.get(ctx, "/notes/"+url.PathEscape(id), url.Values{"format": {"md"}}, &payload); err != nil {
		return Note{}, err
	}
	if payload.ID == "" || payload.Title == "" {
		return Note{}, svchttp.UnexpectedError(service)
	}
	note := Note{ID: payload.ID, Title: payload.Title, UpdatedAt: payload.UpdatedAt, Content: strings.TrimSpace(payload.Content)}
	if runes := []rune(note.Content); len(runes) > maxRunes {
		note.Content, note.Truncated = string(runes[:maxRunes]), true
	}
	return note, nil
}

// Source is a note an answer relied on.
type Source struct {
	ID    string
	Title string
}

// Answer is what Slite wrote in reply to a question about the workspace.
type Answer struct {
	Text    string
	Sources []Source
}

// Ask puts a question to Slite's own assistant and waits for its answer (wait=true: the call can take a while, so it
// has its own, longer, time limit). Slite can switch the feature off (422) or rate limit it (429).
func (c *Client) Ask(ctx context.Context, question string) (Answer, error) {
	var payload struct {
		Answer  string `json:"answer"`
		Sources []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"sources"`
	}
	query := url.Values{"question": {question}, "wait": {strconv.FormatBool(true)}}
	if err := c.getWith(ctx, c.slow, "/ask", query, &payload); err != nil {
		return Answer{}, err
	}
	if strings.TrimSpace(payload.Answer) == "" {
		return Answer{}, svchttp.UnexpectedError(service)
	}
	answer := Answer{Text: strings.TrimSpace(payload.Answer)}
	for _, source := range payload.Sources {
		if source.ID != "" && source.Title != "" {
			answer.Sources = append(answer.Sources, Source{ID: source.ID, Title: source.Title})
		}
	}
	return answer, nil
}
