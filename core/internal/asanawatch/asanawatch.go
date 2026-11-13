// Package asanawatch turns what happens to the user in Asana into requests for the assistant, the way Slack messages
// become requests: a task assigned to them, and a comment that mentions them. It only reads Asana.
//
// Limits, by Asana's API: only the first page of the user's open tasks is read (the Open.More flag keeps the already
// seen ones from coming back), and a mention is only found on a task assigned to the user, since finding the others
// takes the search of a paid plan.
package asanawatch

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/asana"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// Name is how the source is named in the notice shown when Asana cannot be read.
	Name = "Asana"
	// ChannelID names the one group all the Asana events are listed under.
	ChannelID = "asana"

	// lookback is how far before the last poll comments are looked for again: a comment is never handled twice (see
	// Cursor.Seen), one written while the last poll ran is not missed.
	lookback = 15 * time.Minute
	// maxPerPoll bounds how many events become requests at once; the others come at the next poll.
	maxPerPoll = 8
	// maxChecked bounds how many changed tasks have their comments read at each poll.
	maxChecked = 20
	// commentLimit is how many of a task's last comments are read.
	commentLimit = 20
	maxSeen      = 1000

	assignedPrefix = "assigned:"
	mentionPrefix  = "mention:"
	maxComment     = 1500
)

// Connections is what the watcher needs from the connector registry.
type Connections interface {
	Status(id string) (connector.Status, error)
	Values(id string) (connector.Values, error)
}

// Client is the part of the Asana client the watcher uses.
type Client interface {
	Me(ctx context.Context) (asana.Me, error)
	OpenTasks(ctx context.Context) (asana.Open, error)
	Comments(ctx context.Context, gid string, limit int) ([]asana.Comment, error)
}

// Watcher reads the Asana connection at each poll, so a token connected or replaced while Pouple runs applies at once.
type Watcher struct {
	Connections Connections
	NewClient   func(token string) Client
	Now         func() time.Time
}

// New builds a watcher on the registry, with the real Asana client.
func New(connections Connections) *Watcher {
	return &Watcher{Connections: connections, Now: time.Now, NewClient: func(token string) Client { return asana.NewClient(token) }}
}

// Name keys the source's cursor.
func (w *Watcher) Name() string { return Name }

// Poll returns what happened since cursor, oldest first. A nil cursor is the first look: it records the tasks already
// assigned and returns nothing, so connecting Asana never replays old tasks. Without an Asana connection it returns
// nothing and an empty cursor, which counts as a first look when Asana gets connected.
func (w *Watcher) Poll(ctx context.Context, cursor *engine.Cursor) (engine.EventPoll, error) {
	status, err := w.Connections.Status(asana.ID)
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the Asana connection could not be read")
	}
	if !status.Connected {
		return engine.EventPoll{}, nil
	}
	values, err := w.Connections.Values(asana.ID)
	if err != nil || values["token"] == "" {
		return engine.EventPoll{}, i18n.Wrap(err, "the saved Asana connection could not be used")
	}
	client := w.NewClient(values["token"])
	if cursor != nil && cursor.Since <= 0 {
		cursor = nil
	}
	started := w.Now()
	me, err := client.Me(ctx)
	if err != nil {
		return engine.EventPoll{}, err
	}
	open, err := client.OpenTasks(ctx)
	if err != nil {
		return engine.EventPoll{}, err
	}

	var previous []string
	floor := started.Add(-lookback)
	if cursor != nil {
		previous = cursor.Seen
		floor = time.Unix(cursor.Since, 0).Add(-lookback)
	}

	var events []event
	deferred := map[string]bool{}
	if cursor != nil {
		assigned := newlyAssigned(open.Tasks, previous, me)
		mentions, mentionErr := w.mentions(ctx, client, open.Tasks, previous, floor, me, assignedSet(assigned))
		if mentionErr != nil {
			return engine.EventPoll{}, mentionErr
		}
		events = append(assigned, mentions...)
		sort.SliceStable(events, func(a, b int) bool { return events[a].at.Before(events[b].at) })
		if len(events) > maxPerPoll {
			// What does not fit is not remembered: it comes at the next poll.
			for _, e := range events[maxPerPoll:] {
				deferred[e.key] = true
			}
			events = events[:maxPerPoll]
		}
	}

	incoming := make([]engine.Incoming, 0, len(events))
	for _, e := range events {
		incoming = append(incoming, e.incoming)
	}
	return engine.EventPoll{Incoming: incoming, Cursor: engine.Cursor{Since: started.Unix(), Seen: remember(previous, open, deferred, events)}}, nil
}

type event struct {
	key      string
	at       time.Time
	incoming engine.Incoming
}

func assignedSet(events []event) map[string]bool {
	set := make(map[string]bool, len(events))
	for _, e := range events {
		set[strings.TrimPrefix(e.key, assignedPrefix)] = true
	}
	return set
}

