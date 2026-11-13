package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakePipelines struct {
	mu       sync.Mutex
	incoming []Incoming
	err      error
	cursors  []*Cursor
}

func (f *fakePipelines) Poll(_ context.Context, cursor *Cursor) (PipelinePoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cursors = append(f.cursors, cursor)
	if f.err != nil {
		return PipelinePoll{}, f.err
	}
	out := f.incoming
	f.incoming = nil
	return PipelinePoll{Incoming: out, Cursor: Cursor{Since: 7, Seen: []string{"1:2:failed"}}}, nil
}

func pipelineIncoming(headline, ts string) Incoming {
	link := "https://gitlab.example.com/dating/vision/-/pipelines/9"
	return Incoming{
		Source:  Source{Origin: OriginPipeline, ChannelID: "gitlab-pipelines", ChannelName: ptr("GitLab"), SenderID: "gitlab", SenderName: "GitLab", TS: ts, Permalink: &link},
		Text:    headline + "\nPipeline #9 failed in dating/vision.",
		Context: "Failed job \"rspec\" (stage \"test\"), end of its log:\nexpected 1, got 2",
	}
}

func TestAFinishedPipelineBecomesARequestWithItsLogAsData(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"summary": "**The spec fails**"}))
	pipelines := &fakePipelines{}
	h.svc.deps.Pipelines = pipelines
	h.svc.Poll(context.Background())
	pipelines.incoming = []Incoming{pipelineIncoming("❌ dating/vision · feat", "10.000000")}
	h.svc.Poll(context.Background())
	h.svc.Idle()

	requests := h.svc.Snapshot().Requests
	if len(requests) != 1 || requests[0].Source.Origin != OriginPipeline || requests[0].Status != StatusReady {
		t.Fatalf("unexpected requests %+v", requests)
	}
	prompt := h.codex.turns[0].Prompt
	for _, want := range []string{"<pipeline-log>\nFailed job", "expected 1, got 2", "## What to do with this pipeline", "<slack-message>\n❌"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(prompt, "<slack-context>\n") {
		t.Error("the log of a pipeline is not Slack context")
	}
	if pipelines.cursors[0] != nil || pipelines.cursors[1] == nil || pipelines.cursors[1].Since != 7 {
		t.Errorf("the pipeline cursor is kept apart from Slack's: %+v", pipelines.cursors)
	}
	if got := h.notifications(); len(got) != 1 || !strings.HasPrefix(got[0], "❌ dating/vision · feat|") {
		t.Errorf("notifications %v", got)
	}
}

func TestTwoPipelinesInARowAreTwoRequestsWithoutHistory(t *testing.T) {
	h := newHarness(t, reply(nil), reply(nil))
	pipelines := &fakePipelines{}
	h.svc.deps.Pipelines = pipelines
	h.svc.Poll(context.Background())
	pipelines.incoming = []Incoming{pipelineIncoming("❌ a/one · main", "10.000000"), pipelineIncoming("✅ b/two · main", "11.000000")}
	h.svc.Poll(context.Background())
	h.svc.Idle()

	if got := len(h.svc.Snapshot().Requests); got != 2 {
		t.Fatalf("%d requests, want 2", got)
	}
	if strings.Contains(h.codex.turns[1].Prompt, "<assistant-history>\n") {
		t.Error("pipelines of other projects are not one conversation")
	}
}

func TestPipelineTroubleIsANoticeAndDoesNotStopSlack(t *testing.T) {
	h := newHarness(t, reply(nil))
	h.svc.deps.Pipelines = &fakePipelines{err: errors.New("GitLab answered 403")}
	h.inbox.deliver()
	requests := h.receive(message("Can you check the login bug?"))

	if len(requests) != 1 {
		t.Fatalf("Slack was not read: %+v", requests)
	}
	notices := h.svc.Snapshot().Status.Notices
	if len(notices) != 1 || !strings.Contains(notices[0], "GitLab pipelines could not be read") {
		t.Fatalf("notices %v", notices)
	}
	h.svc.deps.Pipelines = &fakePipelines{}
	h.svc.Poll(context.Background())
	if notices := h.svc.Snapshot().Status.Notices; len(notices) != 0 {
		t.Fatalf("the notice stays after GitLab answers again: %v", notices)
	}
	// The poll saves the pipeline cursor in the background: wait for the write, or the temp folder cleanup races it.
	h.svc.Idle()
}
