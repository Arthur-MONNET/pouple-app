// Package githubwatch turns what happens on GitHub pull requests into requests for the assistant, the way Slack
// messages become requests: a review asked of the user, comments and reviews of other people on the user's own pull
// requests, and checks that failed on them. It only reads GitHub.
//
// One request is made per pull request, per kind of event and per poll, so a review of ten remarks is one
// conversation and not ten. GitHub's search has a small rate limit of its own: a poll makes at most two searches,
// inspects at most maxInspected pull requests, and runs at most once every minInterval.
package githubwatch

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/github"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// Name is how the source is named in the notice shown when GitHub cannot be read.
	Name = "GitHub pull requests"
	// ChannelID names the one group all the pull request events are listed under.
	ChannelID = "github-pull-requests"

	// lookback is how far before the last poll events are looked for again: an event is never handled twice (see
	// Cursor.Seen), one that happened while the last poll ran is not missed.
	lookback = 15 * time.Minute
	// minInterval is the least time between two looks at GitHub, whatever the poll cadence of the app.
	minInterval = 45 * time.Second
	// settleGrace is how long after a change a pull request without any check is still watched for checks to appear.
	settleGrace = 30 * time.Minute
	// maxPerPoll bounds how many events become requests at once; the others come at the next poll.
	maxPerPoll = 6
	// maxInspected bounds how many of the user's pull requests are read in detail at each poll.
	maxInspected = 10
	// maxComments is how many comments of one pull request are read, and maxShown how many are shown in a request.
	maxComments = 30
	maxShown    = 10
	maxFailed   = 8
	maxSeen     = 1000
	// maxBody is how much of one comment is shown.
	maxBody = 800
)

// Connections is what the watcher needs from the connector registry.
type Connections interface {
	Status(id string) (connector.Status, error)
	Values(id string) (connector.Values, error)
}

// Client is the part of the GitHub client the watcher uses.
type Client interface {
	SearchMine(ctx context.Context) (github.SearchResult, error)
	SearchReviewRequested(ctx context.Context) (github.SearchResult, error)
	User(ctx context.Context) (github.User, error)
	PullRequest(ctx context.Context, ref github.PRRef) (github.PullRequestDetails, error)
	Discussion(ctx context.Context, ref github.PRRef, since time.Time, limit int) ([]github.Comment, error)
	Checks(ctx context.Context, ref github.PRRef, sha string) (github.Checks, error)
}

// Watcher reads the GitHub connection at each poll, so a token connected or replaced while Pouple runs applies at once.
type Watcher struct {
	Connections Connections
	NewClient   func(address, token string) (Client, error)
	Now         func() time.Time
}

// New builds a watcher on the registry, with the real GitHub client.
func New(connections Connections) *Watcher {
	return &Watcher{
		Connections: connections, Now: time.Now,
		NewClient: func(address, token string) (Client, error) {
			client, err := github.NewClient(address, token)
			if err != nil {
				return nil, err
			}
			return userClient{client}, nil
		},
	}
}

// userClient drops the headers the connector reads at validation: the watcher only needs the account.
type userClient struct{ *github.Client }

func (c userClient) User(ctx context.Context) (github.User, error) {
	user, _, err := c.Client.User(ctx)
	return user, err
}

// Name keys the source's cursor.
func (w *Watcher) Name() string { return Name }

// Poll returns what happened on the user's pull requests since cursor. A nil cursor is the first look: it records the
// place and returns nothing, so connecting GitHub never replays old reviews. Without a GitHub connection it returns
// nothing and an empty cursor, which counts as a first look when GitHub gets connected.
func (w *Watcher) Poll(ctx context.Context, cursor *engine.Cursor) (engine.EventPoll, error) {
	status, err := w.Connections.Status(github.ID)
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the GitHub connection could not be read")
	}
	if !status.Connected {
		return engine.EventPoll{}, nil
	}
	started := w.Now()
	if cursor != nil && cursor.Since <= 0 {
		cursor = nil
	}
	if cursor == nil {
		return engine.EventPoll{Cursor: engine.Cursor{Since: started.Unix()}}, nil
	}
	if started.Sub(time.Unix(cursor.Since, 0)) < minInterval {
		return engine.EventPoll{Cursor: *cursor}, nil
	}
	values, err := w.Connections.Values(github.ID)
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the saved GitHub connection could not be used")
	}
	client, err := w.NewClient(values["url"], values["token"])
	if err != nil {
		return engine.EventPoll{}, i18n.Wrap(err, "the saved GitHub connection could not be used")
	}
	look := &look{client: client, floor: time.Unix(cursor.Since, 0).Add(-lookback), now: started, seen: slices.Clone(cursor.Seen)}
	if err := look.run(ctx); err != nil {
		return engine.EventPoll{}, err
	}
	return engine.EventPoll{Incoming: look.incoming(), Cursor: engine.Cursor{Since: started.Unix(), Seen: look.remembered()}}, nil
}