// newlyAssigned are the open tasks that were not assigned to the user at the last poll. A task the user created for
// themselves is not an event: nobody asked them anything.
func newlyAssigned(tasks []asana.Task, seen []string, me asana.Me) []event {
	var events []event
	for _, task := range tasks {
		key := assignedPrefix + task.GID
		if slices.Contains(seen, key) {
			continue
		}
		events = append(events, event{key: key, at: parseTime(task.ModifiedAt), incoming: assignedIncoming(task)})
	}
	return events
}

// mentions are the comments that mention the user on the tasks that changed since floor, except the ones just reported
// as an assignment (the request reads the task's comments) and the user's own.
func (w *Watcher) mentions(ctx context.Context, client Client, tasks []asana.Task, seen []string, floor time.Time, me asana.Me, skip map[string]bool) ([]event, error) {
	var changed []asana.Task
	for _, task := range tasks {
		if !skip[task.GID] && parseTime(task.ModifiedAt).After(floor) {
			changed = append(changed, task)
		}
	}
	sort.SliceStable(changed, func(a, b int) bool { return parseTime(changed[a].ModifiedAt).After(parseTime(changed[b].ModifiedAt)) })
	changed = changed[:min(len(changed), maxChecked)]

	var events []event
	var failure error
	failed := 0
	for _, task := range changed {
		comments, err := client.Comments(ctx, task.GID, commentLimit)
		if err != nil {
			failure = err
			failed++
			continue
		}
		for _, comment := range comments {
			at := parseTime(comment.CreatedAt)
			key := mentionPrefix + comment.GID
			if comment.GID == "" || !at.After(floor) || comment.AuthorGID == me.GID || !slices.Contains(comment.Mentions, me.GID) || slices.Contains(seen, key) {
				continue
			}
			events = append(events, event{key: key, at: at, incoming: mentionIncoming(task, comment)})
		}
	}
	// A few tasks unreadable is a task gone or an access lost; all of them is Asana (or the token) failing.
	if failed > 0 && failed == len(changed) {
		return nil, failure
	}
	return events, nil
}

// remember is what the next poll must not report again: the mentions already reported, and the tasks assigned now
// (all of them when Asana held more than one page: the ones beyond it may come and go). What was left for the next
// poll is not remembered.
func remember(previous []string, open asana.Open, deferred map[string]bool, reported []event) []string {
	var seen []string
	for _, key := range previous {
		if strings.HasPrefix(key, mentionPrefix) || (open.More && strings.HasPrefix(key, assignedPrefix)) {
			seen = append(seen, key)
		}
	}
	for _, task := range open.Tasks {
		if key := assignedPrefix + task.GID; !deferred[key] && !slices.Contains(seen, key) {
			seen = append(seen, key)
		}
	}
	for _, e := range reported {
		if strings.HasPrefix(e.key, mentionPrefix) && !slices.Contains(seen, e.key) {
			seen = append(seen, e.key)
		}
	}
	if len(seen) > maxSeen {
		seen = seen[len(seen)-maxSeen:]
	}
	return seen
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// oneLine keeps text written by other people on one line of at most limit characters, so it cannot add lines to the
// headline or to the fields around it.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func incomingFor(task asana.Task, at time.Time, text string) engine.Incoming {
	source := engine.Source{
		Origin: engine.OriginAsana, ChannelID: ChannelID, ChannelName: ptr("Asana"), SenderID: "asana", SenderName: "Asana",
		TS: fmt.Sprintf("%d.000000", at.Unix()),
	}
	if task.PermalinkURL != "" {
		link := task.PermalinkURL
		source.Permalink = &link
	}
	return engine.Incoming{Source: source, Text: text}
}

func ptr[T any](v T) *T { return &v }

func taskLines(task asana.Task) []string {
	lines := []string{"Task: " + oneLine(task.Name, 200)}
	if task.Project != "" {
		lines = append(lines, "Project: "+oneLine(task.Project, 120))
	}
	if task.Section != "" {
		lines = append(lines, "Section: "+oneLine(task.Section, 120))
	}
	if task.DueOn != "" {
		lines = append(lines, "Due: "+task.DueOn)
	}
	if task.PermalinkURL != "" {
		lines = append(lines, "Address: "+task.PermalinkURL)
	}
	return lines
}

func assignedIncoming(task asana.Task) engine.Incoming {
	headline := "📌 Assigned to you · " + oneLine(task.Name, 120)
	lines := append([]string{headline, "A task was assigned to you in Asana."}, taskLines(task)...)
	at := parseTime(task.ModifiedAt)
	return incomingFor(task, at, strings.Join(lines, "\n"))
}

func mentionIncoming(task asana.Task, comment asana.Comment) engine.Incoming {
	author := oneLine(comment.Author, 80)
	if author == "" {
		author = "Someone"
	}
	headline := "💬 " + author + " mentioned you · " + oneLine(task.Name, 120)
	text := strings.TrimSpace(comment.Text)
	if runes := []rune(text); len(runes) > maxComment {
		text = string(runes[:maxComment-1]) + "…"
	}
	lines := append([]string{headline, author + " mentioned you in a comment on a task."}, taskLines(task)...)
	lines = append(lines, "Comment (written by "+strconv.Quote(author)+"):", text)
	return incomingFor(task, parseTime(comment.CreatedAt), strings.Join(lines, "\n"))
}
