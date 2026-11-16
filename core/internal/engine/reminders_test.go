package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// reminders are the suggestions of the AI in the shape of its answer.
func suggest(items ...[3]string) map[string]any {
	list := []any{}
	for _, item := range items {
		list = append(list, map[string]any{"label": item[0], "note": item[1], "at": item[2]})
	}
	return map[string]any{"outcome": Answered, "summary": "**Confirm your leave to Jocelyn by Friday**", "reminders": list}
}

// The harness clock is 2026-11-05 10:00 UTC (a Thursday).
const (
	tomorrow9 = "2026-11-06T09:00"
	friday17  = "2026-11-06T17:00"
	past      = "2026-11-05T08:00"
)

func TestSuggestedRemindersWaitForThePickAndDropWhatIsPastOrRepeated(t *testing.T) {
	h := newHarness(t, reply(suggest(
		[3]string{"Confirm leave", "", tomorrow9}, [3]string{"Confirm leave", "", tomorrow9},
		[3]string{"Too late", "", past}, [3]string{"Last call", "Before the weekend", friday17},
	)))
	h.receive(message("Can you confirm your leave by Friday?"))
	request := only(t, h)
	offer := request.Reminders
	if offer == nil || offer.Status != ReminderProposed || offer.ID == "" || len(offer.Items) != 2 {
		t.Fatalf("offer %+v", offer)
	}
	if offer.Items[0].Label != "Confirm leave" || offer.Items[0].At != "2026-11-06T09:00:00Z" || offer.Items[1].ID != "r2" || offer.Items[1].Note != "Before the weekend" {
		t.Fatalf("items %+v", offer.Items)
	}
	if len(h.svc.Snapshot().Alerts) != 0 {
		t.Fatal("nothing may be set before the user picks")
	}
}

func TestAnAnswerWithoutReminderOrWithOnlyPastOnesLeavesNone(t *testing.T) {
	h := newHarness(t, reply(suggest([3]string{"Too late", "", past})), reply(map[string]any{"outcome": NotARequest}))
	h.receive(message("Can you confirm your leave by Friday?"))
	if request := only(t, h); request.Reminders != nil {
		t.Fatalf("only past suggestions made an offer: %+v", request.Reminders)
	}
}

func TestAcceptingSetsTheTickedAlertsAtTheChosenTimes(t *testing.T) {
	h := newHarness(t, reply(suggest([3]string{"Confirm leave", "to Jocelyn", tomorrow9}, [3]string{"Last call", "", friday17}, [3]string{"Third", "", "2026-11-07T10:00"})))
	h.receive(message("Can you confirm your leave by Friday?"))
	request := only(t, h)
	offer := request.Reminders

	picks := []ReminderPick{{ID: "r1", At: "2026-11-06T08:30"}, {ID: "r2"}}
	if err := h.svc.DecideReminders(request.ID, true, offer.ID, picks); err != nil {
		t.Fatal(err)
	}
	alerts := h.svc.Snapshot().Alerts
	if len(alerts) != 2 || alerts[0].Title != "Confirm leave" || alerts[0].Note != "to Jocelyn" || alerts[0].RequestID != request.ID ||
		!alerts[0].At.Equal(time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)) || !alerts[1].At.Equal(time.Date(2026, 11, 6, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("alerts %+v", alerts)
	}
	decided := only(t, h)
	if decided.Reminders.Status != ReminderAccepted || strings.Join(decided.Reminders.Chosen, ",") != "r1,r2" {
		t.Fatalf("offer %+v", decided.Reminders)
	}
	if last := decided.Entries[len(decided.Entries)-1]; last.Role != "user" || !strings.Contains(last.Text, "Confirm leave") {
		t.Fatalf("the journal does not say what was set: %+v", last)
	}
	// A suggestion is decided once.
	if err := h.svc.DecideReminders(request.ID, true, offer.ID, picks[:1]); !errors.Is(err, ErrState) {
		t.Fatalf("a second decision: %v", err)
	}
}

func TestADecisionThatCannotBeMadeSetsNothing(t *testing.T) {
	h := newHarness(t, reply(suggest([3]string{"A", "", tomorrow9}, [3]string{"B", "", friday17})))
	h.receive(message("Can you confirm your leave by Friday?"))
	request := only(t, h)
	id := request.Reminders.ID
	cases := map[string]struct {
		offer string
		picks []ReminderPick
		want  error
	}{
		"nothing ticked":         {id, nil, ErrInput},
		"unknown option":         {id, []ReminderPick{{ID: "r9"}}, ErrInput},
		"option twice":           {id, []ReminderPick{{ID: "r1"}, {ID: "r1"}}, ErrInput},
		"a time in the past":     {id, []ReminderPick{{ID: "r1"}, {ID: "r2", At: past}}, ErrInput},
		"a time that is no time": {id, []ReminderPick{{ID: "r1", At: "soon"}}, ErrInput},
		"another suggestion":     {"stale", []ReminderPick{{ID: "r1"}}, ErrState},
		"unnamed suggestion":     {"", []ReminderPick{{ID: "r1"}}, ErrInput},
	}
	for name, c := range cases {
		if err := h.svc.DecideReminders(request.ID, true, c.offer, c.picks); !errors.Is(err, c.want) {
			t.Errorf("%s: err %v, want %v", name, err, c.want)
		}
	}
	if len(h.svc.Snapshot().Alerts) != 0 || only(t, h).Reminders.Status != ReminderProposed {
		t.Fatal("a refused decision changed something")
	}
	if err := h.svc.DecideReminders(request.ID, false, id, nil); err != nil {
		t.Fatal(err)
	}
	if only(t, h).Reminders.Status != ReminderDeclined || len(h.svc.Snapshot().Alerts) != 0 {
		t.Fatal("declining must set nothing")
	}
}

