package memory

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// The subjects of Pouple's own commits. Any other commit (the one Pouple makes of what changed outside it, or one the
// user made with git) is a change by hand.
const (
	keptMessage    = "Kept a fact"
	droppedMessage = "Dropped a fact"
	privateMessage = "Made a fact private"
	sharedMessage  = "Shared a fact"
	undoMessage    = "Undid "
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	mergedPattern = regexp.MustCompile(`^Merged (\S.*) into (\S.*)$`)
	// poupleSubjects start the subjects of the commits Pouple makes; "Index written again" and `Revert "` are those of
	// the undo it made before it undid by content.
	poupleSubjects = []string{rememberMessage, tidyMessage, keptMessage, droppedMessage, privateMessage, sharedMessage, undoMessage, "Index written again", `Revert "`}
)

// maxMoves bounds how many merges are followed from a note to the one that holds its facts now.
const maxMoves = 8

// Undo takes back one write of facts, by its commit and what it changed (the Written it returned). It is the inverse of
// the write on the notes as they are now, so the facts the review kept, shared or dropped since, and the notes a tidy-up
// merged, are no obstacle: a fact added is taken out, a fact updated gets its previous line back, a fact removed comes
// back at the end of its note. A fact already gone is left so; a fact the user changed by hand meanwhile cannot be found
// by its text, and then nothing is undone: ErrChanged, naming it. All of it is one commit.
func (s *Store) Undo(ctx context.Context, commit string, written []Written) error {
	if !commitPattern.MatchString(commit) || len(written) == 0 {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dir()
	if dir == "" {
		return ErrNotFound
	}
	if err := s.commitOutside(ctx, dir); err != nil {
		return err
	}
	subject, err := s.git(ctx, dir, "log", "-1", "--format=%s", commit)
	if err != nil || !strings.HasPrefix(subject, rememberMessage) {
		return ErrNotFound
	}
	if _, err := s.git(ctx, dir, "merge-base", "--is-ancestor", commit, "HEAD"); err != nil {
		return ErrNotFound
	}
	since, err := s.historySince(ctx, dir, commit)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return i18n.Wrap(err, "The memory folder cannot be read")
	}
	defer root.Close()
	undo := undoing{store: s, ctx: ctx, dir: dir, commit: commit, root: root, since: since, notes: map[string][]string{}, before: map[string][]string{}}
	// The last change first: a write may touch the same note twice.
	for _, item := range slices.Backward(written) {
		if err := undo.item(item); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(undo.notes)) {
		if err := saveNote(root, name, strings.Join(undo.notes[name], "\n")+"\n"); err != nil {
			return err
		}
	}
	if err := s.writeIndex(root); err != nil {
		return err
	}
	return s.commit(ctx, dir, undoMessage+strings.TrimSpace(subject)+" ("+commit[:7]+")")
}

// history is what happened to the notes after a commit: where tidy-ups moved them, and the facts changed by hand.
type history struct {
	// moved maps a note to the note it was merged into.
	moved map[string]string
	// byHand holds the text of every fact a change by hand took out or rewrote.
	byHand map[string]bool
}

// historySince reads the commits after commit, oldest first, with what each changed.
func (s *Store) historySince(ctx context.Context, dir, commit string) (history, error) {
	out, err := s.git(ctx, dir, "-c", "core.quotePath=false", "log", "--reverse", "--unified=0", "-p", "--format=%x1e%B%x1d", commit+"..HEAD")
	if err != nil {
		return history{}, i18n.Wrap(err, "The history of the memory cannot be read")
	}
	since := history{moved: map[string]string{}, byHand: map[string]bool{}}
	for record := range strings.SplitSeq(out, "\x1e") {
		message, patch, _ := strings.Cut(record, "\x1d")
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		subject, _, _ := strings.Cut(message, "\n")
		if strings.HasPrefix(subject, tidyMessage) {
			for line := range strings.SplitSeq(message, "\n") {
				if match := mergedPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
					since.moved[match[1]] = match[2]
				}
			}
		}
		if slices.ContainsFunc(poupleSubjects, func(prefix string) bool { return strings.HasPrefix(subject, prefix) }) {
			continue
		}
		for line := range strings.SplitSeq(patch, "\n") {
			if removed, ok := strings.CutPrefix(line, "-"); ok && !strings.HasPrefix(line, "--- ") && factText(removed) != "" {
				since.byHand[FactText(removed)] = true
			}
		}
	}
	return since, nil
}

