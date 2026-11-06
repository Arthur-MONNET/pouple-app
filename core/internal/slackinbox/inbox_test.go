package slackinbox

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

const me = "U1"

var nowSeconds = testNow.Unix()

// fresh is a cursor taken just before the messages of the tests.
func fresh() *Cursor { return &Cursor{Since: nowSeconds - 100, Seen: []string{}} }

// floorOf is the oldest timestamp a poll from fresh() looks at.
func floorOf(c *Cursor) string { return strconv.FormatInt(c.Since-600, 10) }

func tsAgo(seconds int64) string { return strconv.FormatInt(nowSeconds-seconds, 10) }

// hitSpec describes a message found by Slack search.
type hitSpec struct {
	ts, text, user, channelID, channelName string
	dm                                     bool
	peer                                   string
	thread                                 string
	files                                  []any
}

func hit(mods ...func(*hitSpec)) hitSpec {
	h := hitSpec{ts: tsAgo(30), text: "can you look at <@U2>?", user: "U9", channelID: "C1", channelName: "front"}
	for _, mod := range mods {
		mod(&h)
	}
	return h
}

func (h hitSpec) match() object {
	channel := object{"id": h.channelID, "is_im": h.dm}
	if h.dm {
		channel["user"] = h.peer
	} else {
		channel["name"] = h.channelName
	}
	permalink := "https://x.slack.com/archives/" + h.channelID + "/p1"
	if h.thread != "" {
		permalink += "?thread_ts=" + h.thread + "&cid=" + h.channelID
	}
	match := object{"ts": h.ts, "text": h.text, "user": h.user, "channel": channel, "permalink": permalink}
	if h.files != nil {
		match["files"] = h.files
	}
	return match
}

func matches(hits []hitSpec) object {
	list := []any{}
	for _, h := range hits {
		list = append(list, h.match())
	}
	return ok(object{"messages": object{"matches": list}})
}

func dmHit(mods ...func(*hitSpec)) hitSpec {
	return hit(append([]func(*hitSpec){func(h *hitSpec) {
		h.channelID, h.dm, h.peer, h.text = "D1", true, "U9", "hello"
	}}, mods...)...)
}

type message struct {
	ts, user, text string
	files          []any
}

func msg(secondsAgo int64, user, text string) message {
	return message{ts: tsAgo(secondsAgo), user: user, text: text}
}

// history serves messages the way Slack does: newest first.
func history(messages []message) object {
	list := []any{}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		entry := object{"ts": m.ts, "text": m.text}
		if m.user != "" {
			entry["user"] = m.user
		}
		if m.files != nil {
			entry["files"] = m.files
		}
		list = append(list, entry)
	}
	return ok(object{"messages": list})
}

// world is what the fake workspace holds.
type world struct {
	mentions, directs, keywordHits, myThreads []hitSpec
	replies                                   []message
	groups                                    []object
	groupMessages, channelMessages            []message
	// messageFiles answers the one-message lookup, by timestamp.
	messageFiles map[string][]any
}

type harness struct {
	fake  *fakeSlack
	inbox *Inbox
	world *world
}

// newHarness starts a fake Slack holding w. Names are known for U9 (Morgan) and U2 (Bob).
func newHarness(t *testing.T, w world, opts Options) *harness {
	t.Helper()
	fake := newFakeSlack(t)
	fake.respond("auth.test", ok(object{"user_id": me, "url": "https://x.slack.com/"}))
	fake.on("search.messages", func(c call) any {
		query := c.form.Get("query")
		switch {
		case strings.HasPrefix(query, "is:dm"):
			return matches(w.directs)
		case strings.HasPrefix(query, "from:"):
			return matches(w.myThreads)
		case strings.HasPrefix(query, `"`):
			return matches(w.keywordHits)
		}
		return matches(w.mentions)
	})
	fake.on("conversations.history", func(c call) any {
		switch {
		case c.form.Get("latest") != "" && c.form.Get("oldest") != "":
			files := w.messageFiles[c.form.Get("latest")]
			return history([]message{{ts: c.form.Get("latest"), text: "x", files: files}})
		case c.form.Get("latest") != "":
			return history([]message{{ts: "1", user: "U9", text: "earlier"}})
		case strings.HasPrefix(c.form.Get("channel"), "G"):
			return history(w.groupMessages)
		}
		return history(w.channelMessages)
	})
	fake.on("conversations.replies", func(call) any {
		list := []any{object{"ts": "1730000000.000100", "text": "parent", "user": me}}
		for _, m := range w.replies {
			list = append(list, object{"ts": m.ts, "text": m.text, "user": m.user})
		}
		return ok(object{"messages": list})
	})
	fake.on("users.conversations", func(call) any { return ok(object{"channels": append([]any{}, toAny(w.groups)...)}) })
	fake.on("users.info", func(c call) any {
		names := map[string]string{"U9": "Morgan", "U2": "Bob"}
		return ok(object{"user": object{"profile": object{"display_name": names[c.form.Get("user")]}}})
	})
	if opts.Now == nil {
		opts.Now = func() time.Time { return testNow }
	}
	return &harness{fake: fake, inbox: NewInbox(fake.client(), opts), world: &w}
}

