// Package slack connects Pouple to a Slack workspace with a user token, and tells who that token belongs to.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/slackrt"
)

const apiBase = "https://slack.com/api"

// ID is the connector id, also the prefix of its keychain entries.
const ID = "slack"

const AppTokenField = "appToken"

// Scope is a permission of the Slack app, and what stops working without it.
type Scope struct {
	Name     string
	Required bool
	Without  string
}

// Scopes is everything Pouple asks for. The manifest is generated from this list, so what is asked and what is
// checked cannot drift apart.
var Scopes = []Scope{
	{"search:read", true, "Pouple cannot find the messages that mention you."},
	{"users:read", true, "Pouple cannot show who wrote a message."},
	{"channels:history", true, "Pouple cannot read public channels."},
	{"groups:history", true, "Pouple cannot read private channels."},
	{"im:history", true, "Pouple cannot read your direct messages and your notes to yourself."},
	{"mpim:history", true, "Pouple cannot read group conversations."},
	{"files:read", false, "Photos and files in messages are not read."},
	{"channels:read", false, "Public channels are not listed."},
	{"groups:read", false, "Private channels are not listed."},
	{"im:read", false, "Direct messages are not listed."},
	{"mpim:read", false, "Group conversations are not watched."},
}

// Manifest is the Slack app definition to prefill, with every scope Pouple needs.
func Manifest() string {
	names := make([]string, len(Scopes))
	for i, scope := range Scopes {
		names[i] = scope.Name
	}
	manifest := map[string]any{
		"display_information": map[string]any{
			"name":             "Pouple",
			"description":      "Reads the Slack messages that would notify you and has your local Codex look into them.",
			"background_color": "#262a31",
		},
		"oauth_config": map[string]any{"scopes": map[string]any{"user": names}},
		"settings": map[string]any{
			"org_deploy_enabled":     false,
			"socket_mode_enabled":    true,
			"token_rotation_enabled": false,
			"event_subscriptions": map[string]any{
				"user_events": []string{"message.channels", "message.groups", "message.im", "message.mpim"},
			},
		},
	}
	data, _ := json.Marshal(manifest)
	return string(data)
}

// CreateAppURL opens Slack's "create an app" page with the manifest already filled in.
func CreateAppURL() string {
	return "https://api.slack.com/apps?new_app=1&manifest_json=" + url.QueryEscape(Manifest())
}

// Connector validates a Slack user token.
type Connector struct {
	HTTP *http.Client
	// Base is the Slack API address; tests point it at a local server.
	Base string
}

func New() *Connector {
	return &Connector{HTTP: &http.Client{Timeout: 15 * time.Second}, Base: apiBase}
}

func (c *Connector) Descriptor() connector.Descriptor {
	return connector.Descriptor{
		ID:       ID,
		Name:     "Slack",
		Tagline:  "Where your requests come from",
		Required: true,
		Order:    1,
		Fields: []connector.Field{{
			ID:          "token",
			Label:       "User OAuth Token",
			Help:        "Starts with xoxp-. It stays in your Mac's keychain and is never shown again.",
			Placeholder: "xoxp-…",
			Secret:      true,
			// Broad on purpose: a rotating token (xoxe.…) gets its own explanation in Validate instead of a bare "bad format".
			Pattern:     `^xox[a-z]+[.-][A-Za-z0-9.-]{20,}$`,
			PatternHint: "does not look like a Slack token (it starts with xoxp-)",
		}, {
			ID:          AppTokenField,
			Label:       "App-level Token",
			Help:        "Optional. For live updates, paste the non-rotating xapp- token from Slack. It stays in your Mac’s keychain.",
			Placeholder: "xapp-…",
			Secret:      true,
			Optional:    true,
			Pattern:     `^x[a-z]+[.-][A-Za-z0-9.-]{20,}$`,
			PatternHint: "does not look like an app-level token (it starts with xapp-). Paste it as issued by Slack, or leave this field empty.",
		}},
		Guide: []connector.GuideStep{
			{Title: "Create the Slack app", Body: "One click: Slack opens with the name and every permission already filled in. Choose your workspace and confirm.", Link: &connector.Link{Label: "Create the Slack app", URL: CreateAppURL()}},
			{Title: "Install it to your workspace", Body: "On the app's page, open Install App and click Install to Workspace, then Allow."},
			{Title: "Copy the User OAuth Token", Body: "It is on the same page and starts with xoxp-. Paste it here."},
			{Title: "Enable live Slack updates", Body: "In Basic Information, generate an App-Level Token with connections:write (leave token rotation off: it cannot be undone). For an existing Slack app, enable Socket Mode and subscribe to message.channels, message.groups, message.im, and message.mpim under Subscribe to events on behalf of users, then reinstall the app."},
		},
	}
}

type authTest struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	UserID string `json:"user_id"`
	User   string `json:"user"`
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	URL    string `json:"url"`
}

type userInfo struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	User  struct {
		RealName string `json:"real_name"`
		TZ       string `json:"tz"`
		Profile  struct {
			DisplayName string `json:"display_name"`
			RealName    string `json:"real_name"`
			Title       string `json:"title"`
			Image       string `json:"image_192"`
			Email       string `json:"email"`
		} `json:"profile"`
	} `json:"user"`
}

