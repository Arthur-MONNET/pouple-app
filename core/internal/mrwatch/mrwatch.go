// Package mrwatch turns what reviewers write on the user's own open merge requests into requests for the assistant, the
// way Slack messages become requests. It only reads GitLab.
//
// One request is made per merge request and per poll, holding the new comments of that poll, so a review of ten
// remarks is one conversation and not ten.
package mrwatch

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// Name is how the source is named in the notice shown when GitLab cannot be read.
	Name = "GitLab merge requests"
	// ChannelID names the one group all the merge request feedback is listed under.
	ChannelID = "gitlab-merge-requests"

	// lookback is how far before the last poll comments are looked for again: a comment is never handled twice (see
	// Cursor.Seen), one written while the last poll ran is not missed.
	lookback = 15 * time.Minute
	// maxPerPoll bounds how many merge requests become requests at once; the others come at the next poll.
	maxPerPoll = 6
	// maxChecked bounds how many changed merge requests have their comments read at each poll.
	maxChecked = 15
	// maxNotes is how many comments of one merge request are shown in one request.
	maxNotes = 10
	maxSeen  = 1000
	// maxBody is how much of one comment is shown.
	maxBody = 800
)

// Connections is what the watcher needs from the connector registry.
type Connections interface {
	Status(id string) (connector.Status, error)
	Values(id string) (connector.Values, error)
}

// Client is the part of the GitLab client the watcher uses.
type Client interface {
	User(ctx context.Context) (gitlab.User, error)
	OpenMine(ctx context.Context) ([]gitlab.MergeRequest, error)
	RecentNotes(ctx context.Context, ref gitlab.MRRef, limit int) ([]gitlab.Note, error)
}

// Watcher reads the GitLab connection at each poll, so a token connected or replaced while Pouple runs applies at once.
type Watcher struct {
	Connections Connections
	NewClient   func(address, token string) (Client, error)
	Now         func() time.Time
}

// New builds a watcher on the registry, with the real GitLab client.
func New(connections Connections) *Watcher {
	return &Watcher{
		Connections: connections, Now: time.Now,
		NewClient: func(address, token string) (Client, error) {
			client, err := gitlab.NewClient(address, token)
			return client, err
		},
	}
}

// Name keys the source's cursor.
func (w *Watcher) Name() string { return Name }

func key(ref gitlab.MRRef, id int) string { return "note:" + ref.String() + ":" + strconv.Itoa(id) }

