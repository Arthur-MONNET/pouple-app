package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/memory"
)

// withMemory gives the harness a memory folder holding notes, and returns the folder.
func withMemory(t *testing.T, h *harness, notes map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "SecondBrain")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range notes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.svc.deps.Memory = memory.New(func() string { return dir }, RunGit)
	h.svc.deps.MemoryOn = func() bool { return true }
	return dir
}

func remember(facts ...map[string]any) map[string]any {
	list := []any{}
	for _, fact := range facts {
		list = append(list, fact)
	}
	return map[string]any{"outcome": Answered, "summary": "**Preprod 2 is free**", "memories": list}
}

func fact(note, title, text string) map[string]any {
	return map[string]any{"note": note, "title": title, "fact": text, "replaces": ""}
}

func noteToSelf(text string) Incoming {
	return message(text, func(in *Incoming) {
		in.Source.Origin, in.Source.ChannelID, in.Source.ChannelName, in.Source.SenderName = "note-to-self", "D1", nil, "Arthur"
	})
}

const preprodNote = "# Preprod\n\n- Preprod 2 is reset every Monday. (Slack #qa, 2026-11-02)\n- Lina takes preprod 1 for the payment tests. (Slack, Lina, 2026-11-03) [private]\n"

func TestTheFirstPromptListsTheMemoryAndHidesPrivateNotesFromOthers(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": NotARequest}), reply(map[string]any{"outcome": NotARequest}))
	withMemory(t, h, map[string]string{"preprod.md": preprodNote, "health.md": "# Health\n\n- Doctor on Fridays. [private]\n"})
	h.receive(message("Is preprod 2 free?"))
	shared := h.codex.turns[0].Prompt
	if !strings.Contains(shared, "## Memory") || !strings.Contains(shared, "- preprod.md: Preprod") || strings.Contains(shared, "health.md") {
		t.Fatalf("a request of someone else must see only the notes with shared facts:\n%s", shared)
	}
	if !strings.Contains(shared, "Facts marked [private] are kept from you here") {
		t.Fatal("the AI is not told that private facts are kept from it")
	}
	h.receive(noteToSelf("Remind me what we know about health"))
	own := h.codex.turns[1].Prompt
	if !strings.Contains(own, "- health.md: Health") || !strings.Contains(own, "must never be repeated in a reply meant for someone else") {
		t.Fatalf("a note to oneself lists every note:\n%s", own)
	}
}

func TestNoMemoryNoSectionAndNoOffer(t *testing.T) {
	h := newHarness(t, reply(remember(fact("", "Preprod", "Preprod 2 is reset every Monday."))))
	h.receive(message("Is preprod 2 free?"))
	if strings.Contains(h.codex.turns[0].Prompt, "## Memory") {
		t.Fatal("the memory is described while there is none")
	}
	if request := only(t, h); request.Memories != nil {
		t.Fatalf("an offer without memory: %+v", request.Memories)
	}
}

func TestSuggestedFactsWaitForTheTickAndAreTickedOnlyForTheUsersOwnText(t *testing.T) {
	h := newHarness(t,
		reply(remember(fact("preprod.md", "", "Preprod 2 is free on Thursdays."), fact("ghost.md", "Ghost", "A note that does not exist."))),
		reply(remember(fact("", "Payments", "Refunds go through Stripe."))),
	)
	dir := withMemory(t, h, map[string]string{"preprod.md": preprodNote})
	h.receive(message("Is preprod 2 free?"))
	offer := only(t, h).Memories
	if offer == nil || offer.Status != MemoryProposed || offer.ID == "" || len(offer.Items) != 2 {
		t.Fatalf("offer %+v", offer)
	}
	if offer.Items[0].Note != "preprod.md" || offer.Items[0].NoteTitle != "Preprod" || offer.Items[0].Ticked {
		t.Fatalf("a fact that rests on someone else's message is not ticked in advance: %+v", offer.Items[0])
	}
	if offer.Items[1].Note != "" || offer.Items[1].NoteTitle != "Ghost" {
		t.Fatalf("a path that is no note makes a new note: %+v", offer.Items[1])
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "preprod.md")); string(raw) != preprodNote {
		t.Fatal("nothing may be written before the user ticks")
	}
	h.receive(noteToSelf("Note: refunds go through Stripe"))
	for _, request := range h.svc.Snapshot().Requests {
		if request.Source.Origin == "note-to-self" && (request.Memories == nil || !request.Memories.Items[0].Ticked) {
			t.Fatalf("a note to oneself is ticked in advance: %+v", request.Memories)
		}
	}
}

