package slackinbox

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Origin is why a message reached the inbox.
type Origin string

const (
	OriginMention        Origin = "mention"
	OriginDirectMessage  Origin = "direct-message"
	OriginNoteToSelf     Origin = "note-to-self"
	OriginGroupMessage   Origin = "group-message"
	OriginBroadcast      Origin = "broadcast"
	OriginKeyword        Origin = "keyword"
	OriginThreadReply    Origin = "thread-reply"
	OriginChannelMessage Origin = "channel-message"
)

// Source says where a request came from.
type Source struct {
	Origin    Origin `json:"origin"`
	ChannelID string `json:"channelId"`
	// ChannelName is nil for direct and group messages.
	ChannelName *string `json:"channelName"`
	SenderID    string  `json:"senderId"`
	SenderName  string  `json:"senderName"`
	// TS is the Slack message timestamp, e.g. "1730812345.000100".
	TS        string  `json:"ts"`
	Permalink *string `json:"permalink"`
}

// Incoming is a Slack message that should become a request.
type Incoming struct {
	Source Source
	// Text is the message as received, mentions made readable. Untrusted content.
	Text string
	// Context is the few messages posted just before it (best effort, may be empty). Untrusted content.
	Context string
	// Files came with the message and are still to be downloaded.
	Files []FileRef
}

// ChannelMode is what reaches the assistant from a channel: everything, mentions and broadcasts (the default), only
// direct mentions, or nothing.
type ChannelMode string

const (
	ModeAll      ChannelMode = "all"
	ModeMentions ChannelMode = "mentions"
	ModeDirect   ChannelMode = "direct"
	ModeMuted    ChannelMode = "muted"
)

// Options configure an Inbox.
type Options struct {
	// IgnoreChannels are channel ids whose messages are never requests (e.g. the daily check-in).
	IgnoreChannels []string
	// Channels is the per-channel override keyed by channel id or "#name"; absent channels use ModeMentions.
	Channels map[string]ChannelMode
	// Keywords are words that make a message a request, wherever it is posted.
	Keywords []string
	// Now is the clock; nil means time.Now. Its location decides the day boundaries of Slack's day-granular search.
	Now func() time.Time
}

// Cursor is what the inbox remembers between two polls.
type Cursor struct {
	// Since is the epoch seconds of the newest message seen.
	Since int64 `json:"since"`
	// Seen holds the "channel:ts" keys of messages already turned into requests (or deliberately skipped).
	Seen []string `json:"seen"`
}

// Poll is the result of one look at Slack.
type Poll struct {
	Incoming []Incoming
	Cursor   Cursor
	// Notices name the optional sources that could not be read this time (missing scope...); the others are unaffected.
	Notices []i18n.Message
}

const (
	lookbackSeconds   = 10 * 60
	threadWindowDays  = 7
	maxSeen           = 500
	maxThreads        = 15
	maxChannelsPolled = 25
	groupListTTL      = 10 * time.Minute
	contextMessages   = 6
	maxNameLookups    = 12
	maxTextRunes      = 4000
	daySeconds        = 24 * 3600
)

var (
	userMentionRe = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)
	broadcastRe   = regexp.MustCompile(`<!(here|channel|everyone)(?:\|[^>]*)?>`)
)

// candidate is a message found by one of the sources, with the reason Slack would have notified.
type candidate struct {
	origin Origin
	hit    searchHit
}

func keyOf(hit searchHit) string { return hit.channelID + ":" + hit.ts }

// tsSeconds reads a Slack timestamp as epoch seconds.
func tsSeconds(ts string) (float64, bool) {
	value, err := strconv.ParseFloat(ts, 64)
	return value, err == nil
}

// Inbox finds what Slack itself would notify the user about: mentions, direct and group messages, replies in
// threads they took part in, their highlight words, and @here/@channel and every message in the channels they list.
// Slack does not expose notification settings, so per-channel overrides come from the options.
type Inbox struct {
	client   *Client
	ignored  map[string]bool
	channels map[string]ChannelMode
	keywords []string
	now      func() time.Time

	mu     sync.Mutex
	groups *groupList
}

