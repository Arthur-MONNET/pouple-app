package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

type step = func(context.Context, Turn) (TurnResult, error)

// answerWith is a turn whose final message is the JSON of v.
func answerWith(v any) step {
	data, _ := json.Marshal(v)
	return func(context.Context, Turn) (TurnResult, error) { return TurnResult{FinalMessage: string(data)}, nil }
}

// sortedTo is the answer of a sorting turn.
func sortedTo(topicID, title string) step {
	return answerWith(map[string]any{"topicId": topicID, "title": title})
}

// topicsHarness is a harness whose conversations are sorted into topics: the first turn of each is the sorting.
func topicsHarness(t *testing.T, steps ...step) *harness {
	t.Helper()
	h := newHarness(t, steps...)
	h.svc.deps.Topics = &TopicConfig{SortSchemaFile: "/topic.json", OrganizeSchemaFile: "/organize.json", Dir: t.TempDir()}
	return h
}

func (h *harness) firstTopic() Topic {
	topics := h.svc.deps.Store.Topics()
	if len(topics) == 0 {
		return Topic{}
	}
	return topics[0]
}

// sameChannel is a message of another time, in the same channel as message().
func sameChannel(ts string) func(*Incoming) {
	return func(in *Incoming) { in.Source.TS = ts }
}

// later is a message of another time and place than message().
func later(ts string) func(*Incoming) {
	return func(in *Incoming) { in.Source.TS, in.Source.ChannelID = ts, "C2" }
}

func TestANewConversationStartsATopicNamedByTheAI(t *testing.T) {
	h := topicsHarness(t, sortedTo("", "Login bug"), reply(nil))
	h.receive(message("The login page crashes on Safari, can you look?"))
	request := only(t, h)
	topics := h.svc.Snapshot().Topics
	if len(topics) != 1 || topics[0].Title != "Login bug" || request.TopicID != topics[0].ID {
		t.Fatalf("topics %+v, request in %q", topics, request.TopicID)
	}
	if h.codex.turns[0].SchemaFile != "/topic.json" || h.codex.turns[1].SchemaFile != "/schema.json" {
		t.Fatalf("the sorting turn and the analysis answer with their own schema: %q, %q", h.codex.turns[0].SchemaFile, h.codex.turns[1].SchemaFile)
	}
	if h.codex.turns[0].Sandbox != ReadOnly || h.codex.turns[0].Dir != h.svc.deps.Topics.Dir || h.codex.turns[0].Dir == h.svc.workDir() {
		t.Fatalf("sorting reads nothing and writes nothing: %v in %q", h.codex.turns[0].Sandbox, h.codex.turns[0].Dir)
	}
}

func TestAConversationOfTheSameSubjectJoinsItsTopicAndReadsTheOthers(t *testing.T) {
	var h *harness
	joinFirst := func(ctx context.Context, turn Turn) (TurnResult, error) {
		return sortedTo(h.firstTopic().ID, "")(ctx, turn)
	}
	h = topicsHarness(t, sortedTo("", "Login bug"), reply(nil), joinFirst, reply(nil))
	h.receive(message("The login page crashes on Safari, can you look?"))
	h.receive(message("Any news on the Safari crash?", sameChannel("2.2")))

	requests := h.svc.Snapshot().Requests
	if len(requests) != 2 || requests[0].TopicID == "" || requests[0].TopicID != requests[1].TopicID {
		t.Fatalf("the two conversations are one subject: %+v", requests)
	}
	if len(h.svc.Snapshot().Topics) != 1 {
		t.Fatalf("topics %+v", h.svc.Snapshot().Topics)
	}
	sorting := h.codex.turns[2].Prompt
	if !strings.Contains(sorting, "Login bug") || !strings.Contains(sorting, "crashes on Safari") {
		t.Fatalf("the sorting turn shows the existing topics and what they hold:\n%s", sorting)
	}
	analysis := h.codex.turns[3].Prompt
	if !strings.Contains(analysis, relatedHeading) || !strings.Contains(analysis, "crashes on Safari") {
		t.Fatalf("the analysis reads the other conversation of the subject:\n%s", analysis)
	}
	second, _ := h.svc.deps.Store.Get(requests[1].ID)
	if !second.ForeignSinceGrant || !second.ServicesUsed {
		t.Fatalf("text written by someone else shaped the analysis, so nothing may run on its own after it: %+v", second)
	}
}

