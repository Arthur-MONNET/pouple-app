package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// QuotaError is returned when the day's limit of turns is used up.
type QuotaError struct{ Limit int }

func (e *QuotaError) Error() string {
	return e.ErrorText(i18n.EN)
}

func (e *QuotaError) ErrorText(lang i18n.Lang) string {
	return i18n.T(lang, "The daily limit of %d turns is reached. Raise it in the settings, or wait until tomorrow.", e.Limit)
}

// UsageMeter counts the turns of the day, whichever AI runs them. Each turn costs quota whether or not it succeeds, so it is counted
// when it starts. It also adds up the tokens of each day and keeps each AI's last reported usage windows. Counts are
// kept for two weeks in a small file.
type UsageMeter struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	days map[string]int
	// tokens are by day, then by AI.
	tokens map[string]map[providers.ID]Tokens
	limits map[providers.ID]Limits
}

const keptDays = 14

func NewUsageMeter(path string, now func() time.Time) (*UsageMeter, error) {
	if now == nil {
		now = time.Now
	}
	meter := &UsageMeter{path: path, now: now, days: map[string]int{}, tokens: map[string]map[providers.ID]Tokens{}, limits: map[providers.ID]Limits{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return meter, nil
	}
	if err != nil {
		return nil, err
	}
	var file usageFile
	if json.Unmarshal(data, &file) == nil {
		if file.Days != nil {
			meter.days = file.Days
		}
		if file.TokensByAI != nil {
			meter.tokens = file.TokensByAI
		}
		if file.Limits != nil {
			meter.limits = file.Limits
		}
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

// usageFile is the meter on disk. Files written before tokens and windows were kept hold days only. A first version
// kept the tokens of a day without saying which AI used them ("tokens"): they cannot be told apart, so they are left out.
type usageFile struct {
	Days       map[string]int                     `json:"days"`
	TokensByAI map[string]map[providers.ID]Tokens `json:"tokensByAI,omitempty"`
	Limits     map[providers.ID]Limits            `json:"limits,omitempty"`
}

// Record counts one more turn and returns today's total.
func (m *UsageMeter) Record() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	today := m.day()
	m.days[today]++
	forgetOld(m.days)
	return m.days[today], m.save()
}

// TokensToday are what each AI used today.
func (m *UsageMeter) TokensToday() map[providers.ID]Tokens {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[providers.ID]Tokens{}
	for provider, tokens := range m.tokens[m.day()] {
		out[provider] = tokens
	}
	return out
}

// Limits are each AI's usage windows that still hold: a window that started again since it was reported is left out.
func (m *UsageMeter) Limits() map[providers.ID]Limits {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	out := map[providers.ID]Limits{}
	for provider, limits := range m.limits {
		if current := limits.Current(now); current.FiveHour != nil || current.Week != nil {
			out[provider] = current
		}
	}
	return out
}

// Observe adds what a turn of an AI used, and keeps the usage windows it reported.
func (m *UsageMeter) Observe(provider providers.ID, result TurnResult) error {
	if result.Tokens.IsZero() && result.Limits == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !result.Tokens.IsZero() {
		today := m.day()
		if m.tokens[today] == nil {
			m.tokens[today] = map[providers.ID]Tokens{}
		}
		m.tokens[today][provider] = m.tokens[today][provider].Add(result.Tokens)
		forgetOld(m.tokens)
	}
	if result.Limits != nil {
		m.limits[provider] = m.limits[provider].Merge(*result.Limits)
	}
	return m.save()
}

// forgetOld keeps the last keptDays days of a count.
func forgetOld[T any](days map[string]T) {
	if len(days) <= keptDays {
		return
	}
	keys := make([]string, 0, len(days))
	for key := range days {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys[:len(keys)-keptDays] {
		delete(days, key)
	}
}

func (m *UsageMeter) save() error {
	data, err := json.Marshal(usageFile{Days: m.days, TokensByAI: m.tokens, Limits: m.limits})
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

// Metered wraps a Provider so every turn is counted and refused once the day's limit is used, and what it used is kept.
type Metered struct {
	Inner Provider
	// Provider names the AI, for the usage windows it reports.
	Provider providers.ID
	Meter    *UsageMeter
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
	result, err := m.Inner.Run(ctx, turn)
	// What the AI reported is kept; a failed turn reports nothing (its runner returns an empty result).
	_ = m.Meter.Observe(m.Provider, result)
	return result, err
}
