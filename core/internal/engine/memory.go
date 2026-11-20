package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/memory"
)

// The memory keeps what lasts beyond a conversation, as notes on the user's computer (see the memory package). The AI
// reads it like a service, through "notes" lookups, and changes it with its answers: it adds facts, corrects them and
// takes out those that no longer hold. The changes are made at once, one commit each, which the conversation lists with
// a way to undo them. A fact written from a conversation that held text of other people is marked unconfirmed: nobody
// vouched for it, so reading it counts as reading their text. A request that came from someone else never sees a private
// fact (and so can change none), for the reason topics read only its own channel: what the AI drafts for that person
// could repeat it.

const (
	// maxMemories is how many changes one answer may make.
	maxMemories = 4
	// maxMemoryNote bounds the text of one note handed to the AI, maxIndexNotes how many notes its index lists.
	maxMemoryNote = 8000
	maxIndexNotes = 200
	// memoryWriteTimeout bounds one write of the memory, git included.
	memoryWriteTimeout = 30 * time.Second
	// maxMemoryChanges is how many writes a conversation keeps the record of, to undo them.
	maxMemoryChanges = 10
)

// lifetimes are how long a fact lives before it is reviewed, by what the AI says of it.
var lifetimes = map[string]time.Duration{"weeks": 28 * 24 * time.Hour, "months": 91 * 24 * time.Hour, "years": 365 * 24 * time.Hour}

// IsNotes reports a lookup in the memory instead of a connected service.
func (l Lookup) IsNotes() bool { return l.Service == "notes" }

// OutcomeMemory is one change the AI makes to the memory: a fact to add, one that replaces another (Replaces), or one to
// take out (Remove, with Replaces naming it).
type OutcomeMemory struct {
	// Note is the path of a note of the index; "" for a new note named Title.
	Note  string `json:"note"`
	Title string `json:"title"`
	Fact  string `json:"fact"`
	// Replaces is how a fact the AI read starts: the one this change corrects or takes out; "" otherwise.
	Replaces string `json:"replaces"`
	Remove   bool   `json:"remove"`
	// Lasts says how long the fact holds: "weeks", "months", "years", or "" when the AI cannot tell.
	Lasts string `json:"lasts"`
}

// MemoryChange is one write of the memory made with an answer, as the conversation lists it.
type MemoryChange struct {
	// ID is the commit that holds it: undoing reverts it.
	ID     string             `json:"id"`
	At     string             `json:"at"`
	Undone bool               `json:"undone,omitempty"`
	Items  []MemoryChangeItem `json:"items"`
}

// MemoryChangeItem is one fact changed.
type MemoryChangeItem struct {
	ID string `json:"id"`
	// Kind is "add", "update" or "remove".
	Kind      string `json:"kind"`
	Note      string `json:"note"`
	NoteTitle string `json:"noteTitle"`
	// Fact is what the fact says now (what it said, for one taken out); Previous what it said before an update.
	Fact     string `json:"fact"`
	Previous string `json:"previous,omitempty"`
	// Line is the line as it is in the note, which names the fact to share it.
	Line        string `json:"line"`
	Shared      bool   `json:"shared"`
	Unconfirmed bool   `json:"unconfirmed,omitempty"`
	// Review is when the fact will be looked at again, YYYY-MM-DD; "" for the default.
	Review string `json:"review,omitempty"`
}

// parseMemories keeps the changes the memory could make as written; the others are dropped, like a lookup that names
// nothing, so the answer stays usable.
func parseMemories(raw []OutcomeMemory) []OutcomeMemory {
	kept := []OutcomeMemory{}
	for _, suggestion := range raw {
		if len(kept) == maxMemories {
			break
		}
		fact, err := memory.CheckFact(memory.Fact{
			Note: suggestion.Note, Title: oneLineText(suggestion.Title), Text: oneLineText(suggestion.Fact),
			Replaces: oneLineText(suggestion.Replaces), Remove: suggestion.Remove,
		})
		if err != nil {
			continue
		}
		lasts := suggestion.Lasts
		if _, known := lifetimes[lasts]; !known {
			lasts = ""
		}
		kept = append(kept, OutcomeMemory{Note: fact.Note, Title: fact.Title, Fact: fact.Text, Replaces: fact.Replaces, Remove: fact.Remove, Lasts: lasts})
	}
	return kept
}

// oneLineText joins the lines and spaces the AI may write into one line.
func oneLineText(text string) string { return strings.Join(strings.Fields(text), " ") }

func (s *Service) memoryOn() bool {
	return s.deps.Memory != nil && s.deps.MemoryOn != nil && s.deps.MemoryOn()
}

