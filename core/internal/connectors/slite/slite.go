// Package slite connects Pouple to Slite with an API key. The connector shows whose account it is; the client lets
// the assistant search the notes as data. It never writes.
package slite

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
)

// ID is the connector id, also the prefix of its keychain entries.
const ID = "slite"

const (
	service = "Slite"
	apiBase = "https://api.slite.com/v1"
	// DocsURL is Slite's page explaining where the API key is created.
	DocsURL = "https://developers.slite.com/docs/getting-started"
)

// Client reads Slite. The key is sent in headers, to Slite only.
type Client struct {
	key  string
	base string
	http *http.Client
}

func NewClient(key string) *Client {
	return &Client{key: key, base: apiBase, http: svchttp.NewPublicClient()}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	address := c.base + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	// Slite documents x-slite-api-key and describes the same key as a bearer token: both name the same credential.
	header := http.Header{"X-Slite-Api-Key": {c.key}, "Authorization": {"Bearer " + c.key}}
	_, err := svchttp.GetJSON(ctx, c.http, service, address, header, out)
	return err
}

// Me is the account a key belongs to.
type Me struct {
	DisplayName        string `json:"displayName"`
	Email              string `json:"email"`
	OrganizationName   string `json:"organizationName"`
	OrganizationDomain string `json:"organizationDomain"`
}

// Me returns the owner of the key.
func (c *Client) Me(ctx context.Context) (Me, error) {
	var me Me
	if err := c.get(ctx, "/me", nil, &me); err != nil {
		return Me{}, err
	}
	if me.DisplayName == "" && me.Email == "" {
		return Me{}, svchttp.UnexpectedError(service)
	}
	return me, nil
}

// Hit is a note that matches a search. Every text field comes from Slite and from colleagues: it is data.
type Hit struct {
	ID        string
	Title     string
	Excerpt   string
	Channel   string
	UpdatedAt string
}

type searchPayload struct {
	Hits []struct {
		ID          string                   `json:"id"`
		Title       string                   `json:"title"`
		Highlight   string                   `json:"highlight"`
		ArchivedAt  *string                  `json:"archivedAt"`
		UpdatedAt   string                   `json:"updatedAt"`
		ParentNotes []struct{ Title string } `json:"parentNotes"`
	} `json:"hits"`
}

var markup = regexp.MustCompile(`<[^>]*>`)

// Search finds notes matching the words, best first, archived ones left out.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	var payload searchPayload
	if err := c.get(ctx, "/search-notes", url.Values{"query": {query}, "hitsPerPage": {strconv.Itoa(limit)}}, &payload); err != nil {
		return nil, err
	}
	var hits []Hit
	for _, hit := range payload.Hits {
		if hit.ID == "" || hit.Title == "" || hit.ArchivedAt != nil {
			continue
		}
		found := Hit{ID: hit.ID, Title: hit.Title, UpdatedAt: hit.UpdatedAt, Excerpt: strings.TrimSpace(markup.ReplaceAllString(hit.Highlight, ""))}
		if len(hit.ParentNotes) > 0 {
			found.Channel = hit.ParentNotes[0].Title
		}
		hits = append(hits, found)
	}
	return hits, nil
}

// Connector validates a Slite API key.
type Connector struct {
	// Base is the API address; tests point it at a local server.
	Base string
	HTTP *http.Client
}

func New() *Connector { return &Connector{Base: apiBase, HTTP: svchttp.NewPublicClient()} }

func (c *Connector) Descriptor() connector.Descriptor {
	return connector.Descriptor{
		ID:       ID,
		Name:     "Slite",
		Tagline:  "Your team's notes and docs",
		Required: false,
		Order:    12,
		Fields: []connector.Field{{
			ID:          "token",
			Label:       "API key",
			Help:        "It stays in your Mac's keychain and is never shown again.",
			Placeholder: "Slite API key",
			Secret:      true,
			Pattern:     `^\S{10,}$`,
			PatternHint: "does not look like a Slite API key (no spaces)",
		}},
		Guide: []connector.GuideStep{
			{Title: "Open the Slite settings", Body: "In Slite, open your organization menu (top left), then Settings, then API. Slite explains the same steps on its developer page.", Link: &connector.Link{Label: "Slite API help", URL: DocsURL}},
			{Title: "Create an API key", Body: "On the API page, click Create a new key and follow the instructions. Pouple only searches and reads notes: it never changes anything in Slite."},
			{Title: "Paste the key", Body: "Copy it right away (Slite shows it only once) and paste it here. Pouple shows whose account it is before saving."},
		},
	}
}

func (c *Connector) client(key string) *Client {
	client := NewClient(key)
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
		return connector.Profile{}, connector.Invalid("token", "Slite does not accept this key. Create a new API key in the API settings and paste it again.")
	case err != nil:
		return connector.Profile{}, err
	}
	profile := connector.Profile{Name: me.DisplayName}
	if profile.Name == "" {
		profile.Name = me.Email
	}
	if me.OrganizationName != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "Organization", Value: me.OrganizationName})
	}
	if me.OrganizationDomain != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "Domain", Value: me.OrganizationDomain})
	}
	if me.Email != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "E-mail", Value: me.Email})
	}
	return profile, nil
}
