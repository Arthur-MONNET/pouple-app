package gitlab

import (
	"context"
	"net/http"
	"net/url"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// OpenMergeRequest reuses an open MR for the exact pair before creating one. It never merges or updates one.
func (c *Client) OpenMergeRequest(ctx context.Context, project, source, target, title string) (string, error) {
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
		return link, err
	}
	// A preceding push must have succeeded: never create a MR for an absent remote source.
	var branch struct {
		Name string `json:"name"`
	}
	if _, err := c.get(ctx, path+"/repository/branches/"+url.PathEscape(source), nil, &branch); err != nil {
		return "", err
	}
	if branch.Name != source {
		return "", svchttp.UnexpectedError(service)
	}
	var row mergeRequestPayload
	_, err := svchttp.PostJSON(ctx, c.http, service, c.api+path+"/merge_requests", http.Header{"Private-Token": {c.token}}, map[string]string{"source_branch": source, "target_branch": target, "title": title}, &row)
	if err != nil {
		// A lost response or concurrent retry may have created it. Reconcile without another POST.
		if link, lookupErr := find(); lookupErr == nil && link != "" {
			return link, nil
		}
		if svchttp.Status(err) == http.StatusForbidden || svchttp.Status(err) == http.StatusUnauthorized {
			return "", i18n.NewError("GitLab refused MR creation; reconnect with a token granting api and project write access")
		}
		return "", err
	}
	if row.WebURL == "" {
		return "", svchttp.UnexpectedError(service)
	}
	return row.WebURL, nil
}
