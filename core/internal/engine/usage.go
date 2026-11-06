package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ErrQuota is returned when the day's limit of Codex turns is used up.
type QuotaError struct{ Limit int }

func (e *QuotaError) Error() string {
	return fmt.Sprintf("The daily limit of %d Codex turns is reached. Raise it in the settings, or wait until tomorrow.", e.Limit)
}

// UsageMeter counts the Codex turns of the day. Each turn costs quota whether or not it succeeds, so it is counted
// when it starts. Counts are kept for two weeks in a small file.
type UsageMeter struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	days map[string]int
}

const keptDays = 14

func NewUsageMeter(path string, now func() time.Time) (*UsageMeter, error) {
	if now == nil {
		now = time.Now
	}
	meter := &UsageMeter{path: path, now: now, days: map[string]int{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return meter, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Days map[string]int `json:"days"`
	}
	if json.Unmarshal(data, &file) == nil && file.Days != nil {
		meter.days = file.Days
	}
	return meter, nil
}

func (m *UsageMeter) day() string { return m.now().Format("2006-01-02") }

// Today is the number of turns started today.
func (m *UsageMeter) Today() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.days[m.day()]
}

// Record counts one more turn and returns today's total.
func (m *UsageMeter) Record() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	today := m.day()
	m.days[today]++
	if len(m.days) > keptDays {
		keys := make([]string, 0, len(m.days))
		for key := range m.days {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys[:len(keys)-keptDays] {
			delete(m.days, key)
		}
	}
	return m.days[today], m.save()
}

func (m *UsageMeter) save() error {
	data, err := json.Marshal(map[string]any{"days": m.days})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	temporary := m.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, m.path)
}

// Metered wraps a Codex so every turn is counted and refused once the day's limit is used.
type Metered struct {
	Inner Codex
	Meter *UsageMeter
	// Limit returns the turns allowed per day; 0 or less means no limit.
	Limit func() int
	// OnThreshold is called once a day at 80 % of the limit, and once when it is all used.
	OnThreshold func(level string, used, limit int)
}

func (m *Metered) Run(ctx context.Context, turn Turn) (TurnResult, error) {
	limit := m.Limit()
	if limit > 0 && m.Meter.Today() >= limit {
		return TurnResult{}, &QuotaError{Limit: limit}
	}
	used, _ := m.Meter.Record()
	if limit > 0 && m.OnThreshold != nil {
		switch {
		case used == limit:
			m.OnThreshold("full", used, limit)
		case used == (limit*8+9)/10:
			m.OnThreshold("warning", used, limit)
		}
	}
	return m.Inner.Run(ctx, turn)
}
