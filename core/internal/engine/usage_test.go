package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type countingCodex struct{ calls int }

func (c *countingCodex) Run(context.Context, Turn) (TurnResult, error) {
	c.calls++
	return TurnResult{}, nil
}

func TestMeteredCountsAndRefusesPastTheLimit(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	path := filepath.Join(t.TempDir(), "usage.json")
	meter, _ := NewUsageMeter(path, func() time.Time { return now })
	inner := &countingCodex{}
	var levels []string
	metered := &Metered{Inner: inner, Meter: meter, Limit: func() int { return 5 }, OnThreshold: func(level string, _, _ int) { levels = append(levels, level) }}

	for i := 0; i < 5; i++ {
		if _, err := metered.Run(context.Background(), Turn{}); err != nil {
			t.Fatal(err)
		}
	}
	var quota *QuotaError
	if _, err := metered.Run(context.Background(), Turn{}); !errors.As(err, &quota) || quota.Limit != 5 {
		t.Fatalf("want a quota error, got %v", err)
	}
	if inner.calls != 5 || len(levels) != 2 || levels[0] != "warning" || levels[1] != "full" {
		t.Fatalf("calls %d levels %v", inner.calls, levels)
	}
	reloaded, _ := NewUsageMeter(path, func() time.Time { return now })
	if reloaded.Today() != 5 {
		t.Fatalf("the count must survive a restart: %d", reloaded.Today())
	}
	tomorrow, _ := NewUsageMeter(path, func() time.Time { return now.Add(24 * time.Hour) })
	if tomorrow.Today() != 0 {
		t.Fatal("a new day starts at zero")
	}
}

// reportingAI reports what it used while its turn runs, as the real runners do, and checks what the meter says then.
type reportingAI struct {
	meter *UsageMeter
	seen  []Tokens
}

func (r *reportingAI) Run(_ context.Context, turn Turn) (TurnResult, error) {
	turn.OnUsage(Tokens{}, &Limits{FiveHour: &Window{UsedPercent: 40}, SeenAt: stamp(time.Now())})
	r.seen = append(r.seen, r.meter.TokensToday()["codex"])
	turn.OnUsage(Tokens{Input: 100, Output: 5}, nil)
	r.seen = append(r.seen, r.meter.TokensToday()["codex"])
	return TurnResult{Tokens: Tokens{Input: 100, Output: 5}}, nil
}

func TestMeteredFollowsTheTurnAndNeverCountsItTwice(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	meter, _ := NewUsageMeter(filepath.Join(t.TempDir(), "usage.json"), func() time.Time { return now })
	changes := 0
	meter.SetOnChange(func() { changes++ })
	ai := &reportingAI{meter: meter}
	metered := &Metered{Inner: ai, Provider: "codex", Meter: meter, Limit: func() int { return 0 }}

	if _, err := metered.Run(context.Background(), Turn{}); err != nil {
		t.Fatal(err)
	}
	// The tokens were already counted when the AI reported them, before its turn ended.
	if !ai.seen[0].IsZero() || ai.seen[1].Input != 100 {
		t.Fatalf("usage did not follow the turn: %+v", ai.seen)
	}
	// And the result of the turn did not add them a second time.
	if got := meter.TokensToday()["codex"]; got.Input != 100 || got.Output != 5 {
		t.Fatalf("counted twice: %+v", got)
	}
	// The screens were told at the start of the turn (one more turn), for the windows, and for the tokens.
	if changes != 3 {
		t.Fatalf("clients were told %d times, want 3", changes)
	}
	if limits := meter.Limits()["codex"]; limits.FiveHour == nil || limits.FiveHour.UsedPercent != 40 {
		t.Fatalf("window not kept: %+v", limits)
	}
}