type groupList struct {
	at   time.Time
	list []conversation
}

// NewInbox builds an inbox reading through client.
func NewInbox(client *Client, opts Options) *Inbox {
	ignored := make(map[string]bool, len(opts.IgnoreChannels))
	for _, id := range opts.IgnoreChannels {
		ignored[id] = true
	}
	channels := make(map[string]ChannelMode, len(opts.Channels))
	for key, mode := range opts.Channels {
		channels[key] = mode
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Inbox{client: client, ignored: ignored, channels: channels, keywords: append([]string(nil), opts.Keywords...), now: now}
}

// Identity tells who the token belongs to and the workspace URL.
func (i *Inbox) Identity(ctx context.Context) (userID, teamURL string, err error) {
	return i.client.Identity(ctx)
}

// slackDay formats a day the way Slack's after: operator reads it.
func (i *Inbox) slackDay(epochSeconds int64) string {
	return time.Unix(epochSeconds, 0).In(i.now().Location()).Format("2006-01-02")
}

// Poll returns the requests that arrived since cursor, oldest first, and the cursor to use next. A nil cursor is the
// first poll: it only records what is already there, so starting never replays old messages. Failing to read
// mentions or direct messages fails the poll; the other sources only add a notice.
func (i *Inbox) Poll(ctx context.Context, cursor *Cursor) (Poll, error) {
	userID, teamURL, err := i.client.Identity(ctx)
	if err != nil {
		return Poll{}, i18n.Wrap(err, "slack inbox: identity: %s", err)
	}
	nowSeconds := i.now().Unix()
	floor := nowSeconds - lookbackSeconds
	if cursor != nil {
		floor = cursor.Since - lookbackSeconds
	}
	// Slack's after: is exclusive and day-granular: ask for the day before, filter by timestamp afterwards.
	after := i.slackDay(floor - daySeconds)

	mentions, directs, err := i.coreSearches(ctx, userID, after)
	if err != nil {
		return Poll{}, err
	}
	var candidates []candidate
	for _, hit := range mentions {
		origin := OriginMention
		if hit.isDM {
			origin = OriginDirectMessage
		}
		candidates = append(candidates, candidate{origin, hit})
	}
	for _, hit := range directs {
		candidates = append(candidates, candidate{OriginDirectMessage, hit})
	}

	notices := []i18n.Message{}
	optional := func(label string, source func() ([]candidate, error)) error {
		found, err := source()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			notices = append(notices, i18n.NewMessage("%s: %s", i18n.NewMessage(label), err))
			return nil
		}
		candidates = append(candidates, found...)
		return nil
	}
	sources := []struct {
		label  string
		source func() ([]candidate, error)
	}{
		{"Threads", func() ([]candidate, error) { return i.threadReplies(ctx, userID, teamURL, floor) }},
		{"Keywords", func() ([]candidate, error) { return i.keywordHits(ctx, after) }},
		{"Group chats", func() ([]candidate, error) { return i.groupMessages(ctx, floor, teamURL) }},
		{"Watched channels", func() ([]candidate, error) { return i.watchedChannels(ctx, floor, teamURL) }},
	}
	for _, s := range sources {
		if err := optional(s.label, s.source); err != nil {
			return Poll{}, i18n.Wrap(err, "slack inbox: %s", err)
		}
	}

	// The first source to find a message names the reason; the same message is never handled twice.
	var fresh []candidate
	listed := map[string]bool{}
	for _, c := range candidates {
		seconds, ok := tsSeconds(c.hit.ts)
		if !ok || seconds < float64(floor) || listed[keyOf(c.hit)] {
			continue
		}
		listed[keyOf(c.hit)] = true
		fresh = append(fresh, c)
	}

	seen := newSeenSet(cursor)
	newest := nowSeconds
	if cursor != nil {
		newest = cursor.Since
	}
	var unseen []candidate
	for _, c := range fresh {
		if seconds, _ := tsSeconds(c.hit.ts); int64(seconds) > newest {
			newest = int64(seconds)
		}
		if !seen.has(keyOf(c.hit)) {
			unseen = append(unseen, c)
		}
	}
	for _, c := range unseen {
		seen.add(keyOf(c.hit))
	}
	next := Cursor{Since: newest, Seen: seen.last(maxSeen)}
	if cursor == nil {
		return Poll{Incoming: []Incoming{}, Cursor: next, Notices: notices}, nil
	}

	var wanted []candidate
	for _, c := range unseen {
		if i.isRequest(c, userID) {
			wanted = append(wanted, c)
		}
	}
	sort.SliceStable(wanted, func(a, b int) bool {
		left, _ := tsSeconds(wanted[a].hit.ts)
		right, _ := tsSeconds(wanted[b].hit.ts)
		return left < right
	})
	incoming := make([]Incoming, 0, len(wanted))
	for _, c := range wanted {
		request, err := i.toIncoming(ctx, c, userID)
		if err != nil {
			return Poll{}, err
		}
		incoming = append(incoming, request)
	}
	return Poll{Incoming: incoming, Cursor: next, Notices: notices}, nil
}

