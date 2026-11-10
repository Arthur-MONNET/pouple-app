package pipelinewatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
)

type fakeConnections struct {
	connected bool
}

func (f fakeConnections) Status(string) (connector.Status, error) {
	return connector.Status{Connected: f.connected}, nil
}
func (f fakeConnections) Values(string) (connector.Values, error) {
	return connector.Values{"url": "https://gitlab.example.com", "token": "t"}, nil
}

type fakeClient struct {
	pipelines []gitlab.Pipeline
	err       error
	floors    []time.Time
	jobs      []gitlab.FailedJob
}

func (f *fakeClient) RecentPipelines(_ context.Context, since time.Time) ([]gitlab.Pipeline, error) {
	f.floors = append(f.floors, since)
	return f.pipelines, f.err
}

func (f *fakeClient) Describe(_ context.Context, p gitlab.Pipeline) gitlab.Pipeline {
	p.Project = "dating/vision"
	return p
}

func (f *fakeClient) FailedJobs(context.Context, gitlab.Pipeline) ([]gitlab.FailedJob, error) {
	return f.jobs, nil
}

var now = time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)

func watcher(client *fakeClient, connected bool) *Watcher {
	return &Watcher{
		Connections: fakeConnections{connected},
		NewClient:   func(string, string) (Client, error) { return client, nil },
		Now:         func() time.Time { return now },
	}
}

func finished(id int, status string) gitlab.Pipeline {
	return gitlab.Pipeline{ID: id, ProjectID: 11, Ref: "feat", Status: status, FinishedAt: now.Add(-time.Duration(100-id) * time.Second)}
}

func TestTheFirstLookOnlyRecordsWhatAlreadyFinished(t *testing.T) {
	client := &fakeClient{pipelines: []gitlab.Pipeline{finished(1, "failed")}}
	first, err := watcher(client, true).Poll(context.Background(), nil)
	if err != nil || len(first.Incoming) != 0 || first.Cursor.Since != now.Unix() || len(first.Cursor.Seen) != 1 {
		t.Fatalf("first look: %+v %v", first, err)
	}
	second, err := watcher(client, true).Poll(context.Background(), &first.Cursor)
	if err != nil || len(second.Incoming) != 0 {
		t.Fatalf("what was recorded is not replayed: %+v %v", second.Incoming, err)
	}
}

func TestAPipelineBecomesARequestOnceAndAgainWhenItsOutcomeChanges(t *testing.T) {
	client := &fakeClient{}
	w := watcher(client, true)
	cursor := &engine.Cursor{Since: now.Add(-time.Minute).Unix()}

	client.pipelines = []gitlab.Pipeline{finished(2, "failed"), finished(1, "success")}
	poll, err := w.Poll(context.Background(), cursor)
	if err != nil || len(poll.Incoming) != 2 || !strings.HasPrefix(poll.Incoming[0].Text, "✅") {
		t.Fatalf("oldest first: %+v %v", poll.Incoming, err)
	}
	again, _ := w.Poll(context.Background(), &poll.Cursor)
	if len(again.Incoming) != 0 {
		t.Fatalf("handled twice: %+v", again.Incoming)
	}
	client.pipelines = []gitlab.Pipeline{finished(2, "success")}
	retried, _ := w.Poll(context.Background(), &poll.Cursor)
	if len(retried.Incoming) != 1 || !strings.HasPrefix(retried.Incoming[0].Text, "✅") {
		t.Fatalf("a failed pipeline that succeeds after a retry is news: %+v", retried.Incoming)
	}
	if want := cursor.Since - int64(lookback/time.Second); client.floors[0].Unix() != want {
		t.Errorf("the search starts a little before the last look: %v", client.floors[0])
	}
}