// look is one poll: what it found, and what it will remember once the events are handed over.
type look struct {
	client Client
	floor  time.Time
	now    time.Time
	seen   []string
	events []event
	// settled are the pull requests whose checks are all done at their current version: not read again until they change.
	settled []string
}

// event is one request to be: its time, its text, the keys that mark it handled.
type event struct {
	at   time.Time
	pr   github.PullRequest
	text []string
	keys []string
	// settles is the key that marks the pull request's checks done, kept only if this event is handed over.
	settles string
}

func (l *look) known(key string) bool { return slices.Contains(l.seen, key) }

func (l *look) run(ctx context.Context) error {
	toReview, err := l.client.SearchReviewRequested(ctx)
	if err != nil {
		return err
	}
	for _, pr := range toReview.Items {
		key := "review:" + pr.Ref.String()
		if at := parseTime(pr.UpdatedAt); at.After(l.floor) && !l.known(key) {
			l.events = append(l.events, reviewRequested(pr, at, key))
		}
	}

	mine, err := l.client.SearchMine(ctx)
	if err != nil {
		return err
	}
	me := mine.Me
	var candidates []github.PullRequest
	for _, pr := range mine.Items {
		if parseTime(pr.UpdatedAt).After(l.floor) || !l.known(settledKey(pr)) {
			candidates = append(candidates, pr)
		}
	}
	// The search gives the latest updated first: the most active pull requests are read first.
	candidates = candidates[:min(len(candidates), maxInspected)]
	if len(candidates) > 0 && me == "" {
		user, err := l.client.User(ctx)
		if err != nil {
			return err
		}
		me = user.Login
	}
	var failure error
	failed := 0
	for _, pr := range candidates {
		if err := l.inspect(ctx, pr, me); err != nil {
			failure = err
			failed++
		}
	}
	// A few pull requests unreadable is one gone or an access lost; all of them is GitHub (or the token) failing.
	if failed > 0 && failed == len(candidates) {
		return failure
	}
	return nil
}

// inspect reads one of the user's pull requests: its checks while they may still change, and the new comments and
// reviews of other people.
func (l *look) inspect(ctx context.Context, pr github.PullRequest, me string) error {
	if !l.known(settledKey(pr)) {
		details, err := l.client.PullRequest(ctx, pr.Ref)
		if err != nil {
			return err
		}
		pr.HeadBranch, pr.BaseBranch = details.HeadBranch, details.BaseBranch
		l.inspectChecks(ctx, pr, details.HeadSHA)
	}
	if !parseTime(pr.UpdatedAt).After(l.floor) {
		return nil
	}
	comments, err := l.client.Discussion(ctx, pr.Ref, l.floor, maxComments)
	if err != nil {
		return err
	}
	var fresh []github.Comment
	for _, comment := range comments {
		// Someone else's words that were not reported yet. The user's own remarks and the bots' reports are not
		// news (failed checks have their own event).
		if comment.Author != me && !comment.Bot && !l.known(commentKey(pr.Ref, comment)) {
			fresh = append(fresh, comment)
		}
	}
	if len(fresh) > 0 {
		l.events = append(l.events, feedback(pr, fresh))
	}
	return nil
}

// inspectChecks reports the checks of the pull request's head commit that failed since the last look, and marks the
// pull request settled once its checks are done, so it is not read again until it changes.
func (l *look) inspectChecks(ctx context.Context, pr github.PullRequest, sha string) {
	checks, err := l.client.Checks(ctx, pr.Ref, sha)
	if err != nil {
		// Checks that cannot be read (a token without the Checks and Commit statuses permissions) do not make the pull
		// request unreadable. It is settled all the same: reading it again at every poll would spend the rate limit
		// for nothing. Its next change tries again.
		l.settled = append(l.settled, settledKey(pr))
		return
	}
	settled := !checks.Pending && (checks.Any || l.now.Sub(parseTime(pr.UpdatedAt)) > settleGrace)
	var newly []github.Check
	for _, check := range checks.Failed {
		if parseTime(check.CompletedAt).After(l.floor) {
			newly = append(newly, check)
		}
	}
	key := "checks:" + pr.Ref.String() + ":" + sha
	switch {
	case len(newly) > 0 && !l.known(key):
		failed := checksFailed(pr, newly, key)
		if settled {
			// Settled only once the failure is handed over: one left for the next poll must be looked at again.
			failed.settles = settledKey(pr)
		}
		l.events = append(l.events, failed)
	case settled:
		l.settled = append(l.settled, settledKey(pr))
	}
}

// incoming hands over the oldest events first, at most maxPerPoll of them. What does not fit is not remembered: it
// comes at the next poll.
func (l *look) incoming() []engine.Incoming {
	sort.SliceStable(l.events, func(a, b int) bool { return l.events[a].at.Before(l.events[b].at) })
	if len(l.events) > maxPerPoll {
		l.events = l.events[:maxPerPoll]
	}
	out := make([]engine.Incoming, 0, len(l.events))
	for _, e := range l.events {
		out = append(out, e.incoming())
		l.seen = append(l.seen, e.keys...)
		if e.settles != "" {
			l.seen = append(l.seen, e.settles)
		}
	}
	return out
}

