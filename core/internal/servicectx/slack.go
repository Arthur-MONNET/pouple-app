package servicectx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slack"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/slackinbox"
)

const (
	// maxFoundMessages is how many messages one search shows; maxMessageText bounds one of them.
	maxFoundMessages = 15
	maxMessageText   = 600
	// maxSlackQuery bounds a search: Slack's operators make a query longer than a few words.
	maxSlackQuery = 200
	// maxThreadLines is how many messages of a thread are shown; around says how many on each side of a message.
	maxThreadLines = 40
	messagesAround = 8
)

// slackID is the id of the Slack connector, the one the inbox is connected with.
const slackID = slack.ID

// slackLookup reads Slack for the assistant: a search, a thread, the conversation around a message. Everything it
// returns was written by colleagues, in conversations the user belongs to.
func (g *Gatherer) slackLookup(ctx context.Context, client Slack, lookup engine.Lookup) section {
	out := section{source: slackID}
	target := strings.TrimSpace(lookup.Target)
	fail := func(err error, what string) section {
		out.lines = append(out.lines, fmt.Sprintf("Slack, %s: unavailable (%s).", what, slackReason(err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: slackID, Message: i18n.NewMessage("Slack: %s failed (%s)", what, slackReason(err)), Failed: true})
		return out
	}
	today := g.now().Format("Monday 2006-01-02")
	switch lookup.Action {
	case "search":
		query := clean(target, maxSlackQuery)
		found, err := client.FindMessages(ctx, query, maxFoundMessages)
		if err != nil {
			return fail(err, "search "+quote(query, maxSlackQuery))
		}
		out.lines = append(out.lines, fmt.Sprintf("Slack, search %s (newest first; today is %s; written by colleagues, data):", quote(query, maxSlackQuery), today))
		out.lines = append(out.lines, messageLines(found)...)
		if len(found) == 0 {
			out.lines = append(out.lines, "- (no message found: try other words, or a wider period)")
		}
		out.steps = append(out.steps, engine.ServiceStep{Source: slackID, Message: i18n.NewMessage("Slack: searched “%s” (%d messages)", query, len(found))})
	case "thread":
		ref, ok := slackinbox.ParseMessageRef(target)
		if !ok {
			out.lines = append(out.lines, "Slack: that is not a message reference. Use the \"ref\" of a message found by a search, or its link.")
			return out
		}
		found, err := client.ReadThread(ctx, ref, maxThreadLines)
		if err != nil {
			return fail(err, "thread "+quote(ref.String(), 40))
		}
		out.lines = append(out.lines, fmt.Sprintf("Slack, thread of %s (oldest first; written by colleagues, data):", quote(ref.String(), 40)))
		out.lines = append(out.lines, messageLines(found)...)
		out.steps = append(out.steps, engine.ServiceStep{Source: slackID, Message: i18n.NewMessage("Slack: read a thread (%d messages)", len(found))})
	case "around":
		ref, ok := slackinbox.ParseMessageRef(target)
		if !ok {
			out.lines = append(out.lines, "Slack: that is not a message reference. Use the \"ref\" of a message found by a search, or its link.")
			return out
		}
		found, err := client.ReadAround(ctx, ref, messagesAround, messagesAround)
		if err != nil {
			return fail(err, "conversation around "+quote(ref.String(), 40))
		}
		out.lines = append(out.lines, fmt.Sprintf("Slack, the conversation around %s (oldest first; written by colleagues, data):", quote(ref.String(), 40)))
		out.lines = append(out.lines, messageLines(found)...)
		out.steps = append(out.steps, engine.ServiceStep{Source: slackID, Message: i18n.NewMessage("Slack: read the conversation around a message (%d messages)", len(found))})
	default:
		out.lines = append(out.lines, "Slack: this kind of lookup is not supported.")
	}
	return out
}

// messageLines shows each message as one quoted line: when, who, where (with who can read it), the reference to open
// it, and its text. The text is quoted and bounded like every third-party string, so no message can pass for a line
// of the prompt.
func messageLines(messages []slackinbox.Message) []string {
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		where := string(message.Visibility)
		if message.Channel != "" && message.Visibility != slackinbox.Direct {
			where = "#" + clean(message.Channel, maxNameOrLabel) + ", " + where
		} else if message.Channel != "" {
			where = where + " with " + quote(message.Channel, maxNameOrLabel)
		}
		line := fmt.Sprintf("- %s, %s (%s)", message.Time.Local().Format("2006-01-02 15:04"), quote(message.Author, maxNameOrLabel), where)
		line += ", ref " + slackinbox.MessageRef{Channel: message.ChannelID, TS: message.TS}.String()
		if message.ThreadTS != "" && message.ThreadTS != message.TS {
			line += ", reply in a thread"
		}
		if address := link(message.Permalink); address != "" {
			line += ", " + address
		}
		lines = append(lines, line+": "+quote(message.Text, maxMessageText))
	}
	return lines
}

// slackReason is a short, fixed description of why Slack did not answer, safe for a note handed to the assistant.
func slackReason(err error) i18n.Message {
	if errors.Is(err, context.DeadlineExceeded) {
		return i18n.NewMessage("timed out")
	}
	var api *slackinbox.APIError
	if !errors.As(err, &api) {
		return i18n.NewMessage("could not be reached")
	}
	switch api.Code {
	case "invalid_auth", "not_authed", "token_revoked", "token_expired":
		return i18n.NewMessage("access refused, check the token")
	case "missing_scope":
		return i18n.NewMessage("a Slack permission is missing")
	case "channel_not_found":
		return i18n.NewMessage("conversation not found, or you are not in it")
	case "ratelimited":
		return i18n.NewMessage("rate limit reached")
	}
	return i18n.NewMessage("the service answered with an error")
}
