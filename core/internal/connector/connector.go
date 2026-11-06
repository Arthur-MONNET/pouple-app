// Package connector describes the services Pouple connects to. A connector declares its form and its guide, so the
// setup screens are generated from data, and it validates what the user typed by asking the service who it is.
package connector

import (
	"context"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Field is one input of a connector's form.
type Field struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	// Default is used when the field is left empty (the form can prefill it). Secret fields have none.
	Default string `json:"default,omitempty"`
	// Secret fields are stored in the keychain and are never sent back to the interface.
	Secret bool `json:"secret"`
	// Optional fields may be omitted from the form.
	Optional bool `json:"optional,omitempty"`
	// Pattern is a regular expression the value must match; it lets the form say "this is not a Slack token" early.
	Pattern     string `json:"pattern,omitempty"`
	PatternHint string `json:"patternHint,omitempty"`
}

// Link is a button that opens a web page.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	// HostField names a field holding a web address (a self-hosted server). When the user has typed one, the form
	// may open URL with its scheme and host replaced by that address. Optional: URL alone always works.
	HostField string `json:"hostField,omitempty"`
}

// GuideStep is one step of the instructions shown beside the form.
type GuideStep struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Link  *Link  `json:"link,omitempty"`
}

// ActionInfo is something the setup screen can start for the connector (install a tool, sign in…).
type ActionInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Descriptor is everything the interface needs to draw a connector's setup.
type Descriptor struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Tagline  string       `json:"tagline"`
	Required bool         `json:"required"`
	Order    int          `json:"order"`
	Fields   []Field      `json:"fields"`
	Guide    []GuideStep  `json:"guide"`
	Actions  []ActionInfo `json:"actions,omitempty"`
}

// Fact is one line of a profile card.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Warning is something that works only partly with the connection as it is.
type Warning struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Profile says who the connection belongs to, so the user can check it is the right account before saving.
type Profile struct {
	Name      string    `json:"name"`
	Subtitle  string    `json:"subtitle,omitempty"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
	Facts     []Fact    `json:"facts"`
	Warnings  []Warning `json:"warnings"`
}

// Values are what the user typed, by field id.
type Values map[string]string

// ValidationError is a problem the user can fix: it names the field when there is one.
type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	text    *i18n.Message
}

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) ErrorText(lang i18n.Lang) string {
	if e.text == nil {
		return e.Message
	}
	return e.text.Text(lang)
}

// Invalid builds a ValidationError.
func Invalid(field, format string, args ...any) error {
	text := i18n.NewMessage(format, args...)
	return &ValidationError{Field: field, Message: text.Text(i18n.EN), text: &text}
}

// Connector is a service Pouple can connect to.
type Connector interface {
	Descriptor() Descriptor
	// Validate checks the values against the service and returns who they connect to. It saves nothing.
	Validate(ctx context.Context, values Values) (Profile, error)
}

// Event is one message of a long action (a line of output, an address to open, the end).
type Event struct {
	Type string `json:"type"` // log | open | done | error
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Action is a long task a connector offers during setup. It reports what happens through emit.
type Action func(ctx context.Context, emit func(Event)) error

// WithActions is implemented by connectors that offer actions.
type WithActions interface {
	Action(id string) (Action, bool)
}
