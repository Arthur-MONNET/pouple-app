package slite

import (
	"net/url"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
)

// workspaceSuffix is where every Slite workspace lives: https://<team>.slite.com.
const workspaceSuffix = ".slite.com"

// ParseWorkspaceURL checks the address of a Slite workspace and returns its canonical form (https and host only).
// The API key is never sent there (it only goes to api.slite.com): the address opens the right settings page and
// tells the user which workspace Pouple is connected to. "your-team", "your-team.slite.com" and
// "https://your-team.slite.com/" all give https://your-team.slite.com.
func ParseWorkspaceURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, connector.Invalid("url", "Type the address you use to open Slite, for example https://your-team.slite.com.")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return nil, connector.Invalid("url", "This is not a valid Slite address. Example: https://your-team.slite.com")
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return nil, connector.Invalid("url", "This is not a valid Slite address. Example: https://your-team.slite.com")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !strings.Contains(host, ".") {
		host += workspaceSuffix
	}
	team := strings.TrimSuffix(host, workspaceSuffix)
	if !strings.HasSuffix(host, workspaceSuffix) || team == "" || strings.Contains(team, ".") {
		return nil, connector.Invalid("url", "A Slite address looks like https://your-team.slite.com.")
	}
	return &url.URL{Scheme: "https", Host: host}, nil
}