// memorySection is the part of the first prompt about the memory: its index, how to read it, and what to suggest. ""
// when the memory is off.
func (s *Service) memorySection(r Stored) string {
	if !s.memoryOn() {
		return ""
	}
	shared := sharedReader(r)
	notes, err := s.deps.Memory.Notes(shared)
	index := "(no note yet)"
	if err != nil {
		index = "(the memory cannot be read now)"
		s.deps.OnError(err)
	} else if len(notes) > 0 {
		lines := make([]string, 0, min(len(notes), maxIndexNotes))
		for _, note := range notes[:min(len(notes), maxIndexNotes)] {
			lines = append(lines, "- "+note.Path+": "+oneLine(note.Title, memory.MaxTitle))
		}
		index = strings.Join(lines, "\n")
	}
	return s.deps.Persona().MemorySection(index, shared)
}

// runNotes answers the "notes" lookups: a search in the memory, or a note. What it returns is the user's own text, unless
// it holds a fact marked unconfirmed.
func (s *Service) runNotes(id string, lookups []Lookup) string {
	if !s.memoryOn() {
		return "Memory, unavailable: it is switched off in Settings."
	}
	request, err := s.require(id)
	if err != nil {
		return ""
	}
	shared := sharedReader(request)
	var parts, steps []string
	for _, lookup := range lookups {
		switch lookup.Action {
		case "search":
			steps = append(steps, s.text("Memory: searched “%s”", oneLine(lookup.Target, 80)))
			parts = append(parts, s.searchNotes(lookup.Target, shared))
		case "read":
			steps = append(steps, s.text("Memory: read “%s”", oneLine(lookup.Target, 80)))
			parts = append(parts, s.readNote(lookup.Target, shared))
		}
	}
	entries := make([]Entry, len(steps))
	for i, step := range steps {
		entries[i] = s.entry("assistant", "step", step)
	}
	text := strings.Join(parts, "\n\n")
	// A fact nobody vouched for rests on text of other people: having read it counts like reading their text.
	unconfirmed := memory.IsUnconfirmed(text)
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, entries...)
		if unconfirmed {
			r.ServicesUsed, r.ForeignSinceGrant = true, true
		}
	})
	return text
}

func (s *Service) searchNotes(query string, shared bool) string {
	hits, err := s.deps.Memory.Search(query, shared)
	if err != nil {
		return fmt.Sprintf("Memory, search %q: unavailable (%s).", oneLine(query, 80), oneLine(err.Error(), 200))
	}
	if len(hits) == 0 {
		return fmt.Sprintf("Memory, search %q: nothing found.", oneLine(query, 80))
	}
	lines := []string{fmt.Sprintf("Memory, search %q:", oneLine(query, 80))}
	for _, hit := range hits {
		lines = append(lines, "- "+hit.Note.Path+" ("+oneLine(hit.Note.Title, memory.MaxTitle)+"): "+oneLine(hit.Line, memory.MaxFact+120))
	}
	return strings.Join(lines, "\n")
}

func (s *Service) readNote(target string, shared bool) string {
	note, content, err := s.deps.Memory.Read(target, shared)
	if err != nil {
		return fmt.Sprintf("Memory, note %q: not found. Use a path of the index.", oneLine(target, 120))
	}
	return fmt.Sprintf("Memory, note %s (%s):\n%s", note.Path, oneLine(note.Title, memory.MaxTitle), clipRunes(content, maxMemoryNote))
}

// applyMemories makes the changes an answer asked for, at once, in one commit, and lists them in the conversation. Only an
// answer that reaches the user changes the memory. A fact rests on the user's own word only when the conversation holds
// their text alone; otherwise it is marked unconfirmed. A new fact is private; the user shares it from the list.
func (s *Service) applyMemories(id string, outcome Outcome) {
	if !s.memoryOn() || len(outcome.Memories) == 0 || !slices.Contains([]string{Answered, NeedsInput, ProposesChange, ProposesPlan}, outcome.Outcome) {
		return
	}
	request, err := s.require(id)
	if err != nil {
		return
	}
	shared := sharedReader(request)
	facts := s.memoryFacts(outcome.Memories, !s.ownText(request, 0), shared)
	if len(facts) == 0 {
		return
	}
	source := memorySource(request, s.deps.Persona().Name, outcome.Summary, s.deps.Now())
	ctx, cancel := context.WithTimeout(context.Background(), memoryWriteTimeout)
	defer cancel()
	change, err := s.deps.Memory.Remember(ctx, facts, source, memory.RememberMessage(source.Label))
	if err != nil {
		s.deps.OnError(fmt.Errorf("could not write the memory: %w", err))
		if !errors.Is(err, memory.ErrNotCommitted) {
			s.update(id, func(r *Stored) {
				r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("The memory could not be written: %s", err)))
			})
			return
		}
	}
	if len(change.Written) == 0 {
		return
	}
	record := MemoryChange{ID: change.Commit, At: s.now()}
	for i, written := range change.Written {
		record.Items = append(record.Items, changeItem(fmt.Sprintf("m%d", i+1), written))
	}
	s.update(id, func(r *Stored) {
		r.MemoryChanges = append(r.MemoryChanges, record)
		if len(r.MemoryChanges) > maxMemoryChanges {
			r.MemoryChanges = r.MemoryChanges[len(r.MemoryChanges)-maxMemoryChanges:]
		}
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Memory: %s", changeSummary(record.Items))))
	})
}

