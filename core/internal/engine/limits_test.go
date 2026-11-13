package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

var limitsNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

const thread = "01a1d497-273d-72f1-9e46-1b55e2b3fc73"

func tokenCount(fiveHour, week float64) string {
	return `{"timestamp":"2026-11-13T12:00:00Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","limit_name":null,` +
		`"primary":{"used_percent":` + jsonNumber(fiveHour) + `,"window_minutes":300,"resets_at":1794575345},` +
		`"secondary":{"used_percent":` + jsonNumber(week) + `,"window_minutes":10080,"resets_at":1794970215},"plan_type":"plus"}}}`
}

func jsonNumber(value float64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func writeSession(t *testing.T, home string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "11", "13")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-11-13T12-07-18-"+thread+".jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexLimitsComeFromTheLastReportOfTheSession(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, `{"type":"session_meta","payload":{}}`, tokenCount(2, 20), `{"type":"response_item","payload":{"text":"a long answer"}}`, tokenCount(9, 27))

	limits := codexLimits(home, thread, limitsNow)
	if limits == nil || limits.FiveHour.UsedPercent != 9 || limits.Week.UsedPercent != 27 {
		t.Fatalf("limits %+v", limits)
	}
	if limits.FiveHour.ResetsAt != stamp(time.Unix(1794575345, 0)) || limits.SeenAt != stamp(limitsNow) {
		t.Fatalf("times %+v", limits)
	}
}

func TestCodexLimitsTellTheWindowsApartByTheirLength(t *testing.T) {
	home := t.TempDir()
	swapped := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":40,"window_minutes":10080,"resets_at":0},"secondary":{"used_percent":5,"window_minutes":300,"resets_at":0}}}}`
	writeSession(t, home, swapped)
	limits := codexLimits(home, thread, limitsNow)
	if limits == nil || limits.FiveHour.UsedPercent != 5 || limits.Week.UsedPercent != 40 || limits.Week.ResetsAt != "" {
		t.Fatalf("limits %+v", limits)
	}
}

func TestCodexLimitsAreAbsentRatherThanGuessed(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, `{"type":"event_msg","payload":{"type":"token_count","info":{}}}`)
	for name, got := range map[string]*Limits{
		"no report":       codexLimits(home, thread, limitsNow),
		"no session":      codexLimits(home, "01a1d497-0000-0000-0000-000000000000", limitsNow),
		"not a thread id": codexLimits(home, "../../*", limitsNow),
		"no codex home":   codexLimits("", thread, limitsNow),
		"unknown windows": func() *Limits {
			other := t.TempDir()
			writeSession(t, other, `{"type":"event_msg","payload":{"rate_limits":{"primary":{"used_percent":3,"window_minutes":60}}}}`)
			return codexLimits(other, thread, limitsNow)
		}(),
	} {
		if got != nil {
			t.Fatalf("%s: %+v", name, got)
		}
	}
}

func TestClaudeLimitsReadItsWindowsAsShares(t *testing.T) {
	unified := json.RawMessage(`{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.42,"resetsAt":1794575345},"seven_day":{"utilization":0.1,"resetsAt":1794970215}}}`)
	limits := claudeLimits(unified, limitsNow)
	if limits == nil || limits.FiveHour.UsedPercent != 42 || limits.Week.UsedPercent != 10 {
		t.Fatalf("limits %+v", limits)
	}

	single := json.RawMessage(`{"status":"allowed_warning","rateLimitType":"seven_day","utilization":0.8,"resetsAt":1794970215}`)
	if limits := claudeLimits(single, limitsNow); limits == nil || limits.FiveHour != nil || limits.Week.UsedPercent != 80 {
		t.Fatalf("one window: %+v", limits)
	}
	for _, raw := range []string{`{"status":"allowed"}`, `{"rateLimitType":"overage","utilization":0.5}`, `not json`, ``} {
		if limits := claudeLimits(json.RawMessage(raw), limitsNow); limits != nil {
			t.Fatalf("%s gave %+v", raw, limits)
		}
	}
}

func TestTheRunnersReadTheTokensOfATurn(t *testing.T) {
	codexTokens := interpretLine(`{"type":"turn.completed","usage":{"input_tokens":14760,"cached_input_tokens":12288,"output_tokens":5}}`, i18n.EN).tokens
	if codexTokens != (Tokens{Input: 14760, Cached: 12288, Output: 5}) {
		t.Fatalf("codex %+v", codexTokens)
	}

	stream := &claudeStream{lang: i18n.EN}
	for _, line := range []string{
		`{"type":"rate_limit_event","rate_limit_info":{"rateLimitType":"five_hour","utilization":0.3}}`,
		`{"type":"rate_limit_event","rate_limit_info":{"rateLimitType":"seven_day","utilization":0.6}}`,
		`{"type":"result","subtype":"success","session_id":"s1","result":"{}","usage":{"input_tokens":10,"cache_creation_input_tokens":200,"cache_read_input_tokens":3000,"output_tokens":40}}`,
	} {
		if err := stream.read(line); err != nil {
			t.Fatal(err)
		}
	}
	if stream.result.Tokens != (Tokens{Input: 3210, Cached: 3000, Output: 40}) {
		t.Fatalf("claude tokens %+v", stream.result.Tokens)
	}
	if limits := stream.result.Limits; limits == nil || limits.FiveHour.UsedPercent != 30 || limits.Week.UsedPercent != 60 {
		t.Fatalf("one window per event must add up: %+v", limits)
	}
}

