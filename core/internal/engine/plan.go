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

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
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
	// ScopeSlite is the Slite parent note under which a page will be created.
	ScopeSlite ScopeService = "slite"
	// ScopeTerminal is the interactive terminal of the conversation: its sessions, local or reached from it (ssh).
	ScopeTerminal ScopeService = "terminal"
)

// ConversationTerminal is the only identifier of ScopeTerminal: sessions come and go within a conversation, so an
// approval covers its terminal as a whole, never a session that could be swapped for another under the same name.
const ConversationTerminal = "."

// Scope is one place a write approval covers, such as a project folder, a Slite parent note or the conversation terminal.
type Scope struct {
	Service ScopeService `json:"service"`
	// ID is the service's own identifier (the folder name, the GitLab project path, the Asana project gid, the Slite
	// parent note ID). It is the only part that matches.
	ID string `json:"id"`
	// Name is what the user reads. It plays no part in matching.
	Name string `json:"name,omitempty"`
}

func (s Scope) same(other Scope) bool { return s.Service == other.Service && s.ID == other.ID }

// ActionKind is a kind of write. Each kind writes in one service, so an action can only name a scope of that service.
type ActionKind string

const (
	// EditCode changes files and commits them on a new branch, in a worktree of a project folder, and pushes it.
	EditCode ActionKind = "code.edit"
	// SyncBranch brings a branch of a project up to date with another one (usually the main branch): it fetches, merges,
	// resolves the conflicts, commits and pushes the branch, in a worktree of its own.
	SyncBranch ActionKind = "code.sync"
	// PushBranch pushes that branch to the GitLab project.
	PushBranch ActionKind = "gitlab.push"
	// MergeRequest opens a merge request for exactly the approved source and target branches.
	MergeRequest ActionKind = "gitlab.merge-request"
	CommentTask  ActionKind = "asana.comment"
	CreateTask   ActionKind = "asana.create-task"
	WriteNote    ActionKind = "slite.write"
	// RunTerminal lets the assistant work in the conversation's interactive terminal, as the summary says. Only a request
	// the user wrote may plan it, and a plan that holds it holds nothing else.
	RunTerminal ActionKind = "terminal.run"
)