// memoryFacts turns what the AI asked into changes the memory can make. A note the reader cannot see is a new note: a
// request of someone else may not reach into a note that holds only private facts.
func (s *Service) memoryFacts(asked []OutcomeMemory, unconfirmed, shared bool) []memory.Fact {
	titles := map[string]string{}
	if notes, err := s.deps.Memory.Notes(shared); err == nil {
		for _, note := range notes {
			titles[note.Path] = note.Title
		}
	}
	now := s.deps.Now()
	var facts []memory.Fact
	for _, item := range asked {
		fact := memory.Fact{Note: item.Note, Text: item.Fact, Replaces: item.Replaces, Remove: item.Remove, Private: true, Unconfirmed: unconfirmed, SharedOnly: shared}
		if _, known := titles[item.Note]; !known {
			if item.Remove {
				continue
			}
			fact.Note, fact.Replaces, fact.Title = "", "", item.Title
			if fact.Title == "" {
				fact.Title = strings.ReplaceAll(strings.TrimSuffix(path.Base(item.Note), path.Ext(item.Note)), "-", " ")
			}
		}
		if life, ok := lifetimes[item.Lasts]; ok && !item.Remove {
			fact.Review = now.Add(life)
		}
		if _, err := memory.CheckFact(fact); err != nil {
			continue
		}
		facts = append(facts, fact)
	}
	return facts
}

func changeItem(id string, written memory.Written) MemoryChangeItem {
	item := MemoryChangeItem{
		ID: id, Kind: written.Kind, Note: written.Note.Path, NoteTitle: written.Note.Title, Line: written.Line,
		Fact: memory.FactText(written.Line), Shared: !memory.IsPrivate(written.Line), Unconfirmed: memory.IsUnconfirmed(written.Line),
	}
	if written.Previous != "" {
		item.Previous = memory.FactText(written.Previous)
	}
	if due, ok := memory.ReviewDate(written.Line); ok && strings.Contains(written.Line, "(review ") {
		item.Review = due.Format(time.DateOnly)
	}
	return item
}

// changeSummary says in one line what a write changed.
func changeSummary(items []MemoryChangeItem) string {
	parts := make([]string, len(items))
	for i, item := range items {
		verb := map[string]string{memory.Added: "+", memory.Updated: "~", memory.Removed: "−"}[item.Kind]
		parts[i] = verb + " " + item.NoteTitle + " · " + item.Fact
	}
	return "\n" + strings.Join(parts, "\n")
}

// UndoMemoryChange takes back a write of the memory listed in a conversation, by what it changed: the facts kept, shared
// or dropped since are no obstacle, only a fact the user changed by hand.
func (s *Service) UndoMemoryChange(id, changeID string) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	change, err := s.memoryChange(id, changeID)
	if err != nil {
		return err
	}
	written := make([]memory.Written, len(change.Items))
	for i, item := range change.Items {
		written[i] = memory.Written{Note: memory.Note{Path: item.Note, Title: item.NoteTitle}, Kind: item.Kind, Line: item.Line, Previous: item.Previous}
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryWriteTimeout)
	defer cancel()
	if err := s.deps.Memory.Undo(ctx, changeID, written); err != nil {
		if errors.Is(err, memory.ErrChanged) {
			return i18n.Wrap(ErrState, "%s", err)
		}
		return memoryError(err)
	}
	s.update(id, func(r *Stored) {
		for i := range r.MemoryChanges {
			if r.MemoryChanges[i].ID == changeID {
				r.MemoryChanges[i].Undone = true
			}
		}
		r.Entries = append(r.Entries, s.entry("user", "text", s.text("Undid the changes to the memory")))
	})
	return nil
}

