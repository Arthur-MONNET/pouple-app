package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

// threadReply is a message of someone else in the thread of the message "1.1" of channel C1.
func threadReply(text, ts string) Incoming {
	return message(text, func(in *Incoming) {
		in.Source.Origin = "thread-reply"
		in.Source.SenderID, in.Source.SenderName = "U7", "Gaël"
		in.Source.TS = ts
		in.Source.Permalink = ptr("https://x.slack.com/archives/C1/p2?thread_ts=1.1&cid=C1")
	})
}

func journalOf(r Request) string {
	var texts []string
	for _, entry := range r.Entries {
		texts = append(texts, entry.Text)
	}
	return strings.Join(texts, "|")
}

func TestAMessageInTheThreadOfAnOpenConversationIsReadQuietlyAndChangesNothing(t *testing.T) {
	h := newHarness(t,
		reply(map[string]any{"summary": "**Ready to merge**"}),
		reply(map[string]any{"outcome": NoChange, "summary": ""}))
	h.receive(message("is !93 ready?"))
	before := h.svc.Snapshot().Requests[0]
	notified := len(h.notifications())

	h.receive(threadReply("thanks!", "1.5"))

	requests := h.svc.Snapshot().Requests
	if len(requests) != 1 {
		t.Fatalf("a message in the thread is not a new request: %d requests", len(requests))
	}
	got := requests[0]
	// The assistant read it in the conversation it already had.
	if len(h.codex.turns) != 2 || h.codex.turns[1].ResumeThread != "thread-1" || !strings.Contains(h.codex.turns[1].Prompt, "thanks!") || !strings.Contains(h.codex.turns[1].Prompt, "no_change") {
		t.Fatalf("turns %d", len(h.codex.turns))
	}
	// Quietly: the answer is as it was, nobody was told, the bubble did not come back.
	if got.Status != StatusReady || got.Summary == nil || *got.Summary != "**Ready to merge**" || len(h.notifications()) != notified {
		t.Fatalf("status %s summary %v notified %v", got.Status, got.Summary, h.notifications())
	}
	if got.Closed != before.Closed || got.Engaged != before.Engaged {
		t.Errorf("the bubble state must not change: %+v -> %+v", before, got)
	}
	journal := journalOf(got)
	if !strings.Contains(journal, "New message in the thread from Gaël: thanks!") || !strings.Contains(journal, "Checked: the new messages change nothing") {
		t.Errorf("the conversation says what happened: %s", journal)
	}
	// And the same message is never read twice.
	h.receive(threadReply("thanks!", "1.5"))
	if len(h.codex.turns) != 2 {
		t.Errorf("read twice: %d turns", len(h.codex.turns))
	}
}

func TestAMessageThatChangesWhatWasProposedIsTheOnlyOneThatReachesTheUser(t *testing.T) {
	h := newHarness(t,
		reply(map[string]any{"summary": "**Ready to merge**"}),
		reply(map[string]any{"summary": "**Not ready: a conflict was reported**", "reply": "Let me check the conflict"}))
	h.receive(message("is !93 ready?"))
	notified := len(h.notifications())

	h.receive(threadReply("wait, there is a conflict on main", "1.6"))

	got := h.svc.Snapshot().Requests[0]
	if got.Summary == nil || !strings.Contains(*got.Summary, "Not ready") || len(h.notifications()) != notified+1 {
		t.Fatalf("summary %v notified %v", got.Summary, h.notifications())
	}
}

func TestSeveralMessagesAtOnceAreReadInOneTurn(t *testing.T) {
	h := newHarness(t,
		reply(map[string]any{"summary": "**Ready**"}),
		reply(map[string]any{"outcome": NoChange, "summary": ""}))
	h.receive(message("is !93 ready?"))
	h.svc.deps.SettleDelay = 50 * time.Millisecond
	h.svc.Poll(context.Background())
	h.inbox.deliver(threadReply("one", "1.5"), threadReply("two", "1.6"))
	h.svc.Poll(context.Background())
	time.Sleep(200 * time.Millisecond)
	h.svc.Idle()
	if len(h.codex.turns) != 2 || !strings.Contains(h.codex.turns[1].Prompt, "one") || !strings.Contains(h.codex.turns[1].Prompt, "two") {
		t.Fatalf("turns %d", len(h.codex.turns))
	}
}

func TestAMessageInAThreadWhoseConversationIsOverOrElsewhereOpensANewRequest(t *testing.T) {
	// A closed (ignored) conversation: "a new message in the same thread opens a new request".
	h := newHarness(t, reply(map[string]any{"outcome": NotARequest, "summary": "nothing"}), reply(map[string]any{"summary": "**New**"}))
	h.receive(message("thanks"))
	_ = h.svc.Snapshot()
	requests := h.receive(threadReply("can you check !93?", "1.5"))
	if len(requests) != 2 {
		t.Fatalf("an ignored conversation is not followed: %d requests", len(requests))
	}

	// Another thread, or a note to oneself, is never joined.
	h = newHarness(t, reply(map[string]any{"summary": "**Ready**"}), reply(map[string]any{"summary": "**Other**"}))
	h.receive(message("is !93 ready?"))
	other := threadReply("hello", "9.9")
	other.Source.Permalink = ptr("https://x.slack.com/archives/C1/p2?thread_ts=9.0&cid=C1")
	if requests := h.receive(other); len(requests) != 2 {
		t.Fatalf("another thread: %d requests", len(requests))
	}
}

func TestAnOutcomeThatChangesNothingOutsideAThreadReadIsAnAnswer(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": NoChange, "summary": "**Fine**"}))
	h.receive(message("is !93 ready?"))
	got := h.svc.Snapshot().Requests[0]
	if got.Status != StatusReady || got.Summary == nil {
		t.Fatalf("%+v", got)
	}
}
