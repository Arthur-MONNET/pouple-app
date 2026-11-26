package engine

import (
	"errors"
	"testing"
	"time"
)

func yes() *bool { return ptr(true) }
func no() *bool  { return ptr(false) }

// shelved starts one request that finished with an answer, and returns it with its id.
func shelved(t *testing.T) (*harness, Request) {
	t.Helper()
	h := newHarness(t, reply(map[string]any{"outcome": Answered, "reply": "Done"}))
	h.receive(message("Can you check the retests?"))
	return h, only(t, h)
}

func (h *harness) setStatus(id string, status Status, updated time.Time) {
	h.svc.deps.Store.Update(id, func(r *Stored) { r.Status, r.UpdatedAt = status, stamp(updated) })
}

func (h *harness) get(t *testing.T, id string) Request {
	t.Helper()
	stored, ok := h.svc.deps.Store.Get(id)
	if !ok {
		t.Fatalf("request %s is gone", id)
	}
	return stored.Request
}

func TestPinningAndUnpinningKeepsTheUpdateTime(t *testing.T) {
	h, request := shelved(t)
	h.now = h.now.Add(time.Hour)
	if err := h.svc.Arrange(request.ID, Arrangement{Pinned: yes()}); err != nil {
		t.Fatal(err)
	}
	pinned := h.get(t, request.ID)
	if pinned.PinnedAt != stamp(h.now) || pinned.UpdatedAt != request.UpdatedAt {
		t.Fatalf("pinning changed more than the pin: %+v", pinned)
	}
	if err := h.svc.Arrange(request.ID, Arrangement{Pinned: no()}); err != nil {
		t.Fatal(err)
	}
	if h.get(t, request.ID).PinnedAt != "" {
		t.Fatal("unpinning left the pin")
	}
}

func TestArchivingClosesAndUnpinsAndCanBeUndone(t *testing.T) {
	h, request := shelved(t)
	_ = h.svc.Arrange(request.ID, Arrangement{Pinned: yes()})
	if err := h.svc.Arrange(request.ID, Arrangement{Archived: yes()}); err != nil {
		t.Fatal(err)
	}
	archived := h.get(t, request.ID)
	if archived.ArchivedAt == "" || archived.PinnedAt != "" || !archived.Closed {
		t.Fatalf("archiving did not put the request away: %+v", archived)
	}
	if err := h.svc.Arrange(request.ID, Arrangement{Archived: no()}); err != nil {
		t.Fatal(err)
	}
	if back := h.get(t, request.ID); back.ArchivedAt != "" || back.Closed {
		t.Fatalf("undoing the archive left it away or its bubble closed: %+v", back)
	}
}

func TestBringingASnoozedConversationBackNowShowsItAgain(t *testing.T) {
	h, request := shelved(t)
	until := stamp(h.now.Add(time.Hour))
	_ = h.svc.Arrange(request.ID, Arrangement{SnoozedUntil: &until})
	none := ""
	if err := h.svc.Arrange(request.ID, Arrangement{SnoozedUntil: &none}); err != nil {
		t.Fatal(err)
	}
	if back := h.get(t, request.ID); back.SnoozedUntil != "" || back.Closed {
		t.Fatalf("the conversation did not come back: %+v", back)
	}
}

func TestARequestBeingWorkedOnCannotBeArchived(t *testing.T) {
	h, request := shelved(t)
	h.setStatus(request.ID, StatusAnalyzing, h.now)
	err := h.svc.Arrange(request.ID, Arrangement{Archived: yes()})
	if !errors.Is(err, ErrState) {
		t.Fatalf("archiving a working request: %v", err)
	}
	if h.get(t, request.ID).ArchivedAt != "" {
		t.Fatal("a working request was archived")
	}
}

