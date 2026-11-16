package alerts

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func draft(title string, at time.Time) Draft { return Draft{Title: title, At: at} }

func TestAnAlertNeedsATitleAndAMomentInTheFuture(t *testing.T) {
	store, _ := Open("", nil)
	for name, bad := range map[string]Draft{
		"no title":         draft("  ", now.Add(time.Hour)),
		"title too long":   draft(strings.Repeat("a", MaxTitle+1), now.Add(time.Hour)),
		"note too long":    {Title: "x", Note: strings.Repeat("a", MaxNote+1), At: now.Add(time.Hour)},
		"hidden character": draft("pay‮bill", now.Add(time.Hour)),
		"past":             draft("x", now.Add(-time.Minute)),
		"right now":        draft("x", now),
	} {
		if _, err := store.Add(bad, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if len(store.List()) != 0 {
		t.Fatal("a refused alert was kept")
	}
	alert, err := store.Add(Draft{Title: "  Confirm leave  ", Note: "line 1\nline 2", At: now.Add(time.Hour)}, now)
	if err != nil || alert.Title != "Confirm leave" || alert.ID == "" || !alert.CreatedAt.Equal(now) {
		t.Fatalf("alert %+v, err %v", alert, err)
	}
}

func TestAlertsAreListedSoonestFirstAndDueOnesComeOutInOrder(t *testing.T) {
	store, _ := Open("", nil)
	for _, d := range []Draft{draft("late", now.Add(48*time.Hour)), draft("soon", now.Add(time.Hour)), draft("middle", now.Add(24*time.Hour))} {
		if _, err := store.Add(d, now); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(list []Alert) string {
		var out []string
		for _, a := range list {
			out = append(out, a.Title)
		}
		return strings.Join(out, ",")
	}
	if got := titles(store.List()); got != "soon,middle,late" {
		t.Fatalf("list %s", got)
	}
	if next, ok := store.Next(); !ok || !next.Equal(now.Add(time.Hour)) {
		t.Fatalf("next %v %v", next, ok)
	}
	if got := store.Due(now); len(got) != 0 {
		t.Fatalf("nothing is due yet: %v", got)
	}
	if got := titles(store.Due(now.Add(25 * time.Hour))); got != "soon,middle" {
		t.Fatalf("due %s", got)
	}
}

func TestUpdateChangesOnlyWhatIsGivenAndRefusesThePast(t *testing.T) {
	store, _ := Open("", nil)
	alert, _ := store.Add(Draft{Title: "Call", Note: "back", At: now.Add(time.Hour)}, now)
	title, moment := "Call Jocelyn", now.Add(3*time.Hour)
	updated, err := store.Update(alert.ID, Patch{Title: &title, At: &moment}, now)
	if err != nil || updated.Title != "Call Jocelyn" || updated.Note != "back" || !updated.At.Equal(moment) {
		t.Fatalf("updated %+v, err %v", updated, err)
	}
	cleared := ""
	if got, err := store.Update(alert.ID, Patch{Note: &cleared}, now); err != nil || got.Note != "" || got.Title != "Call Jocelyn" || !got.At.Equal(moment) {
		t.Fatalf("clearing the note changed more than the note: %+v, err %v", got, err)
	}
	past := now.Add(-time.Hour)
	if _, err := store.Update(alert.ID, Patch{At: &past}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a past moment: %v", err)
	}
	empty := " "
	if _, err := store.Update(alert.ID, Patch{Title: &empty}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty title: %v", err)
	}
	if _, err := store.Update("missing", Patch{}, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown alert: %v", err)
	}
	if got := store.List()[0]; got.Title != "Call Jocelyn" {
		t.Fatalf("a refused change was kept: %+v", got)
	}
}

func TestRemoveReportsWhetherTheAlertExisted(t *testing.T) {
	store, _ := Open("", nil)
	alert, _ := store.Add(draft("x", now.Add(time.Hour)), now)
	if !store.Remove(alert.ID) || store.Remove(alert.ID) {
		t.Fatal("remove must succeed once")
	}
}

func TestTheNumberOfUpcomingAlertsIsBounded(t *testing.T) {
	store, _ := Open("", nil)
	for i := 0; i < MaxAlerts; i++ {
		if _, err := store.Add(draft("x", now.Add(time.Hour)), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Add(draft("one more", now.Add(time.Hour)), now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err %v", err)
	}
}

func TestAlertsSurviveARestartAndChangesAreAnnounced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "alerts.json")
	store, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	store.OnChange(func() { changes++ })
	alert, _ := store.Add(Draft{Title: "Pay", At: now.Add(time.Hour), RequestID: "req1"}, now)
	store.Remove("nope")
	if changes != 1 {
		t.Fatalf("changes %d: only a real change is announced", changes)
	}
	again, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := again.List()
	if len(got) != 1 || got[0].ID != alert.ID || got[0].RequestID != "req1" || !got[0].At.Equal(alert.At) {
		t.Fatalf("reloaded %+v", got)
	}
}

func TestParseWhenReadsLocalTimeInTheGivenZoneOrAFullTimestamp(t *testing.T) {
	paris := time.FixedZone("CET", 3600)
	local, err := ParseWhen("2026-11-20T09:00", paris)
	if err != nil || !local.Equal(time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("local %v %v", local, err)
	}
	full, err := ParseWhen("2026-11-20T09:00:00+05:00", paris)
	if err != nil || !full.Equal(time.Date(2026, 11, 20, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("full %v %v", full, err)
	}
	for _, bad := range []string{"", "tomorrow", "2026-11-20", "2026-13-40T09:00"} {
		if _, err := ParseWhen(bad, paris); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
