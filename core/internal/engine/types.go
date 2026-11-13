// Package engine is the assistant: it turns Slack messages into conversations, has the AI (Codex or Claude) investigate them, and
// carries out the changes the user approves. Its JSON shapes are the ones the web interface and the bubbles read.
package engine

import (
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// Status is where a request stands.
type Status string

const (
	StatusQueued       Status = "queued"
	StatusAnalyzing    Status = "analyzing"
	StatusAwaitingUser Status = "awaiting-user"
	StatusReady        Status = "ready"
	StatusApplying     Status = "applying"
	StatusDone         Status = "done"
	StatusIgnored      Status = "ignored"
	StatusFailed       Status = "failed"
)

// Pending statuses are those in which the assistant is working or about to.
func (s Status) Pending() bool {
	return s == StatusQueued || s == StatusAnalyzing || s == StatusApplying
}

// OriginPipeline is the origin of a request that no one wrote: a pipeline of the user's GitLab account finished.
const OriginPipeline = "pipeline"

// Source says where a request came from.
type Source struct {
	Origin      string  `json:"origin"`
	ChannelID   string  `json:"channelId"`
	ChannelName *string `json:"channelName"`
	SenderID    string  `json:"senderId"`
	SenderName  string  `json:"senderName"`
	TS          string  `json:"ts"`
	Permalink   *string `json:"permalink"`
	Label       *string `json:"label,omitempty"`
}

type Option struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Description *string `json:"description"`
}

type Question struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Options []Option `json:"options"`
	// Multiple lets the user pick several options together (several actions to take, for example); otherwise one.
	Multiple bool `json:"multiple,omitempty"`
}

// Proposal is a change the assistant wants to make; nothing is written before the user approves it.
type Proposal struct {
	// ID names this exact proposal (set when listed): a decision that carries it cannot approve another one.
	ID          string `json:"id,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ProjectID   string `json:"projectId"`
}

// AttachmentRef is a file that came with a message. The bytes stay on the server.
type AttachmentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	// Kind is "image" (shown to the AI as an image), "text" (read as text) or "other" (only listed).
	Kind string `json:"kind"`
}

type Entry struct {
	ID          string          `json:"id"`
	At          string          `json:"at"`
	Role        string          `json:"role"` // system | assistant | user
	Kind        string          `json:"kind"` // text | step
	Text        string          `json:"text"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// Request is a conversation as the interface sees it.
type Request struct {
	ID          string          `json:"id"`
	Source      Source          `json:"source"`
	Text        string          `json:"text"`
	Context     string          `json:"context"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	ReceivedAt  string          `json:"receivedAt"`
	UpdatedAt   string          `json:"updatedAt"`
	Status      Status          `json:"status"`
	Summary     *string         `json:"summary"`
	DraftReply  *string         `json:"draftReply"`
	Questions   []Question      `json:"questions"`
	Proposal    *Proposal       `json:"proposal"`
	// Plan is the writes the assistant asks to make, waiting for approval or approved; absent until it writes a plan.
	Plan *Plan `json:"plan,omitempty"`
	// Grants are the write approvals given in this conversation; absent until the user approves a plan.
	Grants  Grants  `json:"grants,omitempty"`
	Entries []Entry `json:"entries"`
	Error   *string `json:"error"`
	// Continuable is set when listed: the AI has a conversation a follow-up can continue.
	Continuable bool `json:"continuable,omitempty"`
	// Engaged is set once the request has reached the user (a question, an answer, a failure, an applied change).
	// From then on its bubble stays while the assistant works on it; before, the work is silent.
	Engaged bool `json:"engaged,omitempty"`
	// Closed is set when the user closed its bubble. The bubble comes back the next time the request needs them.
	Closed bool `json:"closed,omitempty"`
	// Tokens are what the AI used on this conversation; absent until it used any.
	Tokens *Tokens `json:"tokens,omitempty"`
}

// Stored is a request as kept on disk: the visible part plus what stays on the server.
type Stored struct {
	Request
	ThreadID string `json:"threadId,omitempty"`
	// ServicesUsed is set when text from Asana, GitLab or Slite was given to the analysis: written by other people, it
	// may have shaped the proposal, so an automatic approval must not apply it.
	ServicesUsed bool `json:"servicesUsed,omitempty"`
	// WebUsed is set when a turn ran with the web allowed: what the web returns is written by strangers, so, like
	// ServicesUsed, it means an automatic approval must not apply a proposal.
	WebUsed bool `json:"webUsed,omitempty"`
	// ThreadProvider is the AI that owns ThreadID; empty for a thread kept before Claude existed, which is Codex's.
	ThreadProvider providers.ID `json:"threadProvider,omitempty"`
	// PendingFiles are Slack files not downloaded yet.
	PendingFiles []PendingFile `json:"pendingFiles,omitempty"`
}

// threadOwner is the AI that owns the saved thread: Codex for a thread kept before Claude existed.
func (s Stored) threadOwner() providers.ID {
	if s.ThreadProvider == "" {
		return providers.Codex
	}
	return s.ThreadProvider
}

// PendingFile is a Slack file to download when the request is analyzed.
type PendingFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Status of the whole assistant, for the interface.
type StatusInfo struct {
	Active     bool     `json:"active"`
	Problem    *string  `json:"problem"`
	LastPollAt *string  `json:"lastPollAt"`
	Notices    []string `json:"notices"`
	Usage      Usage    `json:"usage"`
	// Provider is the AI that runs the next turn.
	Provider ProviderInfo `json:"provider"`
}

// ProviderInfo names the AI and the model that run the next turn; Model is "" when the CLI picks its default.
type ProviderInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
}

// Usage is the AI turns used today, and the daily limit (nil when there is none).
type Usage struct {
	Today int  `json:"today"`
	Limit *int `json:"limit"`
	// TokensByAI are what each AI used today for Pouple; an AI that used none is absent.
	TokensByAI map[providers.ID]Tokens `json:"tokensByAI,omitempty"`
	// Limits are each AI's own usage windows (five hours, week), as it last reported them; an AI that never did is
	// absent.
	Limits map[providers.ID]Limits `json:"limits,omitempty"`
}

// Snapshot is what GET /api/assistant returns and the stream repeats.
type Snapshot struct {
	Status   StatusInfo `json:"status"`
	Requests []Request  `json:"requests"`
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func ptr[T any](v T) *T { return &v }

// Answer is the user's reply to one question: a proposed option, or free text.
type Answer struct {
	QuestionID string `json:"questionId"`
	OptionID   string `json:"optionId,omitempty"`
	// OptionIDs are the options picked for a question that allows several.
	OptionIDs []string `json:"optionIds,omitempty"`
	Text      string   `json:"text,omitempty"`
}

// chosen are the options an answer picked: one for a single choice, possibly several for a multiple one.
func (a Answer) chosen() []string {
	if a.OptionID == "" {
		return a.OptionIDs
	}
	return append([]string{a.OptionID}, a.OptionIDs...)
}

// reachesUser says whether a request in this status needs the user or tells them something: entering it engages the
// request and opens its bubble again.
func (s Status) reachesUser() bool {
	return s == StatusAwaitingUser || s == StatusReady || s == StatusFailed || s == StatusDone
}
