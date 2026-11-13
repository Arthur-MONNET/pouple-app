package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// Claude only reports its usage windows during a turn, so what Pouple knows would be as old as its last turn. Claude
// Code's own /usage command asks the account instead: it costs no turn and no token, and shows what the Claude app shows.

var (
	sessionLine = regexp.MustCompile(`(?m)^Current session: (\d{1,3})% used(?: · resets (.+))?$`)
	weekLine    = regexp.MustCompile(`(?m)^Current week \(all models\): (\d{1,3})% used(?: · resets (.+))?$`)
	// resetAt reads "Nov 13 at 7:10pm (Europe/Paris)", "Nov 15 at 7pm (Europe/Paris)" or "7:10pm (Europe/Paris)".
	resetAt = regexp.MustCompile(`^(?:([A-Z][a-z]{2}) (\d{1,2}) at )?(\d{1,2})(?::(\d{2}))?([ap]m) \(([^)]+)\)$`)
)

// parseClaudeUsage reads the windows out of the text of /usage; nil when it holds none of them.
func parseClaudeUsage(text string, now time.Time) *Limits {
	limits := Limits{SeenAt: stamp(now)}
	window := func(match []string) *Window {
		if match == nil {
			return nil
		}
		used, _ := strconv.Atoi(match[1])
		reported := &Window{UsedPercent: clampPercent(float64(used))}
		if resets := parseReset(strings.TrimSpace(match[2]), now); !resets.IsZero() {
			reported.ResetsAt = stamp(resets)
		}
		return reported
	}
	limits.FiveHour = window(sessionLine.FindStringSubmatch(text))
	limits.Week = window(weekLine.FindStringSubmatch(text))
	if limits.FiveHour == nil && limits.Week == nil {
		return nil
	}
	return &limits
}

// parseReset is when a window starts again; the zero time when the text is not one Pouple knows.
func parseReset(text string, now time.Time) time.Time {
	match := resetAt.FindStringSubmatch(text)
	if match == nil {
		return time.Time{}
	}
	location, err := time.LoadLocation(match[6])
	if err != nil {
		return time.Time{}
	}
	hour, _ := strconv.Atoi(match[3])
	minute, _ := strconv.Atoi(match[4])
	if hour < 1 || hour > 12 {
		return time.Time{}
	}
	hour %= 12
	if match[5] == "pm" {
		hour += 12
	}
	local := now.In(location)
	day := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	if match[1] != "" {
		month, err := time.ParseInLocation("Jan", match[1], location)
		if err != nil {
			return time.Time{}
		}
		dayOfMonth, _ := strconv.Atoi(match[2])
		day = time.Date(local.Year(), month.Month(), dayOfMonth, hour, minute, 0, 0, location)
		// A date already well past is the same date next year (read in December for a January reset).
		if day.Before(now.AddDate(0, 0, -1)) {
			day = day.AddDate(1, 0, 0)
		}
		return day
	}
	// Only a time: the next time the clock shows it.
	if !day.After(now) {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

// Usage asks Claude Code for the usage windows of the account.
func (r *ClaudeRunner) Usage(ctx context.Context) (*Limits, error) {
	dir := r.opts.Home
	if dir == "" {
		dir = os.TempDir()
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	spec := processSpec{
		Name: "Claude", Shell: r.opts.Shell, Script: claudeScript, Command: r.opts.Command,
		// No user, project or local settings: their hooks would run code on every refresh.
		Args: []string{"-p", "--output-format", "json", "--setting-sources", ""}, Dir: dir, Stdin: "/usage",
		Timeout: 45 * time.Second, KillGrace: r.opts.KillGrace,
		Env: providers.ChildEnv(os.Environ(), claudeHomeEnv(r.opts.Home)...),
	}
	var text string
	end, err := runProcess(ctx, spec, func(line string) error {
		var reply struct {
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal([]byte(line), &reply) == nil && !reply.IsError && reply.Result != "" {
			text = reply.Result
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := end.settle(spec, nil); err != nil {
		return nil, err
	}
	limits := parseClaudeUsage(text, time.Now())
	if limits == nil {
		return nil, i18n.NewError("Claude did not report its usage")
	}
	return limits, nil
}

// ClaudeUsageWatcher keeps the usage windows of Claude as the account has them, not as the last turn left them.
type ClaudeUsageWatcher struct {
	Runner *ClaudeRunner
	Meter  *UsageMeter
	// Connected says whether Claude is signed in: nothing is asked before.
	Connected func() bool
	Every     time.Duration
	// OnError is told when a look fails, once for each run of failures.
	OnError func(error)
}

// Run looks at once and then every Every, until ctx ends.
func (w *ClaudeUsageWatcher) Run(ctx context.Context) {
	every := w.Every
	if every <= 0 {
		every = time.Minute
	}
	failing := false
	for ctx.Err() == nil {
		if w.Connected() {
			limits, err := w.Runner.Usage(ctx)
			switch {
			case err == nil:
				failing = false
				_ = w.Meter.Observe(providers.Claude, TurnResult{Limits: limits})
			case ctx.Err() == nil && !failing && w.OnError != nil:
				failing = true
				w.OnError(fmt.Errorf("claude usage: %w", err))
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(every):
		}
	}
}
