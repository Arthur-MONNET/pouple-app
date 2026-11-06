package slackinbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	defaultBaseURL   = "https://slack.com/api"
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 16 << 20
	maxErrorExcerpt  = 300
)

// Client is a read-only Slack Web API client using a user token. It is safe for concurrent use.
type Client struct {
	token   string
	http    *http.Client
	baseURL string

	mu       sync.Mutex
	identity *identity
	names    map[string]string
}

type identity struct{ userID, teamURL string }

// NewClient builds a client. A nil httpClient gets a default one; baseURL "" means https://slack.com/api and is
// only meant to be replaced by tests. The token is sent in the Authorization header only, never in an address.
func NewClient(token string, httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{token: token, http: httpClient, baseURL: strings.TrimRight(baseURL, "/"), names: map[string]string{}}
}

// APIError is an error Slack reported in its payload, with a message meant to be shown as is.
type APIError struct {
	Method string
	// Code is Slack's own error code, e.g. "missing_scope".
	Code    string
	Message string
	text    i18n.Message
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) ErrorText(lang i18n.Lang) string {
	if localized := e.text.Text(lang); localized != "" {
		return localized
	}
	return e.Message
}

func describeSlackError(code string, needed any) i18n.Message {
	switch code {
	case "invalid_auth", "not_authed", "token_revoked", "token_expired":
		return i18n.NewMessage("Slack rejected the token: check the Slack token")
	case "missing_scope":
		scope := "required"
		if name, ok := needed.(string); ok {
			scope = name
		}
		return i18n.NewMessage(`Slack token lacks the "%s" scope`, scope)
	case "not_allowed_token_type":
		return i18n.NewMessage("Slack search needs a user token (xoxp-): check the Slack token")
	case "channel_not_found":
		return i18n.NewMessage("Slack channel not found: the token owner must be a member of the channel")
	case "ratelimited":
		return i18n.NewMessage("Slack rate limit reached, try again in a moment")
	default:
		return i18n.NewMessage("Slack error: %s", code)
	}
}

func statusHint(status int) i18n.Message {
	switch status {
	case http.StatusUnauthorized:
		return i18n.NewMessage(" (token rejected: check that it is valid and not expired)")
	case http.StatusForbidden:
		return i18n.NewMessage(" (access denied: check the token scopes)")
	}
	return i18n.Message{}
}

