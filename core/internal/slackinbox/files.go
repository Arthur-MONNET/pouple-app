package slackinbox

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	maxDownloadBytes = 8 << 20
	downloadTimeout  = 20 * time.Second
)

// FileRef is a file attached to a Slack message.
type FileRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	// URL is where Slack serves the file from; it needs the token and the files:read scope.
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// ErrNotSlackFile is returned when a file is not hosted by Slack, so the token is never sent to it.
var ErrNotSlackFile = i18n.NewError("this file is not hosted by Slack")

// ErrFileTooLarge is returned when a file is over the download cap.
var ErrFileTooLarge = i18n.NewError("the file is larger than 8 MB")

// FilesAccessError means Slack refused to hand over a file: the token most likely lacks the files:read scope.
type FilesAccessError struct {
	// Status is the HTTP status Slack answered, 0 when it served a web page instead of the file.
	Status int
}

func (e *FilesAccessError) Error() string {
	return "Slack would not give the file: add the files:read scope to the Slack app, reinstall it and update the Slack token"
}

// parseFiles reads the files of a message payload; files hosted elsewhere or deleted are left out. The result is
// never nil, so "the message has no files" differs from "the payload did not say".
func parseFiles(value any) []FileRef {
	files := []FileRef{}
	items, _ := value.([]any)
	for _, item := range items {
		file, ok := asObject(item)
		if !ok || flag(file, "is_external") {
			continue
		}
		if mode, _ := str(file, "mode"); mode == "tombstone" {
			continue
		}
		id, hasID := str(file, "id")
		link, _ := str(file, "url_private")
		if !hasID || link == "" {
			continue
		}
		name := "file"
		for _, key := range []string{"name", "title"} {
			if candidate, _ := str(file, key); candidate != "" {
				name = candidate
				break
			}
		}
		mime := "application/octet-stream"
		if candidate, ok := str(file, "mimetype"); ok {
			mime = candidate
		}
		var size int64
		if number, ok := file["size"].(float64); ok {
			size = int64(number)
		}
		files = append(files, FileRef{ID: id, Name: name, Mime: mime, URL: link, Size: size})
	}
	return files
}

func isSlackHost(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (host == "slack.com" || strings.HasSuffix(host, ".slack.com"))
}

// DownloadFile fetches a file of a message. The token is only sent to Slack's own https hosts (redirects included),
// and a web page instead of the file, which is what Slack serves when the scope is missing, is reported as a
// *FilesAccessError.
func (c *Client) DownloadFile(ctx context.Context, f FileRef) ([]byte, error) {
	target, err := url.Parse(f.URL)
	if err != nil || !isSlackHost(target) {
		return nil, ErrNotSlackFile
	}
	if f.Size > maxDownloadBytes {
		return nil, ErrFileTooLarge
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, i18n.Wrap(err, "slack file %s: build request: %s", f.ID, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)

	// A copy, so the caller's client is untouched: a redirect may not carry the token away from Slack.
	guarded := *c.http
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return i18n.NewError("too many redirects")
		}
		if !isSlackHost(next.URL) {
			return ErrNotSlackFile
		}
		return nil
	}
	response, err := guarded.Do(request)
	if err != nil {
		return nil, i18n.Wrap(err, "slack file %s: Slack is unreachable (%s): %s", f.ID, target.Host, err)
	}
	defer response.Body.Close()

	mime, _, _ := strings.Cut(response.Header.Get("Content-Type"), ";")
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return nil, &FilesAccessError{Status: response.StatusCode}
	case mime == "text/html":
		return nil, &FilesAccessError{}
	case response.StatusCode < 200 || response.StatusCode > 299:
		return nil, i18n.NewError("slack file %s: Slack responded %d for the file", f.ID, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, i18n.Wrap(err, "slack file %s: read body: %s", f.ID, err)
	}
	if len(data) > maxDownloadBytes {
		return nil, ErrFileTooLarge
	}
	return data, nil
}
