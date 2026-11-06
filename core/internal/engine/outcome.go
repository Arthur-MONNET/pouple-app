package engine

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// OutcomeSchema is given to `codex exec --output-schema`: every analysis turn must end with exactly this shape.
const OutcomeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["outcome", "summary", "reply", "questions", "proposal", "sourcesUsed"],
  "properties": {
    "sourcesUsed": {"type": "array", "items": {"type": "string", "enum": ["memory", "slack", "files", "docs"]}},
    "outcome": {"type": "string", "enum": ["not_a_request", "answered", "needs_input", "proposes_change"]},
    "summary": {"type": "string"},
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
)

var contextSources = []string{"memory", "slack", "files", "docs"}

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
}

// ErrOutcome is returned for a final message that does not follow the schema: a model that drifts is reported,
// never trusted.
var ErrOutcome = errors.New("unexpected answer from Codex")

const (
	maxQuestions = 4
	maxOptions   = 6
)

// ParseOutcome validates the final message of a Codex turn.
func ParseOutcome(text string) (Outcome, error) {
	var raw Outcome
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return Outcome{}, errors.Join(ErrOutcome, errors.New("Codex did not answer with the expected JSON"))
	}
	if !slices.Contains([]string{NotARequest, Answered, NeedsInput, ProposesChange}, raw.Outcome) {
		return Outcome{}, errors.Join(ErrOutcome, errors.New("Codex answered with an unexpected shape"))
	}
	out := Outcome{Outcome: raw.Outcome, Summary: raw.Summary, SourcesUsed: []string{}, Questions: []OutcomeQuestion{}}
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
	switch {
	case out.Outcome == NeedsInput && len(out.Questions) == 0:
		return Outcome{}, errors.Join(ErrOutcome, errors.New("Codex asked for input without any question"))
	case out.Outcome == ProposesChange && out.Proposal == nil:
		return Outcome{}, errors.Join(ErrOutcome, errors.New("Codex proposed a change without saying what or where"))
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
	if o.Proposal != nil {
		out.Proposal = &Proposal{Title: redact(o.Proposal.Title), Description: redact(o.Proposal.Description), ProjectID: o.Proposal.ProjectID}
	}
	return out
}