// ShareMemoryFact lets a fact written in a conversation reach the requests of others, or keeps it from them again.
func (s *Service) ShareMemoryFact(id, changeID, itemID string, shared bool) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	change, err := s.memoryChange(id, changeID)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(change.Items, func(item MemoryChangeItem) bool { return item.ID == itemID })
	if index < 0 || change.Items[index].Kind == memory.Removed {
		return ErrNotFound
	}
	item := change.Items[index]
	ctx, cancel := context.WithTimeout(context.Background(), memoryWriteTimeout)
	defer cancel()
	line, err := s.deps.Memory.SetShared(ctx, item.Note, item.Line, shared)
	if errors.Is(err, memory.ErrChanged) {
		return i18n.Wrap(ErrState, "this fact changed since you read it")
	}
	if err != nil {
		return memoryError(err)
	}
	s.update(id, func(r *Stored) {
		for i := range r.MemoryChanges {
			if r.MemoryChanges[i].ID != changeID {
				continue
			}
			for j := range r.MemoryChanges[i].Items {
				if r.MemoryChanges[i].Items[j].ID == itemID {
					r.MemoryChanges[i].Items[j].Line, r.MemoryChanges[i].Items[j].Shared = line, shared
				}
			}
		}
	})
	return nil
}

// memoryChange is a write listed in a conversation that can still be acted on.
func (s *Service) memoryChange(id, changeID string) (MemoryChange, error) {
	if !s.memoryOn() {
		return MemoryChange{}, i18n.Wrap(ErrState, "the memory is switched off in Settings")
	}
	request, err := s.require(id)
	if err != nil {
		return MemoryChange{}, err
	}
	index := slices.IndexFunc(request.MemoryChanges, func(change MemoryChange) bool { return change.ID == changeID })
	switch {
	case changeID == "" || index < 0:
		return MemoryChange{}, ErrNotFound
	case request.MemoryChanges[index].Undone:
		return MemoryChange{}, i18n.Wrap(ErrState, "these changes were undone already")
	}
	return request.MemoryChanges[index], nil
}

// memorySource says where the facts of a turn were learned, as the notes show it. A turn that answers what the user wrote
// in the app (a follow-up, an answer to a question, a plan they approved) learned from them, whatever the conversation
// started from: its facts are the user's, not those of whoever wrote first, so neither that person's name nor the link to
// their message goes with them. Whether such a fact is unconfirmed is decided apart, by what the conversation read.
func memorySource(r Stored, owner, summary string, now time.Time) memory.Source {
	source := memory.Source{Date: now}
	if !r.Source.FromUser() && answersOwner(r.Entries, summary) {
		source.Label = "Pouple conversation"
		if owner = oneLine(owner, 40); owner != "" {
			source.Label = owner + ", " + source.Label
		}
		return source
	}
	if r.Source.Permalink != nil {
		source.Link = *r.Source.Permalink
	}
	switch r.Source.Origin {
	case OriginNoteToSelf:
		source.Label = "Note to self"
	case OriginInApp:
		source.Label = "Pouple conversation"
	case OriginPullRequest:
		source.Label = "GitHub pull request"
	case OriginPipeline:
		source.Label = "GitLab pipeline"
	case OriginAsana:
		source.Label = "Asana"
	case OriginMergeRequest:
		source.Label = "GitLab merge request"
	case OriginReminder:
		source.Label = "Reminder"
	case "direct-message", "group-message":
		source.Label = "Slack, " + oneLine(r.Source.SenderName, 40)
	default:
		name := r.Source.ChannelID
		if r.Source.ChannelName != nil {
			name = *r.Source.ChannelName
		}
		source.Label = "Slack #" + oneLine(name, 40)
	}
	return source
}

// answersOwner says whether the turn that ended with summary answered a message of the user: between its summary and the
// answer before it, the user wrote. What follows the summary (an approval made on its own) belongs to the next turn.
func answersOwner(entries []Entry, summary string) bool {
	end := -1
	for at, entry := range slices.Backward(entries) {
		if entry.Role == "assistant" && entry.Kind == "text" && entry.Text == summary {
			end = at
			break
		}
	}
	if end < 0 {
		return false
	}
	for _, entry := range slices.Backward(entries[:end]) {
		if entry.Kind != "text" {
			continue
		}
		switch entry.Role {
		case "user":
			return true
		case "assistant":
			return false
		}
	}
	return false
}

// memoryError maps what the memory refuses to the errors the API understands.
func memoryError(err error) error {
	switch {
	case errors.Is(err, memory.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, memory.ErrInvalid):
		return i18n.Wrap(ErrInput, "%s", err)
	}
	return err
}
