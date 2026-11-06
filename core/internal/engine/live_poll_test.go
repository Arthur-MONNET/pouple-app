package engine

import (
	"context"
	"testing"
	"time"
)

type countingInbox chan struct{}

func (i countingInbox) Poll(_ context.Context, _ *Cursor) (PollResult, error) {
	i <- struct{}{}
	return PollResult{}, nil
}

func TestLiveWakeBringsForwardTheNextPoll(t *testing.T) {
	h := newHarness(t)
	polls := make(countingInbox, 4)
	h.svc.deps.Inbox = polls
	wake := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer func() {
		cancel()
		<-done
	}()
	cooldown := 40 * time.Millisecond
	go func() {
		defer close(done)
		h.svc.Run(ctx, func() (time.Duration, time.Duration) { return time.Hour, cooldown }, wake)
	}()
	awaitPoll := func() {
		t.Helper()
		select {
		case <-polls:
		case <-time.After(2 * time.Second):
			t.Fatal("poll did not run")
		}
	}
	awaitPoll()
	start := time.Now()
	wake <- struct{}{}
	awaitPoll()
	if time.Since(start) < cooldown/2 {
		t.Fatal("live wake skipped the polling cooldown")
	}
}
