package slite

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// ErrCreationUnconfirmed means the page may already exist. The caller must not retry automatically.
var ErrCreationUnconfirmed = errors.New("Slite creation unconfirmed")

// CreateNote creates one page below an explicit parent and returns its Slite URL.
func (c *Client) CreateNote(ctx context.Context, parentID, title, markdown string) (string, error) {
	if !ValidNoteID(parentID) {
		return "", connector.Invalid("parentNoteId", "This is not a Slite note id.")
	}
	if strings.TrimSpace(title) == "" {
		return "", connector.Invalid("title", "A Slite page needs a title.")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	body := struct {
		ParentID string `json:"parentNoteId"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}{parentID, title, markdown}
	var created struct {
		ID       string `json:"id"`
		ParentID string `json:"parentNoteId"`
		URL      string `json:"url"`
	}
	client := *c.http
	// A 307 or 308 would send the POST again.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if _, err := svchttp.PostJSON(ctx, &client, service, c.base+"/notes", c.headers(), body, &created); err != nil {
		switch svchttp.Status(err) {
		case 400, 401, 403, 404, 413, 422, 429:
			return "", err
		default:
			return "", errors.Join(ErrCreationUnconfirmed, err)
		}
	}
	link, err := url.Parse(created.URL)
	if err != nil || link.Scheme != "https" || link.User != nil || link.Port() != "" ||
		!strings.HasSuffix(strings.ToLower(link.Hostname()), workspaceSuffix) ||
		!ValidNoteID(created.ID) || created.ParentID != parentID {
		return "", ErrCreationUnconfirmed
	}
	return link.String(), nil
}