func toAny(items []object) []any {
	list := make([]any, len(items))
	for i, item := range items {
		list[i] = item
	}
	return list
}

func (h *harness) poll(t *testing.T, cursor *Cursor) Poll {
	t.Helper()
	result, err := h.inbox.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	return result
}

func origins(incoming []Incoming) []Origin {
	list := []Origin{}
	for _, request := range incoming {
		list = append(list, request.Source.Origin)
	}
	return list
}

func sortedOrigins(incoming []Incoming) []Origin {
	list := origins(incoming)
	slices.Sort(list)
	return list
}

func TestFirstPollOnlyRecordsWhatExists(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
	first := h.poll(t, nil)
	if len(first.Incoming) != 0 || first.Incoming == nil {
		t.Fatalf("incoming = %#v, want an empty list", first.Incoming)
	}
	if want := []string{"C1:" + tsAgo(30)}; !reflect.DeepEqual(first.Cursor.Seen, want) {
		t.Fatalf("seen = %v, want %v", first.Cursor.Seen, want)
	}
	if first.Cursor.Since != nowSeconds {
		t.Fatalf("since = %d, want %d", first.Cursor.Since, nowSeconds)
	}
	if again := h.poll(t, &first.Cursor); len(again.Incoming) != 0 {
		t.Fatalf("the recorded message came back: %+v", again.Incoming)
	}
}

func TestNewMentionsAndDirectMessagesBecomeRequestsOldestFirstOnce(t *testing.T) {
	dm := dmHit(func(h *hitSpec) { h.ts = tsAgo(60) })
	h := newHarness(t, world{mentions: []hitSpec{hit()}, directs: []hitSpec{dm}}, Options{})

	result := h.poll(t, fresh())

	if want := []Origin{OriginDirectMessage, OriginMention}; !reflect.DeepEqual(origins(result.Incoming), want) {
		t.Fatalf("origins = %v, want %v", origins(result.Incoming), want)
	}
	mention := result.Incoming[1]
	if mention.Source.SenderName != "Morgan" || mention.Source.SenderID != "U9" || mention.Source.ChannelName == nil || *mention.Source.ChannelName != "front" {
		t.Fatalf("source = %+v", mention.Source)
	}
	if mention.Text != "can you look at @Bob?" || mention.Context != "Morgan: earlier" {
		t.Fatalf("text %q context %q", mention.Text, mention.Context)
	}
	if mention.Source.Permalink == nil || *mention.Source.Permalink != "https://x.slack.com/archives/C1/p1" {
		t.Fatalf("permalink = %v", mention.Source.Permalink)
	}
	if result.Incoming[0].Source.ChannelName != nil {
		t.Fatalf("a direct message has no channel name, got %q", *result.Incoming[0].Source.ChannelName)
	}
	if again := h.poll(t, &result.Cursor); len(again.Incoming) != 0 {
		t.Fatalf("handled twice: %+v", again.Incoming)
	}
}

func TestSearchesAskForTheDayBeforeTheLookback(t *testing.T) {
	h := newHarness(t, world{}, Options{Keywords: []string{"amber"}})
	h.poll(t, fresh())
	var queries []string
	for _, c := range h.fake.callsTo("search.messages") {
		queries = append(queries, c.form.Get("query"))
	}
	slices.Sort(queries)
	want := []string{`"amber" after:2026-11-04`, "<@U1> after:2026-11-04", "from:<@U1> is:thread after:2026-10-29", "is:dm after:2026-11-04"}
	slices.Sort(want)
	if !reflect.DeepEqual(queries, want) {
		t.Fatalf("queries = %q\nwant %q", queries, want)
	}
}

func TestDayBoundariesFollowTheClockLocation(t *testing.T) {
	zone := time.FixedZone("tokyo", 9*3600)
	// 20:00 UTC on Nov 5 is already Nov 6 in Tokyo, so the lookback day moves with it.
	late := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC).In(zone)
	h := newHarness(t, world{}, Options{Now: func() time.Time { return late }})
	h.poll(t, nil)
	if got := h.fake.callsTo("search.messages")[0].form.Get("query"); !strings.HasSuffix(got, "after:2026-11-05") {
		t.Fatalf("query = %q", got)
	}
}