func TestAlertsYouMakeByHandAreCheckedLikeTheOthers(t *testing.T) {
	h := newHarness(t)
	t.Cleanup(h.svc.Idle)
	alert, err := h.svc.CreateAlert("Stand-up", "", "2026-11-06T09:30")
	if err != nil || !alert.At.Equal(time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)) || alert.RequestID != "" {
		t.Fatalf("alert %+v err %v", alert, err)
	}
	for _, bad := range [][3]string{{"", "", tomorrow9}, {"x", "", past}, {"x", "", "whenever"}} {
		if _, err := h.svc.CreateAlert(bad[0], bad[1], bad[2]); !errors.Is(err, ErrInput) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	later := "2026-11-06T11:00"
	if moved, err := h.svc.UpdateAlert(alert.ID, AlertEdit{At: &later}); err != nil || moved.At.Hour() != 11 {
		t.Fatalf("moved %+v err %v", moved, err)
	}
	if _, err := h.svc.UpdateAlert("missing", AlertEdit{At: &later}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	if err := h.svc.DeleteAlert(alert.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.DeleteAlert(alert.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	if got := h.svc.Snapshot().Alerts; got == nil || len(got) != 0 {
		t.Fatalf("the snapshot must carry an empty list, not %#v", got)
	}
}

func TestADueAlertBecomesAReadyBubbleAndIsForgotten(t *testing.T) {
	h := newHarness(t)
	t.Cleanup(h.svc.Idle)
	alert, err := h.svc.CreateAlert("Confirm leave to Jocelyn", "From the Slack thread", "2026-11-06T09:00")
	if err != nil {
		t.Fatal(err)
	}
	h.svc.fireDueAlerts()
	if len(h.svc.Snapshot().Requests) != 0 {
		t.Fatal("an alert fired before its time")
	}
	h.now = time.Date(2026, 11, 6, 9, 0, 30, 0, time.UTC)
	h.svc.fireDueAlerts()
	request := only(t, h)
	if request.ID != "reminder-"+alert.ID || request.Source.Origin != OriginReminder || request.Status != StatusReady || !request.Engaged || request.Closed ||
		request.Text != "Confirm leave to Jocelyn" || request.Summary == nil || *request.Summary != "From the Slack thread" {
		t.Fatalf("reminder request %+v", request)
	}
	if len(request.Entries) != 1 {
		t.Fatalf("an alert on time is not called missed: %+v", request.Entries)
	}
	if len(h.svc.Snapshot().Alerts) != 0 {
		t.Fatal("a fired alert stays in the upcoming list")
	}
	// Firing again changes nothing.
	h.svc.fireDueAlerts()
	if len(h.svc.Snapshot().Requests) != 1 {
		t.Fatal("a reminder fired twice")
	}
}

func TestAnAlertThatCameDueWhilePoupleWasClosedSaysSo(t *testing.T) {
	h := newHarness(t)
	t.Cleanup(h.svc.Idle)
	if _, err := h.svc.CreateAlert("Pay the invoice", "", "2026-11-06T09:00"); err != nil {
		t.Fatal(err)
	}
	h.now = time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)
	h.svc.fireDueAlerts()
	request := only(t, h)
	if len(request.Entries) != 2 || request.Entries[1].Role != "system" || !strings.Contains(request.Entries[1].Text, "while Pouple was not running") {
		t.Fatalf("entries %+v", request.Entries)
	}
}

func TestTheWatcherFiresAnAlertWhenItsTimeComes(t *testing.T) {
	h := newHarness(t)
	t.Cleanup(h.svc.Idle)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go h.svc.watchAlerts(ctx)
	if _, err := h.svc.CreateAlert("Stand-up", "", "2026-11-06T09:30"); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.now = time.Date(2026, 11, 6, 9, 31, 0, 0, time.UTC)
	h.mu.Unlock()
	// A change wakes the watcher; the clock is read again at once.
	h.svc.alertWake <- struct{}{}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.svc.Snapshot().Requests) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.svc.Snapshot().Requests; len(got) != 1 || got[0].Source.Origin != OriginReminder {
		t.Fatalf("requests %+v", got)
	}
}

func TestAReminderIsNotInvestigatedAgain(t *testing.T) {
	h := newHarness(t)
	t.Cleanup(h.svc.Idle)
	if _, err := h.svc.CreateAlert("Stand-up", "", "2026-11-06T09:30"); err != nil {
		t.Fatal(err)
	}
	h.now = time.Date(2026, 11, 6, 9, 31, 0, 0, time.UTC)
	h.svc.fireDueAlerts()
	if err := h.svc.Retry(only(t, h).ID); !errors.Is(err, ErrState) {
		t.Fatalf("err %v", err)
	}
	if h.codex.count() != 0 {
		t.Fatal("an AI turn ran for a reminder")
	}
}

func TestEveryTurnTellsTheAIWhatTimeItIs(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": Answered}))
	h.receive(message("Can you confirm your leave by Friday?"))
	prompt := h.codex.turns[0].Prompt
	if !strings.HasPrefix(prompt, "Current date and time on Arthur's computer: Thursday 5 November 2026, 10:00 (UTC).") || !strings.Contains(prompt, `"reminders"`) {
		t.Fatalf("prompt starts with %q", prompt[:min(len(prompt), 160)])
	}
}
