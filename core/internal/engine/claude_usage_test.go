package engine

import (
	"testing"
	"time"
)

func TestParsesTheUsageClaudeShows(t *testing.T) {
	now := time.Date(2026, 11, 13, 14, 0, 0, 0, time.UTC)
	text := "You are currently using your subscription\n\nCurrent session: 70% used · resets Nov 13 at 7:10pm (Europe/Paris)\n" +
		"Current week (all models): 77% used · resets Nov 15 at 7pm (Europe/Paris)\nCurrent week (Fable): 5% used · resets Nov 15 at 7pm (Europe/Paris)\n"
	limits := parseClaudeUsage(text, now)
	if limits == nil || limits.FiveHour == nil || limits.Week == nil {
		t.Fatalf("windows not read: %+v", limits)
	}
	if limits.FiveHour.UsedPercent != 70 || limits.Week.UsedPercent != 77 {
		t.Fatalf("shares: %v %v", limits.FiveHour.UsedPercent, limits.Week.UsedPercent)
	}
	// 7:10pm in Paris (UTC+1 in November) is 18:10 UTC; 7pm on the 15th is 18:00 UTC.
	if limits.FiveHour.ResetsAt != stamp(time.Date(2026, 11, 13, 18, 10, 0, 0, time.UTC)) || limits.Week.ResetsAt != stamp(time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("resets: %q %q", limits.FiveHour.ResetsAt, limits.Week.ResetsAt)
	}
}

func TestUsageWithoutADateOrWindowsIsHandled(t *testing.T) {
	now := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC) // 21:00 in Paris
	limits := parseClaudeUsage("Current session: 3% used · resets 7:10pm (Europe/Paris)\n", now)
	if limits == nil || limits.FiveHour == nil || limits.Week != nil {
		t.Fatalf("one window: %+v", limits)
	}
	// 7:10pm already passed today: the next one is tomorrow's.
	if limits.FiveHour.ResetsAt != stamp(time.Date(2026, 11, 14, 18, 10, 0, 0, time.UTC)) {
		t.Fatalf("reset: %q", limits.FiveHour.ResetsAt)
	}
	if parseClaudeUsage("Not logged in", now) != nil {
		t.Fatal("a text without windows must give none")
	}
	// A reset text Pouple cannot read keeps the share and drops only the reset time.
	if got := parseClaudeUsage("Current session: 40% used · resets soon\n", now); got == nil || got.FiveHour.UsedPercent != 40 || got.FiveHour.ResetsAt != "" {
		t.Fatalf("unreadable reset: %+v", got)
	}
}

func TestAJanuaryResetReadInDecemberIsNextYear(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
	got := parseReset("Jan 3 at 9am (Europe/Paris)", now)
	if !got.Equal(time.Date(2027, 1, 3, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v", got)
	}
}
