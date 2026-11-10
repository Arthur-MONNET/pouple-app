// Package pipelinewatch turns the pipelines of the user's GitLab account that just finished, failed or succeeded,
// into requests for the assistant, the way Slack messages become requests. It only reads GitLab.
package pipelinewatch

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// lookback is how far before the last poll pipelines are looked for again: the same pipeline is never handled
	// twice (see Cursor.Seen), a pipeline that finished while the last poll ran is not missed.
	lookback = 15 * time.Minute
	// maxPerPoll bounds how many pipelines become requests at once; the others come at the next poll.
	maxPerPoll = 8
	maxSeen    = 500

	// ChannelID names the one group all the pipelines are listed under.
	ChannelID = "gitlab-pipelines"
	// Sender is how the pipelines sign their requests.
	Sender = "GitLab"
)

// Mode is which finished pipelines become requests.
type Mode string

const (
	// ModeAll turns every finished pipeline into a request; it is the default.
	ModeAll Mode = "all"
	// ModeFailures only the failed ones.
	ModeFailures Mode = "failures"
	// ModeOff none: GitLab is not even read.
	ModeOff Mode = "off"
)

// Connections is what the watcher needs from the connector registry.
type Connections interface {
	Status(id string) (connector.Status, error)
	Values(id string) (connector.Values, error)
}

// Client is the part of the GitLab client the watcher uses.
type Client interface {
	RecentPipelines(ctx context.Context, since time.Time) ([]gitlab.Pipeline, error)
	Describe(ctx context.Context, pipeline gitlab.Pipeline) gitlab.Pipeline
	FailedJobs(ctx context.Context, pipeline gitlab.Pipeline) ([]gitlab.FailedJob, error)
}

// Watcher reads the GitLab connection at each poll, so a token connected or replaced while Pouple runs applies at once.
type Watcher struct {
	Connections Connections
	NewClient   func(address, token string) (Client, error)
	Now         func() time.Time
	// Mode is read at each poll, so a change on the connector's card applies at once; nil means every pipeline.
	Mode func() Mode
}

// New builds a watcher on the registry, with the real GitLab client.
func New(connections Connections, mode func() Mode) *Watcher {
	return &Watcher{
		Connections: connections, Now: time.Now, Mode: mode,
		NewClient: func(address, token string) (Client, error) {
			client, err := gitlab.NewClient(address, token)
			return client, err
		},
	}
}

func seenKey(pipeline gitlab.Pipeline) string {
	return strconv.Itoa(pipeline.ProjectID) + ":" + strconv.Itoa(pipeline.ID) + ":" + pipeline.Status
}

// Poll returns the pipelines that finished since cursor, oldest first. A nil cursor is the first look: it records
// what already finished and returns nothing, so connecting GitLab never replays old pipelines. Without a GitLab
// connection it returns nothing and an empty cursor, which counts as a first look when GitLab gets connected.
func (w *Watcher) Poll(ctx context.Context, cursor *engine.Cursor) (engine.PipelinePoll, error) {
	mode := ModeAll
	if w.Mode != nil {
		mode = w.Mode()
	}
	// Off reads nothing and keeps nothing: turning it back on is a first look, so what finished meanwhile is not replayed.
	if mode == ModeOff {
		return engine.PipelinePoll{}, nil
	}
	status, err := w.Connections.Status(gitlab.ID)
	if err != nil {
		return engine.PipelinePoll{}, i18n.Wrap(err, "the GitLab connection could not be read")
	}
	if !status.Connected {
		return engine.PipelinePoll{}, nil
	}
	values, err := w.Connections.Values(gitlab.ID)
	if err != nil {
		return engine.PipelinePoll{}, i18n.Wrap(err, "the saved GitLab connection could not be used")
	}
	client, err := w.NewClient(values["url"], values["token"])
	if err != nil {
		return engine.PipelinePoll{}, i18n.Wrap(err, "the saved GitLab connection could not be used")
	}
	if cursor != nil && cursor.Since <= 0 {
		cursor = nil
	}
	started := w.Now()
	floor := started.Add(-lookback)
	if cursor != nil {
		floor = time.Unix(cursor.Since, 0).Add(-lookback)
	}
	found, err := client.RecentPipelines(ctx, floor)
	if err != nil {
		return engine.PipelinePoll{}, err
	}

	var seen []string
	if cursor != nil {
		seen = append(seen, cursor.Seen...)
	}
	var fresh []gitlab.Pipeline
	for _, pipeline := range found {
		if !slices.Contains(seen, seenKey(pipeline)) {
			fresh = append(fresh, pipeline)
		}
	}
	sort.SliceStable(fresh, func(a, b int) bool { return fresh[a].FinishedAt.Before(fresh[b].FinishedAt) })
	if cursor != nil && len(fresh) > maxPerPoll {
		fresh = fresh[:maxPerPoll]
	}

	var incoming []engine.Incoming
	for _, pipeline := range fresh {
		seen = append(seen, seenKey(pipeline))
		// Whatever the mode, a pipeline is remembered: switching to "all" later does not bring back old successes.
		if cursor == nil || (mode == ModeFailures && pipeline.Status != gitlab.StatusFailed) {
			continue
		}
		incoming = append(incoming, w.incomingFor(ctx, client, pipeline))
	}
	if len(seen) > maxSeen {
		seen = seen[len(seen)-maxSeen:]
	}
	return engine.PipelinePoll{Incoming: incoming, Cursor: engine.Cursor{Since: started.Unix(), Seen: seen}}, nil
}

