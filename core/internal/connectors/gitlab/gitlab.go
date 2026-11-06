package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// ID is the connector id, also the prefix of its keychain entries.
const ID = "gitlab"

// TokensPath is the page of a GitLab server where personal access tokens are created.
const TokensPath = "/-/user_settings/personal_access_tokens"

// expirySoon is how close to its end a token earns a warning.
const expirySoon = 14 * 24 * time.Hour

// Connector validates a GitLab address and personal access token.
type Connector struct {
	// HTTP is the client used for the API; New gives it the dial guard.
	HTTP *http.Client
	// API replaces the API address in tests (the typed address is still validated).
	API string
	now func() time.Time
}

func New() *Connector { return &Connector{HTTP: svchttp.NewClient(), now: time.Now} }

func (c *Connector) Descriptor() connector.Descriptor {
	return connector.Descriptor{
		ID:       ID,
		Name:     "GitLab",
		Tagline:  "Your merge requests and reviews",
		Required: false,
		Order:    10,
		Fields: []connector.Field{{
			ID:          "url",
			Label:       "GitLab address",
			Help:        "Keep https://gitlab.com unless your company hosts its own GitLab: then type its address, without a path.",
			Placeholder: DefaultURL,
			Default:     DefaultURL,
		}, {
			ID:          "token",
			Label:       "Personal access token",
			Help:        "Starts with glpat-. It stays in your Mac's keychain and is never shown again.",
			Placeholder: "glpat-…",
			Secret:      true,
			Pattern:     `^glpat-[A-Za-z0-9_.-]{20,}$`,
			PatternHint: "does not look like a GitLab token (it starts with glpat-)",
		}},
		Guide: []connector.GuideStep{
			{Title: "Open your GitLab tokens page", Body: "Sign in to GitLab, then open Preferences, Access tokens. On a company GitLab, use the same page on your own server.", Link: &connector.Link{Label: "Open GitLab tokens", URL: DefaultURL + TokensPath + "?name=Pouple&scopes=read_api", HostField: "url"}},
			{Title: "Create a read-only token", Body: "Name it Pouple, tick only read_api and create it. Pouple only reads your merge requests: it never changes anything in GitLab."},
			{Title: "Paste the token", Body: "Copy it right away (GitLab shows it only once) and paste it here. Pouple shows whose account it is before saving."},
		},
	}
}

func (c *Connector) client(base *url.URL, token string) *Client {
	api := base.String() + "/api/v4"
	if c.API != "" {
		api = c.API
	}
	return &Client{api: api, token: token, http: c.HTTP}
}

func (c *Connector) Validate(ctx context.Context, values connector.Values) (connector.Profile, error) {
	base, err := ParseBaseURL(values["url"])
	if err != nil {
		return connector.Profile{}, err
	}
	token := strings.TrimSpace(values["token"])
	client := c.client(base, token)

	user, err := client.User(ctx)
	switch {
	case svchttp.Status(err) == http.StatusUnauthorized:
		return connector.Profile{}, connector.Invalid("token", "GitLab does not accept this token. Create a new personal access token with the read_api scope and paste it again.")
	case svchttp.Status(err) == http.StatusForbidden:
		return connector.Profile{}, connector.Invalid("token", "This token lacks the read_api scope. Create a new one and tick read_api.")
	case svchttp.Status(err) == http.StatusNotFound || errors.Is(err, svchttp.ErrUnexpected):
		return connector.Profile{}, connector.Invalid("url", "This address does not look like a GitLab server. Check it, without any path.")
	case err != nil:
		return connector.Profile{}, err
	}

	profile := connector.Profile{Name: firstNonEmpty(user.Name, user.Username), AvatarURL: avatar(base, user.AvatarURL)}
	if user.Username != "" {
		profile.Subtitle = "@" + user.Username
	}
	profile.Facts = append(profile.Facts, connector.Fact{Label: "Host", Value: base.Host})
	if user.Email != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "E-mail", Value: user.Email})
	}

	// What GitLab says about the token itself: scopes and expiry. Not every server can say it, so a refusal here
	// does not stop the connection; the merge request call below is the real test of the scope.
	if info, err := client.Token(ctx); err == nil {
		if info.Revoked || (info.Active != nil && !*info.Active) {
			return connector.Profile{}, connector.Invalid("token", "GitLab says this token is no longer active. Create a new one.")
		}
		canRead := slices.Contains(info.Scopes, "read_api") || slices.Contains(info.Scopes, "api")
		if len(info.Scopes) > 0 && !canRead {
			return connector.Profile{}, connector.Invalid("token", "This token lacks the read_api scope. Create a new one and tick read_api.")
		}
		if slices.Contains(info.Scopes, "api") {
			profile.Warnings = append(profile.Warnings, connector.Warning{Title: "Token can change things", Detail: "This token has the api scope, which can write in GitLab. Pouple only reads: a token with read_api alone is safer."})
		}
		if expires, err := time.Parse("2006-01-02", info.ExpiresAt); err == nil {
			profile.Facts = append(profile.Facts, connector.Fact{Label: "Token expires", Value: expires.Format("2 Jan 2006")})
			if expires.Sub(c.clock()) < expirySoon {
				profile.Warnings = append(profile.Warnings, connector.Warning{Title: "Token expires soon", Detail: "Create a new token before it ends, then connect GitLab again."})
			}
		}
	}

	open, err := client.CountOpenMergeRequests(ctx)
	switch {
	case svchttp.Status(err) == http.StatusUnauthorized || svchttp.Status(err) == http.StatusForbidden:
		return connector.Profile{}, connector.Invalid("token", "This token lacks the read_api scope. Create a new one and tick read_api.")
	case err != nil:
		return connector.Profile{}, err
	}
	profile.Facts = append(profile.Facts, connector.Fact{Label: "Open merge requests", Value: strconv.Itoa(open)})
	return profile, nil
}

func (c *Connector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// avatar resolves the picture address against the server and keeps only web addresses.
func avatar(base *url.URL, raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := base.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
