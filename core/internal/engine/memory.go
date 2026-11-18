package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// reads it like a service, through "notes" lookups, and suggests facts with its answers; nothing is written until the
// user ticks the facts and confirms. A request that came from someone else never sees a private fact, for the reason
// topics read only its own channel: what the AI drafts for that person could repeat it.

const (
	// maxMemories is how many facts one answer may suggest.
	maxMemories = 4
	// maxMemoryNote bounds the text of one note handed to the AI, maxIndexNotes how many notes its index lists.
	maxMemoryNote = 8000
	maxIndexNotes = 200
	// memoryWriteTimeout bounds writing the facts the user confirmed, git included.
	memoryWriteTimeout = 30 * time.Second
)

// IsNotes reports a lookup in the memory instead of a connected service.
func (l Lookup) IsNotes() bool { return l.Service == "notes" }

// OutcomeMemory is one fact the AI suggests keeping.
type OutcomeMemory struct {
	// Note is the path of a note of the index; "" for a new note named Title.
	Note  string `json:"note"`
	Title string `json:"title"`
	Fact  string `json:"fact"`
	// Replaces is the text of a fact the AI read that this one makes wrong; "" otherwise.
	Replaces string `json:"replaces"`
}

// MemoryStatus is where a suggestion of facts stands.
type MemoryStatus string

const (
	MemoryProposed MemoryStatus = "proposed"
	MemoryAccepted MemoryStatus = "accepted"
	MemoryDeclined MemoryStatus = "declined"
)

// MemoryOption is one fact as the user sees it.
type MemoryOption struct {
	ID string `json:"id"`
	// Note is the path of the note it goes to; "" for a new note.
	Note string `json:"note,omitempty"`
	// NoteTitle names the note it goes to, new or not.
	NoteTitle string `json:"noteTitle"`
	Fact      string `json:"fact"`
	Replaces  string `json:"replaces,omitempty"`
	// Ticked is the suggested choice: only for a conversation that never read text written by others.
	Ticked bool `json:"ticked"`
}

// MemoryOffer is what the assistant suggests remembering. Nothing is written until the user picks.
type MemoryOffer struct {
	// ID names this exact suggestion (a fingerprint): a decision that carries it cannot answer another one.
	ID     string         `json:"id"`
	Status MemoryStatus   `json:"status"`
	Items  []MemoryOption `json:"items"`
	// Chosen are the ids of the facts the user kept, once accepted.
	Chosen []string `json:"chosen,omitempty"`
}

// MemoryPick is one fact the user ticked. Shared lets it reach the requests of others; a fact is private otherwise.
type MemoryPick struct {
	ID     string `json:"id"`
	Shared bool   `json:"shared,omitempty"`
}

