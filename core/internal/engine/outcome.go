package engine

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/alerts"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// OutcomeSchema is given to the AI CLI (`codex exec --output-schema`, `claude --json-schema`): every analysis turn must
// end with exactly this shape.
const OutcomeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["outcome", "summary", "reply", "questions", "proposal", "plan", "reminders", "memories", "sourcesUsed", "lookups"],
  "properties": {
    "sourcesUsed": {"type": "array", "items": {"type": "string", "enum": ["memory", "notes", "slack", "files", "docs", "asana", "gitlab", "slite"]}},
    "outcome": {"type": "string", "enum": ["not_a_request", "answered", "needs_input", "proposes_change", "lookup", "no_change", "proposes_plan"]},
    "summary": {"type": "string"},
    "lookups": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["service", "action", "target", "text"],
        "properties": {
          "service": {"type": "string", "enum": ["slite", "gitlab", "asana", "slack", "screenshot", "notes"]},
          "action": {"type": "string", "enum": ["search", "children", "read", "ask", "mr", "diff", "task", "comment", "thread", "around", "capture", "serve"]},
          "target": {"type": "string"},
          "text": {"type": "string"}
        }
      }
    },
    "reply": {"type": ["string", "null"]},
    "reminders": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["label", "note", "at"],
        "properties": {"label": {"type": "string"}, "note": {"type": "string"}, "at": {"type": "string"}}
      }
    },
    "memories": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["note", "title", "fact", "replaces", "remove", "lasts"],
        "properties": {"note": {"type": "string"}, "title": {"type": "string"}, "fact": {"type": "string"}, "replaces": {"type": "string"}, "remove": {"type": "boolean"}, "lasts": {"type": "string", "enum": ["weeks", "months", "years", ""]}}
      }
    },
    "questions": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["text", "multiple", "options"],
        "properties": {
          "text": {"type": "string"},
          "multiple": {"type": "boolean"},
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
    "plan": {
      "type": ["object", "null"],
      "additionalProperties": false,
      "required": ["goal", "brief", "actions"],
      "properties": {
        "goal": {"type": "string"},
        "brief": {"type": "string"},
        "actions": {
          "type": "array",
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["kind", "scope", "summary", "branch", "base"],
            "properties": {
              "kind": {"type": "string", "enum": ["code.edit", "code.sync"]},
              "branch": {"type": "string"},
              "base": {"type": "string"},
              "scope": {
                "type": "object",
                "additionalProperties": false,
                "required": ["service", "id", "name"],
                "properties": {"service": {"type": "string", "enum": ["code"]}, "id": {"type": "string"}, "name": {"type": "string"}}
              },
              "summary": {"type": "string"}
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
	// ProposesPlan asks the user to approve writes in one or more places (a plan), before anything is written.
	ProposesPlan = "proposes_plan"
	// LookupOutcome is not an answer: the AI wants more from a connected service first.
	LookupOutcome = "lookup"
	// NoChange is the answer to new messages of a thread that change nothing of what was said or proposed.
	NoChange = "no_change"
)

var contextSources = []string{"memory", "notes", "slack", "files", "docs", "asana", "gitlab", "slite"}

type OutcomeOption struct {
	Label       string  `json:"label"`
	Description *string `json:"description"`
}

type OutcomeQuestion struct {
	Text     string          `json:"text"`
	Multiple bool            `json:"multiple"`
	Options  []OutcomeOption `json:"options"`
}

// OutcomeReminder is one alert the assistant suggests: a short label, an optional note, and the local date and time
// ("2026-11-20T09:00", on the user's computer clock) it should fire at.
type OutcomeReminder struct {
	Label string `json:"label"`
	Note  string `json:"note"`
	At    string `json:"at"`
}

// Outcome is the validated final answer of an analysis turn.
type Outcome struct {
	Outcome   string            `json:"outcome"`
	Summary   string            `json:"summary"`
	Reply     *string           `json:"reply"`
	Questions []OutcomeQuestion `json:"questions"`
	Proposal  *Proposal         `json:"proposal"`
	Plan      *Plan             `json:"plan"`
	// Reminders are alerts the assistant suggests, for the user to pick from; Pouple sets none by itself.
	Reminders []OutcomeReminder `json:"reminders"`
	// Memories are the changes the assistant makes to the memory: facts to add, correct or take out.
	Memories    []OutcomeMemory `json:"memories"`
	SourcesUsed []string        `json:"sourcesUsed"`
	Lookups     []Lookup        `json:"lookups"`
}

// ErrOutcome is returned for a final message that does not follow the schema: a model that drifts is reported,
// never trusted.
var ErrOutcome = errors.New("unexpected answer from the AI")

const (
	maxQuestions = 4
	maxOptions   = 6
	// maxReminders is how many alerts one answer may suggest.
	maxReminders = 6
	// maxLookups is how many lookups one answer may ask for, and maxLookupTarget how long one request may be.
	maxLookups      = 4
	maxLookupTarget = 500
	// maxCommentText bounds the comment a lookup posts.
	maxCommentText = 4000
)

// lookupActions are the lookups each service understands.
var lookupActions = map[string][]string{
	"slite":  {"search", "children", "read", "ask"},
	"gitlab": {"mr", "diff"},
	"asana":  {"task", "comment"},
	// slack: "search" looks for messages like Slack's search box does; "thread" and "around" open a message found by it.
	"slack": {"search", "thread", "around"},
	// screenshot: "capture" opens a page in a browser (the lookup's text is the screen size, "" or "mobile"); "serve" starts
	// a project's dev server and waits for it (the target is the project, the text the script to start, "" for the usual).
	"screenshot": {"capture", "serve"},
	// notes: "search" finds lines of the memory, "read" gives one of its notes (the target is its path in the index).
	"notes": {"search", "read"},
}

// ParseOutcome validates the final message of a turn.
func ParseOutcome(text string) (Outcome, error) {
	var raw Outcome
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI did not answer with the expected JSON")
	}
	if !slices.Contains([]string{NotARequest, Answered, NeedsInput, ProposesChange, LookupOutcome, NoChange, ProposesPlan}, raw.Outcome) {
		return Outcome{}, i18n.Wrap(ErrOutcome, "The AI answered with an unexpected shape")
	}
	out := Outcome{Outcome: raw.Outcome, Summary: raw.Summary, SourcesUsed: []string{}, Questions: []OutcomeQuestion{}, Lookups: []Lookup{}, Reminders: []OutcomeReminder{}, Memories: parseMemories(raw.Memories)}
	if raw.Reply != nil && strings.TrimSpace(*raw.Reply) != "" {
		out.Reply = raw.Reply
	}
	for _, question := range raw.Questions {
		if len(out.Questions) == maxQuestions {
			break
		}
		kept := OutcomeQuestion{Text: question.Text, Multiple: question.Multiple, Options: []OutcomeOption{}}
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
	if raw.Plan != nil {
		out.Plan = raw.Plan
	}
	for _, reminder := range raw.Reminders {
		// A title is one line: the line breaks and tabs the AI may write would make the alert refused once picked.
		label := clipRunes(strings.Join(strings.Fields(reminder.Label), " "), alerts.MaxTitle)
		note := clipRunes(strings.TrimSpace(strings.ReplaceAll(reminder.Note, "\t", " ")), alerts.MaxNote)
		// A suggestion with no label, an unreadable moment or a text no alert could hold is dropped, like a lookup that
		// names nothing: the answer stays usable.
		if _, err := alerts.ParseWhen(reminder.At, time.UTC); len(out.Reminders) == maxReminders || err != nil || alerts.Check(label, note) != nil {
			continue
		}
		out.Reminders = append(out.Reminders, OutcomeReminder{Label: label, Note: note, At: strings.TrimSpace(reminder.At)})
	}
	for _, lookup := range raw.Lookups {
		target := strings.TrimSpace(lookup.Target)
		if len(out.Lookups) == maxLookups || !slices.Contains(lookupActions[lookup.Service], lookup.Action) || target == "" {
			continue
		}
		kept := Lookup{Service: lookup.Service, Action: lookup.Action, Target: clipRunes(target, maxLookupTarget)}
		if kept.IsScreenshot() {
			// A screen size is a word; what to start is a script name or a command.
			limit := 12
			if kept.Action == "serve" {
				limit = 300
			}
			kept.Text = clipRunes(strings.TrimSpace(lookup.Text), limit)
		}
		if kept.Writes() {
			if kept.Text = strings.TrimSpace(lookup.Text); kept.Text == "" {
				continue
			}
			kept.Text = clipRunes(kept.Text, maxCommentText)
		}
		out.Lookups = append(out.Lookups, kept)
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
		out.Questions[i] = OutcomeQuestion{Text: redact(question.Text), Multiple: question.Multiple, Options: make([]OutcomeOption, len(question.Options))}
		for j, option := range question.Options {
			out.Questions[i].Options[j] = OutcomeOption{Label: redact(option.Label)}
			if option.Description != nil {
				out.Questions[i].Options[j].Description = ptr(redact(*option.Description))
			}
		}
	}
	out.Reminders = make([]OutcomeReminder, len(o.Reminders))
	for i, reminder := range o.Reminders {
		out.Reminders[i] = OutcomeReminder{Label: redact(reminder.Label), Note: redact(reminder.Note), At: reminder.At}
	}
	out.Memories = make([]OutcomeMemory, len(o.Memories))
	for i, fact := range o.Memories {
		out.Memories[i] = OutcomeMemory{Note: fact.Note, Title: redact(fact.Title), Fact: redact(fact.Fact), Replaces: fact.Replaces, Remove: fact.Remove, Lasts: fact.Lasts}
	}
	out.Lookups = make([]Lookup, len(o.Lookups))
	for i, lookup := range o.Lookups {
		out.Lookups[i] = Lookup{Service: lookup.Service, Action: lookup.Action, Target: redact(lookup.Target), Text: redact(lookup.Text)}
	}
	if o.Proposal != nil {
		out.Proposal = &Proposal{Title: redact(o.Proposal.Title), Description: redact(o.Proposal.Description), ProjectID: o.Proposal.ProjectID}
	}
	if o.Plan != nil {
		plan := *o.Plan
		plan.Goal, plan.Brief = redact(o.Plan.Goal), redact(o.Plan.Brief)
		plan.Actions = make([]Action, len(o.Plan.Actions))
		for i, action := range o.Plan.Actions {
			action.Summary = redact(action.Summary)
			plan.Actions[i] = action
		}
		out.Plan = &plan
	}
	return out
}
