package engine

import (
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// settleAfter is how long a finished conversation stays in the list before it puts itself away.
	settleAfter = 7 * 24 * time.Hour
	// maxSnooze bounds a postponement, so a conversation cannot disappear for good by a typo in the date.
	maxSnooze = 366 * 24 * time.Hour
)

// Arrangement is a change to how a conversation is kept in the list. A nil field is left as it is. Filing away never
// changes what the assistant is doing and is not activity: the update time stays, so the order of the list is not shuffled.
type Arrangement struct {
	Pinned   *bool `json:"pinned,omitempty"`
	Archived *bool `json:"archived,omitempty"`
	// SnoozedUntil is an RFC 3339 moment in the future; "" brings the conversation back at once.
	SnoozedUntil *string `json:"snoozedUntil,omitempty"`
}

// Arrange pins, archives or snoozes a conversation (or undoes it). Archiving and snoozing close its bubble, and the two
// exclude each other; archiving also unpins; bringing one back opens its bubble again. The assistant working on a request is not interrupted, but it cannot be
// archived while it works: it would vanish before its answer.
func (s *Service) Arrange(id string, change Arrangement) error {
	now := s.deps.Now()
	var until time.Time
	if change.SnoozedUntil != nil && *change.SnoozedUntil != "" {
		parsed, err := time.Parse(time.RFC3339, *change.SnoozedUntil)
		if err != nil {
			return i18n.Wrap(ErrInput, `"snoozedUntil" must be an RFC 3339 date`)
		}
		if !parsed.After(now) || parsed.After(now.Add(maxSnooze)) {
			return i18n.Wrap(ErrInput, `"snoozedUntil" must be in the future and within a year`)
		}
		until = parsed
	}
	archiving := change.Archived != nil && *change.Archived
	var refused error
	found := s.deps.Store.Update(id, func(r *Stored) {
		if archiving && r.Status.Pending() {
			refused = i18n.Wrap(ErrState, "the assistant is still working on this request")
			return
		}
		if change.Pinned != nil {
			r.PinnedAt = ""
			if *change.Pinned {
				r.PinnedAt = stamp(now)
			}
		}
		wasAway := r.ArchivedAt != "" || r.SnoozedUntil != ""
		if change.Archived != nil {
			r.ArchivedAt = ""
			if archiving {
				r.ArchivedAt, r.PinnedAt, r.SnoozedUntil, r.Closed = stamp(now), "", "", true
			}
		}
		if change.SnoozedUntil != nil {
			r.SnoozedUntil = ""
			if !until.IsZero() {
				r.SnoozedUntil, r.ArchivedAt, r.Closed = stamp(until), "", true
			}
		}
		// Bringing a conversation back (an undo, or "bring back now") shows its bubble again when it needs one.
		if wasAway && r.ArchivedAt == "" && r.SnoozedUntil == "" && r.Status.reachesUser() {
			r.Closed = false
		}
	})
	if !found {
		return ErrNotFound
	}
	return refused
}

// sweepShelf wakes the conversations whose snooze is over and puts away the ones that have been done for a week. It
// runs on the wall clock, so what came due while the Mac slept is handled when it wakes. A conversation that needs the
// user, or is being worked on, is never put away, and a pinned one stays.
func (s *Service) sweepShelf() {
	now := s.deps.Now()
	for _, request := range s.deps.Store.List() {
		if until, err := time.Parse(time.RFC3339, request.SnoozedUntil); err == nil && !until.After(now) {
			s.update(request.ID, func(r *Stored) {
				r.SnoozedUntil = ""
				// It comes back to the top, with its bubble if it needed one.
				if r.Status.reachesUser() {
					r.Closed = false
				}
			})
			continue
		}
		if request.PinnedAt != "" || request.ArchivedAt != "" || request.SnoozedUntil != "" {
			continue
		}
		if request.Status != StatusDone && request.Status != StatusIgnored {
			continue
		}
		if updated, err := time.Parse(time.RFC3339, request.UpdatedAt); err == nil && now.Sub(updated) >= settleAfter {
			s.deps.Store.Update(request.ID, func(r *Stored) { r.ArchivedAt, r.Closed = stamp(now), true })
		}
	}
}