func (w *Watcher) incomingFor(ctx context.Context, client Client, pipeline gitlab.Pipeline) engine.Incoming {
	pipeline = client.Describe(ctx, pipeline)
	var jobs []gitlab.FailedJob
	var jobsErr error
	if pipeline.Status == gitlab.StatusFailed {
		jobs, jobsErr = client.FailedJobs(ctx, pipeline)
	}
	text, logs := Describe(pipeline, jobs, jobsErr != nil)
	link := pipeline.WebURL
	source := engine.Source{
		Origin: engine.OriginPipeline, ChannelID: ChannelID, ChannelName: ptr("GitLab"), SenderID: "gitlab", SenderName: Sender,
		TS: fmt.Sprintf("%d.000000", pipeline.FinishedAt.Unix()),
	}
	if link != "" {
		source.Permalink = &link
	}
	return engine.Incoming{Source: source, Text: text, Context: logs}
}

func ptr[T any](v T) *T { return &v }

func commitShort(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// branchOf is the branch a pipeline ran on; "" for a merge request pipeline, whose ref is not a branch.
func branchOf(pipeline gitlab.Pipeline) string {
	if strings.HasPrefix(pipeline.Ref, "refs/merge-requests/") {
		return ""
	}
	return pipeline.Ref
}

// Describe writes what GitLab says about a finished pipeline, and apart the end of the logs of its failed jobs. The
// first line is what the conversation list shows: the outcome, the project and the branch or merge request.
func Describe(pipeline gitlab.Pipeline, jobs []gitlab.FailedJob, jobsUnreadable bool) (text, logs string) {
	mark, verb := "✅", "succeeded"
	if pipeline.Status == gitlab.StatusFailed {
		mark, verb = "❌", "failed"
	}
	project := pipeline.Project
	if project == "" {
		project = "project " + strconv.Itoa(pipeline.ProjectID)
	}
	where := branchOf(pipeline)
	if where == "" && pipeline.MergeRequest != nil {
		where = "!" + strconv.Itoa(pipeline.MergeRequest.IID)
	}
	title := mark + " " + project
	if where != "" {
		title += " · " + where
	}
	lines := []string{title, fmt.Sprintf("Pipeline #%d %s in %s.", pipeline.ID, verb, project)}
	if branch := branchOf(pipeline); branch != "" {
		lines = append(lines, "Branch: "+branch)
	}
	if pipeline.Source != "" {
		lines = append(lines, "Started by: "+pipeline.Source)
	}
	if pipeline.CommitTitle != "" {
		lines = append(lines, fmt.Sprintf("Commit: %s (%s)", pipeline.CommitTitle, commitShort(pipeline.SHA)))
	}
	if mr := pipeline.MergeRequest; mr != nil {
		lines = append(lines, fmt.Sprintf("Merge request: !%d %s %s", mr.IID, mr.Title, mr.WebURL))
	}
	if pipeline.WebURL != "" {
		lines = append(lines, "Pipeline: "+pipeline.WebURL)
	}
	if jobsUnreadable {
		lines = append(lines, "The failed jobs could not be read.")
	}
	var blocks []string
	for _, job := range jobs {
		header := fmt.Sprintf("Failed job %q (stage %q", job.Name, job.Stage)
		if job.FailureReason != "" {
			header += ", reason: " + job.FailureReason
		}
		header += ")"
		if job.AllowedToFail {
			header += ", allowed to fail"
		}
		if job.LogTail == "" {
			blocks = append(blocks, header+": the log could not be read.")
			continue
		}
		blocks = append(blocks, header+", end of its log:\n"+job.LogTail)
	}
	return strings.Join(lines, "\n"), strings.Join(blocks, "\n\n")
}
