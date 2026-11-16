package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/alerts"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// ReminderStatus is where a suggestion of reminders stands.
type ReminderStatus string

const (
	ReminderProposed ReminderStatus = "proposed"
	ReminderAccepted ReminderStatus = "accepted"
	ReminderDeclined ReminderStatus = "declined"
)

// ReminderOption is one alert the assistant suggests, as the user sees it and may change its time.
type ReminderOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
	// At is the suggested moment, RFC 3339 with the zone of this computer.
	At string `json:"at"`
}

// ReminderOffer is what the assistant suggests to be reminded of. Nothing is set until the user picks.
type ReminderOffer struct {
	// ID names this exact suggestion (a fingerprint): a decision that carries it cannot answer another one.
	ID     string           `json:"id"`
	Status ReminderStatus   `json:"status"`
	Items  []ReminderOption `json:"items"`
	// Chosen are the ids of the options the user turned into alerts, once accepted.
	Chosen []string `json:"chosen,omitempty"`
}

// ReminderPick is one option the user ticked, with the moment they want for it ("" keeps the suggested one).
type ReminderPick struct {
	ID string `json:"id"`
	At string `json:"at,omitempty"`
}

// offerOf turns what the AI suggested into an offer for the user: moments already past are dropped (the AI may have
// counted wrong), and so is a suggestion that repeats another. It returns nil when nothing usable is left.
func offerOf(suggested []OutcomeReminder, now time.Time) *ReminderOffer {
	var items []ReminderOption
	seen := map[string]bool{}
	for _, suggestion := range suggested {
		at, err := alerts.ParseWhen(suggestion.At, now.Location())
		key := suggestion.Label + "\x00" + suggestion.At
		if err != nil || !at.After(now) || seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, ReminderOption{ID: fmt.Sprintf("r%d", len(items)+1), Label: suggestion.Label, Note: suggestion.Note, At: at.Format(time.RFC3339)})
	}
	if len(items) == 0 {
		return nil
	}
	offer := &ReminderOffer{Status: ReminderProposed, Items: items}
	offer.ID = offerFingerprint(*offer)
	return offer
}