func TestConfirmingWritesTheTickedFactsPrivateUnlessShared(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	h := newHarness(t, reply(remember(
		fact("preprod.md", "", "Preprod 2 is free on Thursdays."),
		fact("", "Payments", "Refunds go through Stripe."),
		fact("", "Skipped", "Not ticked."),
	)))
	dir := withMemory(t, h, map[string]string{"preprod.md": preprodNote})
	h.receive(message("Is preprod 2 free?", func(in *Incoming) { in.Source.Permalink = ptr("https://team.slack.com/archives/C1/p11") }))
	request := only(t, h)
	offer := request.Memories
	if err := h.svc.DecideMemories(request.ID, true, offer.ID, []MemoryPick{{ID: "m1", Shared: true}, {ID: "m2"}}); err != nil {
		t.Fatal(err)
	}
	preprod, _ := os.ReadFile(filepath.Join(dir, "preprod.md"))
	if !strings.Contains(string(preprod), "- Preprod 2 is free on Thursdays. ([Slack #front, 2026-11-05](https://team.slack.com/archives/C1/p11))\n") {
		t.Fatalf("a shared fact:\n%s", preprod)
	}
	payments, _ := os.ReadFile(filepath.Join(dir, "payments.md"))
	if !strings.HasPrefix(string(payments), "# Payments\n\n") || !strings.Contains(string(payments), "Refunds go through Stripe.") || !strings.HasSuffix(string(payments), memory.PrivateMark+"\n") {
		t.Fatalf("a fact is private unless shared:\n%s", payments)
	}
	if _, err := os.Stat(filepath.Join(dir, "skipped.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a fact that was not ticked was written")
	}
	decided := only(t, h)
	if decided.Memories.Status != MemoryAccepted || strings.Join(decided.Memories.Chosen, ",") != "m1,m2" {
		t.Fatalf("offer %+v", decided.Memories)
	}
	if last := decided.Entries[len(decided.Entries)-1]; last.Role != "user" || !strings.Contains(last.Text, "Refunds go through Stripe.") {
		t.Fatalf("the journal does not say what was kept: %+v", last)
	}
	if err := h.svc.DecideMemories(request.ID, true, offer.ID, []MemoryPick{{ID: "m3"}}); !errors.Is(err, ErrState) {
		t.Fatalf("a suggestion is decided once: %v", err)
	}
}

func TestADecisionThatCannotBeMadeWritesNothing(t *testing.T) {
	h := newHarness(t, reply(remember(fact("", "Payments", "Refunds go through Stripe."))))
	dir := withMemory(t, h, nil)
	h.receive(message("How are refunds made?"))
	request := only(t, h)
	id := request.Memories.ID
	for name, c := range map[string]struct {
		offer string
		picks []MemoryPick
		want  error
	}{
		"nothing ticked":     {id, nil, ErrInput},
		"unknown fact":       {id, []MemoryPick{{ID: "m9"}}, ErrInput},
		"fact twice":         {id, []MemoryPick{{ID: "m1"}, {ID: "m1"}}, ErrInput},
		"another suggestion": {"stale", []MemoryPick{{ID: "m1"}}, ErrState},
		"unnamed suggestion": {"", []MemoryPick{{ID: "m1"}}, ErrInput},
	} {
		if err := h.svc.DecideMemories(request.ID, true, c.offer, c.picks); !errors.Is(err, c.want) {
			t.Errorf("%s: err %v, want %v", name, err, c.want)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused decision wrote %d files", len(entries))
	}
	if err := h.svc.DecideMemories(request.ID, false, id, nil); err != nil || only(t, h).Memories.Status != MemoryDeclined {
		t.Fatalf("declining: %v", err)
	}
}

func TestNotesLookupsReadTheMemoryWithoutPrivateFactsForOthers(t *testing.T) {
	h := newHarness(t,
		lookupReply(Lookup{Service: "notes", Action: "search", Target: "preprod Lina"}, Lookup{Service: "notes", Action: "read", Target: "preprod.md"}),
		reply(map[string]any{"outcome": Answered}),
		lookupReply(Lookup{Service: "notes", Action: "read", Target: "preprod.md"}),
		reply(map[string]any{"outcome": Answered}),
	)
	withMemory(t, h, map[string]string{"preprod.md": preprodNote})
	h.receive(message("Is preprod 2 free?"))
	shared := h.codex.turns[1].Prompt
	if !strings.Contains(shared, "Preprod 2 is reset every Monday.") || strings.Contains(shared, "Lina takes preprod 1") {
		t.Fatalf("a request of someone else reads no private fact:\n%s", shared)
	}
	h.receive(noteToSelf("Who has preprod 1?"))
	own := h.codex.turns[3].Prompt
	if !strings.Contains(own, "Lina takes preprod 1") {
		t.Fatalf("a note to oneself reads the whole note:\n%s", own)
	}
	for _, request := range h.svc.Snapshot().Requests {
		stored, _ := h.svc.deps.Store.Get(request.ID)
		if request.Source.Origin == "note-to-self" && (stored.ServicesUsed || stored.ForeignSinceGrant) {
			t.Fatal("the memory holds the user's own text: reading it must not count as text of others")
		}
	}
}

func TestParseOutcomeKeepsOnlyFactsTheMemoryCanHold(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"outcome": Answered, "summary": "s", "memories": []any{
		fact("preprod.md", "", "  One\nfact  on two lines. "),
		fact("../escape.md", "", "Climbs out."),
		fact("", "", "No note and no title."),
		fact("", "Long", strings.Repeat("x", memory.MaxFact+1)),
		fact("", "A", "1"), fact("", "B", "2"), fact("", "C", "3"), fact("", "D", "4"),
	}})
	outcome, err := ParseOutcome(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Memories) != maxMemories || outcome.Memories[0].Fact != "One fact on two lines." || outcome.Memories[0].Note != "preprod.md" || outcome.Memories[1].Title != "A" {
		t.Fatalf("%+v", outcome.Memories)
	}
}