func TestOnlyTheUsersOwnTextLeavesTheAnalysisTrusted(t *testing.T) {
	var h *harness
	joinFirst := func(ctx context.Context, turn Turn) (TurnResult, error) {
		return sortedTo(h.firstTopic().ID, "")(ctx, turn)
	}
	own := func(in *Incoming) { in.Source.Origin = "note-to-self" }
	h = topicsHarness(t, sortedTo("", "Release notes"), reply(nil), joinFirst, reply(nil))
	h.receive(message("Write the release notes for 0.2", own))
	h.receive(message("Add the beta channel to the release notes", own, later("2.2")))
	for _, request := range h.svc.deps.Store.List() {
		if request.ForeignSinceGrant || request.ServicesUsed {
			t.Fatalf("the user's own notes are not text of strangers: %+v", request)
		}
	}
}

func TestATopicIsRenamedWhenItsSubjectEvolves(t *testing.T) {
	var h *harness
	renamed := func(ctx context.Context, turn Turn) (TurnResult, error) {
		return sortedTo(h.firstTopic().ID, "  “Safari and Chrome login crash”\n")(ctx, turn)
	}
	h = topicsHarness(t, sortedTo("", "Login bug"), reply(nil), renamed, reply(nil))
	h.receive(message("The login page crashes on Safari, can you look?"))
	h.receive(message("It crashes on Chrome too, can you check?", later("2.2")))
	if topic := h.firstTopic(); topic.Title != "Safari and Chrome login crash" || len(h.svc.deps.Store.Topics()) != 1 {
		t.Fatalf("the title follows the subject, cleaned: %+v", h.svc.deps.Store.Topics())
	}
}

func TestAnUnknownTopicIsANewOneAndAMissingTitleComesFromTheMessage(t *testing.T) {
	h := topicsHarness(t, sortedTo("made-up", "Invoices"), reply(nil), sortedTo("made-up", ""), reply(nil))
	h.receive(message("Where are the March invoices?"))
	h.receive(message("Can you send the contract to Jo?", later("2.2")))
	titles := []string{}
	for _, topic := range h.svc.Snapshot().Topics {
		titles = append(titles, topic.Title)
	}
	if len(titles) != 2 || titles[0] != "Invoices" || titles[1] != "Can you send the contract to Jo?" {
		t.Fatalf("topics %q", titles)
	}
}

func TestAConversationTheAICouldNotSortIsStillAnalyzedAndWaitsForATopic(t *testing.T) {
	var mu sync.Mutex
	var problems []error
	h := topicsHarness(t, failWith(errors.New("boom")), reply(nil))
	h.svc.deps.OnError = func(err error) {
		mu.Lock()
		problems = append(problems, err)
		mu.Unlock()
	}
	h.receive(message("Where are the March invoices?"))
	request := only(t, h)
	if request.Status != StatusReady || request.TopicID != "" || len(h.svc.Snapshot().Topics) != 0 {
		t.Fatalf("analysis goes on with no topic: %+v", request)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "boom") {
		t.Fatalf("the failure is reported once, with its cause: %v", problems)
	}
}

func TestWithoutTheTopicSchemasConversationsAreNotSorted(t *testing.T) {
	h := newHarness(t, reply(nil))
	h.receive(message("Where are the March invoices?"))
	if h.codex.count() != 1 || len(h.svc.Snapshot().Topics) != 0 {
		t.Fatalf("turns %d, topics %+v", h.codex.count(), h.svc.Snapshot().Topics)
	}
}

