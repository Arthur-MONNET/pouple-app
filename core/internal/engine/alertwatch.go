package engine

import (
	"context"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/alerts"
)

const (
	// alertCheckEvery bounds how long a due alert can wait. Go's timers stop while the Mac sleeps, so the wall clock is
	// looked at again at least this often, and an alert due during sleep fires when the Mac wakes.
	alertCheckEvery = 20 * time.Second
	// alertLateAfter is how late an alert must be to be called missed in its conversation.
	alertLateAfter = 2 * time.Minute
)

// watchAlerts fires the alerts that come due, until ctx ends. It is started by Run, and also fires what came due while
// Pouple was closed as soon as it starts.
func (s *Service) watchAlerts(ctx context.Context) {
	for ctx.Err() == nil {
		s.fireDueAlerts()
		// The same clock look wakes the snoozed conversations and puts away the ones that are done.
		s.sweepShelf()
		wait := alertCheckEvery
		if next, ok := s.deps.Alerts.Next(); ok {
			if until := next.Sub(s.deps.Now()); until < wait {
				wait = max(until, 0)
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-timer.C:
		case <-s.alertWake:
		}
		timer.Stop()
	}
}

// fireDueAlerts turns each due alert into a conversation, then forgets the alert. The conversation's id comes from the
// alert's, so an alert that could not be forgotten (a crash between the two steps) never opens a second conversation.
func (s *Service) fireDueAlerts() {
	now := s.deps.Now()
	for _, alert := range s.deps.Alerts.Due(now) {
		id := reminderRequestID(alert)
		if _, exists := s.deps.Store.Get(id); !exists {
			s.deps.Store.Add(s.reminderRequest(alert, now))
			s.alert(id, alert.Title)
		}
		s.deps.Alerts.Remove(alert.ID)
	}
}

func reminderRequestID(alert alerts.Alert) string { return "reminder-" + alert.ID }

// reminderRequest is the conversation an alert becomes: ready, and already engaged, so its bubble shows at once and stays
// until the user presses Done. No AI is involved: the text is the user's own.
func (s *Service) reminderRequest(alert alerts.Alert, now time.Time) Stored {
	received := stamp(now)
	text := alert.Title
	summary := alert.Note
	if summary == "" {
		summary = s.text("The reminder you set (%s).", s.whenText(alert.At))
	}
	entries := []Entry{s.entry("assistant", "text", summary)}
	if now.Sub(alert.At) > alertLateAfter {
		entries = append(entries, s.entry("system", "text", s.text("This reminder was due at %s, while the Mac was asleep or Pouple was not running.", s.whenText(alert.At))))
	}
	source := Source{Origin: OriginReminder, ChannelID: "reminders", SenderID: "pouple", SenderName: "Pouple", RelatedRequestID: alert.RequestID}
	return Stored{Request: Request{
		ID: reminderRequestID(alert), Source: source, Text: text, ReceivedAt: received, UpdatedAt: received,
		Status: StatusReady, Summary: &summary, Questions: []Question{}, Entries: entries, Engaged: true,
	}}
}
