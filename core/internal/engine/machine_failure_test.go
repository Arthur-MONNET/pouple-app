package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/machine"
)

func TestAFailureSaysWhetherTheMachineExplainsIt(t *testing.T) {
	h := newHarness(t, func(context.Context, Turn) (TurnResult, error) {
		return TurnResult{}, errors.New("thread/resume failed: list_turns is not supported yet")
	})
	reads := 0
	h.svc.deps.Machine = func() machine.State {
		reads++
		return machine.State{Disk: &machine.Disk{FreeBytes: 1 << 30, TotalBytes: 500 << 30, Level: machine.Critical}}
	}
	requests := h.receive(message("<@UARTHUR> why does login fail?"))
	if len(requests) != 1 || requests[0].Status != StatusFailed || requests[0].Error == nil {
		t.Fatalf("want one failed request: %+v", requests)
	}
	if reads != 1 {
		t.Errorf("the machine must be read once, when it failed: %d reads", reads)
	}
	failure := *requests[0].Error
	if !strings.HasPrefix(failure, "thread/resume failed") || !strings.Contains(failure, "the disk was almost full (1.0 GB free)") {
		t.Fatalf("the failure must keep its cause and name the full disk: %q", failure)
	}
	last := requests[0].Entries[len(requests[0].Entries)-1].Text
	if !strings.Contains(last, "the disk was almost full") {
		t.Fatalf("the journal must say it too: %q", last)
	}
}

func TestASignInFailureDoesNotBlameTheMachine(t *testing.T) {
	h := newHarness(t, func(context.Context, Turn) (TurnResult, error) {
		return TurnResult{}, ErrLogin
	})
	h.svc.deps.Machine = func() machine.State {
		t.Error("a sign-in failure has a known cause: the machine must not be read")
		return machine.State{}
	}
	requests := h.receive(message("<@UARTHUR> why does login fail?"))
	if len(requests) != 1 || requests[0].Error == nil || strings.Contains(*requests[0].Error, "disk") {
		t.Fatalf("want the sign-in message alone: %+v", requests)
	}
}
