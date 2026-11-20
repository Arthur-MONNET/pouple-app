package engine

import (
	"context"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// supportedKinds are the kinds of write Pouple can carry out today. A plan that asks for another kind is refused when it
// is made, not when the user has already approved it.
var supportedKinds = []ActionKind{EditCode, SyncBranch, MergeRequest}

// proposePlan shows the plan the investigating AI wrote for the user to approve. Inside places the user already approved
// in this conversation it is carried out at once: a grant is for the whole conversation.
func (s *Service) proposePlan(id string, outcome Outcome, summary Entry) {
	refuse := func(reason string) {
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.DraftReply = StatusReady, ptr(outcome.Summary), outcome.Reply
			r.Questions, r.Proposal, r.Plan = []Question{}, nil, nil
			r.Entries = append(r.Entries, summary, s.entry("system", "text", reason))
		})
		s.alert(id, outcome.Summary)
	}
	if outcome.Plan == nil {
		refuse(s.text("A plan was announced but none came with it."))
		return
	}
	plan, err := outcome.Plan.Normalize()
	if err != nil {
		refuse(s.text("The plan could not be used: %s", i18n.ErrorText(s.deps.Language(), err)))
		return
	}
	for _, action := range plan.Actions {
		if !containsKind(supportedKinds, action.Kind) {
			refuse(s.text("The plan asks for a kind of write Pouple cannot do yet: %s", action.Summary))
			return
		}
		if codeKind(action.Kind) && s.projectPath(action.Scope.ID) == "" {
			refuse(s.text("A change was planned for “%s”, which is not a project in your projects folder.", action.Scope.ID))
			return
		}
		if codeKind(action.Kind) && !s.canWriteCode(context.Background(), action.Scope.ID) {
			refuse(s.text("“%s” is not a git repository with a commit, so its code cannot be written on a branch.", action.Scope.ID))
			return
		}
	}
	request, _ := s.deps.Store.Get(id)
	// The user is asked once, when the first plan is made: what they approved then covers the rest of the conversation,
	// branch updates included (for the branches they read). It stops covering as soon as text written by strangers
	// reaches the conversation (a message in its thread, the web), since it may shape a brief the user has not read.
	covered := len(request.Grants.Uncovered(plan.Actions)) == 0 && !request.ForeignSinceGrant
	// The user chose to trust the assistant with every change: a plan is then approved as it is made. It is written on a
	// branch of its own, so this is the gentlest way a change can be applied.
	trusted := s.deps.AutoApproveAll != nil && s.deps.AutoApproveAll()
	// Automatic approval of local code never authorizes GitLab writes.
	for _, a := range plan.Actions {
		if a.Kind == MergeRequest {
			trusted = false
		}
	}
	if covered || trusted {
		plan.Status = PlanApproved
		why := "Inside what you already approved here: carrying it out"
		if !covered {
			// Approved by the setting, not by a person: it grants nothing, so it neither covers the next plans nor lets
			// the follow-ups of this conversation run with full access.
			why, plan.Automatic = "You allowed every change: carrying it out", true
		}
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.Proposal, r.Questions, r.Plan = StatusApplying, ptr(outcome.Summary), nil, []Question{}, &plan
			r.Entries = append(r.Entries, summary, s.entry("assistant", "step", s.text(why)))
		})
		s.enqueue(id, Interactive, func(ctx context.Context) error { return s.executePlan(ctx, id, plan) })
		return
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Summary, r.DraftReply = StatusAwaitingUser, ptr(outcome.Summary), outcome.Reply
		r.Questions, r.Proposal, r.Plan = []Question{}, nil, &plan
		r.Entries = append(r.Entries, summary)
	})
	s.alert(id, s.text("Proposed plan: %s", plan.Goal))
}

// codeKind says whether an action writes in a project folder of this computer.
func codeKind(kind ActionKind) bool { return kind == EditCode || kind == SyncBranch }

func containsKind(kinds []ActionKind, kind ActionKind) bool {
	for _, known := range kinds {
		if known == kind {
			return true
		}
	}
	return false
}