func TestOwnMessagesIgnoredChannelsAndEmptyTextAreSkipped(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{
		hit(func(h *hitSpec) { h.user = me }),
		hit(func(h *hitSpec) { h.ts, h.channelID = tsAgo(20), "CHECKIN" }),
		hit(func(h *hitSpec) { h.ts, h.text = tsAgo(10), " " }),
	}}, Options{IgnoreChannels: []string{"CHECKIN"}})
	if result := h.poll(t, fresh()); len(result.Incoming) != 0 {
		t.Fatalf("incoming = %+v", result.Incoming)
	}
}

func TestNotesToSelfAreRequestsButMyMessagesElsewhereAreNot(t *testing.T) {
	note := hit(func(h *hitSpec) {
		h.user, h.channelID, h.dm, h.peer, h.text = me, "D0", true, me, "check the amber build"
	})
	toSomeone := hit(func(h *hitSpec) {
		h.ts, h.user, h.channelID, h.dm, h.peer = tsAgo(20), me, "D2", true, "U2"
	})
	inChannel := hit(func(h *hitSpec) { h.ts, h.user = tsAgo(10), me })
	h := newHarness(t, world{directs: []hitSpec{note, toSomeone, inChannel}}, Options{})

	result := h.poll(t, fresh())

	if len(result.Incoming) != 1 || result.Incoming[0].Source.Origin != OriginNoteToSelf || result.Incoming[0].Source.ChannelID != "D0" {
		t.Fatalf("incoming = %+v", result.Incoming)
	}
}

func TestADirectMessageFromSomeoneElseIsNotANoteToSelf(t *testing.T) {
	h := newHarness(t, world{directs: []hitSpec{dmHit()}}, Options{})
	result := h.poll(t, fresh())
	if got := origins(result.Incoming); !reflect.DeepEqual(got, []Origin{OriginDirectMessage}) {
		t.Fatalf("origins = %v", got)
	}
}

func TestADirectMessageFoundByTheMentionSearchStaysADirectMessage(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{dmHit()}}, Options{})
	if got := origins(h.poll(t, fresh()).Incoming); !reflect.DeepEqual(got, []Origin{OriginDirectMessage}) {
		t.Fatalf("origins = %v", got)
	}
}

func TestTheRequestSurvivesAnUnreadableConversationHistory(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
	h.fake.on("conversations.history", func(call) any { return slackError("missing_scope", "channels:history") })
	result := h.poll(t, fresh())
	if len(result.Incoming) != 1 || result.Incoming[0].Context != "" {
		t.Fatalf("incoming = %+v", result.Incoming)
	}
	if result.Incoming[0].Files == nil || len(result.Incoming[0].Files) != 0 {
		t.Fatalf("files = %#v, want an empty list", result.Incoming[0].Files)
	}
}

func TestMessagesOlderThanTheLookbackAreIgnored(t *testing.T) {
	c := fresh()
	tooOld := hit(func(h *hitSpec) { h.ts = strconv.FormatInt(c.Since-601, 10) })
	edge := hit(func(h *hitSpec) { h.ts = strconv.FormatInt(c.Since-600, 10); h.channelID = "C2" })
	h := newHarness(t, world{mentions: []hitSpec{tooOld, edge}}, Options{})
	result := h.poll(t, c)
	if len(result.Incoming) != 1 || result.Incoming[0].Source.ChannelID != "C2" {
		t.Fatalf("incoming = %+v", result.Incoming)
	}
	if result.Cursor.Since != c.Since {
		t.Fatalf("since = %d, an old message must not move the cursor", result.Cursor.Since)
	}
}

func TestTheCursorMovesToTheNewestMessage(t *testing.T) {
	c := &Cursor{Since: nowSeconds - 1000}
	newest := hit(func(h *hitSpec) { h.ts = tsAgo(5) })
	h := newHarness(t, world{mentions: []hitSpec{newest}}, Options{})
	if got := h.poll(t, c).Cursor.Since; got != nowSeconds-5 {
		t.Fatalf("since = %d, want %d", got, nowSeconds-5)
	}
}

