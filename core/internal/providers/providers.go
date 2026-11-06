// Package providers names the AIs the assistant can run on (Codex CLI, Claude Code CLI) and holds what is true of
// them whatever the engine does: ids, display names, how a model name is validated, which models are suggested, and
// how the environment of a child process is kept clean. It depends on nothing, so settings, API and engine share it.
package providers

import (
	"regexp"
	"strings"
)

// ID is the id of a provider. It is also the id of the connector that signs the provider in.
type ID string

const (
	Codex  ID = "codex"
	Claude ID = "claude"
)

// All lists the providers in preference order: when both are connected and none is chosen, the first wins.
func All() []ID { return []ID{Codex, Claude} }

// Parse reads a provider id; ok is false for anything unknown (an empty string included).
func Parse(value string) (ID, bool) {
	for _, id := range All() {
		if string(id) == value {
			return id, true
		}
	}
	return "", false
}

// Name is the display name, used in the interface and in every notice that names the AI.
func (id ID) Name() string {
	switch id {
	case Claude:
		return "Claude"
	default:
		return "Codex"
	}
}

// Selection is what runs one turn: a provider and the model it should use ("" is the CLI's own default).
type Selection struct {
	Provider ID
	Model    string
}

const maxModelLength = 80

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]*$`)

// ValidModel reports whether a model name is safe to hand to a CLI: a short word made of letters, digits and . _ : / -
// and never starting with a dash, so it can never be read as an option. The empty name means "the CLI's default".
func ValidModel(model string) bool {
	return len(model) <= maxModelLength && !strings.HasPrefix(model, "-") && modelPattern.MatchString(model)
}

// Suggestion is a model the interface proposes. ID is what is stored; Label is English app copy to translate.
type Suggestion struct {
	ID    string
	Label string
}

// Suggestions are the models worth proposing for a provider, from the CLI's help and Anthropic's model overview.
// Codex cannot list its models without the network, so it proposes none: any model its CLI accepts can be typed.
func Suggestions(id ID) []Suggestion {
	if id != Claude {
		return nil
	}
	return []Suggestion{
		{"opus", "Opus: the most capable, always the latest"},
		{"sonnet", "Sonnet: fast and capable, always the latest"},
		{"haiku", "Haiku: the fastest, always the latest"},
		{"fable", "Fable: for the hardest tasks, always the latest"},
		{"claude-opus-5-5", "Claude Opus 5.5"},
		{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
		{"claude-haiku-5-5", "Claude Haiku 5.5"},
		{"claude-fable-5-1", "Claude Fable 5.1"},
	}
}

// ModelNote is English app copy shown beside the model field.
func ModelNote(id ID) string {
	if id == Claude {
		return "Leave empty to use Claude’s own default. Any model name your Claude CLI accepts can be typed."
	}
	return "Codex cannot list its models offline. Leave empty to use its default, or type any model your Codex CLI supports."
}

// ChildEnv is the environment for an AI CLI: the given one without Pouple’s own variables (the API token) and without
// the variables that pick another account, endpoint or cloud for Claude Code (ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL,
// CLAUDE_CODE_*…), then with set added. Pouple signs in on its own, so nothing from the parent may choose who is billed.
func ChildEnv(environ []string, set ...string) []string {
	out := make([]string, 0, len(environ)+len(set))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "POUPLE_") || strings.HasPrefix(name, "ANTHROPIC_") || strings.HasPrefix(name, "CLAUDE_") || name == "CLAUDECODE" {
			continue
		}
		out = append(out, entry)
	}
	return append(out, set...)
}
