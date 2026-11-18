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
	return map[string]any{"note": note, "title": title, "fact": text, "replaces": "", "remove": false, "lasts": ""}
}

func correct(note, replaces, text string) map[string]any {
	return map[string]any{"note": note, "title": "", "fact": text, "replaces": replaces, "remove": false, "lasts": ""}
}

func forget(note, replaces string) map[string]any {
	return map[string]any{"note": note, "title": "", "fact": "", "replaces": replaces, "remove": true, "lasts": ""}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// only1 is the one request of the harness, as stored.
func stored(t *testing.T, h *harness, origin string) Stored {
	t.Helper()
	for _, request := range h.svc.deps.Store.List() {
		if request.Source.Origin == origin {
			return request
		}
	}
	t.Fatalf("no request from %s", origin)
	return Stored{}
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

func TestNoMemoryNoSectionAndNoChange(t *testing.T) {
	h := newHarness(t, reply(remember(fact("", "Preprod", "Preprod 2 is reset every Monday."))))
	h.receive(message("Is preprod 2 free?"))
	if strings.Contains(h.codex.turns[0].Prompt, "## Memory") {
		t.Fatal("the memory is described while there is none")
	}
	if request := only(t, h); len(request.MemoryChanges) != 0 {
		t.Fatalf("a change without memory: %+v", request.MemoryChanges)
	}
}

func TestAnAnswerChangesTheMemoryAtOnceAndListsWhatChanged(t *testing.T) {
	requireGit(t)
	weeks := fact("", "Payments", "Lina has preprod 1 for the payment tests.")
	weeks["lasts"] = "weeks"
	h := newHarness(t, reply(remember(
		correct("preprod.md", "Preprod 2 is reset every Monday.", "Preprod 2 is reset every Tuesday since November."),
		forget("preprod.md", "Lina takes preprod 1 for the payment tests."),
		weeks,
	)))
	dir := withMemory(t, h, map[string]string{"preprod.md": preprodNote})
	h.receive(noteToSelf("Preprod 2 resets on Tuesdays now, and Lina moved to the payments"))
	note, _ := os.ReadFile(filepath.Join(dir, "preprod.md"))
	if strings.Contains(string(note), "every Monday") || strings.Contains(string(note), "Lina takes") || !strings.Contains(string(note), "- Preprod 2 is reset every Tuesday since November. (Note to self, 2026-11-05)\n") {
		t.Fatalf("a correction keeps the sharing of the fact it replaces, and a fact taken out is gone:\n%s", note)
	}
	payments, _ := os.ReadFile(filepath.Join(dir, "payments.md"))
	if !strings.Contains(string(payments), "(Note to self, 2026-11-05) (review 2026-12-03) [private]") || strings.Contains(string(payments), memory.UnconfirmedMark) {
		t.Fatalf("a new fact is private, reviewed after its lifetime, and the user's own word is not unconfirmed:\n%s", payments)
	}
	changes := only(t, h).MemoryChanges
	if len(changes) != 1 || len(changes[0].ID) < 7 || len(changes[0].Items) != 3 {
		t.Fatalf("changes %+v", changes)
	}
	items := changes[0].Items
	if items[0].Kind != memory.Updated || items[0].Previous != "Preprod 2 is reset every Monday." || items[0].Fact != "Preprod 2 is reset every Tuesday since November." || !items[0].Shared ||
		items[1].Kind != memory.Removed || items[2].Kind != memory.Added || items[2].Shared || items[2].Review != "2026-12-03" || items[2].NoteTitle != "Payments" {
		t.Fatalf("items %+v", items)
	}
}

func TestFactsFromOthersAreUnconfirmedAndReadingThemIsReadingTheirText(t *testing.T) {
	requireGit(t)
	h := newHarness(t,
		reply(remember(fact("", "Deploys", "Deploys go through GitLab CI."))),
		lookupReply(Lookup{Service: "notes", Action: "read", Target: "deploys.md"}),
		reply(map[string]any{"outcome": Answered}),
	)
	dir := withMemory(t, h, nil)
	h.receive(message("FYI deploys go through GitLab CI now"))
	note, _ := os.ReadFile(filepath.Join(dir, "deploys.md"))
	if !strings.Contains(string(note), memory.UnconfirmedMark) || !only(t, h).MemoryChanges[0].Items[0].Unconfirmed {
		t.Fatalf("a fact from someone else's message is unconfirmed:\n%s", note)
	}
	h.receive(noteToSelf("How do we deploy?"))
	if own := stored(t, h, "note-to-self"); !own.ServicesUsed || !own.ForeignSinceGrant {
		t.Fatal("reading an unconfirmed fact is reading text of others: nothing may run on its own after it")
	}
}

func TestARequestOfOthersCannotChangeAPrivateFact(t *testing.T) {
	requireGit(t)
	h := newHarness(t, reply(remember(
		correct("preprod.md", "Lina takes preprod 1 for the payment tests.", "Nobody has preprod 1."),
		forget("preprod.md", "Lina takes preprod 1 for the payment tests."),
	)))
	dir := withMemory(t, h, map[string]string{"preprod.md": preprodNote})
	h.receive(message("Who has preprod 1?"))
	note, _ := os.ReadFile(filepath.Join(dir, "preprod.md"))
	if !strings.Contains(string(note), "Lina takes preprod 1 for the payment tests.") || !strings.Contains(string(note), "Nobody has preprod 1.") {
		t.Fatalf("a private fact the request never saw stays, and the correction is added beside it:\n%s", note)
	}
}

func TestAChangeCanBeUndoneAndAFactShared(t *testing.T) {
	requireGit(t)
	h := newHarness(t, reply(remember(fact("", "Payments", "Refunds go through Stripe."))))
	dir := withMemory(t, h, nil)
	h.receive(noteToSelf("Note: refunds go through Stripe"))
	request := only(t, h)
	change := request.MemoryChanges[0]
	if err := h.svc.ShareMemoryFact(request.ID, change.ID, "m1", true); err != nil {
		t.Fatal(err)
	}
	note, _ := os.ReadFile(filepath.Join(dir, "payments.md"))
	if strings.Contains(string(note), memory.PrivateMark) || !only(t, h).MemoryChanges[0].Items[0].Shared {
		t.Fatalf("a shared fact:\n%s", note)
	}
	if err := h.svc.ShareMemoryFact(request.ID, change.ID, "m9", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown fact: %v", err)
	}
	// The fact was shared after it was written: undoing its write meets that later change.
	if err := h.svc.UndoMemoryChange(request.ID, change.ID); !errors.Is(err, ErrState) {
		t.Fatalf("undoing a change the notes moved on from: %v", err)
	}
	if err := h.svc.ShareMemoryFact(request.ID, change.ID, "m1", false); err != nil {
		t.Fatal(err)
	}
	if out, err := RunGit(context.Background(), dir, "-c", "user.name=t", "-c", "user.email=t@t", "revert", "--no-edit", "HEAD", "HEAD~1"); err != nil {
		t.Fatalf("git revert: %v %s", err, out)
	}
	if err := h.svc.UndoMemoryChange(request.ID, change.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "payments.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("undoing the write of a new note takes the note away")
	}
	if !only(t, h).MemoryChanges[0].Undone {
		t.Fatal("the change is not marked undone")
	}
	if err := h.svc.UndoMemoryChange(request.ID, change.ID); !errors.Is(err, ErrState) {
		t.Fatalf("undoing twice: %v", err)
	}
}

func TestTheReviewComesAsAnAlertAtMostOnceAWeek(t *testing.T) {
	h := newHarness(t)
	withMemory(t, h, map[string]string{"deploys.md": "# Deploys\n\n- Deploys go through Jenkins. (Slack #ops, 2026-01-10)\n- Lina has preprod 1. (Slack #qa, 2026-10-01) (review 2026-10-29)\n"})
	h.svc.Poll(context.Background())
	alerts := h.svc.Snapshot().Alerts
	if len(alerts) != 1 || alerts[0].Title != "Memory: 2 facts to review" || !strings.Contains(alerts[0].Note, "Deploys go through Jenkins.") {
		t.Fatalf("alerts %+v", alerts)
	}
	h.svc.Poll(context.Background())
	if got := len(h.svc.Snapshot().Alerts); got != 1 {
		t.Fatalf("a second alert in the same week: %d", got)
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

func TestParseOutcomeKeepsOnlyChangesTheMemoryCanMake(t *testing.T) {
	odd := fact("", "A", "1")
	odd["lasts"] = "decades"
	raw, _ := json.Marshal(map[string]any{"outcome": Answered, "summary": "s", "memories": []any{
		fact("preprod.md", "", "  One\nfact  on two lines. "),
		fact("../escape.md", "", "Climbs out."),
		fact("", "", "No note and no title."),
		fact("", "Long", strings.Repeat("x", memory.MaxFact+1)),
		forget("preprod.md", ""),
		odd, forget("preprod.md", "Preprod 2 is reset"), fact("", "C", "3"), fact("", "D", "4"),
	}})
	outcome, err := ParseOutcome(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Memories) != maxMemories || outcome.Memories[0].Fact != "One fact on two lines." || outcome.Memories[1].Lasts != "" ||
		!outcome.Memories[2].Remove || outcome.Memories[2].Fact != "" || outcome.Memories[2].Replaces != "Preprod 2 is reset" {
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