// DecidePlan approves or declines the plan that waits for an answer, all of its actions.
func (s *Service) DecidePlan(id string, approve bool, wanted string) error {
	return s.DecidePlanActions(id, approve, wanted, nil)
}

// DecidePlanActions approves or declines the plan that waits for an answer. wanted names the plan the user read and must
// match the one waiting now. Approving grants the places the plan names, for this conversation only. selected holds the
// positions of the actions the user ticked: only those are approved, carried out and granted, in the plan's order; nil
// means all of them, and an empty or invalid selection approves nothing.
func (s *Service) DecidePlanActions(id string, approve bool, wanted string, selected []int) error {
	if wanted == "" {
		return i18n.Wrap(ErrInput, "the plan that was read must be named")
	}
	if approve {
		if err := s.requireActive(); err != nil {
			return err
		}
	}
	if _, err := s.require(id); err != nil {
		return err
	}
	var approved Plan
	var stale, refused, badSelection bool
	started := s.deps.Store.Transition(id, StatusAwaitingUser, func(r *Stored) bool {
		if r.Plan == nil || r.Plan.Status != PlanProposed {
			return false
		}
		plan := *r.Plan
		if wanted != "" && wanted != PlanFingerprint(plan) {
			stale = true
			return false
		}
		if approve && selected != nil {
			// The plan the user read is narrowed to what they ticked: the rest is neither written nor granted.
			chosen, ok := plan.Select(selected)
			if !ok {
				badSelection = true
				return false
			}
			plan = chosen
		}
		label := "Declined the plan: %s"
		if approve {
			plan.Status = PlanApproved
			grants, err := r.Grants.With(plan)
			if err != nil {
				refused = true
				return false
			}
			r.Grants, r.Status, label = grants, StatusApplying, "Approved the plan: %s"
			r.ForeignSinceGrant = false
		} else {
			plan.Status = PlanDeclined
			// Declining ends the conversation: there is nothing left to show in a bubble.
			r.Status, r.Closed = StatusReady, true
		}
		r.Plan = &plan
		approved = plan
		r.Entries = append(r.Entries, s.entry("user", "text", s.text(label, plan.Goal)))
		r.UpdatedAt = s.now()
		return true
	})
	switch {
	case stale:
		return i18n.Wrap(ErrState, "the plan changed since you read it")
	case badSelection:
		return i18n.Wrap(ErrInput, "choose at least one action of the plan, each only once")
	case refused:
		return i18n.Wrap(ErrState, "the plan could not be approved")
	case !started:
		return i18n.Wrap(ErrState, "no plan is waiting for a decision")
	}
	if approve {
		s.enqueue(id, Interactive, func(ctx context.Context) error { return s.executePlan(ctx, id, approved) })
	}
	return nil
}

