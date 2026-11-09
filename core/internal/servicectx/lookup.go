package servicectx

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// lookupTimeout bounds one lookup the assistant asked for; a question put to Slite's own assistant gets longer.
	lookupTimeout = 15 * time.Second
	askLookup     = 70 * time.Second
)

func lookupLimit(lookup engine.Lookup) time.Duration {
	if lookup.Service == slite.ID && lookup.Action == "ask" {
		return askLookup
	}
	return lookupTimeout
}

type lookupRunner func(ctx context.Context, lookup engine.Lookup) section

// runnerFor is how the lookups of one service are run, or a section saying why they cannot be.
func (g *Gatherer) runnerFor(service string) (lookupRunner, section) {
	name := serviceName(service)
	status, err := g.Connections.Status(service)
	if err != nil || !status.Connected {
		return nil, section{source: service,
			lines: []string{name + ": not connected, nothing could be looked up."},
			steps: []engine.ServiceStep{{Source: service, Message: i18n.NewMessage("%s: not connected", name), Failed: true}}}
	}
	values, err := g.Connections.Values(service)
	if err != nil {
		return nil, failure(service, name, i18n.NewMessage("the saved connection could not be used"))
	}
	switch service {
	case slite.ID:
		client := g.NewSlite(values["token"])
		return func(ctx context.Context, lookup engine.Lookup) section { return g.sliteLookup(ctx, client, lookup) }, section{}
	case gitlab.ID:
		client, err := g.NewGitLab(values["url"], values["token"])
		if err != nil {
			return nil, failure(service, name, i18n.NewMessage("the saved connection could not be used"))
		}
		return func(ctx context.Context, lookup engine.Lookup) section { return g.gitlabLookup(ctx, client, lookup) }, section{}
	}
	return nil, section{source: service, lines: []string{name + ": this service cannot be looked up."}}
}

func serviceName(service string) string {
	switch service {
	case slite.ID:
		return "Slite"
	case gitlab.ID:
		return "GitLab"
	}
	return service
}

// Lookup answers what the assistant asked for after reading the first context: searches, folders and notes of Slite,
// merge requests and diffs of GitLab, in parallel, each within its own time limit. Like Gather it never fails: a lookup
// that does not work is a line saying so, so the silence is never read as "nothing exists".
func (g *Gatherer) Lookup(ctx context.Context, lookups []engine.Lookup) engine.ServiceContext {
	if len(lookups) == 0 {
		return engine.ServiceContext{}
	}
	runners := map[string]lookupRunner{}
	problems := map[string]section{}
	for _, lookup := range lookups {
		if _, known := runners[lookup.Service]; known {
			continue
		}
		if _, known := problems[lookup.Service]; known {
			continue
		}
		if runner, problem := g.runnerFor(lookup.Service); runner != nil {
			runners[lookup.Service] = runner
		} else {
			problems[lookup.Service] = problem
		}
	}

	sections := make([]section, len(lookups))
	var wait sync.WaitGroup
	reported := map[string]bool{}
	for i, lookup := range lookups {
		runner, ok := runners[lookup.Service]
		if !ok {
			// A service that cannot be reached is said once, not once per lookup.
			if !reported[lookup.Service] {
				reported[lookup.Service] = true
				sections[i] = problems[lookup.Service]
			}
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() {
				if recover() != nil {
					sections[i] = failure(lookup.Service, serviceName(lookup.Service), i18n.NewMessage("the service answered with an error"))
				}
			}()
			one, cancel := context.WithTimeout(ctx, lookupLimit(lookup))
			defer cancel()
			sections[i] = runner(one, lookup)
		}()
	}
	wait.Wait()

	var out engine.ServiceContext
	var lines []string
	for _, result := range sections {
		lines = append(lines, result.lines...)
		out.Steps = append(out.Steps, result.steps...)
	}
	out.Text = strings.Join(lines, "\n")
	return out
}