func TestTheMeterAddsUpTokensAndKeepsTheWindowsThatStillHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	now := limitsNow
	meter, _ := NewUsageMeter(path, func() time.Time { return now })

	_ = meter.Observe(providers.Codex, TurnResult{Tokens: Tokens{Input: 100, Cached: 60, Output: 10}, Limits: &Limits{
		FiveHour: &Window{UsedPercent: 9, ResetsAt: stamp(now.Add(time.Hour))},
		Week:     &Window{UsedPercent: 27, ResetsAt: stamp(now.Add(48 * time.Hour))},
	}})
	_ = meter.Observe(providers.Claude, TurnResult{Tokens: Tokens{Input: 50, Output: 5}})
	if got := meter.TokensToday(); len(got) != 2 || got[providers.Codex] != (Tokens{Input: 100, Cached: 60, Output: 10}) || got[providers.Claude] != (Tokens{Input: 50, Output: 5}) {
		t.Fatalf("each AI keeps its own tokens: %+v", got)
	}
	if limits := meter.Limits(); len(limits) != 1 || limits[providers.Codex].FiveHour.UsedPercent != 9 {
		t.Fatalf("limits %+v", limits)
	}

	// A report of one window keeps the other.
	_ = meter.Observe(providers.Codex, TurnResult{Limits: &Limits{Week: &Window{UsedPercent: 30, ResetsAt: stamp(now.Add(48 * time.Hour))}}})
	if codex := meter.Limits()[providers.Codex]; codex.FiveHour == nil || codex.Week.UsedPercent != 30 {
		t.Fatalf("merge %+v", codex)
	}

	// Two hours later, the five-hour window has started again: what it said no longer holds.
	now = now.Add(2 * time.Hour)
	if codex := meter.Limits()[providers.Codex]; codex.FiveHour != nil || codex.Week == nil {
		t.Fatalf("an expired window is still shown: %+v", codex)
	}
	reloaded, _ := NewUsageMeter(path, func() time.Time { return now })
	if reloaded.TokensToday()[providers.Codex].Output != 10 || reloaded.Limits()[providers.Codex].Week.UsedPercent != 30 {
		t.Fatalf("not kept on disk: %+v %+v", reloaded.TokensToday(), reloaded.Limits())
	}
}

func TestAMeterFileFromBeforeTokensStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	// Days only, and the first version's tokens, which do not say which AI used them.
	legacy := `{"days":{"2026-11-13":7},"tokens":{"2026-11-13":{"input":1579857,"cached":1265408,"output":8132}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	meter, err := NewUsageMeter(path, func() time.Time { return limitsNow })
	if err != nil || meter.Today() != 7 || len(meter.TokensToday()) != 0 || len(meter.Limits()) != 0 {
		t.Fatalf("legacy meter: %v %d", err, meter.Today())
	}
	if err := meter.Observe(providers.Codex, TurnResult{Tokens: Tokens{Input: 1}}); err != nil {
		t.Fatal(err)
	}
}

func TestAConversationAddsUpItsTokensWithoutCountingAsActivity(t *testing.T) {
	answer := reply(map[string]any{"outcome": Answered})
	h := newHarness(t, func(ctx context.Context, turn Turn) (TurnResult, error) {
		result, err := answer(ctx, turn)
		result.Tokens = Tokens{Input: 1000, Cached: 800, Output: 50}
		return result, err
	})
	h.receive(message("Can you check the login?"))
	request := only(t, h)
	if request.Tokens == nil || *request.Tokens != (Tokens{Input: 1000, Cached: 800, Output: 50}) {
		t.Fatalf("tokens %+v", request.Tokens)
	}
}

func TestALaterLineWithoutWindowsDoesNotHideTheLastReport(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, tokenCount(9, 27), `{"type":"event_msg","payload":{"type":"token_count","info":{},"rate_limits":null}}`)
	if limits := codexLimits(home, thread, limitsNow); limits == nil || limits.FiveHour.UsedPercent != 9 {
		t.Fatalf("limits %+v", limits)
	}
}

func TestAReportFarFromTheEndOfALongSessionIsStillFound(t *testing.T) {
	home := t.TempDir()
	long := `{"type":"response_item","payload":{"text":"` + strings.Repeat("x", sessionTail) + `"}}`
	writeSession(t, home, tokenCount(12, 30), long, long)
	if limits := codexLimits(home, thread, limitsNow); limits == nil || limits.Week.UsedPercent != 30 {
		t.Fatalf("limits %+v", limits)
	}
}

func TestClaudeUtilizationAboveOneIsAlreadyAPercentage(t *testing.T) {
	limits := claudeLimits(json.RawMessage(`{"rateLimitType":"five_hour","utilization":42}`), limitsNow)
	if limits == nil || limits.FiveHour.UsedPercent != 42 {
		t.Fatalf("limits %+v", limits)
	}
}

func TestAWindowWithoutResetTimeGoesAwayAfterAWholeWindow(t *testing.T) {
	limits := Limits{FiveHour: &Window{UsedPercent: 10}, Week: &Window{UsedPercent: 20}, SeenAt: stamp(limitsNow)}
	later := limits.Current(limitsNow.Add(6 * time.Hour))
	if later.FiveHour != nil || later.Week == nil {
		t.Fatalf("after six hours: %+v", later)
	}
	if soon := limits.Current(limitsNow.Add(time.Hour)); soon.FiveHour == nil {
		t.Fatal("dropped too early")
	}
}