// executePlan carries out an approved plan, one action after another, each in a fresh turn that sees the plan and never
// the request's own text.
func (s *Service) executePlan(ctx context.Context, id string, plan Plan) error {
	if plan.Status != PlanApproved {
		return i18n.NewError("The plan has not been approved")
	}
	access, err := s.acquireTerminal(ctx, id, "", !plan.Automatic)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, terminalAccessKey{}, access)
	var written []string
	codePushes := map[string]bool{}
	for _, action := range plan.Actions {
		switch action.Kind {
		case EditCode, SyncBranch:
			run := s.runCodeEdit
			if action.Kind == SyncBranch {
				run = s.runBranchSync
			}
			line, err := run(ctx, id, plan, action)
			if err != nil {
				if ctx.Err() != nil {
					return ErrCancelled
				}
				return err
			}
			codePushes[action.Branch] = s.hasOrigin(ctx, s.projectPath(action.Scope.ID)) && (line != "" || action.Kind == SyncBranch)
			if line != "" {
				written = append(written, line)
				s.update(id, func(r *Stored) {
					r.Summary = ptr(line)
					r.Entries = append(r.Entries, s.entry("system", "text", line))
				})
			}
		case MergeRequest:
			if pushed, ran := codePushes[action.Branch]; ran && !pushed {
				return i18n.NewError("The source branch was not pushed; the merge request was not created")
			}
			if plan.Automatic || s.deps.OpenMergeRequest == nil {
				return i18n.NewError("Merge request creation needs user approval and a connected GitLab account")
			}
			link, err := s.deps.OpenMergeRequest(ctx, action, plan.Goal)
			if err != nil {
				failure := i18n.Wrap(err, "Merge request %s → %s could not be opened: %s", action.Branch, action.Base, err)
				if len(written) > 0 {
					return i18n.Wrap(failure, "%s\n%s", strings.Join(written, "\n"), failure)
				}
				return failure
			}
			line := s.text("Merge request: %s", link)
			written = append(written, line)
			s.update(id, func(r *Stored) { r.Summary = ptr(line); r.Entries = append(r.Entries, s.entry("system", "text", line)) })
		default:
			return i18n.NewError("Pouple cannot carry out this kind of write yet")
		}
	}
	if len(written) == 0 {
		return nil
	}
	s.update(id, func(r *Stored) { r.Status = StatusDone })
	// The user is told what was pushed: it left their computer without a question.
	s.alert(id, strings.Join(written, "\n"))
	return nil
}

// runCodeEdit writes the code of one action, on a branch of its own, and commits it. It returns what was committed, or
// "" when nothing was changed (the request then stays open, saying so).
func (s *Service) runCodeEdit(ctx context.Context, id string, plan Plan, action Action) (string, error) {
	wt, err := s.prepareWorktree(ctx, plan, action)
	if err != nil {
		return "", err
	}
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Working on the branch %s", wt.Branch)))
	})
	result, provider, err := s.runProvider(ctx, id, Turn{Prompt: s.deps.Persona().PlanApplyPrompt(plan, action, !plan.Automatic), Dir: wt.Path, Sandbox: writeSandbox(!plan.Automatic), SchemaFile: s.deps.SchemaFile}, false)
	if err != nil {
		s.discardWorktree(ctx, wt)
		return "", err
	}
	if result.FinalMessage == "" {
		s.discardWorktree(ctx, wt)
		return "", i18n.NewError("%s finished without an answer", provider.Name())
	}
	outcome, err := ParseOutcome(result.FinalMessage)
	if err != nil {
		s.discardWorktree(ctx, wt)
		return "", err
	}
	outcome = outcome.Redact(s.deps.Redact())
	line, err := s.commitWorktree(ctx, wt, plan.Goal)
	if err != nil {
		return "", err
	}
	if line == "" {
		// The AI stopped on something it lacked: say it plainly and keep the request open.
		s.update(id, func(r *Stored) {
			r.Status = StatusReady
			r.Entries = append(r.Entries, s.entry("assistant", "text", outcome.Summary), s.entry("system", "text", s.text("No file was changed.")))
		})
		s.alert(id, s.text("Nothing was changed: %s", outcome.Summary))
		return "", nil
	}
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry("assistant", "text", outcome.Summary),
			s.entry("system", "step", s.text("Committed on branch %s (%s)", wt.Branch, wt.Path)), s.entry("system", "step", line))
	})
	if !s.hasOrigin(ctx, s.projectPath(wt.Project)) {
		return s.text("Branch %s committed (this project has no origin to push it to): %s", wt.Branch, plan.Goal), nil
	}
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Pushing %s to %s", wt.Branch, origin)))
	})
	// The user allowed the push with the plan: nothing is forced, and the branch is new, so it cannot overwrite anyone's work.
	if out, err := s.pushWorktree(ctx, wt, "--set-upstream", origin, "refs/heads/"+wt.Branch+":refs/heads/"+wt.Branch); err != nil {
		return "", i18n.NewError("The work is committed on the branch %s of “%s” but could not be pushed: %s", wt.Branch, wt.Project, clipRunes(out, 300))
	}
	return s.text("Branch %s pushed: %s", wt.Branch, plan.Goal), nil
}
