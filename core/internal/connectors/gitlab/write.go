package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// OpenMergeRequest reuses an open MR for the exact pair before creating one. It never merges or updates one.
func (c *Client) OpenMergeRequest(ctx context.Context, project, source, target, title string) (string, error) {
	return c.OpenMergeRequestChecked(ctx, project, source, target, title, nil)
}

// OpenMergeRequestChecked rechecks settings at the point of mutation.
func (c *Client) OpenMergeRequestChecked(ctx context.Context, project, source, target, title string, check func() error) (string, error) {
	path := "/projects/" + url.PathEscape(project)
	query := url.Values{"state": {"opened"}, "source_branch": {source}, "target_branch": {target}, "per_page": {"100"}}
	find := func() (string, error) {
		var rows []mergeRequestPayload
		_, err := c.get(ctx, path+"/merge_requests", query, &rows)
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			if row.SourceBranch == source && row.TargetBranch == target && row.WebURL != "" {
				return row.WebURL, nil
			}
		}
		return "", nil
	}
	if link, err := find(); err != nil || link != "" {
		return link, mrError(err)
	}
	// Introspection is optional (older servers and some token types do not support it).
	// Only deny a known missing scope; an unavailable introspection is not evidence.
	if info, err := c.Token(ctx); err == nil && info.Scopes != nil {
		if info.Revoked || (info.Active != nil && !*info.Active) {
			return "", i18n.NewError("GitLab token is inactive or revoked; reconnect in Settings")
		}
		if !slices.Contains(info.Scopes, "api") {
			return "", i18n.NewError("GitLab token lacks the api scope required to create merge requests; reconnect in Settings with api, or keep read_api for reading only")
		}
	} else if svchttp.Status(err) == http.StatusUnauthorized {
		return "", mrError(err)
	}
	// A preceding push must have succeeded: never create a MR for an absent remote source.
	var branch struct {
		Name string `json:"name"`
	}
	if _, err := c.get(ctx, path+"/repository/branches/"+url.PathEscape(source), nil, &branch); err != nil {
		return "", mrError(err)
	}
	if branch.Name != source {
		return "", svchttp.UnexpectedError(service)
	}
	if check != nil {
		if err := check(); err != nil {
			return "", err
		}
	}
	var row mergeRequestPayload
	_, err := svchttp.PostJSON(ctx, c.http, service, c.api+path+"/merge_requests", http.Header{"Private-Token": {c.token}}, map[string]string{"source_branch": source, "target_branch": target, "title": title}, &row)
	if err != nil {
		// A lost response or concurrent retry may have created it. Reconcile without another POST.
		if link, lookupErr := find(); lookupErr == nil && link != "" {
			return link, nil
		}
		return "", mrError(err)
	}
	if row.WebURL == "" {
		return "", svchttp.UnexpectedError(service)
	}
	return row.WebURL, nil
}

// mrError keeps the status and a fixed, safe interpretation, never GitLab's raw body.
func mrError(err error) error {
	status := svchttp.Status(err)
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return err
	}
	var response *svchttp.StatusError
	code := ""
	if errors.As(err, &response) {
		code = response.Code
	}
	reason := "access refused; GitLab did not establish whether the token scope or project permissions caused it; check the connection in Settings"
	switch {
	case status == http.StatusUnauthorized:
		reason = "authentication rejected; the token may be invalid, expired or revoked; reconnect in Settings"
	case code == "insufficient_scope":
		reason = "GitLab reports insufficient token scope; MR creation requires api; reconnect in Settings"
	case code == "project_access":
		reason = "GitLab reports that this account cannot create merge requests in this project; check project access"
	}
	return i18n.Wrap(err, "GitLab HTTP %d: %s", status, i18n.NewMessage(reason))
}
