package memory

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// ErrChanged is returned when the line a client read is no longer in its note: someone changed it meanwhile.
var ErrChanged = i18n.NewError("this fact changed since you read it")

const (
	// StaleAfter is how old a fact gets before it is offered for review: about six months.
	StaleAfter = 183 * 24 * time.Hour
	// maxReview bounds the facts offered for review at once, the oldest first.
	maxReview = 50
)

var (
	datePattern   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	keptPattern   = regexp.MustCompile(` \(kept \d{4}-\d{2}-\d{2}\)`)
	reviewPattern = regexp.MustCompile(` \(review (\d{4}-\d{2}-\d{2})\)`)
	// minLife is the shortest a fact lives before it is reviewed again once kept.
	minLife = 14 * 24 * time.Hour
)

// ToReview is a fact to look at again: its time came, or nobody vouched for it.
type ToReview struct {
	Note Note `json:"note"`
	// Line is the line as it is in the note: the identity of the fact for Keep and Drop.
	Line string `json:"line"`
	// Fact is what it says, without its list marker, its review date and its marks.
	Fact string `json:"fact"`
	// Date is when it was learned or last kept, YYYY-MM-DD.
	Date string `json:"date"`
	// Unconfirmed is set for a fact the AI wrote from text of other people.
	Unconfirmed bool `json:"unconfirmed"`
}

// CountFacts is how many list items a note holds.
func CountFacts(content string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		if factText(line) != "" {
			count++
		}
	}
	return count
}

// FactDate is when a fact was learned or last kept: the last date of its line, its review date aside. False when it has
// none.
func FactDate(line string) (time.Time, bool) {
	dates := datePattern.FindAllString(reviewPattern.ReplaceAllString(line, ""), -1)
	if len(dates) == 0 {
		return time.Time{}, false
	}
	at, err := time.Parse(time.DateOnly, dates[len(dates)-1])
	return at, err == nil
}

// ReviewDate is when a fact is due: its own review date, or StaleAfter after it was learned or kept. False when the line
// says neither: nothing tells how old a fact written by hand without a date is.
func ReviewDate(line string) (time.Time, bool) {
	if match := reviewPattern.FindStringSubmatch(line); match != nil {
		if at, err := time.Parse(time.DateOnly, match[1]); err == nil {
			return at, true
		}
	}
	learned, ok := FactDate(line)
	return learned.Add(StaleAfter), ok
}

// Review lists the facts to look at again: those whose time came, and those nobody vouched for, the oldest first.
func (s *Store) Review(now time.Time) ([]ToReview, error) {
	notes, err := s.Notes(false)
	if err != nil {
		return nil, err
	}
	var listed []ToReview
	for _, note := range notes {
		_, content, err := s.Read(note.Path, false)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(content, "\n") {
			text := factText(line)
			due, dated := ReviewDate(line)
			unconfirmed := IsUnconfirmed(line)
			if text == "" || (!unconfirmed && (!dated || now.Before(due))) {
				continue
			}
			learned, _ := FactDate(line)
			item := ToReview{Note: note, Line: line, Fact: FactText(line), Unconfirmed: unconfirmed}
			if !learned.IsZero() {
				item.Date = learned.Format(time.DateOnly)
			}
			listed = append(listed, item)
		}
	}
	slices.SortStableFunc(listed, func(a, b ToReview) int { return strings.Compare(a.Date, b.Date) })
	return listed[:min(len(listed), maxReview)], nil
}

// FactText is what a line of a note says: no list marker, no marks, and none of the dated parentheses that follow the
// fact (where and when it was learned, kept, reviewed).
func FactText(line string) string {
	text := strings.TrimSpace(line)
	if item := factText(text); item != "" {
		text = item
	}
	for _, mark := range []string{PrivateMark, UnconfirmedMark} {
		text = strings.TrimSpace(strings.ReplaceAll(text, mark, ""))
	}
	for strings.HasSuffix(text, ")") {
		open := strings.LastIndex(text, " (")
		if open < 0 || !datePattern.MatchString(text[open:]) {
			break
		}
		text = strings.TrimSpace(text[:open])
	}
	return text
}

// Keep confirms a fact: it is no longer unconfirmed, and it is due again after as long as it lived since it was learned
// (at least two weeks; StaleAfter when it had no review date). line must be the line as it was read: ErrChanged otherwise.
func (s *Store) Keep(ctx context.Context, note, line string, now time.Time) error {
	return s.reviewLine(ctx, note, line, keptMessage, func(lines []string, at int) []string {
		lines[at] = keptLine(lines[at], now)
		return lines
	})
}

// Drop takes a fact out of its note; see Keep. A note left with no fact and no other text is removed.
func (s *Store) Drop(ctx context.Context, note, line string) error {
	return s.reviewLine(ctx, note, line, droppedMessage, func(lines []string, at int) []string {
		return slices.Delete(lines, at, itemEnd(lines, at))
	})
}

// SetShared lets a fact reach the requests of others, or keeps it from them; it returns the line as it is now.
func (s *Store) SetShared(ctx context.Context, note, line string, shared bool) (string, error) {
	message := privateMessage
	if shared {
		message = sharedMessage
	}
	var changed string
	err := s.reviewLine(ctx, note, line, message, func(lines []string, at int) []string {
		lines[at] = withPrivate(lines[at], !shared)
		changed = lines[at]
		return lines
	})
	return changed, err
}

func withPrivate(line string, private bool) string {
	trimmed := strings.TrimRight(line, " ")
	if !private {
		return strings.TrimRight(strings.ReplaceAll(trimmed, " "+PrivateMark, ""), " ")
	}
	if IsPrivate(trimmed) {
		return trimmed
	}
	return trimmed + " " + PrivateMark
}

func (s *Store) reviewLine(ctx context.Context, raw, line, message string, change func([]string, int) []string) error {
	name, ok := CleanPath(raw)
	if !ok || strings.TrimSpace(line) == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dir()
	if dir == "" {
		return ErrNotFound
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return ErrNotFound
	}
	defer root.Close()
	if err := s.commitOutside(ctx, dir); err != nil {
		return err
	}
	content, err := readNote(root, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrChanged
		}
		return i18n.Wrap(err, "The note “%s” cannot be read", name)
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	at := slices.Index(lines, strings.TrimRight(line, "\n"))
	if at < 0 {
		return ErrChanged
	}
	if err := saveNote(root, name, strings.Join(change(lines, at), "\n")+"\n"); err != nil {
		return err
	}
	if err := s.writeIndex(root); err != nil {
		return err
	}
	return s.commit(ctx, dir, message+" in "+name)
}

// keptLine confirms a fact today: no unconfirmed mark, no older kept mark, and a review date as far ahead as the fact had
// lived when it was kept (at least minLife; StaleAfter when it had no review date).
func keptLine(line string, now time.Time) string {
	life := StaleAfter
	if match := reviewPattern.FindStringSubmatch(line); match != nil {
		review, errReview := time.Parse(time.DateOnly, match[1])
		learned, dated := FactDate(line)
		if errReview == nil && dated {
			life = max(review.Sub(learned), minLife)
		}
	}
	private := IsPrivate(line)
	line = keptPattern.ReplaceAllString(line, "")
	line = reviewPattern.ReplaceAllString(line, "")
	for _, mark := range []string{" " + UnconfirmedMark, " " + PrivateMark} {
		line = strings.ReplaceAll(line, mark, "")
	}
	line = strings.TrimRight(line, " ") + " (kept " + now.Format(time.DateOnly) + ") (review " + now.Add(life).Format(time.DateOnly) + ")"
	if private {
		line += " " + PrivateMark
	}
	return line
}
