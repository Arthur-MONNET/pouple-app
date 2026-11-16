// Package alerts keeps the reminders the user asked for: a title and a moment. It knows nothing of conversations or
// bubbles: the assistant engine watches the clock and turns a due alert into a conversation.
package alerts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Errors the callers map to their own statuses.
var (
	ErrInvalid  = i18n.NewError("invalid alert")
	ErrNotFound = i18n.NewError("alert not found")
)

const (
	// MaxTitle and MaxNote bound what is shown in a bubble and in the list.
	MaxTitle = 120
	MaxNote  = 1000
	// MaxAlerts bounds the upcoming alerts, so a loop that creates them cannot fill the disk or the screen.
	MaxAlerts = 200
)

// LocalLayout is the shape of a date and time typed in the interface (`<input type="datetime-local">`): no zone, so it
// is read on this computer's clock.
const LocalLayout = "2006-01-02T15:04"

// Alert is a reminder that has not fired yet.
type Alert struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
	// At is when the alert fires; marshalled with its zone offset.
	At time.Time `json:"at"`
	// RequestID is the conversation the alert came from; empty for an alert the user made by hand.
	RequestID string    `json:"requestId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Draft is what makes a new alert.
type Draft struct {
	Title     string
	Note      string
	At        time.Time
	RequestID string
}

// Patch changes an alert; a nil field stays as it is.
type Patch struct {
	Title *string
	Note  *string
	At    *time.Time
}

// ParseWhen reads a moment sent by a client: a full RFC 3339 time, or a local date and time without zone, which is read
// in loc.
func ParseWhen(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return at, nil
	}
	if at, err := time.ParseInLocation(LocalLayout, value, loc); err == nil {
		return at, nil
	}
	return time.Time{}, i18n.Wrap(ErrInvalid, "the date and time of an alert must look like 2026-11-20T09:00")
}

// Store keeps the upcoming alerts in memory and, when it has a path, on disk.
type Store struct {
	path     string
	mu       sync.Mutex
	items    []Alert
	onChange func()
	onError  func(error)
}

// Open reads the alerts kept at path. An empty path keeps them in memory only (tests).
func Open(path string, onError func(error)) (*Store, error) {
	store := &Store{path: path, onError: onError}
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &store.items); err != nil {
		return nil, err
	}
	return store, nil
}

// OnChange registers the function called after every change (it must not block).
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// List returns the alerts, soonest first.
func (s *Store) List() []Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sortedLocked()
}

// Due returns the alerts whose moment is now or past, soonest first.
func (s *Store) Due(now time.Time) []Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []Alert
	for _, alert := range s.sortedLocked() {
		if alert.At.After(now) {
			break
		}
		due = append(due, alert)
	}
	return due
}

// Next is the moment of the soonest alert; false when there is none.
func (s *Store) Next() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sorted := s.sortedLocked()
	if len(sorted) == 0 {
		return time.Time{}, false
	}
	return sorted[0].At, true
}

// Add makes an alert. The moment must be after now.
func (s *Store) Add(draft Draft, now time.Time) (Alert, error) {
	title, note, err := clean(draft.Title, draft.Note)
	if err != nil {
		return Alert{}, err
	}
	if !draft.At.After(now) {
		return Alert{}, i18n.Wrap(ErrInvalid, "the moment of an alert must be in the future")
	}
	s.mu.Lock()
	if len(s.items) >= MaxAlerts {
		s.mu.Unlock()
		return Alert{}, i18n.Wrap(ErrInvalid, "there are too many upcoming alerts: delete some first")
	}
	alert := Alert{ID: newID(), Title: title, Note: note, At: draft.At, RequestID: draft.RequestID, CreatedAt: now}
	s.items = append(s.items, alert)
	s.changed()
	return alert, nil
}

// Update changes an alert that has not fired. A new moment must be after now.
func (s *Store) Update(id string, patch Patch, now time.Time) (Alert, error) {
	s.mu.Lock()
	index := slices.IndexFunc(s.items, func(a Alert) bool { return a.ID == id })
	if index < 0 {
		s.mu.Unlock()
		return Alert{}, ErrNotFound
	}
	updated := s.items[index]
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.Note != nil {
		updated.Note = *patch.Note
	}
	title, note, err := clean(updated.Title, updated.Note)
	if err != nil {
		s.mu.Unlock()
		return Alert{}, err
	}
	updated.Title, updated.Note = title, note
	if patch.At != nil {
		if !patch.At.After(now) {
			s.mu.Unlock()
			return Alert{}, i18n.Wrap(ErrInvalid, "the moment of an alert must be in the future")
		}
		updated.At = *patch.At
	}
	s.items[index] = updated
	s.changed()
	return updated, nil
}

// Remove deletes an alert and reports whether it existed.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	index := slices.IndexFunc(s.items, func(a Alert) bool { return a.ID == id })
	if index < 0 {
		s.mu.Unlock()
		return false
	}
	s.items = slices.Delete(s.items, index, index+1)
	s.changed()
	return true
}

func (s *Store) sortedLocked() []Alert {
	out := slices.Clone(s.items)
	slices.SortStableFunc(out, func(a, b Alert) int { return a.At.Compare(b.At) })
	return out
}

// changed saves the alerts and tells the listener; it releases the lock. Saving under the lock keeps two changes from
// writing out of order, and an alert file is small.
func (s *Store) changed() {
	var saveErr error
	if s.path != "" {
		saveErr = s.saveLocked()
	}
	notify, report := s.onChange, s.onError
	s.mu.Unlock()
	if saveErr != nil && report != nil {
		report(saveErr)
	}
	if notify != nil {
		notify()
	}
}

func (s *Store) saveLocked() error {
	data, err := json.Marshal(s.items)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

// clean trims a title and a note and refuses what the user could not read as written: an empty title, a text that is too
// long, a control or format character (a bidirectional override, for one). Line breaks stay in a note.
func clean(title, note string) (string, string, error) {
	title, note = strings.TrimSpace(title), strings.TrimSpace(note)
	switch {
	case title == "":
		return "", "", i18n.Wrap(ErrInvalid, "an alert needs a title")
	case len([]rune(title)) > MaxTitle:
		return "", "", i18n.Wrap(ErrInvalid, "the title of an alert is too long")
	case len([]rune(note)) > MaxNote:
		return "", "", i18n.Wrap(ErrInvalid, "the note of an alert is too long")
	case hasHiddenRune(title, false) || hasHiddenRune(note, true):
		return "", "", i18n.Wrap(ErrInvalid, "an alert cannot hold control characters")
	}
	return title, note, nil
}

func hasHiddenRune(text string, allowNewlines bool) bool {
	for _, r := range text {
		if allowNewlines && (r == '\n' || r == '\r') {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

func newID() string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}