func TestRemovingTheLastConversationOfATopicRemovesTheTopic(t *testing.T) {
	var h *harness
	joinFirst := func(ctx context.Context, turn Turn) (TurnResult, error) {
		return sortedTo(h.firstTopic().ID, "")(ctx, turn)
	}
	h = topicsHarness(t, sortedTo("", "Login bug"), reply(nil), joinFirst, reply(nil))
	h.receive(message("The login page crashes on Safari, can you look?"))
	h.receive(message("Any news on the Safari crash?", later("2.2")))
	requests := h.svc.Snapshot().Requests
	if err := h.svc.Remove(requests[0].ID); err != nil || len(h.svc.Snapshot().Topics) != 1 {
		t.Fatalf("a topic with a conversation left stays: %v %+v", err, h.svc.Snapshot().Topics)
	}
	if err := h.svc.Remove(requests[1].ID); err != nil || len(h.svc.Snapshot().Topics) != 0 {
		t.Fatalf("the last conversation takes its topic with it: %v %+v", err, h.svc.Snapshot().Topics)
	}
}

// stored adds a finished conversation to the store, as it was before topics existed.
func (h *harness) stored(id, text, topicID string) {
	h.svc.deps.Store.Add(Stored{Request: Request{
		ID: id, Source: Source{Origin: "mention", ChannelID: "C1", SenderName: "Morgan", TS: id}, Text: text, TopicID: topicID,
		ReceivedAt: "2026-11-01T10:00:0" + id[1:] + ".000Z", UpdatedAt: "2026-11-01T10:00:00.000Z", Status: StatusDone, Questions: []Question{}, Entries: []Entry{},
	}})
}

func (h *harness) topicOf(id string) string {
	request, _ := h.svc.deps.Store.Get(id)
	return request.TopicID
}

func TestReorganizingPlacesMergesAndRenamesAndIgnoresWhatDoesNotExist(t *testing.T) {
	h := topicsHarness(t)
	now := "2026-11-01T09:00:00.000Z"
	h.svc.deps.Store.AddTopic(Topic{ID: "t1", Title: "Login bug", CreatedAt: now, UpdatedAt: now})
	h.svc.deps.Store.AddTopic(Topic{ID: "t2", Title: "Safari crash", CreatedAt: "2026-10-30T09:00:00.000Z", UpdatedAt: now})
	h.stored("r1", "Export the March invoices", "")
	h.stored("r2", "Invoices export is slow", "")
	h.stored("r3", "Login is broken again", "")
	h.stored("r4", "The Safari crash is back", "t2")
	h.stored("r5", "Another one", "")
	h.codex.steps = []step{answerWith(map[string]any{
		"places": []any{
			map[string]any{"requestId": "r1", "topicId": "new-1", "title": "Invoice export"},
			map[string]any{"requestId": "r2", "topicId": "new-1", "title": ""},
			map[string]any{"requestId": "r3", "topicId": "t1", "title": ""},
			map[string]any{"requestId": "ghost", "topicId": "t1", "title": ""},
			map[string]any{"requestId": "r5", "topicId": "nowhere", "title": ""},
		},
		"merges":  []any{map[string]any{"from": "t2", "into": "t1"}, map[string]any{"from": "t1", "into": "t1"}, map[string]any{"from": "ghost", "into": "t1"}},
		"renames": []any{map[string]any{"id": "t1", "title": "Login crashes"}, map[string]any{"id": "ghost", "title": "x"}},
	}),
		// The conversation the first round could not place is asked again alone; the AI still has nothing for it, so that is the end.
		answerWith(map[string]any{"places": []any{}, "merges": []any{}, "renames": []any{}}),
	}
	if err := h.svc.organize(context.Background()); err != nil {
		t.Fatal(err)
	}
	topics := h.svc.deps.Store.Topics()
	titles := map[string]string{}
	for _, topic := range topics {
		titles[topic.ID] = topic.Title
	}
	invoices := h.topicOf("r1")
	if invoices == "" || invoices != h.topicOf("r2") || titles[invoices] != "Invoice export" {
		t.Fatalf("the two invoice conversations share one new topic: %q %q %v", invoices, h.topicOf("r2"), titles)
	}
	if h.topicOf("r3") != "t1" || h.topicOf("r4") != "t1" || titles["t1"] != "Login crashes" {
		t.Fatalf("login conversations are one renamed topic: %q %q %v", h.topicOf("r3"), h.topicOf("r4"), titles)
	}
	if _, merged := titles["t2"]; merged || len(topics) != 2 {
		t.Fatalf("the merged topic is gone: %v", titles)
	}
	if h.topicOf("r5") != "" {
		t.Fatalf("a topic that does not exist and has no title places nothing: %q", h.topicOf("r5"))
	}
	if topics[0].CreatedAt != "2026-10-30T09:00:00.000Z" {
		t.Fatalf("the merged topic keeps the older start: %+v", topics[0])
	}
	if h.svc.deps.Store.Organization().At == "" {
		t.Fatal("the look was not recorded")
	}
	if h.codex.count() != 2 {
		t.Fatalf("a conversation nothing can be said of is not asked about over and over: %d turns", h.codex.count())
	}
}

