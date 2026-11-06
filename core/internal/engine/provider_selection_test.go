package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

func TestTheProviderAndModelAreChosenAtEachTurn(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": NeedsInput, "questions": []any{map[string]any{"text": "Which?", "options": []any{map[string]any{"label": "Email", "description": nil}}}}}))
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){reply(map[string]any{"summary": "Found it"})}
	h.choose(providers.Codex, "gpt-x")
	request := h.receive(message("login?"))[0]
	if h.codex.count() != 1 || h.codex.turns[0].Model != "gpt-x" || h.claude.count() != 0 {
		t.Fatalf("first turn: codex %d claude %d", h.codex.count(), h.claude.count())
	}

	// Switching between two turns, with no restart, sends the next one to the other AI with its own model.
	h.choose(providers.Claude, "opus")
	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionID: "A"}}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if h.codex.count() != 1 || h.claude.count() != 1 || h.claude.turns[0].Model != "opus" {
		t.Fatalf("second turn: codex %d claude %d", h.codex.count(), h.claude.count())
	}
	// Codex's thread means nothing to Claude: the conversation restarts there with what was said so far.
	second := h.claude.turns[0]
	if second.ResumeThread != "" || !strings.Contains(second.Prompt, "Conversation so far") || !strings.Contains(second.Prompt, "Email") {
		t.Fatalf("a thread of the other AI must not be resumed: %+v", second)
	}
	if got := h.svc.Snapshot().Requests[0]; got.Status != StatusReady || *got.Summary != "Found it" {
		t.Fatalf("got %+v", got)
	}
}

func TestAThreadIsResumedOnlyByTheAIThatOwnsIt(t *testing.T) {
	ask := reply(map[string]any{"outcome": NeedsInput, "questions": []any{map[string]any{"text": "Which?", "options": []any{map[string]any{"label": "Email", "description": nil}}}}})
	h := newHarness(t, ask)
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){
		func(context.Context, Turn) (TurnResult, error) {
			data := `{"outcome":"needs_input","summary":"s","reply":null,"questions":[{"text":"Again?","options":[{"label":"Yes","description":null}]}],"proposal":null,"sourcesUsed":[]}`
			return TurnResult{ThreadID: "claude-session-1", FinalMessage: data}, nil
		},
		reply(nil),
	}
	h.choose(providers.Claude, "")
	request := h.receive(message("login?"))[0]
	if h.claude.count() != 1 {
		t.Fatalf("claude should have run: %d", h.claude.count())
	}
	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionID: "A"}}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if got := h.claude.turns[1].ResumeThread; got != "claude-session-1" {
		t.Fatalf("the same AI continues its own session, got %q", got)
	}
}

func TestAThreadKeptBeforeClaudeExistedBelongsToCodex(t *testing.T) {
	stored := Stored{ThreadID: "old"}
	if stored.threadOwner() != providers.Codex {
		t.Fatal("a thread without owner is Codex's")
	}
	stored.ThreadProvider = providers.Claude
	if stored.threadOwner() != providers.Claude {
		t.Fatal("the owner is kept")
	}
}

func TestALimitPausesOnlyTheProviderThatReportedIt(t *testing.T) {
	h := newHarness(t, failWith(ErrLimit))
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){reply(nil)}
	h.svc.Poll(context.Background())
	h.inbox.deliver(message("first thing to check"))
	h.svc.Poll(context.Background())
	h.svc.Idle()
	if notices := h.svc.Snapshot().Status.Notices; len(notices) != 1 || !strings.Contains(notices[0], "Codex's usage limit") {
		t.Fatalf("the active AI must be named: %v", notices)
	}
	if got := h.notifications(); len(got) != 1 || !strings.HasPrefix(got[0], "Codex limit reached") {
		t.Fatalf("notifications %v", got)
	}

	// Claude is chosen: its notices are clean and a request from someone else is not held back by Codex's pause.
	h.choose(providers.Claude, "")
	if notices := h.svc.Snapshot().Status.Notices; len(notices) != 0 {
		t.Fatalf("Codex's pause must not show while Claude runs: %v", notices)
	}
	h.inbox.deliver(message("second thing to check", func(in *Incoming) { in.Source.ChannelID = "C2" }))
	h.svc.Poll(context.Background())
	h.svc.Idle()
	if h.claude.count() != 1 || h.svc.Snapshot().Requests[1].Status != StatusReady {
		t.Fatalf("claude must run while codex is paused: %d turns, %+v", h.claude.count(), h.svc.Snapshot().Requests[1].Status)
	}

	// Back to Codex: its pause is still in force, and said.
	h.choose(providers.Codex, "")
	if notices := h.svc.Snapshot().Status.Notices; len(notices) != 1 || !strings.Contains(notices[0], "Codex's usage limit") {
		t.Fatalf("notices %v", notices)
	}
}