func TestSeenKeysAreCappedKeepingTheNewest(t *testing.T) {
	var old []string
	for i := range 499 {
		old = append(old, fmt.Sprintf("C9:%d", i))
	}
	hits := []hitSpec{
		hit(func(h *hitSpec) { h.ts = tsAgo(30) }),
		hit(func(h *hitSpec) { h.ts = tsAgo(20) }),
		hit(func(h *hitSpec) { h.ts = tsAgo(10) }),
	}
	h := newHarness(t, world{mentions: hits}, Options{})
	result := h.poll(t, &Cursor{Since: nowSeconds - 100, Seen: old})
	seen := result.Cursor.Seen
	if len(seen) != 500 {
		t.Fatalf("len(seen) = %d, want 500", len(seen))
	}
	if seen[0] != "C9:2" || seen[len(seen)-1] != "C1:"+tsAgo(10) {
		t.Fatalf("the oldest keys must be forgotten first: first %q last %q", seen[0], seen[len(seen)-1])
	}
}

func TestAMessageSeenBeforeIsNotReplayed(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
	c := fresh()
	c.Seen = []string{"C1:" + tsAgo(30)}
	if result := h.poll(t, c); len(result.Incoming) != 0 {
		t.Fatalf("incoming = %+v", result.Incoming)
	}
}

