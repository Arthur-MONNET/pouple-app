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
	datePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	keptPattern = regexp.MustCompile(` \(kept \d{4}-\d{2}-\d{2}\)`)
)

// ToReview is a fact that nothing confirmed for a long time.
type ToReview struct {
	Note Note `json:"note"`
	// Line is the line as it is in the note: the identity of the fact for Review.
	Line string `json:"line"`
	// Fact is what it says, without its list marker and its private mark.
	Fact string `json:"fact"`
	// Date is the last date the fact carries (learned, or kept since), YYYY-MM-DD.
	Date string `json:"date"`
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

// FactDate is the last date a line carries, the one that says how recent it is; false when it has none.
func FactDate(line string) (time.Time, bool) {
	dates := datePattern.FindAllString(line, -1)
	if len(dates) == 0 {
		return time.Time{}, false
	}
	at, err := time.Parse(time.DateOnly, dates[len(dates)-1])
	return at, err == nil
}

// Review lists the facts older than StaleAfter, the oldest first. A fact written by hand without a date is never listed:
// nothing says how old it is.
func (s *Store) Review(now time.Time) ([]ToReview, error) {
	notes, err := s.Notes(false)
	if err != nil {
		return nil, err
	}
	var stale []ToReview
	for _, note := range notes {
		_, content, err := s.Read(note.Path, false)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(content, "\n") {
			text := factText(line)
			at, dated := FactDate(line)
			if text == "" || !dated || now.Sub(at) < StaleAfter {
				continue
			}
			display := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), PrivateMark))
			stale = append(stale, ToReview{Note: note, Line: line, Fact: display, Date: at.Format(time.DateOnly)})
		}
	}
	slices.SortStableFunc(stale, func(a, b ToReview) int { return strings.Compare(a.Date, b.Date) })
	return stale[:min(len(stale), maxReview)], nil
}

// Keep marks a fact confirmed today, so it leaves the review for six months; Drop takes it out of its note, with the
// lines indented under it. line must be the line as it was read: ErrChanged otherwise.
func (s *Store) Keep(ctx context.Context, note, line string, now time.Time) error {
	return s.reviewLine(ctx, note, line, "Kept a fact", func(lines []string, at int) []string {
		lines[at] = keptLine(lines[at], now)
		return lines
	})
}

// Drop takes a fact out of its note; see Keep.
func (s *Store) Drop(ctx context.Context, note, line string) error {
	return s.reviewLine(ctx, note, line, "Dropped a fact", func(lines []string, at int) []string {
		end := at + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" && indentWidth(lines[end]) > indentWidth(lines[at]) {
			end++
		}
		return slices.Delete(lines, at, end)
	})
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
	if err := replaceFile(root, name, strings.Join(change(lines, at), "\n")+"\n"); err != nil {
		return err
	}
	if err := s.writeIndex(root); err != nil {
		return err
	}
	return s.commit(ctx, dir, message+" in "+name)
}

// keptLine says when a fact was last confirmed: one "(kept …)" mark, before the private mark.
func keptLine(line string, now time.Time) string {
	line = keptPattern.ReplaceAllString(line, "")
	mark := " (kept " + now.Format(time.DateOnly) + ")"
	trimmed := strings.TrimRight(line, " ")
	if strings.HasSuffix(strings.ToLower(trimmed), PrivateMark) {
		base := strings.TrimRight(trimmed[:len(trimmed)-len(PrivateMark)], " ")
		return base + mark + " " + trimmed[len(trimmed)-len(PrivateMark):]
	}
	return trimmed + mark
}