func TestALoginProblemNamesAndStaysWithItsProvider(t *testing.T) {
	h := newHarness(t)
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){failWith(ErrLogin)}
	h.choose(providers.Claude, "")
	h.receive(message("check this please", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
	notices := h.svc.Snapshot().Status.Notices
	if len(notices) != 1 || !strings.Contains(notices[0], "Claude is not signed in any more") {
		t.Fatalf("notices %v", notices)
	}
	if got := h.notifications(); len(got) != 1 || !strings.HasPrefix(got[0], "Claude needs you to sign in again") {
		t.Fatalf("notifications %v", got)
	}
	failed := h.svc.Snapshot().Requests[0]
	if failed.Error == nil || !strings.Contains(*failed.Error, "Claude is not signed in") {
		t.Fatalf("the request must name Claude: %+v", failed.Error)
	}
	h.choose(providers.Codex, "")
	if notices := h.svc.Snapshot().Status.Notices; len(notices) != 0 {
		t.Fatalf("Codex works: %v", notices)
	}
}

func TestTheJournalAndNoticesAreWrittenInTheChosenLanguageWithTheProviderName(t *testing.T) {
	h := newHarness(t)
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){failWith(ErrLimit)}
	h.choose(providers.Claude, "")
	h.svc.deps.Language = func() i18n.Lang { return i18n.FR }
	h.svc.Poll(context.Background())
	h.inbox.deliver(message("question", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
	h.svc.Poll(context.Background())
	h.svc.Idle()
	notices := h.svc.Snapshot().Status.Notices
	if len(notices) != 1 || !strings.Contains(notices[0], "La limite d’utilisation de Claude est atteinte") {
		t.Fatalf("notices %v", notices)
	}
	if got := h.notifications(); len(got) != 1 || !strings.HasPrefix(got[0], "Limite Claude atteinte") {
		t.Fatalf("notifications %v", got)
	}
}

func TestAnUnavailableProviderIsReportedNotGuessed(t *testing.T) {
	h := newHarness(t)
	delete(h.svc.deps.Providers, providers.Claude)
	h.choose(providers.Claude, "")
	h.receive(message("check this please", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
	if got := h.svc.Snapshot().Requests[0]; got.Status != StatusFailed || got.Error == nil || !strings.Contains(*got.Error, "Claude is not available") {
		t.Fatalf("got %+v", got)
	}
	if h.codex.count() != 0 {
		t.Fatal("no silent fallback to the other AI")
	}
}

func TestWithoutASelectionTheServiceRunsOnCodex(t *testing.T) {
	h := newHarness(t)
	if got := NewService(Deps{Store: h.svc.deps.Store, Providers: h.svc.deps.Providers}).deps.Selection(); got.Provider != providers.Codex || got.Model != "" {
		t.Fatalf("default selection %+v", got)
	}
}

func TestTheMeterCountsTurnsOfBothProvidersInOneQuota(t *testing.T) {
	meter, err := NewUsageMeter(filepath.Join(t.TempDir(), "usage.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	limit := 2
	codex, claude := &countingCodex{}, &countingCodex{}
	wrap := func(inner Provider) Provider {
		return &Metered{Inner: inner, Meter: meter, Limit: func() int { return limit }}
	}
	a, b := wrap(codex), wrap(claude)
	if _, err := a.Run(context.Background(), Turn{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Run(context.Background(), Turn{}); err != nil {
		t.Fatal(err)
	}
	var quota *QuotaError
	if _, err := a.Run(context.Background(), Turn{}); !errors.As(err, &quota) {
		t.Fatalf("the limit applies to whichever runs: %v", err)
	}
	if _, err := b.Run(context.Background(), Turn{}); !errors.As(err, &quota) {
		t.Fatalf("the limit applies to whichever runs: %v", err)
	}
	if codex.calls != 1 || claude.calls != 1 || meter.Today() != 2 {
		t.Fatalf("calls %d %d, today %d", codex.calls, claude.calls, meter.Today())
	}
}

func TestTestProviderRunsOneMeteredReadOnlyTurnWithTheGivenModel(t *testing.T) {
	h := newHarness(t)
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){
		func(_ context.Context, turn Turn) (TurnResult, error) {
			return TurnResult{FinalMessage: " ok \n", Model: "claude-sonnet-5"}, nil
		},
	}
	result, err := h.svc.TestProvider(context.Background(), providers.Claude, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	turn := h.claude.turns[0]
	if turn.Sandbox != ReadOnly || turn.Model != "sonnet" || turn.SchemaFile != "" || turn.Dir != h.root || !strings.Contains(turn.Prompt, "ok") {
		t.Fatalf("test turn %+v", turn)
	}
	if result.Model != "claude-sonnet-5" || result.Reply != "ok" || result.Latency < 0 {
		t.Fatalf("result %+v", result)
	}
	if h.codex.count() != 0 {
		t.Fatal("only the tested provider runs")
	}
	// Without a model reported by the CLI, the model asked for is shown.
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){reply(nil)}
	if got, err := h.svc.TestProvider(context.Background(), providers.Claude, "opus"); err != nil || got.Model != "opus" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestTestProviderExplainsWhatWentWrong(t *testing.T) {
	h := newHarness(t)
	run := func(step func(context.Context, Turn) (TurnResult, error), model string) error {
		h.claude.mu.Lock()
		h.claude.steps = []func(context.Context, Turn) (TurnResult, error){step}
		h.claude.mu.Unlock()
		_, err := h.svc.TestProvider(i18n.WithLanguage(context.Background(), i18n.EN), providers.Claude, model)
		return err
	}
	if err := run(failWith(ErrLogin), ""); !errors.Is(err, ErrLogin) || !strings.Contains(i18n.ErrorText(i18n.EN, err), "Claude is not signed in any more") {
		t.Fatalf("login: %v", err)
	}
	if err := run(failWith(ErrLimit), ""); !errors.Is(err, ErrLimit) || !strings.Contains(i18n.ErrorText(i18n.FR, err), "La limite d’utilisation de Claude") {
		t.Fatalf("limit: %v", err)
	}
	if err := run(func(context.Context, Turn) (TurnResult, error) { return TurnResult{}, nil }, ""); err == nil || !strings.Contains(err.Error(), "Claude finished without an answer") {
		t.Fatalf("empty answer: %v", err)
	}
	if err := run(reply(nil), "--evil"); err == nil {
		t.Fatal("an invalid model must be refused")
	}
	if _, err := h.svc.TestProvider(context.Background(), providers.ID("gemini"), ""); err == nil {
		t.Fatal("an unknown provider must be refused")
	}

	previous := providerTestTimeout
	providerTestTimeout = 100 * time.Millisecond
	defer func() { providerTestTimeout = previous }()
	err := run(func(ctx context.Context, _ Turn) (TurnResult, error) { <-ctx.Done(); return TurnResult{}, ErrCancelled }, "")
	if !errors.Is(err, ErrTestTimeout) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestASuccessfulTestEndsTheProvidersPauseAndSignInNotice(t *testing.T) {
	h := newHarness(t, failWith(ErrLimit))
	h.receive(message("something", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
	if len(h.svc.Snapshot().Status.Notices) != 1 {
		t.Fatalf("a pause is expected: %v", h.svc.Snapshot().Status.Notices)
	}
	h.codex.mu.Lock()
	h.codex.steps = []func(context.Context, Turn) (TurnResult, error){reply(nil)}
	h.codex.mu.Unlock()
	if _, err := h.svc.TestProvider(context.Background(), providers.Codex, ""); err != nil {
		t.Fatal(err)
	}
	if notices := h.svc.Snapshot().Status.Notices; len(notices) != 0 {
		t.Fatalf("a provider that answers works again: %v", notices)
	}
}