func TestAFailureCarriesItsLogsAndTheSourceOfAPipelineGroup(t *testing.T) {
	client := &fakeClient{
		pipelines: []gitlab.Pipeline{{ID: 9, ProjectID: 11, Ref: "feat", SHA: "abcdef1234567890", Status: "failed", WebURL: "https://gitlab.example.com/p/9", FinishedAt: now}},
		jobs:      []gitlab.FailedJob{{Name: "rspec", Stage: "test", FailureReason: "script_failure", LogTail: "expected 1, got 2"}},
	}
	poll, err := watcher(client, true).Poll(context.Background(), &engine.Cursor{Since: now.Add(-time.Minute).Unix()})
	if err != nil || len(poll.Incoming) != 1 {
		t.Fatalf("%+v %v", poll, err)
	}
	in := poll.Incoming[0]
	if in.Source.Origin != engine.OriginPipeline || in.Source.ChannelID != ChannelID || in.Source.SenderName != Sender || in.Source.Permalink == nil {
		t.Errorf("source: %+v", in.Source)
	}
	if !strings.HasPrefix(in.Text, "❌ dating/vision · feat\n") || !strings.Contains(in.Text, "Pipeline #9 failed") {
		t.Errorf("text: %q", in.Text)
	}
	if !strings.Contains(in.Context, `Failed job "rspec" (stage "test", reason: script_failure)`) || !strings.Contains(in.Context, "expected 1, got 2") {
		t.Errorf("context: %q", in.Context)
	}
}

func TestAMergeRequestPipelineIsNamedAfterItsMergeRequest(t *testing.T) {
	text, logs := Describe(gitlab.Pipeline{
		ID: 3, ProjectID: 11, Project: "dating/vision", Ref: "refs/merge-requests/7/head", Status: gitlab.StatusSuccess,
		MergeRequest: &gitlab.PipelineMergeRequest{IID: 7, Title: "Add vision API", WebURL: "https://gitlab.example.com/dating/vision/-/merge_requests/7"},
	}, nil, false)
	if !strings.HasPrefix(text, "✅ dating/vision · !7\n") || !strings.Contains(text, "merge_requests/7") || logs != "" {
		t.Errorf("%q %q", text, logs)
	}
}

func TestWithoutGitLabNothingHappensAndTheCursorStaysEmpty(t *testing.T) {
	poll, err := watcher(&fakeClient{}, false).Poll(context.Background(), nil)
	if err != nil || len(poll.Incoming) != 0 || poll.Cursor.Since != 0 {
		t.Fatalf("%+v %v", poll, err)
	}
	// An empty cursor counts as a first look, so connecting GitLab later never replays old pipelines.
	client := &fakeClient{pipelines: []gitlab.Pipeline{finished(1, "failed")}}
	poll, err = watcher(client, true).Poll(context.Background(), &poll.Cursor)
	if err != nil || len(poll.Incoming) != 0 || len(poll.Cursor.Seen) != 1 {
		t.Fatalf("%+v %v", poll, err)
	}
}

func TestFailuresOnlyKeepsTheSuccessesOutButRemembersThem(t *testing.T) {
	client := &fakeClient{pipelines: []gitlab.Pipeline{finished(1, "success"), finished(2, "failed")}}
	w := watcher(client, true)
	w.Mode = func() Mode { return ModeFailures }
	cursor := &engine.Cursor{Since: now.Add(-time.Minute).Unix()}

	poll, err := w.Poll(context.Background(), cursor)
	if err != nil || len(poll.Incoming) != 1 || !strings.HasPrefix(poll.Incoming[0].Text, "❌") {
		t.Fatalf("%+v %v", poll.Incoming, err)
	}
	w.Mode = func() Mode { return ModeAll }
	later, _ := w.Poll(context.Background(), &poll.Cursor)
	if len(later.Incoming) != 0 {
		t.Fatalf("the success skipped earlier does not come back when the mode widens: %+v", later.Incoming)
	}
}

func TestOffReadsNothingAndForgetsThePlace(t *testing.T) {
	client := &fakeClient{pipelines: []gitlab.Pipeline{finished(1, "failed")}}
	w := watcher(client, true)
	w.Mode = func() Mode { return ModeOff }
	poll, err := w.Poll(context.Background(), &engine.Cursor{Since: now.Unix()})
	if err != nil || len(poll.Incoming) != 0 || poll.Cursor.Since != 0 || len(client.floors) != 0 {
		t.Fatalf("%+v %v %v", poll, err, client.floors)
	}
}

func TestAGitLabFailureFailsThePoll(t *testing.T) {
	_, err := watcher(&fakeClient{err: errors.New("boom")}, true).Poll(context.Background(), nil)
	if err == nil {
		t.Fatal("the failure is reported, the engine shows it as a notice")
	}
}
