package gitlab

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	secondaryToken  = "additionalToken"
	primaryName     = "primaryName"
	secondaryName   = "additionalName"
	primaryScopes   = "primaryScopes"
	secondaryScopes = "additionalScopes"
	selectedToken   = "selectedToken"
	accessMode      = "accessMode"
	ReadOnly        = "read-only"
	Write           = "write"
)

type TokenSetting struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type Access struct {
	Mode     string         `json:"mode"`
	Selected string         `json:"selected"`
	Tokens   []TokenSetting `json:"tokens"`
}

type AccessUpdate struct {
	Mode        *string `json:"mode"`
	Selected    *string `json:"selected"`
	PrimaryName *string `json:"primaryName"`
	Name        string  `json:"name"`
	Token       string  `json:"token"`
	Verify      bool    `json:"verify"`
}

func AccessOf(values connector.Values) Access {
	access := Access{
		Mode: ReadOnly, Selected: "primary",
		Tokens: []TokenSetting{{ID: "primary", Name: firstNonEmpty(values[primaryName], "Pouple"), Scopes: strings.Fields(values[primaryScopes])}},
	}
	if values[secondaryToken] != "" {
		access.Tokens = append(access.Tokens, TokenSetting{ID: "additional", Name: values[secondaryName], Scopes: strings.Fields(values[secondaryScopes])})
		if values[selectedToken] == "additional" {
			access.Selected = "additional"
		}
	}
	if values[accessMode] == Write &&
		(values[selectedToken] != "additional" || access.Selected == "additional") &&
		slices.Contains(access.selectedScopes(), "api") {
		access.Mode = Write
	}
	return access
}

func (a Access) selectedScopes() []string {
	for _, token := range a.Tokens {
		if token.ID == a.Selected {
			return token.Scopes
		}
	}
	return nil
}

// ReadToken also accepts legacy connections whose scopes have yet to be verified.
func ReadToken(values connector.Values) string {
	if values[selectedToken] == "additional" && values[secondaryToken] != "" {
		return values[secondaryToken]
	}
	return values["token"]
}

func WriteToken(values connector.Values) (string, error) {
	if AccessOf(values).Mode != Write {
		return "", i18n.NewError("GitLab is in read-only mode")
	}
	return ReadToken(values), nil
}

func (c *Connector) verifyScopes(ctx context.Context, base *url.URL, token string) ([]string, error) {
	info, err := c.client(base, token).Token(ctx)
	if err != nil {
		return nil, i18n.Wrap(err, "Could not verify the GitLab token scopes: %s", err)
	}
	if info.Revoked || (info.Active != nil && !*info.Active) {
		return nil, connector.Invalid("token", "GitLab says this token is no longer active. Create a new one.")
	}
	if !slices.Contains(info.Scopes, "read_api") && !slices.Contains(info.Scopes, "api") {
		return nil, connector.Invalid("token", "This token lacks the read_api scope. Create a new one and tick read_api.")
	}
	return info.Scopes, nil
}

var personalTokenPattern = regexp.MustCompile(`^glpat-[A-Za-z0-9_.-]{20,}$`)

func (c *Connector) UpdateAccess(ctx context.Context, registry *connector.Registry, update AccessUpdate) (access Access, err error) {
	err = registry.UpdateManaged(ID, func(values connector.Values) (connector.Values, error) {
		patch := connector.Values{}
		set := func(key, value string) { patch[key], values[key] = value, value }
		if update.PrimaryName != nil {
			name := strings.TrimSpace(*update.PrimaryName)
			if name == "" || len([]rune(name)) > 80 {
				return nil, connector.Invalid("primaryName", "Enter a token name of 1 to 80 characters")
			}
			set(primaryName, name)
		}
		if update.Verify || update.Token != "" {
			base, err := ParseBaseURL(values["url"])
			if err != nil {
				return nil, err
			}
			if update.Verify {
				for _, pair := range [][2]string{{"token", primaryScopes}, {secondaryToken, secondaryScopes}} {
					if values[pair[0]] == "" {
						continue
					}
					scopes, err := c.verifyScopes(ctx, base, values[pair[0]])
					if err != nil {
						return nil, err
					}
					set(pair[1], strings.Join(scopes, " "))
				}
			}
			if update.Token != "" {
				token, name := strings.TrimSpace(update.Token), strings.TrimSpace(update.Name)
				if name == "" || len([]rune(name)) > 80 {
					return nil, connector.Invalid("name", "Enter a token name of 1 to 80 characters")
				}
				if token == values["token"] || !personalTokenPattern.MatchString(token) {
					return nil, connector.Invalid("token", "Enter a distinct GitLab personal access token")
				}
				scopes, err := c.verifyScopes(ctx, base, token)
				if err != nil {
					return nil, err
				}
				if !slices.Contains(scopes, "api") {
					return nil, connector.Invalid("token", "The additional GitLab token requires the api scope")
				}
				set(secondaryName, name)
				set(secondaryToken, token)
				set(secondaryScopes, strings.Join(scopes, " "))
				// Replacing an active credential never grants writes to the replacement.
				if values[selectedToken] == "additional" {
					set(accessMode, ReadOnly)
				}
			}
		}
		if update.Selected != nil {
			switch *update.Selected {
			case "primary":
			case "additional":
				if values[secondaryToken] == "" {
					return nil, connector.Invalid("selected", "Add an additional GitLab token first")
				}
			default:
				return nil, connector.Invalid("selected", "Choose a GitLab token")
			}
			if AccessOf(values).Selected != *update.Selected {
				set(accessMode, ReadOnly)
			}
			set(selectedToken, *update.Selected)
		}
		if update.Mode != nil {
			switch *update.Mode {
			case ReadOnly:
			case Write:
				if !slices.Contains(AccessOf(values).selectedScopes(), "api") {
					return nil, connector.Invalid("mode", "GitLab write mode requires a verified api token")
				}
			default:
				return nil, connector.Invalid("mode", "Choose a GitLab access mode")
			}
			set(accessMode, *update.Mode)
		}
		access = AccessOf(values)
		set(accessMode, access.Mode)
		return patch, nil
	})
	return access, err
}

// CheckPush recognizes the public GitLab service and the configured self-managed host.
func CheckPush(registry *connector.Registry, targets []string) error {
	values, err := registry.Values(ID)
	if err == nil && AccessOf(values).Mode == Write {
		return nil
	}
	// If the Keychain is unavailable, still recognize the host saved with the connection.
	host := remoteHost(values["url"])
	if host == "" {
		if status, err := registry.Status(ID); err == nil && status.Profile != nil {
			for _, fact := range status.Profile.Facts {
				if fact.Label == "Host" {
					host = remoteHost("https://" + fact.Value)
					break
				}
			}
		}
	}
	for _, target := range targets {
		if targetHost := remoteHost(target); targetHost != "" &&
			(targetHost == "gitlab.com" || targetHost == "altssh.gitlab.com" || targetHost == host) {
			return i18n.NewError("GitLab is in read-only mode")
		}
	}
	return nil
}

func remoteHost(remote string) string {
	if !strings.Contains(remote, "://") {
		// Git also uses SCP-style SSH addresses, such as git@gitlab.com:group/project.
		address, _, ok := strings.Cut(remote, ":")
		if !ok || strings.ContainsAny(address, "/\\") {
			return ""
		}
		remote = "ssh://" + address
	}
	parsed, err := url.Parse(remote)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
}