// Poll returns the merge requests that got new comments since cursor. A nil cursor is the first look: it records the
// place and returns nothing, so connecting GitLab never replays old reviews. Without a GitLab connection it returns
// nothing and an empty cursor, which counts as a first look when GitLab gets connected.
func (w *Watcher) Poll(ctx context.Context, cursor *engine.Cursor) (engine.EventPoll, error) {
	status, err := w.Connections.Status(gitlab.ID)
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the GitLab connection could not be read")
	}
	if !status.Connected {
		return engine.EventPoll{}, nil
	}
	values, err := w.Connections.Values(gitlab.ID)
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the saved GitLab connection could not be used")
	}
	client, err := w.NewClient(values["url"], gitlab.ReadToken(values))
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the saved GitLab connection could not be used")
	}
	started := w.Now()
	if cursor != nil && cursor.Since <= 0 {
		cursor = nil
	}
	if cursor == nil {
		return engine.EventPoll{Cursor: engine.Cursor{Since: started.Unix()}}, nil
	}
	floor := time.Unix(cursor.Since, 0).Add(-lookback)

	me, err := client.User(ctx)
	if err != nil {
		return engine.EventPoll{}, err
	}
	mine, err := client.OpenMine(ctx)
	if err != nil {
		return engine.EventPoll{}, err
	}
	var changed []gitlab.MergeRequest
	for _, mr := range mine {
		if parseTime(mr.UpdatedAt).After(floor) {
			changed = append(changed, mr)
		}
	}
	changed = changed[:min(len(changed), maxChecked)]

	seen := slices.Clone(cursor.Seen)
	var withNews []feedback
	var failure error
	failed := 0
	for _, mr := range changed {
		ref := gitlab.MRRef{Project: mr.ProjectPath, IID: mr.IID}
		notes, err := client.RecentNotes(ctx, ref, 30)
		if err != nil {
			failure = err
			failed++
			continue
		}
		var fresh []gitlab.Note
		for _, note := range notes {
			// Someone else's comment that was not reported yet. The user's own remarks, and the ones from before the
			// last look, are not news.
			if note.Author != me.Username && parseTime(note.CreatedAt).After(floor) && !slices.Contains(seen, key(ref, note.ID)) {
				fresh = append(fresh, note)
			}
		}
		if len(fresh) > 0 {
			withNews = append(withNews, feedback{mr: mr, ref: ref, notes: fresh})
		}
	}
	// A few merge requests unreadable is one gone or an access lost; all of them is GitLab (or the token) failing.
	if failed > 0 && failed == len(changed) {
		return engine.EventPoll{}, failure
	}

	sort.SliceStable(withNews, func(a, b int) bool { return withNews[a].newest().Before(withNews[b].newest()) })
	if len(withNews) > maxPerPoll {
		// What does not fit is not remembered: it comes at the next poll.
		withNews = withNews[:maxPerPoll]
	}
	incoming := make([]engine.Incoming, 0, len(withNews))
	for _, f := range withNews {
		incoming = append(incoming, f.incoming())
		for _, note := range f.notes {
			seen = append(seen, key(f.ref, note.ID))
		}
	}
	if len(seen) > maxSeen {
		seen = seen[len(seen)-maxSeen:]
	}
	return engine.EventPoll{Incoming: incoming, Cursor: engine.Cursor{Since: started.Unix(), Seen: seen}}, nil
}

// feedback is a merge request with the comments that are new on it.
type feedback struct {
	mr    gitlab.MergeRequest
	ref   gitlab.MRRef
	notes []gitlab.Note
}

func (f feedback) newest() time.Time {
	var newest time.Time
	for _, note := range f.notes {
		if at := parseTime(note.CreatedAt); at.After(newest) {
			newest = at
		}
	}
	return newest
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
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func (f feedback) incoming() engine.Incoming {
	count := len(f.notes)
	what := "1 new comment"
	if count > 1 {
		what = fmt.Sprintf("%d new comments", count)
	}
	headline := fmt.Sprintf("💬 %s · %s", f.ref, what)
	lines := []string{
		headline,
		fmt.Sprintf("New feedback on your merge request %s: %s", f.ref, oneLine(f.mr.Title, 200)),
	}
	if f.mr.SourceBranch != "" {
		lines = append(lines, fmt.Sprintf("Branch: %s → %s", oneLine(f.mr.SourceBranch, 120), oneLine(f.mr.TargetBranch, 120)))
	}
	if f.mr.WebURL != "" {
		lines = append(lines, "Merge request: "+f.mr.WebURL)
	}
	// Oldest first, as a conversation reads.
	notes := slices.Clone(f.notes)
	slices.Reverse(notes)
	for i, note := range notes {
		if i == maxNotes {
			lines = append(lines, fmt.Sprintf("(%d more comments not shown)", len(notes)-maxNotes))
			break
		}
		where := ""
		if note.File != "" {
			where = " on " + oneLine(note.File, 160)
		}
		lines = append(lines, fmt.Sprintf("Comment by %s%s:", strconv.Quote(oneLine(note.Author, 80)), where), clip(note.Body, maxBody))
	}
	source := engine.Source{
		Origin: engine.OriginMergeRequest, ChannelID: ChannelID, ChannelName: ptr("GitLab"), SenderID: "gitlab", SenderName: "GitLab",
		TS: fmt.Sprintf("%d.000000", f.newest().Unix()),
	}
	if f.mr.WebURL != "" {
		link := f.mr.WebURL
		source.Permalink = &link
	}
	return engine.Incoming{Source: source, Text: strings.Join(lines, "\n")}
}

func ptr[T any](v T) *T { return &v }
