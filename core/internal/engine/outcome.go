package engine

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// OutcomeSchema is given to the AI CLI (`codex exec --output-schema`, `claude --json-schema`): every analysis turn must
// end with exactly this shape.
const OutcomeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["outcome", "summary", "reply", "questions", "proposal", "sourcesUsed", "lookups"],
  "properties": {
    "sourcesUsed": {"type": "array", "items": {"type": "string", "enum": ["memory", "slack", "files", "docs", "asana", "gitlab", "slite"]}},
    "outcome": {"type": "string", "enum": ["not_a_request", "answered", "needs_input", "proposes_change", "lookup"]},
    "summary": {"type": "string"},
    "lookups": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["service", "action", "target"],
        "properties": {
          "service": {"type": "string", "enum": ["slite", "gitlab"]},
          "action": {"type": "string", "enum": ["search", "children", "read", "ask", "mr", "diff"]},
          "target": {"type": "string"}
        }
      }
    },
    "reply": {"type": ["string", "null"]},
    "questions": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["text", "options"],
        "properties": {
          "text": {"type": "string"},
          "options": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["label", "description"],
              "properties": {"label": {"type": "string"}, "description": {"type": ["string", "null"]}}
            }
          }
        }
      }
    },
    "proposal": {
      "type": ["object", "null"],
      "additionalProperties": false,
      "required": ["title", "description", "projectId"],
      "properties": {"title": {"type": "string"}, "description": {"type": "string"}, "projectId": {"type": "string"}}
    }
  }
}`

// Outcome names.
const (
	NotARequest    = "not_a_request"
	Answered       = "answered"
	NeedsInput     = "needs_input"
	ProposesChange = "proposes_change"
	// LookupOutcome is not an answer: the AI wants more from a connected service first.
	LookupOutcome = "lookup"
)

var contextSources = []string{"memory", "slack", "files", "docs", "asana", "gitlab", "slite"}

type OutcomeOption struct {
	Label       string  `json:"label"`
	Description *string `json:"description"`
}

type OutcomeQuestion struct {
	Text    string          `json:"text"`
	Options []OutcomeOption `json:"options"`
}

// Outcome is the validated final answer of an analysis turn.
type Outcome struct {
	Outcome     string            `json:"outcome"`
	Summary     string            `json:"summary"`
	Reply       *string           `json:"reply"`
	Questions   []OutcomeQuestion `json:"questions"`
	Proposal    *Proposal         `json:"proposal"`
	SourcesUsed []string          `json:"sourcesUsed"`
	Lookups     []Lookup          `json:"lookups"`
}

// ErrOutcome is returned for a final message that does not follow the schema: a model that drifts is reported,
// never trusted.
var ErrOutcome = errors.New("unexpected answer from the AI")

const (
	maxQuestions = 4
	maxOptions   = 6
	// maxLookups is how many lookups one answer may ask for, and maxLookupTarget how long one request may be.
	maxLookups      = 4
	maxLookupTarget = 500
)

// lookupActions are the lookups each service understands.
var lookupActions = map[string][]string{
	"slite":  {"search", "children", "read", "ask"},
	"gitlab": {"mr", "diff"},
}

// ParseOutcome validates the final message of a turn.
func ParseOutcome(text string) (Outcome, error) {
	var raw Outcome
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI did not answer with the expected JSON")
	}
	if !slices.Contains([]string{NotARequest, Answered, NeedsInput, ProposesChange, LookupOutcome}, raw.Outcome) {
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI answered with an unexpected shape")
	}
	out := Outcome{Outcome: raw.Outcome, Summary: raw.Summary, SourcesUsed: []string{}, Questions: []OutcomeQuestion{}, Lookups: []Lookup{}}
	if raw.Reply != nil && strings.TrimSpace(*raw.Reply) != "" {
		out.Reply = raw.Reply
	}
	for _, question := range raw.Questions {
		if len(out.Questions) == maxQuestions {
			break
		}
		kept := OutcomeQuestion{Text: question.Text, Options: []OutcomeOption{}}
		for _, option := range question.Options {
			if len(kept.Options) < maxOptions && strings.TrimSpace(option.Label) != "" {
				kept.Options = append(kept.Options, option)
			}
		}
		out.Questions = append(out.Questions, kept)
	}
	if raw.Proposal != nil {
		out.Proposal = raw.Proposal
	}
	for _, lookup := range raw.Lookups {
		target := strings.TrimSpace(lookup.Target)
		if len(out.Lookups) == maxLookups || !slices.Contains(lookupActions[lookup.Service], lookup.Action) || target == "" {
			continue
		}
		out.Lookups = append(out.Lookups, Lookup{Service: lookup.Service, Action: lookup.Action, Target: clipRunes(target, maxLookupTarget)})
	}
	switch {
	case out.Outcome == LookupOutcome && len(out.Lookups) == 0:
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI asked for a lookup without saying what to look for")
	case out.Outcome == NeedsInput && len(out.Questions) == 0:
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI asked for input without any question")
	case out.Outcome == ProposesChange && out.Proposal == nil:
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI proposed a change without saying what or where")
	}
	for _, source := range raw.SourcesUsed {
		if slices.Contains(contextSources, source) && !slices.Contains(out.SourcesUsed, source) {
			out.SourcesUsed = append(out.SourcesUsed, source)
		}
	}
	return out, nil
}

// Redact applies a redactor to everything the model wrote.
func (o Outcome) Redact(redact Redactor) Outcome {
	out := o
	out.Summary = redact(o.Summary)
	if o.Reply != nil {
		out.Reply = ptr(redact(*o.Reply))
	}
	out.Questions = make([]OutcomeQuestion, len(o.Questions))
	for i, question := range o.Questions {
		out.Questions[i] = OutcomeQuestion{Text: redact(question.Text), Options: make([]OutcomeOption, len(question.Options))}
		for j, option := range question.Options {
			out.Questions[i].Options[j] = OutcomeOption{Label: redact(option.Label)}
			if option.Description != nil {
				out.Questions[i].Options[j].Description = ptr(redact(*option.Description))
			}
		}
	}
	out.Lookups = make([]Lookup, len(o.Lookups))
	for i, lookup := range o.Lookups {
		out.Lookups[i] = Lookup{Service: lookup.Service, Action: lookup.Action, Target: redact(lookup.Target)}
	}
	if o.Proposal != nil {
		out.Proposal = &Proposal{Title: redact(o.Proposal.Title), Description: redact(o.Proposal.Description), ProjectID: o.Proposal.ProjectID}
	}
	return out
}