// coreSearches reads mentions and direct messages, the heart of the inbox, side by side.
func (i *Inbox) coreSearches(ctx context.Context, userID, after string) (mentions, directs []searchHit, err error) {
	var mentionsErr, directsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		mentions, mentionsErr = i.client.search(ctx, fmt.Sprintf("<@%s> after:%s", userID, after), 50)
	}()
	go func() {
		defer wg.Done()
		directs, directsErr = i.client.search(ctx, "is:dm after:"+after, 50)
	}()
	wg.Wait()
	if mentionsErr != nil {
		return nil, nil, i18n.Wrap(mentionsErr, "slack inbox: mentions: %s", mentionsErr)
	}
	if directsErr != nil {
		return nil, nil, i18n.Wrap(directsErr, "slack inbox: direct messages: %s", directsErr)
	}
	return mentions, directs, nil
}

// seenSet is the insertion-ordered set of message keys, so the oldest ones are the first to be forgotten.
type seenSet struct {
	order []string
	index map[string]bool
}

func newSeenSet(cursor *Cursor) *seenSet {
	set := &seenSet{index: map[string]bool{}}
	if cursor != nil {
		for _, key := range cursor.Seen {
			set.add(key)
		}
	}
	return set
}

func (s *seenSet) has(key string) bool { return s.index[key] }

func (s *seenSet) add(key string) {
	if !s.index[key] {
		s.index[key] = true
		s.order = append(s.order, key)
	}
}

func (s *seenSet) last(limit int) []string {
	if len(s.order) <= limit {
		return append([]string{}, s.order...)
	}
	return append([]string{}, s.order[len(s.order)-limit:]...)
}

// ---- sources ----

type threadRef struct {
	channelID, channelName string
	isDM                   bool
	threadTS               string
}

// threadReplies finds replies written by others in the threads the user took part in recently.
func (i *Inbox) threadReplies(ctx context.Context, userID, teamURL string, floor int64) ([]candidate, error) {
	windowStart := i.slackDay(floor - threadWindowDays*daySeconds)
	mine, err := i.client.search(ctx, fmt.Sprintf("from:<@%s> is:thread after:%s", userID, windowStart), 100)
	if err != nil {
		return nil, err
	}
	var order []string
	threads := map[string]threadRef{}
	for _, hit := range mine {
		threadTS := hit.threadTS
		if threadTS == "" {
			threadTS = hit.ts
		}
		key := hit.channelID + ":" + threadTS
		if _, known := threads[key]; !known {
			order = append(order, key)
		}
		threads[key] = threadRef{channelID: hit.channelID, channelName: hit.channelName, isDM: hit.isDM, threadTS: threadTS}
	}
	if len(order) > maxThreads {
		order = order[:maxThreads]
	}
	var found []candidate
	for _, key := range order {
		thread := threads[key]
		replies, err := i.client.replies(ctx, thread.channelID, thread.threadTS, floor)
		if err != nil {
			return nil, err
		}
		for _, reply := range replies {
			if reply.userID == "" || reply.userID == userID {
				continue
			}
			link := "?thread_ts=" + thread.threadTS + "&cid=" + thread.channelID
			found = append(found, candidate{OriginThreadReply, fromMessage(reply, thread.channelID, thread.channelName, thread.isDM, teamURL, link)})
		}
	}
	return found, nil
}