func TestConversationsThatWaitForATopicAreOrganizedByThePollAndNotAskedAgainAtOnce(t *testing.T) {
	h := topicsHarness(t, answerWith(map[string]any{
		"places":  []any{map[string]any{"requestId": "r1", "topicId": "new-1", "title": "Invoice export"}},
		"merges":  []any{},
		"renames": []any{},
	}))
	h.stored("r1", "Export the March invoices", "")
	h.svc.Poll(context.Background())
	h.svc.Idle()
	if h.topicOf("r1") == "" || h.codex.count() != 1 {
		t.Fatalf("the waiting conversation got a topic in one turn: %q, %d turns", h.topicOf("r1"), h.codex.count())
	}
	h.svc.Poll(context.Background())
	h.svc.Idle()
	if h.codex.count() != 1 {
		t.Fatalf("nothing waits, nothing is asked: %d turns", h.codex.count())
	}
}

func TestAFailedOrganizationWaitsBeforeTryingAgain(t *testing.T) {
	h := topicsHarness(t, failWith(errors.New("boom")), answerWith(map[string]any{"places": []any{}, "merges": []any{}, "renames": []any{}}))
	h.stored("r1", "Export the March invoices", "")
	h.svc.Poll(context.Background())
	h.svc.Idle()
	h.svc.Poll(context.Background())
	h.svc.Idle()
	if h.codex.count() != 1 {
		t.Fatalf("a failed attempt is not repeated at every poll: %d turns", h.codex.count())
	}
	h.mu.Lock()
	h.now = h.now.Add(organizeRetry)
	h.mu.Unlock()
	h.svc.Poll(context.Background())
	h.svc.Idle()
	if h.codex.count() != 2 {
		t.Fatalf("it tries again after the pause: %d turns", h.codex.count())
	}
}

func TestParsingWhatTheAIAnsweredRefusesWhatIsNotJSON(t *testing.T) {
	if _, err := ParseTopicChoice("the topic is login"); !errors.Is(err, ErrOutcome) {
		t.Fatalf("err %v", err)
	}
	if _, err := ParseOrganizeAnswer("{"); !errors.Is(err, ErrOutcome) {
		t.Fatalf("err %v", err)
	}
	choice, err := ParseTopicChoice(`{"topicId":" t1 ","title":"  Login bug "}`)
	if err != nil || choice.TopicID != "t1" || choice.Title != "Login bug" {
		t.Fatalf("choice %+v, err %v", choice, err)
	}
}

func TestATitleIsOneCleanLine(t *testing.T) {
	cases := map[string]string{
		"  “Login\n bug”.":            "Login bug",
		"Pay\u202ebill\u200b now\x07": "Paybill now",
		strings.Repeat("a", 200):      clipRunes(strings.Repeat("a", 200), maxTopicTitleRunes),
		"\"\"":                        "",
	}
	for raw, want := range cases {
		if got := cleanTitle(raw); got != want {
			t.Errorf("%q became %q, want %q", raw, got, want)
		}
	}
}

func TestASecretIsTakenOutOfATitleBeforeItIsShortened(t *testing.T) {
	secret := "sk-live-abcdefghijklmnopqrstuvwxyz0123456789"
	h := topicsHarness(t, sortedTo("", strings.Repeat("x", 60)+" "+secret), reply(nil))
	h.svc.deps.Redact = func() Redactor { return NewRedactor(secret) }
	h.receive(message("Where are the March invoices?"))
	title := h.firstTopic().Title
	if strings.Contains(title, "sk-live") || !strings.Contains(title, "[redacted]") {
		t.Fatalf("the secret is redacted whole, before the title is cut: %q", title)
	}
}