func TestWhatElseSlackWouldNotify(t *testing.T) {
	t.Run("broadcasts of the channels listed by id, and only those messages", func(t *testing.T) {
		h := newHarness(t,
			world{channelMessages: []message{msg(40, "U9", "<!channel> deploy at 5"), msg(30, "U9", "lunch anyone?")}},
			Options{Channels: map[string]ChannelMode{"C0WATCH12": ModeMentions, "#front": ModeMentions}},
		)
		c := fresh()
		result := h.poll(t, c)
		var watched []call
		for _, c := range h.fake.callsTo("conversations.history") {
			if c.form.Get("latest") == "" {
				watched = append(watched, c)
			}
		}
		if len(watched) != 1 || watched[0].form.Get("channel") != "C0WATCH12" || watched[0].form.Get("oldest") != floorOf(c) {
			t.Fatalf("history calls = %+v", watched)
		}
		if len(result.Incoming) != 1 || result.Incoming[0].Source.Origin != OriginBroadcast || result.Incoming[0].Text != "@channel deploy at 5" {
			t.Fatalf("incoming = %+v", result.Incoming)
		}
	})

	t.Run("replies in a thread I took part in, never my own", func(t *testing.T) {
		mine := hit(func(h *hitSpec) { h.user, h.thread, h.channelID, h.channelName = me, "1730000000.000100", "C3", "tech" })
		h := newHarness(t, world{myThreads: []hitSpec{mine}, replies: []message{msg(40, "U9", "any update?"), msg(30, me, "working on it")}}, Options{})
		c := fresh()

		result := h.poll(t, c)

		call := h.fake.callsTo("conversations.replies")[0]
		if call.form.Get("channel") != "C3" || call.form.Get("ts") != "1730000000.000100" || call.form.Get("oldest") != floorOf(c) {
			t.Fatalf("replies call = %v", call.form)
		}
		if len(result.Incoming) != 1 {
			t.Fatalf("incoming = %+v", result.Incoming)
		}
		reply := result.Incoming[0]
		if reply.Source.Origin != OriginThreadReply || reply.Source.ChannelID != "C3" || reply.Text != "any update?" {
			t.Fatalf("reply = %+v", reply)
		}
		if reply.Source.ChannelName == nil || *reply.Source.ChannelName != "tech" {
			t.Fatalf("channel name = %v", reply.Source.ChannelName)
		}
		if link := *reply.Source.Permalink; !strings.Contains(link, "thread_ts=1730000000.000100") || !strings.HasPrefix(link, "https://x.slack.com/archives/C3/p") {
			t.Fatalf("permalink = %q", link)
		}
	})

	t.Run("a top-level message of mine is the parent of its own thread", func(t *testing.T) {
		mine := hit(func(h *hitSpec) { h.user, h.ts = me, "1730000000.000100" })
		h := newHarness(t, world{myThreads: []hitSpec{mine}}, Options{})
		h.poll(t, fresh())
		if got := h.fake.callsTo("conversations.replies")[0].form.Get("ts"); got != "1730000000.000100" {
			t.Fatalf("ts = %q", got)
		}
	})

	t.Run("at most 15 threads are followed, each once", func(t *testing.T) {
		var threads []hitSpec
		for i := range 20 {
			ts := fmt.Sprintf("17300000%02d.000100", i)
			threads = append(threads, hit(func(h *hitSpec) { h.user, h.thread, h.channelID = me, ts, "C3" }))
		}
		threads = append(threads, threads[0])
		h := newHarness(t, world{myThreads: threads}, Options{})
		h.poll(t, fresh())
		if n := len(h.fake.callsTo("conversations.replies")); n != 15 {
			t.Fatalf("threads followed = %d, want 15", n)
		}
	})

	t.Run("highlight words", func(t *testing.T) {
		found := hit(func(h *hitSpec) { h.text, h.channelID = "Amber build is red", "C5" })
		h := newHarness(t, world{keywordHits: []hitSpec{found}}, Options{Keywords: []string{"amber"}})
		if got := origins(h.poll(t, fresh()).Incoming); !reflect.DeepEqual(got, []Origin{OriginKeyword}) {
			t.Fatalf("origins = %v", got)
		}
	})

	t.Run("a search result that does not contain the word is dropped", func(t *testing.T) {
		found := hit(func(h *hitSpec) { h.text, h.channelID = "unrelated", "C5" })
		h := newHarness(t, world{keywordHits: []hitSpec{found}}, Options{Keywords: []string{"amber"}})
		if result := h.poll(t, fresh()); len(result.Incoming) != 0 {
			t.Fatalf("incoming = %+v", result.Incoming)
		}
	})

	t.Run("quotes in a keyword cannot break the query, blank keywords are not searched", func(t *testing.T) {
		h := newHarness(t, world{}, Options{Keywords: []string{`am"ber`, "  ", ""}})
		h.poll(t, fresh())
		var keywordQueries []string
		for _, c := range h.fake.callsTo("search.messages") {
			if q := c.form.Get("query"); strings.HasPrefix(q, `"`) {
				keywordQueries = append(keywordQueries, q)
			}
		}
		if want := []string{`"amber" after:2026-11-04`}; !reflect.DeepEqual(keywordQueries, want) {
			t.Fatalf("keyword queries = %q", keywordQueries)
		}
	})

	t.Run("group chats", func(t *testing.T) {
		h := newHarness(t, world{groups: []object{{"id": "G1", "name": "mpdm-a--b"}}, groupMessages: []message{msg(20, "U9", "lunch?"), msg(10, "", "bot noise")}}, Options{})
		result := h.poll(t, fresh())
		if len(result.Incoming) != 1 || result.Incoming[0].Source.Origin != OriginGroupMessage || result.Incoming[0].Source.ChannelID != "G1" {
			t.Fatalf("incoming = %+v", result.Incoming)
		}
	})

	t.Run("the group list is cached for ten minutes", func(t *testing.T) {
		clock := testNow
		h := newHarness(t, world{}, Options{Now: func() time.Time { return clock }})
		h.poll(t, nil)
		clock = clock.Add(9 * time.Minute)
		h.poll(t, nil)
		if n := len(h.fake.callsTo("users.conversations")); n != 1 {
			t.Fatalf("group list fetched %d times, want 1", n)
		}
		clock = clock.Add(2 * time.Minute)
		h.poll(t, nil)
		if n := len(h.fake.callsTo("users.conversations")); n != 2 {
			t.Fatalf("group list fetched %d times, want 2", n)
		}
	})

	t.Run("every message of the channels set to all, naming broadcasts as such", func(t *testing.T) {
		h := newHarness(t,
			world{channelMessages: []message{msg(20, "U9", "anyone around?"), msg(15, "", "bot"), msg(10, "U9", "<!here> now")}},
			Options{Channels: map[string]ChannelMode{"C0ALL1234": ModeAll}},
		)
		result := h.poll(t, fresh())
		var got [][2]string
		for _, request := range result.Incoming {
			got = append(got, [2]string{string(request.Source.Origin), request.Text})
		}
		want := [][2]string{{"channel-message", "anyone around?"}, {"broadcast", "@here now"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("incoming = %v, want %v", got, want)
		}
	})

	t.Run("a message is listed once, under the first reason found", func(t *testing.T) {
		both := hit(func(h *hitSpec) { h.text = "Amber is red <@U1>" })
		h := newHarness(t, world{mentions: []hitSpec{both}, keywordHits: []hitSpec{both}}, Options{Keywords: []string{"amber"}})
		if got := origins(h.poll(t, fresh()).Incoming); !reflect.DeepEqual(got, []Origin{OriginMention}) {
			t.Fatalf("origins = %v", got)
		}
	})

	t.Run("at most 25 watched channels are read", func(t *testing.T) {
		channels := map[string]ChannelMode{}
		for i := range 30 {
			channels[fmt.Sprintf("C%08d", i)] = ModeAll
		}
		h := newHarness(t, world{}, Options{Channels: channels})
		h.poll(t, fresh())
		var read []string
		for _, c := range h.fake.callsTo("conversations.history") {
			read = append(read, c.form.Get("channel"))
		}
		if len(read) != 25 || !slices.IsSorted(read) || read[0] != "C00000000" {
			t.Fatalf("channels read = %v", read)
		}
	})
}

func TestOptionalSourcesFailIntoNoticesWhileTheOthersGoOn(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(*harness)
		opts   Options
		want   string
	}{
		{"group chats", func(h *harness) { h.fake.respond("users.conversations", slackError("missing_scope", "mpim:read")) }, Options{}, `Group chats: Slack token lacks the "mpim:read" scope`},
		{"threads", func(h *harness) { h.fake.respond("conversations.replies", slackError("channel_not_found")) }, Options{}, "Threads: Slack channel not found"},
		{"watched channels", func(h *harness) {
			h.fake.on("conversations.history", func(c call) any {
				if c.form.Get("latest") != "" || strings.HasPrefix(c.form.Get("channel"), "G") {
					return history(nil)
				}
				return slackError("missing_scope", "channels:history")
			})
		}, Options{Channels: map[string]ChannelMode{"C0WATCH12": ModeAll}}, `Watched channels: Slack token lacks the "channels:history" scope`},
		{"keywords", func(h *harness) {
			h.fake.on("search.messages", func(c call) any {
				if strings.HasPrefix(c.form.Get("query"), `"`) {
					return slackError("ratelimited")
				}
				return matches(h.world.mentions)
			})
		}, Options{Keywords: []string{"amber"}}, "Keywords: Slack rate limit reached"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, world{
				mentions:    []hitSpec{hit()},
				myThreads:   []hitSpec{hit(func(h *hitSpec) { h.user, h.thread = me, "1730000000.000100" })},
				groups:      []object{{"id": "G1", "name": "x"}},
				keywordHits: nil,
			}, tt.opts)
			tt.break_(h)
			result := h.poll(t, fresh())
			if len(result.Notices) != 1 || !strings.HasPrefix(result.Notices[0], tt.want) {
				t.Fatalf("notices = %q, want %q", result.Notices, tt.want)
			}
			if got := origins(result.Incoming); !reflect.DeepEqual(got, []Origin{OriginMention}) {
				t.Fatalf("the mention must still arrive, got %v", got)
			}
		})
	}
}

