package remote

// Scope is what a remote route lets a paired phone do.
type Scope string

const (
	// ScopeRead reads the conversations and what they show.
	ScopeRead Scope = "read"
	// ScopeInteract acts on a conversation without approving a write: answers, messages, stop, retry, alerts.
	ScopeInteract Scope = "interact"
	// ScopeStepUp needs a fresh signature by the device's fingerprint-bound key on top of the token.
	ScopeStepUp Scope = "step-up"
)

// Facts are what a route's step-up condition can depend on.
type Facts struct {
	// Decision is the "decision" field of the JSON body ("approve", "decline", or anything else).
	Decision string
	// Granted is set when the conversation named in the path has write approvals (an approved plan): what is typed
	// there can lead to writes without a new approval.
	Granted bool
	// AutoApproveAll is set when the user lets every proposed change apply without asking.
	AutoApproveAll bool
}

// Rule is the policy of one allowed route.
type Rule struct {
	Scope Scope
	// StepUpWhen raises an interact route to a step-up in the cases it returns true for.
	StepUpWhen func(Facts) bool
	// Upload is a route whose body is a file: it is not buffered (no step-up condition may read it).
	Upload bool
}

// needsStepUp says whether a request on this route must carry a step-up signature.
func (r Rule) needsStepUp(facts Facts) bool {
	return r.Scope == ScopeStepUp || (r.StepUpWhen != nil && r.StepUpWhen(facts))
}

// unlessDeclined: approving needs the fingerprint, declining does not (and anything else is refused by the route).
func unlessDeclined(f Facts) bool { return f.Decision != "decline" }

// textThatCanWrite: text typed in a conversation that already has write approvals, or while every change applies
// without asking, can lead to writes no one reads first.
func textThatCanWrite(f Facts) bool { return f.Granted || f.AutoApproveAll }

// newConversationThatCanWrite: a new conversation has no approvals yet; only the setting can make it write unread.
func newConversationThatCanWrite(f Facts) bool { return f.AutoApproveAll }

// policy is the explicit allowlist of the remote API: the route patterns (as registered on the mux) a paired phone may
// call, and how. Every other route of the mux, and every path it does not know, is refused with 403: connectors and
// secrets, settings, providers, GitLab access, the terminal, updates, the memory and the writing style are never
// reachable from a phone.
var policy = map[string]Rule{
	"GET /api/assistant":        {Scope: ScopeRead},
	"GET /api/assistant/stream": {Scope: ScopeRead},
	"GET /api/assistant/requests/{id}/attachments/{attachmentId}": {Scope: ScopeRead},
	"GET /api/assistant/requests/{id}/browser":                    {Scope: ScopeRead},

	"POST /api/assistant/requests/{id}/answers":       {Scope: ScopeInteract, StepUpWhen: textThatCanWrite},
	"POST /api/assistant/requests/{id}/messages":      {Scope: ScopeInteract, StepUpWhen: textThatCanWrite},
	"POST /api/assistant/requests/{id}/reminders":     {Scope: ScopeInteract},
	"POST /api/assistant/requests/{id}/stop":          {Scope: ScopeInteract},
	"POST /api/assistant/requests/{id}/retry":         {Scope: ScopeInteract},
	"POST /api/assistant/requests/{id}/ignore":        {Scope: ScopeInteract},
	"POST /api/assistant/requests/{id}/close":         {Scope: ScopeInteract},
	"POST /api/assistant/requests/{id}/continue-with": {Scope: ScopeInteract},
	"POST /api/assistant/requests/{id}/attachments":   {Scope: ScopeInteract, Upload: true},
	"POST /api/assistant/requests":                    {Scope: ScopeInteract, StepUpWhen: newConversationThatCanWrite},
	"POST /api/assistant/attachments":                 {Scope: ScopeInteract, Upload: true},
	"GET /api/assistant/projects":                     {Scope: ScopeInteract},
	"POST /api/alerts":                                {Scope: ScopeInteract},
	"PATCH /api/alerts/{id}":                          {Scope: ScopeInteract},
	"DELETE /api/alerts/{id}":                         {Scope: ScopeInteract},

	"POST /api/assistant/requests/{id}/proposal": {Scope: ScopeInteract, StepUpWhen: unlessDeclined},
	"POST /api/assistant/requests/{id}/plan":     {Scope: ScopeInteract, StepUpWhen: unlessDeclined},
	"DELETE /api/assistant/requests/{id}":        {Scope: ScopeStepUp},
}

// AllowedRoutes lists the route patterns of the allowlist, for the tests that check them against the mounted routes.
func AllowedRoutes() map[string]Scope {
	out := make(map[string]Scope, len(policy))
	for pattern, rule := range policy {
		out[pattern] = rule.Scope
	}
	return out
}