func (i *Inbox) keywordHits(ctx context.Context, after string) ([]candidate, error) {
	var found []candidate
	for _, keyword := range i.keywords {
		if strings.TrimSpace(keyword) == "" {
			continue
		}
		hits, err := i.client.search(ctx, `"`+strings.ReplaceAll(keyword, `"`, "")+`" after:`+after, 50)
		if err != nil {
			return nil, err
		}
		for _, hit := range hits {
			if strings.Contains(strings.ToLower(hit.text), strings.ToLower(keyword)) {
				found = append(found, candidate{OriginKeyword, hit})
			}
		}
	}
	return found, nil
}

// groupMessages reads group conversations. It needs mpim:read; the list is cached because it rarely changes.
func (i *Inbox) groupMessages(ctx context.Context, floor int64, teamURL string) ([]candidate, error) {
	groups, err := i.cachedGroups(ctx)
	if err != nil {
		return nil, err
	}
	if len(groups) > maxChannelsPolled {
		groups = groups[:maxChannelsPolled]
	}
	var found []candidate
	for _, group := range groups {
		messages, err := i.client.messagesSince(ctx, group.id, floor, 50)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if message.userID == "" {
				continue
			}
			found = append(found, candidate{OriginGroupMessage, fromMessage(message, group.id, "", false, teamURL, "")})
		}
	}
	return found, nil
}

func (i *Inbox) cachedGroups(ctx context.Context) ([]conversation, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.groups == nil || i.now().Sub(i.groups.at) > groupListTTL {
		list, err := i.client.groupConversations(ctx)
		if err != nil {
			return nil, err
		}
		i.groups = &groupList{at: i.now(), list: list}
	}
	return i.groups.list, nil
}

// watchedChannels reads the history of the channels listed by id in the options. Slack search cannot find
// @here / @channel, so this is the only way to see them: in a "mentions" channel only the broadcasts are kept, in an
// "all" channel everything.
func (i *Inbox) watchedChannels(ctx context.Context, floor int64, teamURL string) ([]candidate, error) {
	var ids []string
	for key, mode := range i.channels {
		if !strings.HasPrefix(key, "#") && (mode == ModeAll || mode == ModeMentions) {
			ids = append(ids, key)
		}
	}
	// Map order is random: a stable order keeps which channels are read, when there are too many, reproducible.
	sort.Strings(ids)
	if len(ids) > maxChannelsPolled {
		ids = ids[:maxChannelsPolled]
	}
	var found []candidate
	for _, id := range ids {
		messages, err := i.client.messagesSince(ctx, id, floor, 50)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if message.userID == "" {
				continue
			}
			var origin Origin
			switch {
			case broadcastRe.MatchString(message.text):
				origin = OriginBroadcast
			case i.channels[id] == ModeAll:
				origin = OriginChannelMessage
			default:
				continue
			}
			found = append(found, candidate{origin, fromMessage(message, id, "", false, teamURL, "")})
		}
	}
	return found, nil
}

// ---- filtering and conversion ----

func (i *Inbox) modeOf(hit searchHit) ChannelMode {
	if mode, ok := i.channels[hit.channelID]; ok {
		return mode
	}
	if hit.channelName != "" {
		if mode, ok := i.channels["#"+hit.channelName]; ok {
			return mode
		}
	}
	return ModeMentions
}