// parseMemories keeps the suggested facts the memory could hold as written; the others are dropped, like a lookup that
// names nothing, so the answer stays usable.
func parseMemories(raw []OutcomeMemory) []OutcomeMemory {
	kept := []OutcomeMemory{}
	for _, suggestion := range raw {
		if len(kept) == maxMemories {
			break
		}
		fact, err := memory.CheckFact(memory.Fact{
			Note: suggestion.Note, Title: oneLineText(suggestion.Title), Text: oneLineText(suggestion.Fact), Replaces: oneLineText(suggestion.Replaces),
		})
		if err != nil {
			continue
		}
		kept = append(kept, OutcomeMemory{Note: fact.Note, Title: fact.Title, Fact: fact.Text, Replaces: fact.Replaces})
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

// runNotes answers the "notes" lookups: a search in the memory, or a note. The memory holds only what the user confirmed
// or wrote, so what it returns is theirs: it does not count as text of others.
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
	s.update(id, func(r *Stored) { r.Entries = append(r.Entries, entries...) })
	return strings.Join(parts, "\n\n")
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

// attachMemories shows the facts an answer suggested keeping. Like reminders, only an answer that reaches the user can
// carry them, and a new answer without any takes away a suggestion nobody picked from.
func (s *Service) attachMemories(id string, outcome Outcome) {
	if !s.memoryOn() || !slices.Contains([]string{Answered, NeedsInput, ProposesChange, ProposesPlan}, outcome.Outcome) {
		return
	}
	request, err := s.require(id)
	if err != nil {
		return
	}
	offer := s.memoryOfferOf(outcome.Memories, s.ownText(request, 0), sharedReader(request))
	s.update(id, func(r *Stored) {
		switch {
		case offer != nil:
			r.Memories = offer
		case r.Memories != nil && r.Memories.Status == MemoryProposed:
			r.Memories = nil
		}
	})
}

// memoryOfferOf turns suggested facts into an offer. A fact is ticked in advance only when the conversation holds the
// user's own text alone; the note it names is shown by its title, and a path that is not a note (or that a request of
// someone else cannot see) makes a new note instead.
func (s *Service) memoryOfferOf(suggested []OutcomeMemory, own, shared bool) *MemoryOffer {
	if len(suggested) == 0 {
		return nil
	}
	titles := map[string]string{}
	if notes, err := s.deps.Memory.Notes(shared); err == nil {
		for _, note := range notes {
			titles[note.Path] = note.Title
		}
	}
	offer := &MemoryOffer{Status: MemoryProposed}
	for _, suggestion := range suggested {
		option := MemoryOption{Fact: suggestion.Fact, Replaces: suggestion.Replaces, Ticked: own}
		if title, ok := titles[suggestion.Note]; ok {
			option.Note, option.NoteTitle = suggestion.Note, title
		} else {
			option.NoteTitle = suggestion.Title
			if option.NoteTitle == "" {
				option.NoteTitle = strings.ReplaceAll(strings.TrimSuffix(path.Base(suggestion.Note), path.Ext(suggestion.Note)), "-", " ")
			}
			option.Replaces = ""
		}
		// Only what could be written is offered: a decision on it never fails on the fact itself.
		candidate := memory.Fact{Note: option.Note, Text: option.Fact}
		if option.Note == "" {
			// The title of an existing note is whatever the user wrote; only a new one must be plain words.
			candidate.Title = option.NoteTitle
		}
		if _, err := memory.CheckFact(candidate); err != nil {
			continue
		}
		option.ID = fmt.Sprintf("m%d", len(offer.Items)+1)
		offer.Items = append(offer.Items, option)
	}
	if len(offer.Items) == 0 {
		return nil
	}
	offer.ID = memoryFingerprint(*offer)
	return offer
}

func memoryFingerprint(offer MemoryOffer) string {
	hash := sha256.New()
	for _, item := range offer.Items {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x01", item.ID, item.Note, item.NoteTitle, item.Fact, item.Replaces)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

// DecideMemories writes the facts the user ticked in the memory, or declines the suggestion. offerID names the
// suggestion the user read; it must be the one waiting now. All the facts are written, or none.
func (s *Service) DecideMemories(id string, approve bool, offerID string, picks []MemoryPick) error {
	if offerID == "" {
		return i18n.Wrap(ErrInput, "the suggestion that was read must be named")
	}
	if approve && !s.memoryOn() {
		return i18n.Wrap(ErrState, "the memory is switched off in Settings")
	}
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	request, err := s.require(id)
	if err != nil {
		return err
	}
	offer := request.Memories
	switch {
	case offer == nil || offer.Status != MemoryProposed:
		return i18n.Wrap(ErrState, "nothing waits to be remembered")
	case offer.ID != offerID:
		return i18n.Wrap(ErrState, "the suggested facts changed since you read them")
	}
	if !approve {
		if !s.settleMemories(id, offerID, func(r *Stored) {
			r.Memories.Status = MemoryDeclined
			r.Entries = append(r.Entries, s.entry("user", "text", s.text("Declined the facts to remember")))
		}) {
			return i18n.Wrap(ErrState, "the suggested facts changed since you read them")
		}
		return nil
	}
	facts, chosen, err := factsFor(*offer, picks)
	if err != nil {
		return err
	}
	source := memorySource(request, s.deps.Now())
	// Writing is a few files and git commands on this computer: a bound keeps a stuck git from holding the decision.
	ctx, cancel := context.WithTimeout(context.Background(), memoryWriteTimeout)
	defer cancel()
	written, err := s.deps.Memory.Remember(ctx, facts, source, memory.RememberMessage(source.Label))
	if err != nil && !errors.Is(err, memory.ErrNotCommitted) {
		return memoryError(err)
	}
	journal := s.text("Remembered:\n%s", factLines(written))
	if err != nil {
		s.deps.OnError(err)
		journal += "\n" + s.text("Git could not commit it: the notes are written, but their history does not show it yet.")
	}
	if !s.settleMemories(id, offerID, func(r *Stored) {
		r.Memories.Status, r.Memories.Chosen = MemoryAccepted, chosen
		r.Entries = append(r.Entries, s.entry("user", "text", journal))
	}) {
		// The facts are in the notes: the conversation moved on meanwhile, so the journal is all that is lost.
		return i18n.Wrap(ErrState, "the suggested facts changed since you read them")
	}
	return nil
}

// settleMemories applies change only while the suggestion offerID still waits for a choice, and reports whether it did.
func (s *Service) settleMemories(id, offerID string, change func(*Stored)) bool {
	settled := false
	s.update(id, func(r *Stored) {
		if r.Memories == nil || r.Memories.ID != offerID || r.Memories.Status != MemoryProposed {
			return
		}
		change(r)
		settled = true
	})
	return settled
}

// factsFor checks the user's picks against the suggestion and turns them into facts to write.
func factsFor(offer MemoryOffer, picks []MemoryPick) ([]memory.Fact, []string, error) {
	if len(picks) == 0 {
		return nil, nil, i18n.Wrap(ErrInput, "choose at least one fact, each only once")
	}
	var facts []memory.Fact
	var chosen []string
	for _, pick := range picks {
		index := slices.IndexFunc(offer.Items, func(item MemoryOption) bool { return item.ID == pick.ID })
		if index < 0 || slices.Contains(chosen, pick.ID) {
			return nil, nil, i18n.Wrap(ErrInput, "choose at least one fact, each only once")
		}
		item := offer.Items[index]
		fact := memory.Fact{Note: item.Note, Text: item.Fact, Replaces: item.Replaces, Private: !pick.Shared}
		if item.Note == "" {
			fact.Title = item.NoteTitle
		}
		facts = append(facts, fact)
		chosen = append(chosen, pick.ID)
	}
	return facts, chosen, nil
}

// memorySource says where the facts of a conversation were learned, as the notes show it.
func memorySource(r Stored, now time.Time) memory.Source {
	source := memory.Source{Date: now}
	if r.Source.Permalink != nil {
		source.Link = *r.Source.Permalink
	}
	switch r.Source.Origin {
	case "note-to-self":
		source.Label = "Note to self"
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

func factLines(written []memory.Written) string {
	lines := make([]string, len(written))
	for i, fact := range written {
		lines[i] = "- " + fact.Note.Title + " · " + strings.TrimPrefix(fact.Line, "- ")
	}
	return strings.Join(lines, "\n")
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
