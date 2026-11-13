package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Tokens are what a turn, a conversation or a day used.
type Tokens struct {
	// Input are the tokens the AI read, cached ones included.
	Input int64 `json:"input"`
	// Cached are the input tokens read from the AI's cache: cheaper, and counted apart by the plans.
	Cached int64 `json:"cached"`
	Output int64 `json:"output"`
}

// Add returns the sum of two counts.
func (t Tokens) Add(other Tokens) Tokens {
	return Tokens{Input: t.Input + other.Input, Cached: t.Cached + other.Cached, Output: t.Output + other.Output}
}

// IsZero reports a count with nothing in it.
func (t Tokens) IsZero() bool { return t == Tokens{} }

// Window is one usage window of an AI subscription, as the AI last reported it.
type Window struct {
	// UsedPercent is the share of the window used, from 0 to 100.
	UsedPercent float64 `json:"usedPercent"`
	// ResetsAt is when the window starts again; "" when the AI did not say.
	ResetsAt string `json:"resetsAt,omitempty"`
}

// Limits are an AI's own usage windows: the current five hours and the current week. A window the AI did not report
// is nil.
type Limits struct {
	FiveHour *Window `json:"fiveHour,omitempty"`
	Week     *Window `json:"week,omitempty"`
	// SeenAt is when the AI last reported them.
	SeenAt string `json:"seenAt"`
}

// Merge returns the limits with the windows of newer ones laid over them: an AI may report one window at a time.
func (l Limits) Merge(newer Limits) Limits {
	out := l
	if newer.FiveHour != nil {
		out.FiveHour = newer.FiveHour
	}
	if newer.Week != nil {
		out.Week = newer.Week
	}
	if newer.SeenAt != "" {
		out.SeenAt = newer.SeenAt
	}
	return out
}

// Current drops the windows that started again since they were reported: what they said no longer holds. A window
// without a reset time is dropped once a whole window has passed since it was reported.
func (l Limits) Current(now time.Time) Limits {
	out := l
	if expired(l.FiveHour, l.SeenAt, fiveHourMinutes, now) {
		out.FiveHour = nil
	}
	if expired(l.Week, l.SeenAt, weekMinutes, now) {
		out.Week = nil
	}
	return out
}

func expired(window *Window, seenAt string, minutes int, now time.Time) bool {
	if window == nil {
		return false
	}
	if window.ResetsAt == "" {
		seen, err := time.Parse(time.RFC3339Nano, seenAt)
		return err != nil || !seen.Add(time.Duration(minutes)*time.Minute).After(now)
	}
	resets, err := time.Parse(time.RFC3339Nano, window.ResetsAt)
	return err == nil && !resets.After(now)
}

const (
	fiveHourMinutes = 5 * 60
	weekMinutes     = 7 * 24 * 60
)

// codexThreadID is the shape of a Codex thread ID; anything else never reaches a file pattern.
var codexThreadID = regexp.MustCompile(`^[0-9A-Fa-f-]{8,64}$`)

// codexLimits reads the usage windows Codex wrote in the session file of a thread, under its CODEX_HOME:
// `codex exec --json` does not report them, the session file does. Nil when the file or the windows are missing.
func codexLimits(home, threadID string, now time.Time) *Limits {
	if home == "" || !codexThreadID.MatchString(threadID) {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+threadID+".jsonl"))
	if err != nil || len(files) == 0 {
		return nil
	}
	file, err := os.Open(files[len(files)-1])
	if err != nil {
		return nil
	}
	defer file.Close()
	// The report is near the end, and the file grows with the conversation: read its tail first, and the whole file
	// only when the tail holds no report.
	report := lastRateLimits(tail(file, sessionTail))
	if report == nil {
		if _, err := file.Seek(0, io.SeekStart); err == nil {
			report = lastRateLimits(file)
		}
	}
	if report == nil {
		return nil
	}
	limits := Limits{SeenAt: stamp(now)}
	for _, window := range []*codexWindow{report.Primary, report.Secondary} {
		if window == nil {
			continue
		}
		// Which window is which is said by its length, not by its place.
		reported := &Window{UsedPercent: clampPercent(window.UsedPercent)}
		if window.ResetsAt > 0 {
			reported.ResetsAt = stamp(time.Unix(window.ResetsAt, 0))
		}
		switch window.WindowMinutes {
		case fiveHourMinutes:
			limits.FiveHour = reported
		case weekMinutes:
			limits.Week = reported
		}
	}
	if limits.FiveHour == nil && limits.Week == nil {
		return nil
	}
	return &limits
}