func TestFailingToReadMentionsOrDirectMessagesFailsThePoll(t *testing.T) {
	for _, tt := range []struct{ name, prefix string }{{"mentions", "<@"}, {"direct messages", "is:dm"}} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, world{}, Options{})
			h.fake.on("search.messages", func(c call) any {
				if strings.HasPrefix(c.form.Get("query"), tt.prefix) {
					return slackError("missing_scope", "search:read")
				}
				return matches(nil)
			})
			_, err := h.inbox.Poll(context.Background(), fresh())
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != "missing_scope" || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAFailingIdentityFailsThePoll(t *testing.T) {
	h := newHarness(t, world{}, Options{})
	h.fake.respond("auth.test", slackError("invalid_auth"))
	if _, err := h.inbox.Poll(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("err = %v", err)
	}
}

func TestACancelledContextFailsThePollInsteadOfBecomingANotice(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	h.fake.on("users.conversations", func(call) any {
		cancel()
		return slackError("ratelimited")
	})
	if _, err := h.inbox.Poll(ctx, fresh()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestChannelModes(t *testing.T) {
	mention := hit(func(h *hitSpec) { h.ts = tsAgo(20) })
	reply := hit(func(h *hitSpec) { h.user, h.thread = me, "1730000000.000100" })
	tests := []struct {
		mode ChannelMode
		want []Origin
	}{
		{ModeMentions, []Origin{OriginMention, OriginThreadReply}},
		{ModeDirect, []Origin{OriginMention}},
		{ModeMuted, []Origin{}},
	}
	for _, tt := range tests {
		for _, key := range []string{"C1", "#front"} {
			t.Run(fmt.Sprintf("%s by %s", tt.mode, key), func(t *testing.T) {
				h := newHarness(t, world{mentions: []hitSpec{mention}, myThreads: []hitSpec{reply}, replies: []message{msg(40, "U9", "any update?")}},
					Options{Channels: map[string]ChannelMode{key: tt.mode}})
				want := append([]Origin{}, tt.want...)
				slices.Sort(want)
				if got := sortedOrigins(h.poll(t, fresh()).Incoming); !reflect.DeepEqual(got, want) {
					t.Fatalf("origins = %v, want %v", got, want)
				}
			})
		}
	}

	t.Run("an unlisted channel behaves like mentions", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{mention}}, Options{})
		if got := origins(h.poll(t, fresh()).Incoming); !reflect.DeepEqual(got, []Origin{OriginMention}) {
			t.Fatalf("origins = %v", got)
		}
	})

	t.Run("an id wins over a name", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{mention}}, Options{Channels: map[string]ChannelMode{"C1": ModeMuted, "#front": ModeAll}})
		if got := h.poll(t, fresh()).Incoming; len(got) != 0 {
			t.Fatalf("incoming = %+v", got)
		}
	})

	t.Run("direct messages are never muted", func(t *testing.T) {
		dm := dmHit(func(h *hitSpec) { h.channelID = "C1" })
		h := newHarness(t, world{directs: []hitSpec{dm}}, Options{Channels: map[string]ChannelMode{"C1": ModeMuted}})
		if got := h.poll(t, fresh()).Incoming; len(got) != 1 {
			t.Fatalf("incoming = %+v", got)
		}
	})

	t.Run("keywords need a channel that lets more than direct mentions through", func(t *testing.T) {
		found := hit(func(h *hitSpec) { h.text = "amber" })
		h := newHarness(t, world{keywordHits: []hitSpec{found}}, Options{Keywords: []string{"amber"}, Channels: map[string]ChannelMode{"C1": ModeDirect}})
		if got := h.poll(t, fresh()).Incoming; len(got) != 0 {
			t.Fatalf("incoming = %+v", got)
		}
	})
}