func TestAConversationOfSomeoneElseReadsOnlyTheConversationsOfItsOwnChannel(t *testing.T) {
	var h *harness
	joinFirst := func(ctx context.Context, turn Turn) (TurnResult, error) {
		return sortedTo(h.firstTopic().ID, "")(ctx, turn)
	}
	own := func(in *Incoming) { in.Source.Origin = "note-to-self"; in.Source.ChannelID = "D-self" }
	h = topicsHarness(t, sortedTo("", "Reorganisation"), reply(nil), joinFirst, reply(nil), joinFirst, reply(nil))
	h.receive(message("Draft my answer about the reorganisation, it is private", own))
	h.receive(message("What do you think of the reorganisation?", later("2.2")))
	if strings.Contains(h.codex.turns[3].Prompt, "it is private") {
		t.Fatalf("what the user wrote to themselves must not reach a conversation whose reply goes to someone else:\n%s", h.codex.turns[3].Prompt)
	}
	h.receive(message("Anything to add on the reorganisation?", own))
	if !strings.Contains(h.codex.turns[5].Prompt, "What do you think of the reorganisation?") {
		t.Fatalf("a note to oneself reads the whole topic:\n%s", h.codex.turns[5].Prompt)
	}
}

func TestANoteShapedByAServiceOrAReminderOfAForeignConversationIsNotTheUsersOwnText(t *testing.T) {
	h := topicsHarness(t)
	h.svc.deps.Store.Add(Stored{Request: Request{ID: "n1", Source: Source{Origin: "note-to-self"}, Status: StatusDone}, ServicesUsed: true})
	h.svc.deps.Store.Add(Stored{Request: Request{ID: "n2", Source: Source{Origin: "note-to-self"}, Status: StatusDone}})
	h.svc.deps.Store.Add(Stored{Request: Request{ID: "m1", Source: Source{Origin: "mention"}, Status: StatusDone}})
	reminder := func(id, from string) Stored {
		return Stored{Request: Request{ID: id, Source: Source{Origin: OriginReminder, RelatedRequestID: from}, Status: StatusDone}}
	}
	cases := map[string]bool{"n1": false, "n2": true, "m1": false}
	for id, want := range cases {
		request, _ := h.svc.deps.Store.Get(id)
		if got := h.svc.ownText(request, 0); got != want {
			t.Errorf("%s: own %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"r-own": true, "r-foreign": false, "r-service": false, "r-lost": false} {
		from := map[string]string{"r-own": "n2", "r-foreign": "m1", "r-service": "n1", "r-lost": "gone"}[id]
		if got := h.svc.ownText(reminder(id, from), 0); got != want {
			t.Errorf("%s: own %v, want %v", id, got, want)
		}
	}
}

func TestACommentOfAServiceIsAnsweredSoItReadsOnlyItsOwnService(t *testing.T) {
	for origin, wantsDM := range map[string]bool{OriginAsana: false, OriginMergeRequest: false, OriginPipeline: true} {
		var h *harness
		joinFirst := func(ctx context.Context, turn Turn) (TurnResult, error) {
			return sortedTo(h.firstTopic().ID, "")(ctx, turn)
		}
		h = topicsHarness(t, sortedTo("", "Payments"), reply(nil), joinFirst, reply(nil))
		h.receive(message("Lina is leaving, keep it quiet", func(in *Incoming) {
			in.Source.Origin, in.Source.ChannelID = "direct-message", "D-lina"
		}))
		h.receive(message("Who owns the payment flow now?", func(in *Incoming) {
			in.Source.Origin, in.Source.ChannelID, in.Source.TS = origin, "service-"+origin, "2.2"
		}))
		if read := strings.Contains(h.codex.turns[3].Prompt, "Lina is leaving"); read != wantsDM {
			t.Errorf("%s: read the direct message = %v, want %v", origin, read, wantsDM)
		}
	}
}
