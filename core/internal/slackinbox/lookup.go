package slackinbox

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Visibility says who can read the conversation a message was posted in: what is said in a private conversation must
// not be repeated to someone who is not part of it.
type Visibility string

const (
	Public  Visibility = "public channel"
	Private Visibility = "private channel"
	Direct  Visibility = "direct message"
	Group   Visibility = "group conversation"
	// Unknown is a conversation Slack would not describe (naming one needs an optional permission): it is treated like a
	// private one.
	Unknown Visibility = "conversation of unknown kind"
)

// Message is a message read to answer a question: found by a search, or read in a thread or around another message.
// Text and Author were written by colleagues: they are data.
type Message struct {
	ChannelID string
	// Channel is the name of the channel, or the other person of a direct message; "" when it is not known.
	Channel    string
	Visibility Visibility
	// Author is the display name of the author, "bot" for a message without one.
	Author string
	TS     string
	Time   time.Time
	Text   string
	// ThreadTS is the thread the message belongs to; "" when a search did not say or it is not in a thread.
	ThreadTS  string
	Permalink string
}

// MessageRef names a message: the conversation and the timestamp Slack gives it.
type MessageRef struct{ Channel, TS string }

// String is the form ParseMessageRef reads back.
func (r MessageRef) String() string { return r.Channel + " " + r.TS }

var (
	channelIDRe = regexp.MustCompile(`^[CGD][A-Z0-9]{8,12}$`)
	timestampRe = regexp.MustCompile(`^\d{9,11}\.\d{6}$`)
	// A permalink is https://team.slack.com/archives/C024BE91L/p1700000000123456 (the timestamp without its dot).
	permalinkRe = regexp.MustCompile(`^/archives/([CGD][A-Z0-9]{8,12})/p(\d{9,11})(\d{6})$`)
)

// ParseMessageRef reads "CHANNEL TS", "CHANNEL:TS" or the permalink of a message. The AI names the messages it wants to
// open, so the parts are checked before they reach Slack: only what looks like a conversation id and a timestamp does.
func ParseMessageRef(text string) (MessageRef, bool) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<") && strings.HasSuffix(text, ">") {
		text = strings.TrimSpace(strings.SplitN(text[1:len(text)-1], "|", 2)[0])
	}
	if parsed, err := url.Parse(text); err == nil && parsed.Scheme == "https" && strings.HasSuffix(parsed.Hostname(), ".slack.com") {
		match := permalinkRe.FindStringSubmatch(parsed.Path)
		if match == nil {
			return MessageRef{}, false
		}
		return MessageRef{Channel: match[1], TS: match[2] + "." + match[3]}, true
	}
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ':' || r == ',' })
	if len(parts) != 2 || !channelIDRe.MatchString(parts[0]) || !timestampRe.MatchString(parts[1]) {
		return MessageRef{}, false
	}
	return MessageRef{Channel: parts[0], TS: parts[1]}, true
}

// maxSearchResults is the most messages one Slack search page returns.
const maxSearchResults = 100