func TestFiles(t *testing.T) {
	file := object{"id": "F1", "name": "error.png", "mimetype": "image/png", "size": 10.0, "url_private": "https://files.slack.com/f/error.png"}
	want := []FileRef{{ID: "F1", Name: "error.png", Mime: "image/png", URL: "https://files.slack.com/f/error.png", Size: 10}}

	t.Run("carries the files a search hit lists, without asking Slack again", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{hit(func(h *hitSpec) { h.files = []any{file} })}}, Options{})
		result := h.poll(t, fresh())
		if !reflect.DeepEqual(result.Incoming[0].Files, want) {
			t.Fatalf("files = %+v", result.Incoming[0].Files)
		}
		for _, c := range h.fake.callsTo("conversations.history") {
			if c.form.Get("oldest") != "" {
				t.Fatalf("Slack was asked for the files again: %v", c.form)
			}
		}
	})

	t.Run("asks Slack for the files of a message when search did not say", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{hit()}, messageFiles: map[string][]any{tsAgo(30): {file}}}, Options{})
		if got := h.poll(t, fresh()).Incoming[0].Files; !reflect.DeepEqual(got, want) {
			t.Fatalf("files = %+v", got)
		}
	})

	t.Run("goes on without files when Slack cannot say", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
		h.fake.on("conversations.history", func(c call) any {
			if c.form.Get("oldest") != "" && c.form.Get("latest") != "" {
				return slackError("missing_scope", "channels:history")
			}
			return history(nil)
		})
		files := h.poll(t, fresh()).Incoming[0].Files
		if files == nil || len(files) != 0 {
			t.Fatalf("files = %#v, want an empty list", files)
		}
	})

	t.Run("files of messages read from a conversation come along", func(t *testing.T) {
		h := newHarness(t, world{groups: []object{{"id": "G1", "name": "x"}}, groupMessages: []message{{ts: tsAgo(20), user: "U9", text: "see this", files: []any{file}}}}, Options{})
		if got := h.poll(t, fresh()).Incoming[0].Files; !reflect.DeepEqual(got, want) {
			t.Fatalf("files = %+v", got)
		}
	})
}