func offerFingerprint(offer ReminderOffer) string {
	hash := sha256.New()
	for _, item := range offer.Items {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x01", item.ID, item.Label, item.Note, item.At)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

// attachReminders shows the reminders an answer suggested. Only an answer that reaches the user can carry them. A new
// answer without any takes away a suggestion nobody picked from, but never the record of what was chosen.
func (s *Service) attachReminders(id string, outcome Outcome) {
	if !slices.Contains([]string{Answered, NeedsInput, ProposesChange, ProposesPlan}, outcome.Outcome) {
		return
	}
	offer := offerOf(outcome.Reminders, s.deps.Now())
	s.update(id, func(r *Stored) {
		switch {
		case offer != nil:
			r.Reminders = offer
		case r.Reminders != nil && r.Reminders.Status == ReminderProposed:
			r.Reminders = nil
		}
	})
}

// clockNote is what each investigation turn starts with.
func (s *Service) clockNote() string { return s.deps.Persona().clockNote(s.deps.Now()) }

// DecideReminders sets the alerts the user ticked, or declines the suggestion. offerID names the suggestion the user read;
// it must be the one waiting now. Each pick may carry its own moment. All of them are set, or none.
func (s *Service) DecideReminders(id string, approve bool, offerID string, picks []ReminderPick) error {
	if offerID == "" {
		return i18n.Wrap(ErrInput, "the suggestion that was read must be named")
	}
	// One decision at a time: two clicks cannot set the same alerts twice.
	s.reminderMu.Lock()
	defer s.reminderMu.Unlock()
	request, err := s.require(id)
	if err != nil {
		return err
	}
	offer := request.Reminders
	switch {
	case offer == nil || offer.Status != ReminderProposed:
		return i18n.Wrap(ErrState, "no reminder is waiting for a choice")
	case offer.ID != offerID:
		return i18n.Wrap(ErrState, "the suggested reminders changed since you read them")
	}
	if !approve {
		if !s.settleOffer(id, offerID, func(r *Stored) {
			r.Reminders.Status = ReminderDeclined
			r.Entries = append(r.Entries, s.entry("user", "text", s.text("Declined the suggested reminders")))
		}) {
			return i18n.Wrap(ErrState, "the suggested reminders changed since you read them")
		}
		return nil
	}
	drafts, chosen, err := s.draftsFor(*offer, picks, id)
	if err != nil {
		return err
	}
	var created []alerts.Alert
	undo := func() {
		for _, done := range created {
			s.deps.Alerts.Remove(done.ID)
		}
	}
	for _, draft := range drafts {
		alert, err := s.deps.Alerts.Add(draft, s.deps.Now())
		if err != nil {
			undo()
			return alertError(err)
		}
		created = append(created, alert)
	}
	if !s.settleOffer(id, offerID, func(r *Stored) {
		r.Reminders.Status, r.Reminders.Chosen = ReminderAccepted, chosen
		r.Entries = append(r.Entries, s.entry("user", "text", s.text("Set the reminders:\n%s", s.alertLines(created))))
	}) {
		undo()
		return i18n.Wrap(ErrState, "the suggested reminders changed since you read them")
	}
	return nil
}

// settleOffer applies change only while the suggestion offerID still waits for a choice, and reports whether it did. A
// new answer can replace or take away the suggestion while the alerts are being set: reminderMu does not cover it.
func (s *Service) settleOffer(id, offerID string, change func(*Stored)) bool {
	settled := false
	s.update(id, func(r *Stored) {
		if r.Reminders == nil || r.Reminders.ID != offerID || r.Reminders.Status != ReminderProposed {
			return
		}
		change(r)
		settled = true
	})
	return settled
}

// draftsFor checks the user's picks against the suggestion and turns them into alerts to set.
func (s *Service) draftsFor(offer ReminderOffer, picks []ReminderPick, requestID string) ([]alerts.Draft, []string, error) {
	if len(picks) == 0 {
		return nil, nil, i18n.Wrap(ErrInput, "choose at least one reminder, each only once")
	}
	now := s.deps.Now()
	var drafts []alerts.Draft
	var chosen []string
	for _, pick := range picks {
		index := slices.IndexFunc(offer.Items, func(item ReminderOption) bool { return item.ID == pick.ID })
		if index < 0 || slices.Contains(chosen, pick.ID) {
			return nil, nil, i18n.Wrap(ErrInput, "choose at least one reminder, each only once")
		}
		item := offer.Items[index]
		moment := item.At
		if strings.TrimSpace(pick.At) != "" {
			moment = pick.At
		}
		at, err := alerts.ParseWhen(moment, now.Location())
		if err != nil {
			return nil, nil, alertError(err)
		}
		if !at.After(now) {
			return nil, nil, i18n.Wrap(ErrInput, "“%s” is already past: choose a later time", item.Label)
		}
		drafts = append(drafts, alerts.Draft{Title: item.Label, Note: item.Note, At: at, RequestID: requestID})
		chosen = append(chosen, pick.ID)
	}
	return drafts, chosen, nil
}

// alertLines lists alerts one per line, for the journal of a conversation.
func (s *Service) alertLines(set []alerts.Alert) string {
	lines := make([]string, len(set))
	for i, alert := range set {
		lines[i] = "- " + alert.Title + " · " + s.whenText(alert.At)
	}
	return strings.Join(lines, "\n")
}

// whenText says a moment the way people read it: the day, then the time of day.
func (s *Service) whenText(at time.Time) string {
	at = at.In(s.deps.Now().Location())
	return i18n.Date(s.deps.Language(), at) + " · " + i18n.Time(at)
}

// AlertEdit is what the user changes of an alert; a nil field stays as it is. At is read like ReminderPick.At.
type AlertEdit struct {
	Title *string
	Note  *string
	At    *string
}

// CreateAlert sets an alert the user wrote by hand. at is a local date and time ("2026-11-20T09:00") or RFC 3339.
func (s *Service) CreateAlert(title, note, at string) (alerts.Alert, error) {
	moment, err := alerts.ParseWhen(at, s.deps.Now().Location())
	if err != nil {
		return alerts.Alert{}, alertError(err)
	}
	alert, err := s.deps.Alerts.Add(alerts.Draft{Title: title, Note: note, At: moment}, s.deps.Now())
	return alert, alertError(err)
}

// UpdateAlert changes an alert that has not fired yet.
func (s *Service) UpdateAlert(id string, edit AlertEdit) (alerts.Alert, error) {
	patch := alerts.Patch{Title: edit.Title, Note: edit.Note}
	if edit.At != nil {
		moment, err := alerts.ParseWhen(*edit.At, s.deps.Now().Location())
		if err != nil {
			return alerts.Alert{}, alertError(err)
		}
		patch.At = &moment
	}
	alert, err := s.deps.Alerts.Update(id, patch, s.deps.Now())
	return alert, alertError(err)
}

// DeleteAlert cancels an alert that has not fired yet.
func (s *Service) DeleteAlert(id string) error {
	if !s.deps.Alerts.Remove(id) {
		return ErrNotFound
	}
	return nil
}

// alertError maps what the alert store refuses to the errors the API understands.
func alertError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, alerts.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, alerts.ErrInvalid):
		return i18n.Wrap(ErrInput, "%s", err)
	}
	return err
}
