package engine

import (
	"context"
	"slices"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// ServiceQuery is what the optional connected services (Asana, GitLab, Slite) are asked about.
type ServiceQuery struct {
	// Text is the Slack message being investigated.
	Text string
}

// ServiceStep is one line of the journal about a lookup in a connected service.
type ServiceStep struct {
	// Source is the service id: "asana", "gitlab" or "slite".
	Source  string
	Message i18n.Message
	// Failed is set when the lookup did not work: the data is missing, whatever Codex relies on.
	Failed bool
}

// ServiceContext is what the connected services say that may help with a request. Text is untrusted: it holds
// titles and notes written by other people. It reaches Codex only inside a delimited block, as data.
type ServiceContext struct {
	Text  string
	Steps []ServiceStep
}

// Lookup is something the assistant asks a connected service for after reading the first context: a search with other
// words, the content of a folder, a note, or a question put to the service's own assistant.
type Lookup struct {
	Service string `json:"service"`
	Action  string `json:"action"`
	Target  string `json:"target"`
}

// maxServiceChars bounds what the connected services add to a prompt: enough for a merge request with its code.
const maxServiceChars = 40000

// serviceSources are the sourcesUsed values that name a connected service.
var serviceSources = []string{"asana", "gitlab", "slite"}

var serviceNames = map[string]string{"asana": "Asana", "gitlab": "GitLab", "slite": "Slite"}

func clipRunes(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max-1]) + "…"
}

// gatherServices asks the connected services for data about the request and writes what was looked up in the
// journal. It never fails: a service that does not answer is a line in the journal and a note in the context.
func (s *Service) gatherServices(ctx context.Context, request Stored) string {
	if s.deps.Services == nil {
		return ""
	}
	return s.recordServices(request.ID, s.deps.Services(ctx, ServiceQuery{Text: request.Text}))
}

// recordServices writes what the services were asked in the journal and returns the text to hand to the AI.
func (s *Service) recordServices(id string, result ServiceContext) string {
	if len(result.Steps) > 0 {
		lang := s.deps.Language()
		entries := make([]Entry, len(result.Steps))
		for i, step := range result.Steps {
			entries[i] = s.entry("assistant", "step", step.Message.Text(lang))
		}
		s.update(id, func(r *Stored) { r.Entries = append(r.Entries, entries...) })
	}
	text := clipRunes(strings.TrimSpace(result.Text), maxServiceChars)
	if text != "" {
		s.update(id, func(r *Stored) { r.ServicesUsed = true })
	}
	return text
}

// noteServiceSources says in the journal which connected services the answer really relied on.
func (s *Service) noteServiceSources(id string, outcome Outcome) {
	var names []string
	for _, source := range serviceSources {
		if slices.Contains(outcome.SourcesUsed, source) {
			names = append(names, serviceNames[source])
		}
	}
	if len(names) == 0 {
		return
	}
	text := s.text("Relied on: %s", strings.Join(names, ", "))
	s.update(id, func(r *Stored) { r.Entries = append(r.Entries, s.entry("assistant", "step", text)) })
}
