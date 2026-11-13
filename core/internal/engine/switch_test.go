package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// limitedOnClaude is a harness whose chosen AI is Claude, which stops on its usage limit, with Codex ready to answer.
func limitedOnClaude(t *testing.T, codexConnected bool) (*harness, *[]providers.ID) {
	t.Helper()
	h := newHarness(t, reply(map[string]any{"outcome": Answered, "summary": "Codex took over"}))
	h.claude.steps = []func(context.Context, Turn) (TurnResult, error){failWith(ErrLimit)}
	h.choice = providers.Selection{Provider: providers.Claude}
	used := &[]providers.ID{}
	h.svc.deps.Connected = func(id providers.ID) bool { return id == providers.Claude || codexConnected }
	h.svc.deps.UseProvider = func(id providers.ID) error {
		*used = append(*used, id)
		h.mu.Lock()
		h.choice = providers.Selection{Provider: id}
		h.mu.Unlock()
		return nil
	}
	h.receive(message("what is left to do on the iOS app?"))
	return h, used
}

func TestARequestStoppedByAUsageLimitOffersTheOtherAI(t *testing.T) {
	h, used := limitedOnClaude(t, true)
	request := only(t, h)
	if request.Status != StatusFailed || request.SwitchTo != providers.Codex {
		t.Fatalf("no offer to go on with Codex: %+v", request)
	}

	if err := h.svc.ContinueWith(request.ID, providers.Codex); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if len(*used) != 1 || (*used)[0] != providers.Codex {
		t.Fatalf("Codex was not made the AI in use: %v", *used)
	}
	done := only(t, h)
	if done.Status != StatusReady || done.SwitchTo != "" || h.codex.count() != 1 {
		t.Fatalf("Codex did not take over: %+v (codex turns %d)", done, h.codex.count())
	}
}

func TestNoOfferWhenTheOtherAICannotTakeOver(t *testing.T) {
	h, _ := limitedOnClaude(t, false)
	request := only(t, h)
	if request.SwitchTo != "" {
		t.Fatalf("offered an AI that is not connected: %+v", request)
	}
	if err := h.svc.ContinueWith(request.ID, providers.Codex); !errors.Is(err, ErrState) {
		t.Fatalf("switched to an AI that is not connected: %v", err)
	}
}

func TestOnlyTheOfferedAICanTakeOver(t *testing.T) {
	h, used := limitedOnClaude(t, true)
	if err := h.svc.ContinueWith(only(t, h).ID, providers.Claude); !errors.Is(err, ErrState) || len(*used) != 0 {
		t.Fatalf("the AI at its limit was chosen again: %v %v", err, *used)
	}
}

func TestAFailureThatIsNotALimitOffersNoOtherAI(t *testing.T) {
	h := newHarness(t, failWith(errors.New("the CLI crashed")))
	h.svc.deps.Connected = func(providers.ID) bool { return true }
	h.svc.deps.UseProvider = func(providers.ID) error { return nil }
	h.receive(message("what is left to do on the iOS app?"))
	if request := only(t, h); request.Status != StatusFailed || request.SwitchTo != "" {
		t.Fatalf("offered another AI for a crash: %+v", request)
	}
}