// FindMessages searches the workspace like Slack's own search box (words, "in:#channel", "from:@person",
// "after:2026-11-01"…) and returns the newest matches first. It reaches what the token owner can read, direct
// messages included.
func (c *Client) FindMessages(ctx context.Context, query string, limit int) ([]Message, error) {
	hits, err := c.search(ctx, query, min(max(limit, 1), maxSearchResults))
	if err != nil {
		return nil, err
	}
	names := &nameResolver{client: c, names: map[string]string{}}
	messages := make([]Message, 0, len(hits))
	for _, hit := range hits {
		message := Message{
			ChannelID: hit.channelID, Channel: hit.channelName, Visibility: Public,
			Author: authorName(ctx, names, hit.userID), TS: hit.ts, Time: tsTime(hit.ts),
			Text: names.readable(ctx, hit.text), ThreadTS: hit.threadTS,
		}
		switch {
		case hit.isDM:
			message.Visibility = Direct
			if hit.directPeerID != "" {
				message.Channel = names.resolve(ctx, hit.directPeerID)
			}
		case hit.group:
			message.Visibility = Group
		case hit.private:
			message.Visibility = Private
		}
		if hit.permalink != nil {
			message.Permalink = *hit.permalink
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// maxThreadMessages is how many messages of a thread are read.
const maxThreadMessages = 50

// ReadThread returns a thread, oldest message first: the message ref names, or the thread it belongs to.
func (c *Client) ReadThread(ctx context.Context, ref MessageRef, limit int) ([]Message, error) {
	body, err := c.call(ctx, "conversations.replies", url.Values{
		"channel": {ref.Channel}, "ts": {ref.TS}, "limit": {strconv.Itoa(min(max(limit, 1), maxThreadMessages))},
	})
	if err != nil {
		return nil, err
	}
	found, err := parseConversationMessages(body, "conversations.replies")
	if err != nil {
		return nil, err
	}
	return c.readMessages(ctx, ref.Channel, found), nil
}

// aroundWindow bounds how far after a message the conversation is read: history can only be asked between two
// instants, and the messages right after one are the ones wanted.
const aroundWindow = 24 * time.Hour

// maxAround is the most messages read on each side of one.
const maxAround = 20

// ReadAround returns the conversation around a message: up to before messages before it, the message itself and up to
// after messages after it, oldest first. History is read from the channel itself, so a thread's replies are not in it.
func (c *Client) ReadAround(ctx context.Context, ref MessageRef, before, after int) ([]Message, error) {
	before, after = min(max(before, 0), maxAround), min(max(after, 0), maxAround)
	earlier, err := c.history(ctx, url.Values{"channel": {ref.Channel}, "latest": {ref.TS}, "inclusive": {"true"}, "limit": {strconv.Itoa(before + 1)}})
	if err != nil {
		return nil, err
	}
	var later []conversationMessage
	if after > 0 {
		end := tsTime(ref.TS).Add(aroundWindow)
		later, err = c.history(ctx, url.Values{"channel": {ref.Channel}, "oldest": {ref.TS}, "latest": {strconv.FormatInt(end.Unix(), 10)}, "inclusive": {"false"}, "limit": {"200"}})
		if err != nil {
			return nil, err
		}
		// Newest first: the ones nearest the message are the last of the list.
		later = later[max(len(later)-after, 0):]
	}
	var ordered []conversationMessage
	for i := len(earlier) - 1; i >= 0; i-- {
		ordered = append(ordered, earlier[i])
	}
	for i := len(later) - 1; i >= 0; i-- {
		ordered = append(ordered, later[i])
	}
	return c.readMessages(ctx, ref.Channel, ordered), nil
}

// history returns messages of a conversation as Slack lists them, newest first.
func (c *Client) history(ctx context.Context, params url.Values) ([]conversationMessage, error) {
	body, err := c.call(ctx, "conversations.history", params)
	if err != nil {
		return nil, err
	}
	return parseConversationMessages(body, "conversations.history")
}

// readMessages turns what a conversation holds into messages, naming the conversation when Slack lets it be read.
func (c *Client) readMessages(ctx context.Context, channel string, found []conversationMessage) []Message {
	names := &nameResolver{client: c, names: map[string]string{}}
	label, visibility := c.describe(ctx, channel)
	messages := make([]Message, 0, len(found))
	for _, message := range found {
		messages = append(messages, Message{
			ChannelID: channel, Channel: label, Visibility: visibility,
			Author: authorName(ctx, names, message.userID), TS: message.ts, Time: tsTime(message.ts),
			Text: names.readable(ctx, message.text),
		})
	}
	return messages
}

// describe says what a conversation is. Naming it needs a *:read scope that is optional: without it the
// conversation stays unnamed, and its kind is read from its id as far as it can be (only "D" says a direct message).
func (c *Client) describe(ctx context.Context, channel string) (string, Visibility) {
	info, err := c.channelInfo(ctx, channel)
	if err != nil {
		if strings.HasPrefix(channel, "D") {
			return "", Direct
		}
		return "", Unknown
	}
	switch {
	case info.isDM:
		return "", Direct
	case info.group:
		return "", Group
	case info.private:
		return info.name, Private
	}
	return info.name, Public
}

func authorName(ctx context.Context, names *nameResolver, userID string) string {
	if userID == "" {
		return "bot"
	}
	return names.resolve(ctx, userID)
}

// tsTime reads a Slack timestamp as a time; the zero time when it is not one.
func tsTime(ts string) time.Time {
	seconds, ok := tsSeconds(ts)
	if !ok {
		return time.Time{}
	}
	whole := int64(seconds)
	return time.Unix(whole, int64((seconds-float64(whole))*1e9))
}