// call posts a form to a Slack method and returns its decoded payload once Slack said ok.
func (c *Client) call(ctx context.Context, method string, params url.Values) (object, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, i18n.Wrap(err, "slack %s: build request: %s", method, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, i18n.Wrap(err, "slack %s: Slack is unreachable: %s", method, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, i18n.Wrap(err, "slack %s: read response: %s", method, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		var retry i18n.Message
		if response.StatusCode == http.StatusTooManyRequests {
			after := response.Header.Get("Retry-After")
			if after == "" {
				after = "?"
			}
			retry = i18n.NewMessage(" (retry after %ss)", after)
		}
		excerpt := strings.TrimSpace(string(raw))
		if len(excerpt) > maxErrorExcerpt {
			excerpt = excerpt[:maxErrorExcerpt]
		}
		if excerpt != "" {
			excerpt = ": " + excerpt
		}
		return nil, i18n.NewError("slack %s: Slack responded %d%s%s%s", method, response.StatusCode, statusHint(response.StatusCode), retry, excerpt)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, i18n.Wrap(err, "slack %s: Slack returned a body that is not valid JSON: %s", method, err)
	}
	body, ok := asObject(decoded)
	if !ok {
		return nil, i18n.NewError("slack %s: Slack returned an unexpected payload", method)
	}
	if !flag(body, "ok") {
		code, ok := str(body, "error")
		if !ok {
			code = "unknown_error"
		}
		message := describeSlackError(code, body["needed"])
		return nil, &APIError{Method: method, Code: code, Message: message.Text(i18n.EN), text: message}
	}
	return body, nil
}

func unexpected(method string) error {
	return i18n.NewError("slack %s: Slack returned an unexpected payload", method)
}

// Identity tells who the token belongs to and the workspace URL (with a trailing slash). It is looked up once.
func (c *Client) Identity(ctx context.Context) (userID, teamURL string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != nil {
		return c.identity.userID, c.identity.teamURL, nil
	}
	body, err := c.call(ctx, "auth.test", url.Values{})
	if err != nil {
		return "", "", err
	}
	userID, okUser := str(body, "user_id")
	teamURL, okURL := str(body, "url")
	if !okUser || !okURL {
		return "", "", unexpected("auth.test")
	}
	if !strings.HasSuffix(teamURL, "/") {
		teamURL += "/"
	}
	c.identity = &identity{userID: userID, teamURL: teamURL}
	return userID, teamURL, nil
}

// searchHit is a message found by Slack search, with where it was posted.
type searchHit struct {
	ts        string
	text      string
	userID    string
	channelID string
	// channelName is empty for direct messages.
	channelName string
	isDM        bool
	// files is nil when search did not say, and non-nil (possibly empty) when it did.
	files []FileRef
	// threadTS is the parent of the thread the message belongs to (from its permalink); empty for a top-level message.
	threadTS string
	// directPeerID is the other participant of a direct message (the user themself for a note to self).
	directPeerID string
	permalink    *string
}

// conversationMessage is a message read from a conversation; userID is empty for bots and system messages.
type conversationMessage struct {
	ts     string
	text   string
	userID string
	files  []FileRef
}

type conversation struct{ id, name string }

type contextMessage struct{ userID, text string }

var peerIDRe = regexp.MustCompile(`^[UW][A-Z0-9]+$`)

func threadTSOf(permalink string) string {
	parsed, err := url.Parse(permalink)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("thread_ts")
}

// search returns the newest matches first. It needs the search:read user scope.
func (c *Client) search(ctx context.Context, query string, count int) ([]searchHit, error) {
	body, err := c.call(ctx, "search.messages", url.Values{
		"query":    {query},
		"count":    {strconv.Itoa(count)},
		"sort":     {"timestamp"},
		"sort_dir": {"desc"},
	})
	if err != nil {
		return nil, err
	}
	container, _ := asObject(body["messages"])
	matches, ok := list(container, "matches")
	if !ok {
		return nil, unexpected("search.messages")
	}
	hits := []searchHit{}
	for _, item := range matches {
		match, ok := asObject(item)
		if !ok {
			continue
		}
		ts, okTS := str(match, "ts")
		text, okText := str(match, "text")
		channel, _ := asObject(match["channel"])
		channelID, okChannel := str(channel, "id")
		userID, okUser := str(match, "user")
		if !okTS || !okText || !okChannel || !okUser {
			continue
		}
		hit := searchHit{ts: ts, text: text, userID: userID, channelID: channelID, isDM: flag(channel, "is_im")}
		if _, listed := match["files"].([]any); listed {
			hit.files = parseFiles(match["files"])
		}
		if hit.isDM {
			// For a direct message Slack puts the other person's id in `user` (and often in `name`).
			for _, key := range []string{"user", "name"} {
				if value, ok := str(channel, key); ok && peerIDRe.MatchString(value) {
					hit.directPeerID = value
					break
				}
			}
		} else if name, ok := str(channel, "name"); ok {
			hit.channelName = name
		}
		if link, ok := str(match, "permalink"); ok {
			hit.permalink = &link
			hit.threadTS = threadTSOf(link)
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func parseConversationMessages(body object, method string) ([]conversationMessage, error) {
	items, ok := list(body, "messages")
	if !ok {
		return nil, unexpected(method)
	}
	messages := []conversationMessage{}
	for _, item := range items {
		message, ok := asObject(item)
		if !ok {
			continue
		}
		ts, okTS := str(message, "ts")
		text, okText := str(message, "text")
		if !okTS || !okText {
			continue
		}
		userID, _ := str(message, "user")
		messages = append(messages, conversationMessage{ts: ts, text: text, userID: userID, files: parseFiles(message["files"])})
	}
	return messages, nil
}

// messagesSince returns the messages posted in a conversation since oldest (epoch seconds), oldest first. It needs
// the matching *:history scope.
func (c *Client) messagesSince(ctx context.Context, channel string, oldest int64, limit int) ([]conversationMessage, error) {
	body, err := c.call(ctx, "conversations.history", url.Values{
		"channel": {channel}, "oldest": {strconv.FormatInt(oldest, 10)}, "limit": {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	messages, err := parseConversationMessages(body, "conversations.history")
	if err != nil {
		return nil, err
	}
	// Slack answers newest first.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// replies returns the replies of a thread posted since oldest (epoch seconds), parent message excluded, oldest first.
func (c *Client) replies(ctx context.Context, channel, threadTS string, oldest int64) ([]conversationMessage, error) {
	body, err := c.call(ctx, "conversations.replies", url.Values{
		"channel": {channel}, "ts": {threadTS}, "oldest": {strconv.FormatInt(oldest, 10)}, "limit": {"50"},
	})
	if err != nil {
		return nil, err
	}
	messages, err := parseConversationMessages(body, "conversations.replies")
	if err != nil {
		return nil, err
	}
	replies := messages[:0]
	for _, message := range messages {
		if message.ts != threadTS {
			replies = append(replies, message)
		}
	}
	return replies, nil
}

// groupConversations lists the group conversations the user is in. It needs the mpim:read scope.
func (c *Client) groupConversations(ctx context.Context) ([]conversation, error) {
	body, err := c.call(ctx, "users.conversations", url.Values{"types": {"mpim"}, "limit": {"200"}, "exclude_archived": {"true"}})
	if err != nil {
		return nil, err
	}
	items, ok := list(body, "channels")
	if !ok {
		return nil, unexpected("users.conversations")
	}
	groups := []conversation{}
	for _, item := range items {
		channel, ok := asObject(item)
		if !ok {
			continue
		}
		id, ok := str(channel, "id")
		if !ok {
			continue
		}
		name, ok := str(channel, "name")
		if !ok {
			name = id
		}
		groups = append(groups, conversation{id: id, name: name})
	}
	return groups, nil
}

// messageFiles returns the files of one message, when a search result did not say.
func (c *Client) messageFiles(ctx context.Context, channel, ts string) ([]FileRef, error) {
	body, err := c.call(ctx, "conversations.history", url.Values{
		"channel": {channel}, "latest": {ts}, "oldest": {ts}, "inclusive": {"true"}, "limit": {"1"},
	})
	if err != nil {
		return nil, err
	}
	if items, ok := list(body, "messages"); ok && len(items) > 0 {
		if first, ok := asObject(items[0]); ok {
			return parseFiles(first["files"]), nil
		}
	}
	return []FileRef{}, nil
}

// messagesBefore returns the limit messages posted in a conversation just before ts, oldest first.
func (c *Client) messagesBefore(ctx context.Context, channel, ts string, limit int) ([]contextMessage, error) {
	body, err := c.call(ctx, "conversations.history", url.Values{
		"channel": {channel}, "latest": {ts}, "inclusive": {"false"}, "limit": {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	items, ok := list(body, "messages")
	if !ok {
		return nil, unexpected("conversations.history")
	}
	messages := []contextMessage{}
	for i := len(items) - 1; i >= 0; i-- {
		message, ok := asObject(items[i])
		if !ok {
			continue
		}
		text, ok := str(message, "text")
		if !ok {
			continue
		}
		userID, _ := str(message, "user")
		messages = append(messages, contextMessage{userID: userID, text: text})
	}
	return messages, nil
}

// displayName resolves a user id to the name people know them by, caching the answer.
func (c *Client) displayName(ctx context.Context, userID string) (string, error) {
	c.mu.Lock()
	known, ok := c.names[userID]
	c.mu.Unlock()
	if ok {
		return known, nil
	}
	body, err := c.call(ctx, "users.info", url.Values{"user": {userID}})
	if err != nil {
		return "", err
	}
	user, _ := asObject(body["user"])
	profile, _ := asObject(user["profile"])
	name := userID
	for _, candidate := range []struct {
		from object
		key  string
	}{{profile, "display_name"}, {user, "real_name"}, {user, "name"}} {
		if value, _ := str(candidate.from, candidate.key); value != "" {
			name = value
			break
		}
	}
	c.mu.Lock()
	c.names[userID] = name
	c.mu.Unlock()
	return name, nil
}
