package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// A write goes through a plan the user approves. The investigating AI, which reads the request, writes the plan; the AI
// that writes receives the plan and never the request's own text. Approving a plan grants its scopes for the rest of the
// conversation, and an action outside the granted scopes needs a new approval. See docs/PRODUCT.md, "Writing".

// ScopeService is a service Pouple may write in once the user agrees.
type ScopeService string

const (
	// ScopeCode is a project folder on this computer.
	ScopeCode ScopeService = "code"
	// ScopeGitLab is a GitLab project.
	ScopeGitLab ScopeService = "gitlab"
	// ScopeAsana is an Asana project.
	ScopeAsana ScopeService = "asana"
	// ScopeSlite is a Slite space.
	ScopeSlite ScopeService = "slite"
)

// Scope is one place a write approval covers: a project folder, a GitLab project, an Asana project or a Slite space.
type Scope struct {
	Service ScopeService `json:"service"`
	// ID is the service's own identifier (the folder name, the GitLab project path, the Asana project gid, the Slite
	// space id). It is the only part that matches.
	ID string `json:"id"`
	// Name is what the user reads. It plays no part in matching.
	Name string `json:"name,omitempty"`
}

func (s Scope) same(other Scope) bool { return s.Service == other.Service && s.ID == other.ID }

// ActionKind is a kind of write. Each kind writes in one service, so an action can only name a scope of that service.
type ActionKind string

const (
	// EditCode changes files and commits them on a branch of their own, in a worktree of a project folder.
	EditCode ActionKind = "code.edit"
	// PushBranch pushes that branch to the GitLab project.
	PushBranch ActionKind = "gitlab.push"
	// MergeRequest opens the merge request of that branch, and updates it after a failed pipeline or a review.
	MergeRequest ActionKind = "gitlab.merge-request"
	CommentTask  ActionKind = "asana.comment"
	CreateTask   ActionKind = "asana.create-task"
	WriteNote    ActionKind = "slite.write"
)

var actionServices = map[ActionKind]ScopeService{
	EditCode:     ScopeCode,
	PushBranch:   ScopeGitLab,
	MergeRequest: ScopeGitLab,
	CommentTask:  ScopeAsana,
	CreateTask:   ScopeAsana,
	WriteNote:    ScopeSlite,
}

// Service is the service an action of this kind writes in; false for a kind Pouple does not know.
func (k ActionKind) Service() (ScopeService, bool) {
	service, ok := actionServices[k]
	return service, ok
}

// Action is one write a plan asks for.
type Action struct {
	Kind  ActionKind `json:"kind"`
	Scope Scope      `json:"scope"`
	// Summary says what the action will do, in the investigating AI's words: the user approves it as written.
	Summary string `json:"summary"`
}

// PlanStatus is where a plan stands.
type PlanStatus string

const (
	PlanProposed PlanStatus = "proposed"
	PlanApproved PlanStatus = "approved"
	PlanDeclined PlanStatus = "declined"
)

// Plan is what the investigating AI wants to write, for the user to approve before anything is written.
type Plan struct {
	// ID names this exact plan (set when listed): an approval that carries it cannot approve another one.
	ID   string `json:"id,omitempty"`
	Goal string `json:"goal"`
	// Brief is what the writing needs to know about the request, restated by the investigating AI. The AI that writes
	// reads this, never the request's own text.
	Brief   string     `json:"brief"`
	Actions []Action   `json:"actions"`
	Status  PlanStatus `json:"status"`
}

const (
	maxPlanActions   = 12
	maxPlanGoal      = 500
	maxPlanBrief     = 4000
	maxActionSummary = 300
	maxScopeID       = 200
	maxScopeName     = 200
)

// scopeIDs say what an identifier of each service looks like, checked before a place is ever shown. A project folder
// is a plain folder name directly under the projects root, never hidden (the rule of projectPath); a GitLab project is
// its numeric ID or its "group/project" path; an Asana project is its numeric gid; a Slite space is a plain ID.
var scopeIDs = map[ScopeService]func(string) bool{
	ScopeCode:   func(id string) bool { return projectID.MatchString(id) && !strings.HasPrefix(id, ".") },
	ScopeGitLab: gitlabProjectID.MatchString,
	ScopeAsana:  asanaGID.MatchString,
	ScopeSlite:  sliteID.MatchString,
}