// isRequest applies the same rules Slack applies before it notifies.
func (i *Inbox) isRequest(c candidate, userID string) bool {
	hit := c.hit
	if i.ignored[hit.channelID] || strings.TrimSpace(hit.text) == "" {
		return false
	}
	// Your own messages are never requests, except the ones you write to yourself.
	noteToSelf := hit.isDM && hit.directPeerID == userID && hit.userID == userID
	if hit.userID == userID && !noteToSelf {
		return false
	}
	if hit.isDM || c.origin == OriginGroupMessage {
		return true
	}
	mode := i.modeOf(hit)
	switch c.origin {
	case OriginMention:
		return mode != ModeMuted
	case OriginBroadcast, OriginKeyword, OriginThreadReply:
		return mode == ModeMentions || mode == ModeAll
	default:
		return mode == ModeAll
	}
}

func fromMessage(message conversationMessage, channelID, channelName string, isDM bool, teamURL, query string) searchHit {
	link := teamURL + "archives/" + channelID + "/p" + strings.Replace(message.ts, ".", "", 1) + query
	return searchHit{
		ts:          message.ts,
		text:        message.text,
		userID:      message.userID,
		channelID:   channelID,
		channelName: channelName,
		isDM:        isDM,
		files:       message.files,
		permalink:   &link,
	}
}

// nameResolver turns user ids into display names for one request, looking up at most maxNameLookups distinct users
// and falling back to the id when Slack cannot say.
type nameResolver struct {
	client  *Client
	names   map[string]string
	lookups int
}

func (r *nameResolver) resolve(ctx context.Context, id string) string {
	if name, ok := r.names[id]; ok {
		return name
	}
	name := id
	if r.lookups < maxNameLookups {
		r.lookups++
		if resolved, err := r.client.displayName(ctx, id); err == nil {
			name = resolved
		}
	}
	r.names[id] = name
	return name
}

// readable replaces user mentions by @name and broadcasts by @here, and caps the length.
func (r *nameResolver) readable(ctx context.Context, text string) string {
	text = userMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		return "@" + r.resolve(ctx, userMentionRe.FindStringSubmatch(match)[1])
	})
	text = broadcastRe.ReplaceAllString(text, "@$1")
	if runes := []rune(text); len(runes) > maxTextRunes {
		text = string(runes[:maxTextRunes])
	}
	return text
}

func (i *Inbox) toIncoming(ctx context.Context, c candidate, userID string) (Incoming, error) {
	hit := c.hit
	names := &nameResolver{client: i.client, names: map[string]string{}}
	senderName := names.resolve(ctx, hit.userID)

	// Missing history scopes or an inaccessible conversation only cost context, never the request.
	before, err := i.client.messagesBefore(ctx, hit.channelID, hit.ts, contextMessages)
	if err != nil {
		if ctx.Err() != nil {
			return Incoming{}, i18n.Wrap(ctx.Err(), "slack inbox: %s", ctx.Err())
		}
		before = nil
	}
	lines := make([]string, 0, len(before))
	for _, message := range before {
		author := "bot"
		if message.userID != "" {
			author = names.resolve(ctx, message.userID)
		}
		lines = append(lines, author+": "+names.readable(ctx, message.text))
	}

	// A direct message whose other participant is the user is the conversation with themself.
	origin := c.origin
	// ...and the message must be theirs: someone else's DM never gets the trust of a note to oneself.
	if hit.isDM && hit.directPeerID == userID && hit.userID == userID {
		origin = OriginNoteToSelf
	}
	// Search does not always list the files of a message: ask for them, once, when it said nothing.
	files := hit.files
	if files == nil {
		files, err = i.client.messageFiles(ctx, hit.channelID, hit.ts)
		if err != nil {
			if ctx.Err() != nil {
				return Incoming{}, i18n.Wrap(ctx.Err(), "slack inbox: %s", ctx.Err())
			}
			files = []FileRef{}
		}
	}
	source := Source{
		Origin:     origin,
		ChannelID:  hit.channelID,
		SenderID:   hit.userID,
		SenderName: senderName,
		TS:         hit.ts,
		Permalink:  hit.permalink,
	}
	if hit.channelName != "" {
		name := hit.channelName
		source.ChannelName = &name
	}
	return Incoming{Source: source, Text: names.readable(ctx, hit.text), Context: strings.Join(lines, "\n"), Files: files}, nil
}