func TestSnoozingNeedsAFutureDateWithinAYear(t *testing.T) {
	h, request := shelved(t)
	for name, value := range map[string]string{
		"not a date": "tomorrow",
		"the past":   stamp(h.now.Add(-time.Minute)),
		"now":        stamp(h.now),
		"too far":    stamp(h.now.Add(maxSnooze + time.Hour)),
	} {
		if err := h.svc.Arrange(request.ID, Arrangement{SnoozedUntil: &value}); !errors.Is(err, ErrInput) {
			t.Errorf("%s: want an input error, got %v", name, err)
		}
	}
	if h.get(t, request.ID).SnoozedUntil != "" {
		t.Fatal("a refused snooze was kept")
	}
	if err := h.svc.Arrange("missing", Arrangement{Pinned: yes()}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown request: %v", err)
	}
}

func TestSnoozedConversationComesBackWithItsBubbleWhenTheMomentPasses(t *testing.T) {
	h, request := shelved(t)
	until := stamp(h.now.Add(2 * time.Hour))
	if err := h.svc.Arrange(request.ID, Arrangement{SnoozedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	if snoozed := h.get(t, request.ID); snoozed.SnoozedUntil != until || !snoozed.Closed {
		t.Fatalf("snooze: %+v", snoozed)
	}

	h.now = h.now.Add(time.Hour)
	h.svc.sweepShelf()
	if h.get(t, request.ID).SnoozedUntil == "" {
		t.Fatal("it woke before its time")
	}

	h.now = h.now.Add(time.Hour)
	h.svc.sweepShelf()
	awake := h.get(t, request.ID)
	if awake.SnoozedUntil != "" || awake.Closed || awake.UpdatedAt != stamp(h.now) {
		t.Fatalf("a snooze that ended did not bring the conversation back to the top: %+v", awake)
	}
}

func TestAnythingThatReachesTheUserBringsAnArchivedOrSnoozedConversationBack(t *testing.T) {
	h, request := shelved(t)
	until := stamp(h.now.Add(time.Hour))
	_ = h.svc.Arrange(request.ID, Arrangement{SnoozedUntil: &until})
	h.svc.update(request.ID, func(r *Stored) { r.Status = StatusAwaitingUser })
	if back := h.get(t, request.ID); back.SnoozedUntil != "" || back.Closed {
		t.Fatalf("a question stayed snoozed: %+v", back)
	}

	_ = h.svc.Arrange(request.ID, Arrangement{Archived: yes()})
	h.svc.update(request.ID, func(r *Stored) { r.Status = StatusFailed })
	if back := h.get(t, request.ID); back.ArchivedAt != "" || back.Closed {
		t.Fatalf("a failure stayed archived: %+v", back)
	}
}

func TestADoneConversationPutsItselfAwayAfterAWeekButNothingElseDoes(t *testing.T) {
	h, request := shelved(t)
	old := h.now.Add(-settleAfter - time.Hour)
	h.setStatus(request.ID, StatusDone, old)
	h.svc.sweepShelf()
	settled := h.get(t, request.ID)
	if settled.ArchivedAt != stamp(h.now) || !settled.Closed || settled.UpdatedAt != stamp(old) {
		t.Fatalf("a week-old done conversation was not put away quietly: %+v", settled)
	}

	// Whatever still needs the user, is being worked on, or is pinned stays, however old.
	for _, status := range []Status{StatusAwaitingUser, StatusReady, StatusFailed, StatusQueued, StatusAnalyzing, StatusApplying} {
		h.svc.deps.Store.Update(request.ID, func(r *Stored) { r.ArchivedAt, r.Closed = "", false })
		h.setStatus(request.ID, status, old)
		h.svc.sweepShelf()
		if h.get(t, request.ID).ArchivedAt != "" {
			t.Errorf("a %s conversation was put away", status)
		}
	}
	h.svc.deps.Store.Update(request.ID, func(r *Stored) { r.PinnedAt = stamp(old) })
	h.setStatus(request.ID, StatusDone, old)
	h.svc.sweepShelf()
	if h.get(t, request.ID).ArchivedAt != "" {
		t.Error("a pinned conversation was put away")
	}
}

func TestADoneConversationOfLessThanAWeekStays(t *testing.T) {
	h, request := shelved(t)
	h.setStatus(request.ID, StatusDone, h.now.Add(-settleAfter+time.Hour))
	h.svc.sweepShelf()
	if h.get(t, request.ID).ArchivedAt != "" {
		t.Fatal("a conversation done for less than a week was put away")
	}
}
