package asana

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// ID is the connector id, also the prefix of its keychain entries.
const ID = "asana"

// Connector validates an Asana personal access token.
type Connector struct {
	// Base is the API address; tests point it at a local server.
	Base string
	HTTP *http.Client
}

func New() *Connector { return &Connector{Base: apiBase, HTTP: svchttp.NewPublicClient()} }

func (c *Connector) Descriptor() connector.Descriptor {
	return connector.Descriptor{
		ID:       ID,
		Name:     "Asana",
		Tagline:  "Your tasks and what is due",
		Required: false,
		Order:    11,
		Fields: []connector.Field{{
			ID:          "token",
			Label:       "Personal access token",
			Help:        "It stays in your Mac's keychain and is never shown again.",
			Placeholder: "1/1234567890…",
			Secret:      true,
			Pattern:     `^\S{16,}$`,
			PatternHint: "does not look like an Asana token (no spaces, at least 16 characters)",
		}},
		Guide: []connector.GuideStep{
			{Title: "Open the Asana developer console", Body: "Sign in to Asana, then open My apps.", Link: &connector.Link{Label: "Open Asana My apps", URL: "https://app.asana.com/0/my-apps"}},
			{Title: "Create a personal access token", Body: "Under Personal access tokens, click Create new token, name it Pouple and accept the terms. Pouple reads your tasks. It posts a comment on a task only when you ask for one yourself in Pouple, and never changes anything else in Asana."},
			{Title: "Paste the token", Body: "Copy it right away (Asana shows it only once) and paste it here. Pouple shows whose account it is before saving."},
		},
	}
}

func (c *Connector) client(token string) *Client {
	client := NewClient(token)
	if c.Base != "" {
		client.base = c.Base
	}
	if c.HTTP != nil {
		client.http = c.HTTP
	}
	return client
}

func (c *Connector) Validate(ctx context.Context, values connector.Values) (connector.Profile, error) {
	client := c.client(strings.TrimSpace(values["token"]))
	me, err := client.Me(ctx)
	switch {
	case svchttp.Status(err) == http.StatusUnauthorized || svchttp.Status(err) == http.StatusForbidden:
		return connector.Profile{}, connector.Invalid("token", "Asana does not accept this token. Create a personal access token on the My apps page and paste it again.")
	case err != nil:
		return connector.Profile{}, err
	}
	client.workspaces, client.loaded = me.Workspaces[:min(len(me.Workspaces), maxWorkspaces)], true
	profile := connector.Profile{Name: me.Name, AvatarURL: webURL(me.PhotoURL)}
	if profile.Name == "" {
		profile.Name = me.Email
	}
	var names []string
	for _, workspace := range me.Workspaces[:min(len(me.Workspaces), maxWorkspaces)] {
		names = append(names, workspace.Name)
	}
	if len(names) > 0 {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "Workspaces", Value: strings.Join(names, ", ")})
	} else {
		profile.Warnings = append(profile.Warnings, connector.Warning{Title: "No workspace", Detail: "This account belongs to no Asana workspace, so there are no tasks to read."})
	}
	if me.Email != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "E-mail", Value: me.Email})
	}
	// The count is a courtesy: a failure here does not stop the connection.
	if len(names) > 0 {
		if open, err := client.OpenTasks(ctx); err == nil {
			count := strconv.Itoa(len(open.Tasks))
			if open.More {
				count += "+"
			}
			profile.Facts = append(profile.Facts, connector.Fact{Label: "Open tasks", Value: count})
		}
	}
	return profile, nil
}

func webURL(raw string) string {
	if strings.HasPrefix(raw, "https://") {
		return raw
	}
	return ""
}