// undoing holds the notes an undo changes, read once and written at the end, so that a refusal writes nothing.
type undoing struct {
	store  *Store
	ctx    context.Context
	dir    string
	commit string
	root   *os.Root
	since  history
	// notes are the lines of the notes changed so far, by path; a note that does not exist has none.
	notes map[string][]string
	// before are the lines of the notes before the write, by the path they had then.
	before map[string][]string
}

func (u *undoing) item(item Written) error {
	text := FactText(item.Line)
	if text == "" {
		return nil
	}
	name := u.current(item.Note.Path)
	lines, err := u.lines(name)
	if err != nil {
		return err
	}
	at := lastFact(lines, text)
	switch item.Kind {
	case Added, Updated:
		if at < 0 {
			if u.since.byHand[text] {
				return i18n.Wrap(ErrChanged, "“%s” was changed by hand in %s since: change it on the Memory page or in the note", clipRunes(text, 80), name)
			}
			// The review dropped it, or the user did with Pouple: it is gone already.
			return nil
		}
		if item.Kind == Added {
			u.notes[name] = slices.Delete(lines, at, itemEnd(lines, at))
			return nil
		}
		previous, err := u.previousLines(item.Note.Path, FactText(item.Previous), item.Previous)
		if err != nil {
			return err
		}
		// The lines indented under the fact stay: only the fact itself was rewritten.
		u.notes[name] = slices.Replace(lines, at, at+1, previous[0])
	case Removed:
		if at >= 0 {
			return nil
		}
		block, err := u.previousLines(item.Note.Path, text, item.Line)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			lines = []string{"# " + item.Note.Title, ""}
			if item.Note.Title == "" {
				lines[0] = "# " + titleOf(name, "")
			}
		}
		u.notes[name] = append(lines, block...)
	}
	return nil
}

// current is the note that holds the facts of name now: the one tidy-ups merged it into, if any.
func (u *undoing) current(name string) string {
	for range maxMoves {
		into, ok := u.since.moved[name]
		if !ok {
			break
		}
		name = into
	}
	return name
}

// lines are the lines of a note as the undo left them so far; none for a note that does not exist.
func (u *undoing) lines(name string) ([]string, error) {
	if lines, ok := u.notes[name]; ok {
		return lines, nil
	}
	content, err := readNote(u.root, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, i18n.Wrap(err, "The note “%s” cannot be read", name)
	}
	return strings.Split(strings.TrimRight(content, "\n"), "\n"), nil
}

// previousLines is the fact whose text is text as the note held it before the write, with the lines indented under it;
// fallback alone when the history does not hold it (fallback is then a line of a note, or just the text of the fact).
func (u *undoing) previousLines(name, text, fallback string) ([]string, error) {
	lines, ok := u.before[name]
	if !ok {
		content, err := u.store.git(u.ctx, u.dir, "show", u.commit+"^:"+name)
		if err == nil {
			lines = strings.Split(strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), "\n")
		}
		u.before[name] = lines
	}
	if at := lastFact(lines, text); at >= 0 {
		return slices.Clone(lines[at:itemEnd(lines, at)]), nil
	}
	if factText(fallback) == "" {
		if strings.TrimSpace(fallback) == "" {
			return nil, ErrNotFound
		}
		fallback = "- " + strings.TrimSpace(fallback)
	}
	return []string{fallback}, nil
}

// lastFact is the index of the last list item that says text; -1 when none does.
func lastFact(lines []string, text string) int {
	for at, line := range slices.Backward(lines) {
		if factText(line) != "" && FactText(line) == text {
			return at
		}
	}
	return -1
}
