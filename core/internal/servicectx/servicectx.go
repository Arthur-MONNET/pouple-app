// Package servicectx asks the optional services the user connected (Asana, GitLab, Slite) for what may help with
// a Slack request, and turns the answers into a block of text for the assistant.
//
// What it returns is data written by other people (task titles, branch names, notes). The caller hands it to
// Codex between delimiters, as data. Here, every third-party string is reduced to one quoted, bounded line, and
// the whole lookup has a hard time budget: a service that is slow, down or refused becomes one line saying so,
// never a failed turn.
//
// Credentials are read from the registry at each call, so a service connected or replaced while Pouple runs
// applies to the next request. Nothing here can write to a service.
package servicectx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/asana"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Budget is how long all the services together may take.
const Budget = 3 * time.Second

const (
	maxOpenTasks   = 20
	maxFoundTasks  = 8
	maxMergeReqs   = 10
	maxNotes       = 5
	maxTitle       = 120
	maxExcerpt     = 240
	maxNameOrLabel = 80
)

var errPanic = errors.New("lookup failed")

// Connections is what the gatherer needs from the connector registry.
type Connections interface {
	Status(id string) (connector.Status, error)
	Values(id string) (connector.Values, error)
}

// Asana is the part of the Asana client the gatherer uses.
type Asana interface {
	OpenTasks(ctx context.Context) (asana.Open, error)
	SearchTasks(ctx context.Context, words []string) ([]asana.Task, error)
}

// GitLab is the part of the GitLab client the gatherer uses.
type GitLab interface {
	Overview(ctx context.Context) (gitlab.Overview, error)
}

// Slite is the part of the Slite client the gatherer uses.
type Slite interface {
	Search(ctx context.Context, query string, limit int) ([]slite.Hit, error)
}

// Gatherer collects the context of a request from the connected services.
type Gatherer struct {
	Connections Connections
	Budget      time.Duration
	Now         func() time.Time

	NewAsana  func(token string) Asana
	NewGitLab func(address, token string) (GitLab, error)
	NewSlite  func(token string) Slite
}

// New builds a gatherer on the registry, with the real clients.
func New(connections Connections) *Gatherer {
	return &Gatherer{
		Connections: connections, Budget: Budget, Now: time.Now,
		NewAsana: func(token string) Asana { return asana.NewClient(token) },
		NewGitLab: func(address, token string) (GitLab, error) {
			client, err := gitlab.NewClient(address, token)
			return client, err
		},
		NewSlite: func(token string) Slite { return slite.NewClient(token) },
	}
}

// section is what one service contributes.
type section struct {
	source string
	lines  []string
	steps  []engine.ServiceStep
}

type job struct {
	source string
	name   string
	run    func(ctx context.Context) section
}

// Gather asks every connected service, in parallel, within the budget.
func (g *Gatherer) Gather(ctx context.Context, query engine.ServiceQuery) engine.ServiceContext {
	jobs := g.jobs(query)
	if len(jobs) == 0 {
		return engine.ServiceContext{}
	}
	ctx, cancel := context.WithTimeout(ctx, g.budget())
	defer cancel()

	results := make(chan section, len(jobs))
	for _, j := range jobs {
		go func() {
			defer func() {
				if recover() != nil {
					results <- failure(j.source, j.name, i18n.NewMessage("the service answered with an error"))
				}
			}()
			results <- j.run(ctx)
		}()
	}
	got := map[string]section{}
collect:
	for len(got) < len(jobs) {
		select {
		case result := <-results:
			got[result.source] = result
		case <-ctx.Done():
			break collect
		}
	}
	var out engine.ServiceContext
	var lines []string
	for _, j := range jobs {
		result, ok := got[j.source]
		if !ok {
			result = failure(j.source, j.name, i18n.NewMessage("timed out"))
		}
		lines = append(lines, result.lines...)
		out.Steps = append(out.Steps, result.steps...)
	}
	out.Text = strings.Join(lines, "\n")
	return out
}

func (g *Gatherer) budget() time.Duration {
	if g.Budget > 0 {
		return g.Budget
	}
	return Budget
}

func (g *Gatherer) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// failure is a service that gave nothing: one line for the assistant (so it does not read the silence as "no
// data"), one for the journal.
func failure(source, name string, reason i18n.Message) section {
	return section{
		source: source,
		lines:  []string{fmt.Sprintf("%s: unavailable (%s). Do not assume anything about its content.", name, reason.Text(i18n.EN))},
		steps:  []engine.ServiceStep{{Source: source, Message: i18n.NewMessage("%s: could not be read (%s)", name, reason), Failed: true}},
	}
}

