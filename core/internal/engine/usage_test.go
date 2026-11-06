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