var (
	gitlabProjectID = regexp.MustCompile(`^([0-9]+|[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)+)$`)
	asanaGID        = regexp.MustCompile(`^[0-9]+$`)
	sliteID         = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// Normalize checks a plan written by the AI before it is shown for approval, and trims its texts. A plan that does not
// hold together, or a text that is too long or hides characters, is refused, never cut or repaired into something the
// AI did not say; only an exact duplicate action is dropped. The result is a proposed plan.
func (p Plan) Normalize() (Plan, error) {
	goal, ok := planText(p.Goal, maxPlanGoal, false)
	if goal == "" {
		return Plan{}, i18n.Wrap(ErrOutcome, "The plan does not say what it is for")
	}
	if !ok {
		return Plan{}, i18n.Wrap(ErrOutcome, "The plan has a text that is too long or holds hidden characters")
	}
	brief, ok := planText(p.Brief, maxPlanBrief, true)
	if !ok {
		return Plan{}, i18n.Wrap(ErrOutcome, "The plan has a text that is too long or holds hidden characters")
	}
	if len(p.Actions) == 0 {
		return Plan{}, i18n.Wrap(ErrOutcome, "The plan has no action")
	}
	if len(p.Actions) > maxPlanActions {
		return Plan{}, i18n.Wrap(ErrOutcome, "The plan has more than %d actions", maxPlanActions)
	}
	out := Plan{Goal: goal, Brief: brief, Actions: []Action{}, Status: PlanProposed}
	for _, action := range p.Actions {
		kept, err := action.normalize()
		if err != nil {
			return Plan{}, err
		}
		// The same write twice is one write: a duplicate would only make the plan longer to read.
		if !slices.Contains(out.Actions, kept) {
			out.Actions = append(out.Actions, kept)
		}
	}
	return out, nil
}

func (a Action) normalize() (Action, error) {
	service, known := a.Kind.Service()
	if !known {
		return Action{}, i18n.Wrap(ErrOutcome, "The plan asks for a kind of write Pouple does not know")
	}
	if a.Scope.Service != service {
		return Action{}, i18n.Wrap(ErrOutcome, "The plan names a place that does not match its action")
	}
	id := strings.TrimSpace(a.Scope.ID)
	if len(id) > maxScopeID || !scopeIDs[service](id) {
		return Action{}, i18n.Wrap(ErrOutcome, "The plan names a place without a usable identifier")
	}
	summary, ok := planText(a.Summary, maxActionSummary, false)
	if summary == "" {
		return Action{}, i18n.Wrap(ErrOutcome, "An action of the plan does not say what it will do")
	}
	name, nameOK := planText(a.Scope.Name, maxScopeName, false)
	if !ok || !nameOK {
		return Action{}, i18n.Wrap(ErrOutcome, "The plan has a text that is too long or holds hidden characters")
	}
	return Action{Kind: a.Kind, Scope: Scope{Service: service, ID: id, Name: name}, Summary: summary}, nil
}

// planText trims a text of the plan and reports whether it may be shown for approval: not longer than max characters,
// and nothing the user would not see as written. That is a control character (a line break too, unless lineBreaks), a
// format character such as a bidirectional override or a zero-width space, or a line or paragraph separator: the user
// approves the text as it reads.
func planText(text string, max int, lineBreaks bool) (string, bool) {
	text = strings.TrimSpace(text)
	hidden := strings.ContainsFunc(text, func(r rune) bool {
		if lineBreaks && (r == '\n' || r == '\t') {
			return false
		}
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
	return text, utf8.RuneCountInString(text) <= max && !hidden
}

// PlanFingerprint identifies a plan, so that an approval names the plan the user actually read: everything shown to them
// counts, the listing ID and the status do not.
func PlanFingerprint(p Plan) string {
	fields, _ := json.Marshal(struct {
		Goal    string   `json:"goal"`
		Brief   string   `json:"brief"`
		Actions []Action `json:"actions"`
	}{p.Goal, p.Brief, p.Actions})
	sum := sha256.Sum256(fields)
	return hex.EncodeToString(sum[:8])
}

// Grant is the write approval for one scope: the kinds of action the user agreed to there. It lasts for the
// conversation that granted it.
type Grant struct {
	Scope Scope        `json:"scope"`
	Kinds []ActionKind `json:"kinds"`
}

// Grants are the write approvals of one conversation.
type Grants []Grant

// With returns the grants with those an approved plan gives: each scope it names, with the kinds of action it asks for
// there. Only a plan that was checked by Normalize and then approved grants anything; g is left as it is.
func (g Grants) With(p Plan) (Grants, error) {
	if p.Status != PlanApproved {
		return nil, i18n.Wrap(ErrState, "only an approved plan can grant writes")
	}
	for _, action := range p.Actions {
		if checked, err := action.normalize(); err != nil || checked != action {
			return nil, i18n.Wrap(ErrState, "the plan was not checked before its approval")
		}
	}
	out := make(Grants, len(g))
	for i, grant := range g {
		out[i] = Grant{Scope: grant.Scope, Kinds: slices.Clone(grant.Kinds)}
	}
	for _, action := range p.Actions {
		index := slices.IndexFunc(out, func(grant Grant) bool { return grant.Scope.same(action.Scope) })
		if index < 0 {
			out = append(out, Grant{Scope: action.Scope, Kinds: []ActionKind{}})
			index = len(out) - 1
		}
		if !slices.Contains(out[index].Kinds, action.Kind) {
			out[index].Kinds = append(out[index].Kinds, action.Kind)
			slices.Sort(out[index].Kinds)
		}
	}
	return out, nil
}

// Covers reports whether an action is already approved: its scope was granted, with its kind. An action whose kind does
// not write in its scope's service is never covered.
func (g Grants) Covers(action Action) bool {
	if service, known := action.Kind.Service(); !known || service != action.Scope.Service {
		return false
	}
	return slices.ContainsFunc(g, func(grant Grant) bool {
		return grant.Scope.same(action.Scope) && slices.Contains(grant.Kinds, action.Kind)
	})
}

// Uncovered returns the actions the grants do not cover: those that need the user's approval before they run.
func (g Grants) Uncovered(actions []Action) []Action {
	out := []Action{}
	for _, action := range actions {
		if !g.Covers(action) {
			out = append(out, action)
		}
	}
	return out
}