var actionServices = map[ActionKind]ScopeService{
	EditCode:     ScopeCode,
	SyncBranch:   ScopeCode,
	PushBranch:   ScopeGitLab,
	MergeRequest: ScopeGitLab,
	CommentTask:  ScopeAsana,
	CreateTask:   ScopeAsana,
	WriteNote:    ScopeSlite,
	RunTerminal:  ScopeTerminal,
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
	// Branch and Base name the two branches of a SyncBranch action (Branch is updated with what Base holds) and of an
	// EditCode action (Branch is created from Base, and pushed). They are part of what the user approves, and of the plan's
	// fingerprint.
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`
	// Title and Markdown are the exact contents approved and sent to Slite.
	Title    string `json:"title,omitempty"`
	Markdown string `json:"markdown,omitempty"`
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
	// Automatic is set when the plan was approved by the "every change" setting rather than by the user reading it: it is
	// then written without full access, and grants nothing in the conversation.
	Automatic bool `json:"automatic,omitempty"`
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
// its numeric ID or its "group/project" path; an Asana project is its numeric gid; a Slite destination is a note ID; the
// terminal is the conversation's own (ConversationTerminal).
var scopeIDs = map[ScopeService]func(string) bool{
	ScopeCode:     func(id string) bool { return projectID.MatchString(id) && !strings.HasPrefix(id, ".") },
	ScopeGitLab:   gitlabProjectID.MatchString,
	ScopeAsana:    asanaGID.MatchString,
	ScopeSlite:    slite.ValidNoteID,
	ScopeTerminal: func(id string) bool { return id == ConversationTerminal },
}

var (
	gitlabProjectID = regexp.MustCompile(`^([0-9]+|[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)+)$`)
	asanaGID        = regexp.MustCompile(`^[0-9]+$`)
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

	// Terminal work is approved on its own: mixed with code or service writes, a click on the plan would hand over the
	// terminal with them without the user reading that it does.
	terminal := slices.ContainsFunc(out.Actions, func(a Action) bool { return a.Kind == RunTerminal })
	if terminal && slices.ContainsFunc(out.Actions, func(a Action) bool { return a.Kind != RunTerminal }) {
		return Plan{}, i18n.Wrap(ErrOutcome, "Work in the terminal is planned on its own, not with other writes")
	}

	for i, a := range out.Actions {
		if a.Kind != MergeRequest {
			continue
		}
		for j, code := range out.Actions {
			if codeKind(code.Kind) && code.Branch == a.Branch && j > i {
				return Plan{}, i18n.Wrap(ErrOutcome, "A merge request must follow the code push")
			}
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
	branch, base := strings.TrimSpace(a.Branch), strings.TrimSpace(a.Base)
	if err := checkBranches(a.Kind, branch, base); err != nil {
		return Action{}, err
	}
	if a.Kind == WriteNote {
		if strings.TrimSpace(a.Title) == "" || !validPlanText(a.Title, maxActionSummary, false) ||
			len(a.Markdown) > 64<<10 || !validPlanText(a.Markdown, 64<<10, true) {
			return Action{}, i18n.Wrap(ErrOutcome, "A Slite page needs a valid title and Markdown")
		}
	} else if a.Title != "" || a.Markdown != "" {
		return Action{}, i18n.Wrap(ErrOutcome, "Only Slite actions can contain a page title and Markdown")
	}
	a.Scope, a.Summary, a.Branch, a.Base = Scope{Service: service, ID: id, Name: name}, summary, branch, base
	return a, nil
}

// checkBranches says whether the branches of an action fit its kind. A SyncBranch names a branch to update and the one to
// bring in, never the same, and whatever its name (the user reads it in the plan). An EditCode names the new branch its work
// is committed and pushed on, which reads as a type and a subject ("fix/login-redirect", "feature/offer-layout"), never
// as a branch of Pouple's own, and the branch it starts from ("" for the project's current one).
func checkBranches(kind ActionKind, branch, base string) error {
	switch kind {
	case SyncBranch, MergeRequest:
		if !validBranchName(branch) || !validBranchName(base) || branch == base {
			return i18n.Wrap(ErrOutcome, "The plan names a branch that cannot be used")
		}
	case EditCode:
		if !typedBranch.MatchString(branch) || strings.HasPrefix(branch, "pouple/") || !validBranchName(branch) || (base != "" && !validBranchName(base)) || branch == base {
			return i18n.Wrap(ErrOutcome, "The plan names a branch that cannot be used")
		}
	default:
		if branch != "" || base != "" {
			return i18n.Wrap(ErrOutcome, "The plan names branches for an action that does not use any")
		}
		return nil
	}
	return nil
}

// typedBranch is a new branch's name: a lowercase type, a slash, then a subject, as in "fix/login-redirect".
var typedBranch = regexp.MustCompile(`^(?:[a-z]+)/[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// branchName is what a branch of a plan may look like: git's own rules are wider, but everything here is accepted by git
// and can never be read as an option or as a path.
var branchName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]*$`)

func validBranchName(name string) bool {
	return len(name) <= maxScopeID && branchName.MatchString(name) &&
		!strings.Contains(name, "..") && !strings.Contains(name, "//") && !strings.Contains(name, "/.") &&
		!strings.HasSuffix(name, "/") && !strings.HasSuffix(name, ".") && !strings.HasSuffix(name, ".lock")
}

// planText trims a text of the plan and reports whether it may be shown for approval: not longer than max characters,
// and nothing the user would not see as written. That is a control character (a line break too, unless lineBreaks), a
// format character such as a bidirectional override or a zero-width space, or a line or paragraph separator: the user
// approves the text as it reads.
func planText(text string, max int, lineBreaks bool) (string, bool) {
	text = strings.TrimSpace(text)
	return text, validPlanText(text, max, lineBreaks)
}

func validPlanText(text string, max int, lineBreaks bool) bool {
	hidden := strings.ContainsFunc(text, func(r rune) bool {
		if lineBreaks && (r == '\n' || r == '\t') {
			return false
		}
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= max && !hidden
}

func (p Plan) hasSlitePages() bool {
	return slices.ContainsFunc(p.Actions, func(action Action) bool { return action.Kind == WriteNote })
}

// Select returns the plan with only the actions at these positions, in the plan's order. It reports false for no position,
// a position outside the plan, or the same position twice.
func (p Plan) Select(positions []int) (Plan, bool) {
	if len(positions) == 0 {
		return Plan{}, false
	}
	picked := make([]bool, len(p.Actions))
	for _, position := range positions {
		if position < 0 || position >= len(p.Actions) || picked[position] {
			return Plan{}, false
		}
		picked[position] = true
	}
	out := p
	out.Actions = []Action{}
	for position, action := range p.Actions {
		if picked[position] {
			out.Actions = append(out.Actions, action)
		}
	}
	return out, true
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
	// Merge requests require an exact project and branch pair; legacy kind grants never cover them.
	Actions []Action `json:"actions,omitempty"`
}

// Grants are the write approvals of one conversation.
type Grants []Grant

// With returns the grants with those an approved plan gives: each scope it names, with the kinds of action it asks for
// there. Only a plan that was checked by Normalize and then approved grants anything; g is left as it is.
func (g Grants) With(p Plan) (Grants, error) {
	if p.Status != PlanApproved || p.Automatic {
		return nil, i18n.Wrap(ErrState, "only an approved plan can grant writes")
	}
	for _, action := range p.Actions {
		if checked, err := action.normalize(); err != nil || checked != action {
			return nil, i18n.Wrap(ErrState, "the plan was not checked before its approval")
		}
	}
	out := make(Grants, len(g))
	for i, grant := range g {
		out[i] = Grant{Scope: grant.Scope, Kinds: slices.Clone(grant.Kinds), Actions: slices.Clone(grant.Actions)}
	}
	for _, action := range p.Actions {
		if action.Kind == WriteNote {
			continue // Publishing requires approval of each page, never a standing grant.
		}
		index := slices.IndexFunc(out, func(grant Grant) bool { return grant.Scope.same(action.Scope) })
		if index < 0 {
			out = append(out, Grant{Scope: action.Scope, Kinds: []ActionKind{}})
			index = len(out) - 1
		}
		if action.Kind == MergeRequest {
			out[index].Actions = append(out[index].Actions, action)
			continue
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
	if service, known := action.Kind.Service(); !known || service != action.Scope.Service || action.Kind == WriteNote {
		return false
	}
	return slices.ContainsFunc(g, func(grant Grant) bool {
		if action.Kind == MergeRequest {
			return grant.Scope.same(action.Scope) && slices.ContainsFunc(grant.Actions, func(a Action) bool {
				return a.Kind == action.Kind && a.Branch == action.Branch && a.Base == action.Base
			})
		}
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
