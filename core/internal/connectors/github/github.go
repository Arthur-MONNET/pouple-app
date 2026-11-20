package github

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
const ID = "github"

// TokensURL is the page of github.com where fine-grained personal access tokens are created.
const TokensURL = "https://github.com/settings/personal-access-tokens/new"

// expirySoon is how close to its end a token earns a warning.
const expirySoon = 14 * 24 * time.Hour

// Connector validates a GitHub address and personal access token.
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
		Name:     "GitHub",
		Tagline:  "Your pull requests and reviews",
		Required: false,
		Order:    11,
		Fields: []connector.Field{{
			ID:          "token",
			Label:       "Personal access token",
			Help:        "Starts with github_pat_ or ghp_. It stays in your Mac's keychain and is never shown again.",
			Placeholder: "github_pat_…",
			Secret:      true,
			Pattern:     `^(ghp_|github_pat_)[A-Za-z0-9_]{20,}$`,
			PatternHint: "does not look like a GitHub token (it starts with github_pat_ or ghp_)",
		}, {
			ID:          "url",
			Label:       "API address",
			Help:        "Only for GitHub Enterprise. Keep the default for github.com.",
			Placeholder: DefaultURL,
			Default:     DefaultURL,
			Optional:    true,
		}},
		Guide: []connector.GuideStep{
			{Title: "Create a fine-grained token", Body: "In GitHub, open Settings, Developer settings, then Personal access tokens. The button below takes you there.", Link: &connector.Link{Label: "Open GitHub tokens", URL: TokensURL}},
			{Title: "Give it read-only access", Body: "Choose the repositories, then read-only access to Pull requests, Checks, Commit statuses, Contents and Metadata. Pouple only reads: it never changes anything in GitHub."},
			{Title: "Paste the token", Body: "Copy it right away (GitHub shows it only once) and paste it in this connection form, never in chat. For GitHub Enterprise, enter its API address too."},
		},
	}
}

func (c *Connector) client(base Base, token string) *Client {
	api := base.API
	if c.API != "" {
		api = c.API
	}
	return &Client{api: api, web: base.Web, token: token, http: c.HTTP}
}

func (c *Connector) Validate(ctx context.Context, values connector.Values) (connector.Profile, error) {
	base, err := ParseBaseURL(values["url"])
	if err != nil {
		return connector.Profile{}, err
	}
	client := c.client(base, strings.TrimSpace(values["token"]))

	user, header, err := client.User(ctx)
	switch {
	case svchttp.Status(err) == http.StatusUnauthorized:
		return connector.Profile{}, connector.Invalid("token", "GitHub does not accept this token. Create a new personal access token and paste it again.")
	case svchttp.Status(err) == http.StatusForbidden:
		return connector.Profile{}, connector.Invalid("token", "GitHub refused this token. Check that it is still valid and that your organization allows it.")
	case svchttp.Status(err) == http.StatusNotFound || errors.Is(err, svchttp.ErrUnexpected):
		return connector.Profile{}, connector.Invalid("url", "This address does not look like a GitHub API. Keep https://api.github.com, or give the address of your GitHub Enterprise.")
	case err != nil:
		return connector.Profile{}, err
	}

	profile := connector.Profile{Name: firstNonEmpty(user.Name, user.Login), Subtitle: "@" + user.Login, AvatarURL: avatar(base, user.AvatarURL)}
	profile.Facts = append(profile.Facts, connector.Fact{Label: "Host", Value: base.Web})
	profile.Warnings = append(profile.Warnings, scopeWarnings(header)...)
	if expires, ok := expiry(header); ok {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "Token expires", Value: expires.Format("2 Jan 2006")})
		if expires.Sub(c.clock()) < expirySoon {
			profile.Warnings = append(profile.Warnings, connector.Warning{Title: "Token expires soon", Detail: "Create a new token before it ends, then connect GitHub again."})
		}
	}

	mine, toReview, searchHeader, err := client.CountOpen(ctx)
	switch {
	case svchttp.Status(err) == http.StatusUnauthorized || svchttp.Status(err) == http.StatusForbidden:
		return connector.Profile{}, connector.Invalid("token", "GitHub refused to search your pull requests with this token. Check its permissions.")
	case err != nil:
		return connector.Profile{}, err
	}
	if sso(header) || sso(searchHeader) {
		profile.Warnings = append(profile.Warnings, connector.Warning{Title: "Single sign-on authorization needed", Detail: "An organization uses SAML single sign-on: on GitHub, open the token and choose Configure SSO to authorize it, or that organization's pull requests stay invisible."})
	}
	profile.Facts = append(profile.Facts,
		connector.Fact{Label: "Open pull requests", Value: strconv.Itoa(mine)},
		connector.Fact{Label: "Reviews requested", Value: strconv.Itoa(toReview)},
	)
	return profile, nil
}

// scopeWarnings reads what the token may do. A classic token lists its scopes in X-OAuth-Scopes; a fine-grained one
// sends no such header, and its permissions cannot be read: the user is told which ones Pouple needs.
func scopeWarnings(header http.Header) []connector.Warning {
	raw, classic := header[http.CanonicalHeaderKey("X-OAuth-Scopes")]
	if !classic {
		return []connector.Warning{{Title: "Check the token's permissions", Detail: "Pouple cannot read the permissions of a fine-grained token. For the repositories you want it to see, give read-only access to Pull requests, Checks, Commit statuses, Contents and Metadata."}}
	}
	var scopes []string
	for _, value := range raw {
		for _, scope := range strings.Split(value, ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes = append(scopes, scope)
			}
		}
	}
	switch {
	case slices.Contains(scopes, "repo"):
		return []connector.Warning{{Title: "Token can change things", Detail: "This classic token has the repo scope, which can also write in your repositories. Pouple only reads: a fine-grained token with read-only access is safer."}}
	case slices.Contains(scopes, "public_repo"):
		return []connector.Warning{{Title: "Public repositories only", Detail: "This classic token has public_repo: Pouple cannot see the pull requests of private repositories. Use a fine-grained read-only token to include them."}}
	default:
		return []connector.Warning{{Title: "Missing repository scope", Detail: "This classic token has neither repo nor public_repo: Pouple may miss pull requests. A fine-grained read-only token is the better choice."}}
	}
}

// sso reports whether GitHub says that some of the user's organizations need the token to be authorized for SAML SSO.
func sso(header http.Header) bool { return header.Get("X-GitHub-SSO") != "" }

// expiry reads when the token ends, when GitHub says it.
func expiry(header http.Header) (time.Time, bool) {
	raw := header.Get("GitHub-Authentication-Token-Expiration")
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (c *Connector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// avatar keeps the picture address only when it is on the hosts GitHub serves avatars from. The web view fetches it by
// itself, without the guard of the HTTP client: anything else could point the view at this Mac or the network.
func avatar(base Base, raw string) string {
	parsed, err := url.Parse(raw)
	if raw == "" || err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	web := strings.ToLower(base.Web)
	if host == "avatars.githubusercontent.com" || host == web || host == "avatars."+web {
		return parsed.String()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