// sessionTail is how much of the end of a session file is read first.
const sessionTail = 512 << 10

type codexRateLimits struct {
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}

// tail is the last size bytes of a file, from the start of a line; the whole file when it is shorter.
func tail(file *os.File, size int64) io.Reader {
	info, err := file.Stat()
	if err != nil || info.Size() <= size {
		return file
	}
	if _, err := file.Seek(info.Size()-size, io.SeekStart); err != nil {
		return file
	}
	reader := bufio.NewReader(io.LimitReader(file, size))
	_, _ = reader.ReadBytes('\n') // the first line is cut: skip it
	return reader
}

// lastRateLimits is the last report of the windows in a session: a line that says nothing about them (no limits,
// "rate_limits": null) does not hide an earlier one.
func lastRateLimits(source io.Reader) *codexRateLimits {
	var last *codexRateLimits
	reader := bufio.NewReader(source)
	for {
		line, err := reader.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"rate_limits"`)) {
			var event struct {
				Payload struct {
					RateLimits *codexRateLimits `json:"rate_limits"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &event) == nil && event.Payload.RateLimits != nil &&
				(event.Payload.RateLimits.Primary != nil || event.Payload.RateLimits.Secondary != nil) {
				last = event.Payload.RateLimits
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil
			}
			return last
		}
	}
}

type codexWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

// claudeRateLimit is the `rate_limit_info` of Claude Code's `rate_limit_event`. Its utilizations are shares from 0 to 1,
// as the CLI's own schema and the API's headers give them; a value above 1 is taken as a percentage already.
type claudeRateLimit struct {
	RateLimitType  string               `json:"rateLimitType"`
	Utilization    *float64             `json:"utilization"`
	ResetsAt       int64                `json:"resetsAt"`
	UnifiedWindows map[string]claudeUse `json:"unifiedWindows"`
}

type claudeUse struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    int64   `json:"resetsAt"`
}

// claudeLimits turns a Claude rate limit event into usage windows; nil when it says nothing about them.
func claudeLimits(raw json.RawMessage, now time.Time) *Limits {
	var info claudeRateLimit
	if len(raw) == 0 || json.Unmarshal(raw, &info) != nil {
		return nil
	}
	window := func(use claudeUse) *Window {
		used := use.Utilization
		if used <= 1 {
			used *= 100
		}
		reported := &Window{UsedPercent: clampPercent(used)}
		if use.ResetsAt > 0 {
			reported.ResetsAt = stamp(time.Unix(use.ResetsAt, 0))
		}
		return reported
	}
	limits := Limits{SeenAt: stamp(now)}
	if use, ok := info.UnifiedWindows["five_hour"]; ok {
		limits.FiveHour = window(use)
	}
	if use, ok := info.UnifiedWindows["seven_day"]; ok {
		limits.Week = window(use)
	}
	// Without the windows, the event still names the window it is about.
	if info.Utilization != nil {
		use := claudeUse{Utilization: *info.Utilization, ResetsAt: info.ResetsAt}
		switch {
		case info.RateLimitType == "five_hour" && limits.FiveHour == nil:
			limits.FiveHour = window(use)
		case info.RateLimitType == "seven_day" && limits.Week == nil:
			limits.Week = window(use)
		}
	}
	if limits.FiveHour == nil && limits.Week == nil {
		return nil
	}
	return &limits
}

func clampPercent(value float64) float64 {
	return min(max(value, 0), 100)
}