// jobs lists the connected services, reading their credentials now.
func (g *Gatherer) jobs(query engine.ServiceQuery) []job {
	words := SearchTerms(query.Text)
	var jobs []job
	for _, source := range []string{asana.ID, gitlab.ID, slite.ID} {
		status, err := g.Connections.Status(source)
		if err != nil || !status.Connected {
			continue
		}
		name := status.Descriptor.Name
		values, err := g.Connections.Values(source)
		if err != nil {
			jobs = append(jobs, job{source, name, func(context.Context) section {
				return failure(source, name, i18n.NewMessage("the saved connection could not be used"))
			}})
			continue
		}
		switch source {
		case asana.ID:
			jobs = append(jobs, job{source, name, func(ctx context.Context) section {
				return g.asana(ctx, g.NewAsana(values["token"]), words)
			}})
		case gitlab.ID:
			jobs = append(jobs, job{source, name, func(ctx context.Context) section {
				client, err := g.NewGitLab(values["url"], values["token"])
				if err != nil {
					return failure(source, name, i18n.NewMessage("the saved connection could not be used"))
				}
				return g.gitlab(ctx, client)
			}})
		case slite.ID:
			if len(words) == 0 {
				continue // nothing to search for: small talk
			}
			jobs = append(jobs, job{source, name, func(ctx context.Context) section {
				return g.slite(ctx, g.NewSlite(values["token"]), words)
			}})
		}
	}
	return jobs
}

