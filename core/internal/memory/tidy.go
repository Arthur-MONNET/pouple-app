package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Tidying up merges the notes that are one subject and writes the index again. It is one commit whose message starts with
// tidyMessage, made even when nothing changed: the history itself says when the memory was last tidied.
const (
	tidyMessage     = "Tidy up"
	rememberMessage = "Remember from "
	// maxMerges bounds what one tidy-up changes.
	maxMerges = 5
)

// Merge moves every fact of one note into another, and removes the first.
type Merge struct {
	From string `json:"from"`
	Into string `json:"into"`
}

// TidyState is what the history says: when the memory was last tidied, and how many writes of facts came since.
type TidyState struct {
	At time.Time
	// Remembered counts the writes of facts since the last tidy-up (or ever, when there was none).
	Remembered int
}

// State reads the history of the folder. A folder that is not a repository yet has nothing to tidy.
func (s *Store) State(ctx context.Context) (TidyState, error) {
	dir := s.dir()
	if dir == "" {
		return TidyState{}, nil
	}
	if _, err := os.Stat(path.Join(dir, ".git")); errors.Is(err, fs.ErrNotExist) {
		return TidyState{}, nil
	}
	out, err := s.git(ctx, dir, "log", "--max-count=500", "--format=%cI%x09%s")
	if err != nil {
		// A repository without any commit yet has no history to read.
		if strings.Contains(out, "does not have any commits") {
			return TidyState{}, nil
		}
		return TidyState{}, i18n.Wrap(err, "The history of the memory cannot be read")
	}
	var state TidyState
	for entry := range strings.SplitSeq(out, "\n") {
		when, subject, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		if strings.HasPrefix(subject, tidyMessage) {
			state.At, _ = time.Parse(time.RFC3339, when)
			break
		}
		if strings.HasPrefix(subject, rememberMessage) {
			state.Remembered++
		}
	}
	return state, nil
}

// RememberMessage is the commit message of facts learned from source.
func RememberMessage(source string) string { return rememberMessage + source }

// Tidy merges notes and writes the index again, in one commit. A merge that names a note that is not there (or the same
// note twice, or a note already merged away) is left out; it returns the merges made.
func (s *Store) Tidy(ctx context.Context, merges []Merge) ([]Merge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dir()
	if dir == "" {
		return nil, nil
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err := s.commitOutside(ctx, dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, i18n.Wrap(err, "The memory folder cannot be read")
	}
	defer root.Close()
	var done []Merge
	gone := map[string]bool{}
	for _, merge := range merges[:min(len(merges), maxMerges)] {
		from, okFrom := CleanPath(merge.From)
		into, okInto := CleanPath(merge.Into)
		if !okFrom || !okInto || from == into || gone[from] || gone[into] {
			continue
		}
		if err := mergeNote(root, from, into); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return done, err
		}
		gone[from] = true
		done = append(done, Merge{From: from, Into: into})
	}
	if err := s.writeIndex(root); err != nil {
		return done, err
	}
	message := tidyMessage
	for _, merge := range done {
		message += "\n\nMerged " + merge.From + " into " + merge.Into
	}
	return done, s.commitAlways(ctx, dir, message)
}

// mergeNote appends the facts of from under a heading of its title in into, then removes from.
func mergeNote(root *os.Root, from, into string) error {
	source, err := readNote(root, from)
	if err != nil {
		return err
	}
	target, err := readNote(root, into)
	if err != nil {
		return err
	}
	var body []string
	headingSeen := false
	for line := range strings.SplitSeq(strings.TrimRight(source, "\n"), "\n") {
		// The title of the merged note becomes a section of the other one.
		if !headingSeen && headingLevel(strings.TrimSpace(line)) == 1 {
			headingSeen = true
			continue
		}
		body = append(body, line)
	}
	section := strings.TrimSpace(strings.Join(body, "\n"))
	if section == "" {
		return root.Remove(from)
	}
	text := strings.TrimRight(target, "\n") + "\n\n## " + titleOf(from, source) + "\n\n" + section + "\n"
	if err := replaceFile(root, into, text); err != nil {
		return err
	}
	return root.Remove(from)
}

// commitAlways commits what changed, or an empty commit when nothing did: the message is what counts.
func (s *Store) commitAlways(ctx context.Context, dir, message string) error {
	if out, err := s.git(ctx, dir, "add", "--all"); err != nil {
		return i18n.Wrap(ErrNotCommitted, "git could not commit the memory: %s", out)
	}
	args := append(append([]string{}, gitIdentity...), "commit", "--quiet", "--no-verify", "--allow-empty", "-m", message)
	if out, err := s.git(ctx, dir, args...); err != nil {
		return i18n.Wrap(ErrNotCommitted, "git could not commit the memory: %s", out)
	}
	return nil
}