func TestReadableText(t *testing.T) {
	t.Run("mentions and broadcasts become plain handles", func(t *testing.T) {
		text := "<@U2|bob> and <@W9ZZ> see <!here|here> <!channel> <!everyone>"
		h := newHarness(t, world{mentions: []hitSpec{hit(func(h *hitSpec) { h.text = text })}}, Options{})
		if got := h.poll(t, fresh()).Incoming[0].Text; got != "@Bob and @W9ZZ see @here @channel @everyone" {
			t.Fatalf("text = %q", got)
		}
	})

	t.Run("the context is written author by author, bots included", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
		h.fake.on("conversations.history", func(c call) any {
			if c.form.Get("latest") != "" {
				return history([]message{{ts: "1", user: "U9", text: "ping <@U2>"}, {ts: "2", text: "deployed"}})
			}
			return history(nil)
		})
		if got := h.poll(t, fresh()).Incoming[0].Context; got != "Morgan: ping @Bob\nbot: deployed" {
			t.Fatalf("context = %q", got)
		}
	})

	t.Run("a name Slack cannot give falls back to the id", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{hit(func(h *hitSpec) { h.text = "hi <@U5>"; h.user = "U7" })}}, Options{})
		h.fake.respond("users.info", slackError("missing_scope", "users:read"))
		request := h.poll(t, fresh()).Incoming[0]
		if request.Text != "hi @U5" || request.Source.SenderName != "U7" {
			t.Fatalf("request = %+v", request)
		}
	})

	t.Run("only twelve distinct users are looked up per request", func(t *testing.T) {
		var mentions []string
		for i := range 14 {
			mentions = append(mentions, fmt.Sprintf("<@U1%02d>", i))
		}
		h := newHarness(t, world{mentions: []hitSpec{hit(func(h *hitSpec) { h.text = strings.Join(mentions, " ") })}}, Options{})
		text := h.poll(t, fresh()).Incoming[0].Text
		if n := len(h.fake.callsTo("users.info")); n != 12 {
			t.Fatalf("users.info called %d times, want 12", n)
		}
		// The sender took the first lookup, so the last three mentions stay as ids.
		if !strings.HasSuffix(text, "@U111 @U112 @U113") {
			t.Fatalf("text = %q", text)
		}
	})

	t.Run("the text is capped at 4000 characters, whatever they are", func(t *testing.T) {
		h := newHarness(t, world{mentions: []hitSpec{hit(func(h *hitSpec) { h.text = strings.Repeat("é", 5000) })}}, Options{})
		if got := h.poll(t, fresh()).Incoming[0].Text; len([]rune(got)) != 4000 {
			t.Fatalf("length = %d runes", len([]rune(got)))
		}
	})
}

func TestIdentityIsExposedThroughTheInbox(t *testing.T) {
	h := newHarness(t, world{}, Options{})
	userID, teamURL, err := h.inbox.Identity(context.Background())
	if err != nil || userID != me || teamURL != "https://x.slack.com/" {
		t.Fatalf("identity = %q %q %v", userID, teamURL, err)
	}
}

func TestMalformedSearchEntriesDoNotBreakAPoll(t *testing.T) {
	h := newHarness(t, world{mentions: []hitSpec{hit()}}, Options{})
	h.fake.on("search.messages", func(c call) any {
		if strings.HasPrefix(c.form.Get("query"), "<@") {
			return ok(object{"messages": object{"matches": []any{"junk", object{"ts": 5}, hit().match()}}})
		}
		return matches(nil)
	})
	if got := h.poll(t, fresh()).Incoming; len(got) != 1 {
		t.Fatalf("incoming = %+v", got)
	}
}

func TestPollsAreDeterministic(t *testing.T) {
	build := func() Poll {
		h := newHarness(t, world{
			mentions: []hitSpec{hit()},
			directs:  []hitSpec{dmHit(func(h *hitSpec) { h.ts = tsAgo(60) })},
			groups:   []object{{"id": "G1", "name": "x"}}, groupMessages: []message{msg(20, "U9", "lunch?")},
			channelMessages: []message{msg(25, "U9", "<!here> hi")},
		}, Options{Channels: map[string]ChannelMode{"C7": ModeAll, "C5": ModeMentions, "C6": ModeAll}})
		return h.poll(t, fresh())
	}
	first := build()
	for range 5 {
		if again := build(); !reflect.DeepEqual(first, again) {
			t.Fatalf("polls differ:\n%+v\n%+v", first, again)
		}
	}
}

func TestPollNeverReachesSlackWithTheTokenInAnAddress(t *testing.T) {
	// fakeSlack fails the test on every request whose address or form carries the token; this drives every source.
	h := newHarness(t, world{
		mentions:        []hitSpec{hit()},
		directs:         []hitSpec{dmHit()},
		keywordHits:     []hitSpec{hit(func(h *hitSpec) { h.text, h.ts = "amber", tsAgo(10) })},
		myThreads:       []hitSpec{hit(func(h *hitSpec) { h.user, h.thread = me, "1730000000.000100" })},
		replies:         []message{msg(20, "U9", "ok")},
		groups:          []object{{"id": "G1", "name": "x"}},
		groupMessages:   []message{msg(15, "U9", "yo")},
		channelMessages: []message{msg(12, "U9", "<!here>")},
	}, Options{Keywords: []string{"amber"}, Channels: map[string]ChannelMode{"C0WATCH12": ModeAll}})
	result := h.poll(t, fresh())
	if len(result.Incoming) < 6 {
		t.Fatalf("every source should have produced a request, got %v", origins(result.Incoming))
	}
	if len(h.fake.callsTo("auth.test")) == 0 {
		t.Fatal("no call reached the fake Slack")
	}
}