func (g *Gatherer) asana(ctx context.Context, client Asana, words []string) section {
	out := section{source: asana.ID}
	type openResult struct {
		open asana.Open
		err  error
	}
	type foundResult struct {
		tasks []asana.Task
		err   error
	}
	openCh, foundCh := make(chan openResult, 1), make(chan foundResult, 1)
	go func() {
		defer func() {
			if recover() != nil {
				openCh <- openResult{err: errPanic}
			}
		}()
		open, err := client.OpenTasks(ctx)
		openCh <- openResult{open, err}
	}()
	if len(words) > 0 {
		go func() {
			defer func() {
				if recover() != nil {
					foundCh <- foundResult{err: errPanic}
				}
			}()
			tasks, err := client.SearchTasks(ctx, words)
			foundCh <- foundResult{tasks, err}
		}()
	}
	mine := <-openCh
	shown := map[string]bool{}
	if mine.err != nil {
		out.lines = append(out.lines, fmt.Sprintf("Asana, the user's open tasks: unavailable (%s).", svchttp.Reason(mine.err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: asana.ID, Message: i18n.NewMessage("Asana: could not read your tasks (%s)", svchttp.Reason(mine.err)), Failed: true})
	} else {
		tasks := mine.open.Tasks[:min(len(mine.open.Tasks), maxOpenTasks)]
		total := fmt.Sprint(len(mine.open.Tasks))
		if mine.open.More {
			total += " or more"
		}
		out.lines = append(out.lines, fmt.Sprintf("Asana, the user's open tasks (%s in all, soonest due date first, the first %d at most):", total, maxOpenTasks))
		for _, task := range tasks {
			shown[task.GID] = true
			out.lines = append(out.lines, g.taskLine(task, false))
		}
		if len(tasks) == 0 {
			out.lines = append(out.lines, "- (none)")
		}
		out.steps = append(out.steps, engine.ServiceStep{Source: asana.ID, Message: i18n.NewMessage("Asana: read your open tasks (%d)", len(mine.open.Tasks))})
	}
	if len(words) == 0 {
		return out
	}
	found := <-foundCh
	query := strings.Join(words, " ")
	if found.err != nil {
		out.lines = append(out.lines, fmt.Sprintf("Asana, tasks matching the message: unavailable (%s).", svchttp.Reason(found.err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: asana.ID, Message: i18n.NewMessage("Asana: could not search tasks (%s)", svchttp.Reason(found.err)), Failed: true})
		return out
	}
	var matching []asana.Task
	for _, task := range found.tasks {
		if !shown[task.GID] && len(matching) < maxFoundTasks {
			matching = append(matching, task)
		}
	}
	out.lines = append(out.lines, fmt.Sprintf("Asana, other open tasks matching the message (search: %s):", query))
	for _, task := range matching {
		out.lines = append(out.lines, g.taskLine(task, true))
	}
	if len(matching) == 0 {
		out.lines = append(out.lines, "- (none)")
	}
	out.steps = append(out.steps, engine.ServiceStep{Source: asana.ID, Message: i18n.NewMessage("Asana: searched “%s” (%d tasks)", query, len(found.tasks))})
	return out
}

func (g *Gatherer) taskLine(task asana.Task, withAssignee bool) string {
	parts := []string{"- " + quote(task.Name, maxTitle)}
	if due := date(task.DueOn); due != "" {
		if due < g.now().Format("2006-01-02") {
			due += " (overdue)"
		}
		parts = append(parts, "due "+due)
	}
	if task.Section != "" {
		parts = append(parts, "section "+quote(task.Section, maxNameOrLabel))
	}
	if task.Project != "" {
		parts = append(parts, "project "+quote(task.Project, maxNameOrLabel))
	}
	if withAssignee && task.Assignee != "" {
		parts = append(parts, "assigned to "+quote(task.Assignee, maxNameOrLabel))
	}
	line := parts[0]
	if len(parts) > 1 {
		line += " (" + strings.Join(parts[1:], ", ") + ")"
	}
	if address := link(task.PermalinkURL); address != "" {
		line += " " + address
	}
	return line
}

func (g *Gatherer) gitlab(ctx context.Context, client GitLab) section {
	out := section{source: gitlab.ID}
	overview, err := client.Overview(ctx)
	if err != nil {
		out.lines = append(out.lines, fmt.Sprintf("GitLab, the user's merge requests: unavailable (%s).", svchttp.Reason(err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: gitlab.ID, Message: i18n.NewMessage("GitLab: could not read your merge requests (%s)", svchttp.Reason(err)), Failed: true})
		return out
	}
	out.lines = append(out.lines, fmt.Sprintf("GitLab, the user's open merge requests (%d):", overview.MineTotal))
	out.lines = append(out.lines, mergeRequestLines(overview.Mine)...)
	out.lines = append(out.lines, fmt.Sprintf("GitLab, merge requests waiting for the user's review (%d):", overview.ToReviewTotal))
	out.lines = append(out.lines, mergeRequestLines(overview.ToReview)...)
	out.steps = append(out.steps, engine.ServiceStep{Source: gitlab.ID, Message: i18n.NewMessage("GitLab: read your merge requests (%d open, %d to review)", overview.MineTotal, overview.ToReviewTotal)})
	return out
}

func mergeRequestLines(list []gitlab.MergeRequest) []string {
	var lines []string
	for _, mr := range list[:min(len(list), maxMergeReqs)] {
		details := []string{"branch " + quote(mr.SourceBranch, maxNameOrLabel) + " into " + quote(mr.TargetBranch, maxNameOrLabel)}
		if mr.Draft {
			details = append(details, "draft")
		}
		if mr.Author != "" {
			details = append(details, "by "+quote(mr.Author, maxNameOrLabel))
		}
		status := "none"
		if mr.PipelineStatus != "" {
			status = word(mr.PipelineStatus)
		}
		details = append(details, "pipeline: "+status)
		if mr.MergeStatus != "" {
			details = append(details, "merge status: "+word(mr.MergeStatus))
		}
		if mr.HasConflicts {
			details = append(details, "has conflicts")
		}
		if updated := date(mr.UpdatedAt); updated != "" {
			details = append(details, "updated "+updated)
		}
		line := fmt.Sprintf("- %s!%d %s (%s)", path(mr.ProjectPath), mr.IID, quote(mr.Title, maxTitle), strings.Join(details, ", "))
		if address := link(mr.WebURL); address != "" {
			line += " " + address
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, "- (none)")
	}
	return lines
}

func (g *Gatherer) slite(ctx context.Context, client Slite, words []string) section {
	out := section{source: slite.ID}
	query := strings.Join(words, " ")
	hits, err := client.Search(ctx, query, maxNotes)
	if err != nil {
		out.lines = append(out.lines, fmt.Sprintf("Slite, notes matching the message: unavailable (%s).", svchttp.Reason(err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: could not search (%s)", svchttp.Reason(err)), Failed: true})
		return out
	}
	hits = hits[:min(len(hits), maxNotes)]
	out.lines = append(out.lines, fmt.Sprintf("Slite, notes matching the message (search: %s; written by colleagues):", query))
	for _, hit := range hits {
		line := "- " + quote(hit.Title, maxTitle)
		var where []string
		if hit.Channel != "" {
			where = append(where, "in "+quote(hit.Channel, maxNameOrLabel))
		}
		if updated := date(hit.UpdatedAt); updated != "" {
			where = append(where, "updated "+updated)
		}
		if len(where) > 0 {
			line += " (" + strings.Join(where, ", ") + ")"
		}
		if hit.Excerpt != "" {
			line += ": excerpt " + quote(hit.Excerpt, maxExcerpt)
		}
		out.lines = append(out.lines, line)
	}
	if len(hits) == 0 {
		out.lines = append(out.lines, "- (no note found)")
	}
	out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: searched “%s” (%d notes)", query, len(hits))})
	return out
}