func (c *Connector) call(ctx context.Context, token, method string, query url.Values, out any) (http.Header, error) {
	endpoint := c.Base + "/" + method
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// The token goes to Slack in a header, never in the address.
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, i18n.Wrap(err, "Slack could not be reached: %s", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, i18n.NewError("Slack answered %d", response.StatusCode)
	}
	return response.Header, json.NewDecoder(response.Body).Decode(out)
}

func (c *Connector) Validate(ctx context.Context, values connector.Values) (connector.Profile, error) {
	token := strings.TrimSpace(values["token"])
	// A Slack app with token rotation switched on (it cannot be switched off) issues tokens that expire after a short
	// time: they start with "xoxe". Pouple cannot renew them, so say what to do instead of failing hours later.
	if strings.HasPrefix(token, "xoxe") {
		return connector.Profile{}, connector.Invalid("token", "This Slack app uses token rotation (its tokens start with xoxe): they expire quickly and Pouple cannot renew them. Rotation cannot be turned off on an existing app. Create a new app with the Pouple button above (rotation off), install it, and copy its token.")
	}
	if appToken := strings.TrimSpace(values[AppTokenField]); appToken != "" {
		if strings.HasPrefix(appToken, "xoxe") {
			return connector.Profile{}, connector.Invalid(AppTokenField, "This app-level token rotates (it starts with xoxe): it would stop working within hours. Rotation cannot be turned off on this Slack app. Create a new app with the Pouple button above (rotation off), then generate the app-level token there, or leave this field empty.")
		}
		if !strings.HasPrefix(appToken, "xapp-") {
			return connector.Profile{}, connector.Invalid(AppTokenField, "An app-level token starts with xapp-. Copy it from Basic Information in Slack, or leave this field empty.")
		}
	}
	if strings.HasPrefix(token, "xoxb-") {
		return connector.Profile{}, connector.Invalid("token", "This is a bot token (xoxb-). Pouple needs the User OAuth Token, which starts with xoxp-.")
	}
	var auth authTest
	header, err := c.call(ctx, token, "auth.test", nil, &auth)
	if err != nil {
		return connector.Profile{}, err
	}
	if !auth.OK {
		return connector.Profile{}, connector.Invalid("token", "%s", explainError(auth.Error))
	}
	var info userInfo
	if _, err := c.call(ctx, token, "users.info", url.Values{"user": {auth.UserID}}, &info); err != nil || !info.OK {
		// The token works; the details just are not available (users:read missing is reported below).
		info = userInfo{}
	}
	granted := grantedScopes(header.Get("X-OAuth-Scopes"))
	profile := connector.Profile{Name: firstNonEmpty(info.User.RealName, info.User.Profile.RealName, auth.User), AvatarURL: info.User.Profile.Image, Subtitle: info.User.Profile.Title}
	profile.Facts = append(profile.Facts, connector.Fact{Label: "Workspace", Value: auth.Team})
	if handle := firstNonEmpty(info.User.Profile.DisplayName, auth.User); handle != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "Handle", Value: "@" + handle})
	}
	if info.User.Profile.Email != "" {
		profile.Facts = append(profile.Facts, connector.Fact{Label: "E-mail", Value: info.User.Profile.Email})
	}
	profile.Facts = append(profile.Facts, connector.Fact{Label: "Permissions", Value: fmt.Sprintf("%d of %d granted", countGranted(granted), len(Scopes))})

	var missingRequired []string
	for _, scope := range Scopes {
		if granted[scope.Name] {
			continue
		}
		if scope.Required {
			missingRequired = append(missingRequired, scope.Name)
			continue
		}
		profile.Warnings = append(profile.Warnings, connector.Warning{Title: "Missing " + scope.Name, Detail: scope.Without})
	}
	if len(missingRequired) > 0 && len(granted) > 0 {
		sort.Strings(missingRequired)
		return connector.Profile{}, connector.Invalid("token", "This token lacks required permissions (%s). Create the app with the button above, or add them under OAuth & Permissions and reinstall the app.", strings.Join(missingRequired, ", "))
	}
	if appToken := values[AppTokenField]; appToken != "" {
		live := slackrt.New(appToken)
		live.Base, live.HTTP = c.Base, c.HTTP
		if err := live.Check(ctx); err != nil {
			if errors.Is(err, slackrt.ErrToken) {
				return connector.Profile{}, connector.Invalid(AppTokenField, "Slack refused this app-level token. Copy it again from Basic Information in Slack, or leave this field empty to continue without live updates.")
			}
			return connector.Profile{}, connector.Invalid(AppTokenField, "Slack could not verify the app-level token. Try again.")
		}
	}
	return profile, nil
}

func grantedScopes(header string) map[string]bool {
	granted := map[string]bool{}
	for _, name := range strings.Split(header, ",") {
		if name = strings.TrimSpace(name); name != "" {
			granted[name] = true
		}
	}
	return granted
}

func countGranted(granted map[string]bool) int {
	count := 0
	for _, scope := range Scopes {
		if granted[scope.Name] {
			count++
		}
	}
	return count
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func explainError(code string) i18n.Message {
	switch code {
	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		return i18n.NewMessage("Slack does not accept this token. Copy it again from the app's Install App page.")
	case "token_expired":
		return i18n.NewMessage("This token expired. Reinstall the app to the workspace and copy the new token.")
	default:
		return i18n.NewMessage("Slack refused the token (%s).", code)
	}
}