func (l *look) remembered() []string {
	seen := append(l.seen, l.settled...)
	if len(seen) > maxSeen {
		seen = seen[len(seen)-maxSeen:]
	}
	return seen
}

// settledKey names a pull request at its current version: a push or a comment changes its update time, and so the key.
func settledKey(pr github.PullRequest) string {
	return "settled:" + pr.Ref.String() + "@" + pr.UpdatedAt
}

func commentKey(ref github.PRRef, comment github.Comment) string {
	return "comment:" + ref.String() + ":" + comment.Kind + ":" + strconv.FormatInt(comment.ID, 10)
}

func reviewRequested(pr github.PullRequest, at time.Time, key string) event {
	lines := []string{
		fmt.Sprintf("👀 %s · review requested", pr.Ref),
		fmt.Sprintf("Your review is requested on the pull request %s: %s", pr.Ref, oneLine(pr.Title, 200)),
	}
	if pr.Author != "" {
		lines = append(lines, "Author: "+strconv.Quote(oneLine(pr.Author, 80)))
	}
	if pr.Draft {
		lines = append(lines, "It is a draft.")
	}
	return event{at: at, pr: pr, text: lines, keys: []string{key}}
}

func feedback(pr github.PullRequest, comments []github.Comment) event {
	count := len(comments)
	what := "1 new comment"
	if count > 1 {
		what = fmt.Sprintf("%d new comments", count)
	}
	lines := []string{
		fmt.Sprintf("💬 %s · %s", pr.Ref, what),
		fmt.Sprintf("New feedback on your pull request %s: %s", pr.Ref, oneLine(pr.Title, 200)),
	}
	shown := comments
	if len(shown) > maxShown {
		shown = shown[len(shown)-maxShown:]
		lines = append(lines, fmt.Sprintf("(%d earlier comments not shown)", count-maxShown))
	}
	var keys []string
	var newest time.Time
	for _, comment := range comments {
		keys = append(keys, commentKey(pr.Ref, comment))
		if at := parseTime(comment.CreatedAt); at.After(newest) {
			newest = at
		}
	}
	for _, comment := range shown {
		author := strconv.Quote(oneLine(comment.Author, 80))
		var heading string
		switch {
		case comment.Kind == "review":
			heading = fmt.Sprintf("Review by %s (%s):", author, comment.State)
		case comment.File != "":
			heading = fmt.Sprintf("Comment by %s on %s:", author, oneLine(comment.File, 160))
		default:
			heading = fmt.Sprintf("Comment by %s:", author)
		}
		lines = append(lines, heading)
		if body := clip(comment.Body, maxBody); body != "" {
			lines = append(lines, body)
		}
	}
	return event{at: newest, pr: pr, text: lines, keys: keys}
}

func checksFailed(pr github.PullRequest, checks []github.Check, key string) event {
	var names []string
	var newest time.Time
	for _, check := range checks {
		if at := parseTime(check.CompletedAt); at.After(newest) {
			newest = at
		}
		if len(names) < maxFailed {
			names = append(names, strconv.Quote(oneLine(check.Name, 80)))
		}
	}
	if len(checks) > maxFailed {
		names = append(names, fmt.Sprintf("and %d more", len(checks)-maxFailed))
	}
	lines := []string{
		fmt.Sprintf("❌ %s · checks failed", pr.Ref),
		fmt.Sprintf("Checks failed on your pull request %s: %s", pr.Ref, oneLine(pr.Title, 200)),
		"Failed: " + strings.Join(names, ", "),
	}
	return event{at: newest, pr: pr, text: lines, keys: []string{key}}
}

func (e event) incoming() engine.Incoming {
	lines := slices.Clone(e.text)
	var extra []string
	if e.pr.HeadBranch != "" {
		extra = append(extra, fmt.Sprintf("Branch: %s → %s", oneLine(e.pr.HeadBranch, 120), oneLine(e.pr.BaseBranch, 120)))
	}
	if e.pr.WebURL != "" {
		extra = append(extra, "Pull request: "+e.pr.WebURL)
	}
	// The facts go right after the two first lines, before the comments.
	lines = append(lines[:2], append(extra, lines[2:]...)...)
	source := engine.Source{
		Origin: engine.OriginPullRequest, ChannelID: ChannelID, ChannelName: ptr("GitHub"), SenderID: "github", SenderName: "GitHub",
		TS: fmt.Sprintf("%d.000000", e.at.Unix()),
	}
	if e.pr.WebURL != "" {
		link := e.pr.WebURL
		source.Permalink = &link
	}
	return engine.Incoming{Source: source, Text: strings.Join(lines, "\n")}
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

func ptr[T any](v T) *T { return &v }