func TestAnAsanaCommentOrAReviewSeesNoPrivateFact(t *testing.T) {
	for _, origin := range []string{OriginAsana, OriginMergeRequest} {
		h := newHarness(t, reply(map[string]any{"outcome": NotARequest}))
		withMemory(t, h, map[string]string{"health.md": "# Health\n\n- Doctor on Fridays. [private]\n"})
		h.receive(message("Can you share what you know?", func(in *Incoming) { in.Source.Origin = origin }))
		if prompt := h.codex.turns[0].Prompt; strings.Contains(prompt, "health.md") || !strings.Contains(prompt, "kept from you here") {
			t.Errorf("%s: its reply is drafted for someone else, so no private fact:\n%s", origin, prompt)
		}
	}
}

func TestTidyingUpAsksWhichNotesAreOneSubjectAndMergesThem(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	h := topicsHarness(t, answerWith(map[string]any{"merges": []any{map[string]any{"from": "stripe.md", "into": "payments.md"}}}))
	h.svc.deps.MemoryTidySchemaFile = "/tidy.json"
	dir := withMemory(t, h, map[string]string{
		"payments.md": "# Payments\n\n- Refunds go through Stripe. (Slack #qa, 2026-11-01)\n",
		"stripe.md":   "# Stripe\n\n- Stripe keys rotate every year. (Slack #ops, 2026-11-02) [private]\n",
	})
	if err := h.svc.TidyMemory(); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	turn := h.codex.turns[0]
	if turn.SchemaFile != "/tidy.json" || turn.Dir != h.svc.deps.Topics.Dir || turn.Sandbox != ReadOnly || !strings.Contains(turn.Prompt, "- stripe.md: Stripe") {
		t.Fatalf("the tidy-up turn reads nothing but the notes it is given: %+v", turn)
	}
	if _, err := os.Stat(filepath.Join(dir, "stripe.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the notes were not merged")
	}
	view, err := h.svc.MemoryView(context.Background())
	if err != nil || !view.Enabled || !view.Exists || len(view.Notes) != 1 || view.Notes[0].Facts != 2 || view.TidiedAt == "" {
		t.Fatalf("view %+v, err %v", view, err)
	}
}

func TestTheMemoryPageListsOldFactsAndKeepsOrDropsThem(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	h := newHarness(t)
	withMemory(t, h, map[string]string{"deploys.md": "# Deploys\n\n- Deploys go through Jenkins. (Slack #ops, 2026-01-10)\n"})
	view, _ := h.svc.MemoryView(context.Background())
	if len(view.Review) != 1 || view.Review[0].NoteTitle != "Deploys" || view.Review[0].Date != "2026-01-10" {
		t.Fatalf("review %+v", view.Review)
	}
	line := view.Review[0].Line
	if err := h.svc.ReviewMemory("deploys.md", line+" edited", true); !errors.Is(err, ErrState) {
		t.Fatalf("a line that changed: %v", err)
	}
	if err := h.svc.ReviewMemory("deploys.md", line, false); err != nil {
		t.Fatal(err)
	}
	if view, _ := h.svc.MemoryView(context.Background()); len(view.Review) != 0 || view.Notes[0].Facts != 0 {
		t.Fatalf("after dropping: %+v", view)
	}
	h.svc.deps.MemoryOn = func() bool { return false }
	if err := h.svc.ReviewMemory("deploys.md", line, true); !errors.Is(err, ErrState) {
		t.Fatalf("the memory is off: %v", err)
	}
	if err := h.svc.TidyMemory(); !errors.Is(err, ErrState) {
		t.Fatalf("the memory is off: %v", err)
	}
}
