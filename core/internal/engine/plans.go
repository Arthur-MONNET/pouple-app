package engine

import (
	"context"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// supportedKinds are the kinds of write Pouple can carry out today. A plan that asks for another kind is refused when it
// is made, not when the user has already approved it.
var supportedKinds = []ActionKind{EditCode}

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
		if action.Kind == EditCode && s.projectPath(action.Scope.ID) == "" {
			refuse(s.text("A change was planned for “%s”, which is not a project in your projects folder.", action.Scope.ID))
			return
		}
		if action.Kind == EditCode && !s.canWriteCode(context.Background(), action.Scope.ID) {
			refuse(s.text("“%s” is not a git repository with a commit, so its code cannot be written on a branch.", action.Scope.ID))
			return
		}
	}
	request, _ := s.deps.Store.Get(id)
	// A plan inside what was approved runs at once only while nothing written by other people has entered the
	// conversation: their text may have shaped the brief, which the user has not read for this plan.
	covered := len(request.Grants.Uncovered(plan.Actions)) == 0 && !request.ServicesUsed && !request.WebUsed
	// The user chose to trust the assistant with every change: a plan is then approved as it is made. It is written on a
	// branch of its own, so this is the gentlest way a change can be applied.
	trusted := s.deps.AutoApproveAll != nil && s.deps.AutoApproveAll()
	if covered || trusted {
		plan.Status = PlanApproved
		why := "Inside what you already approved here: carrying it out"
		grants := request.Grants
		if !covered {
			why = "You allowed every change: carrying it out"
			var err error
			if grants, err = request.Grants.With(plan); err != nil {
				refuse(s.text("The plan could not be used: %s", i18n.ErrorText(s.deps.Language(), err)))
				return
			}
		}
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.Proposal, r.Questions, r.Plan, r.Grants = StatusApplying, ptr(outcome.Summary), nil, []Question{}, &plan, grants
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

func containsKind(kinds []ActionKind, kind ActionKind) bool {
	for _, known := range kinds {
		if known == kind {
			return true
		}
	}
	return false
}

// DecidePlan approves or declines the plan that waits for an answer. wanted names the plan the user read and must match
// the one waiting now. Approving grants the places the plan names, for this conversation only.
func (s *Service) DecidePlan(id string, approve bool, wanted string) error {
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
	var stale, refused bool
	started := s.deps.Store.Transition(id, StatusAwaitingUser, func(r *Stored) bool {
		if r.Plan == nil || r.Plan.Status != PlanProposed {
			return false
		}
		plan := *r.Plan
		if wanted != "" && wanted != PlanFingerprint(plan) {
			stale = true
			return false
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
	var written []string
	for _, action := range plan.Actions {
		switch action.Kind {
		case EditCode:
			line, err := s.runCodeEdit(ctx, id, plan, action)
			if err != nil {
				return err
			}
			if line != "" {
				written = append(written, line)
			}
		default:
			return i18n.NewError("Pouple cannot carry out this kind of write yet")
		}
	}
	if len(written) == 0 {
		return nil
	}
	s.update(id, func(r *Stored) { r.Status = StatusDone })
	s.alert(id, s.text("Change prepared: %s", plan.Goal))
	return nil
}

// runCodeEdit writes the code of one action, on a branch of its own, and commits it. It returns what was committed, or
// "" when nothing was changed (the request then stays open, saying so).
func (s *Service) runCodeEdit(ctx context.Context, id string, plan Plan, action Action) (string, error) {
	wt, err := s.prepareWorktree(ctx, plan, action.Scope.ID)
	if err != nil {
		return "", err
	}
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Working on a branch of its own: %s", wt.Branch)))
	})
	result, provider, err := s.runProvider(ctx, id, Turn{Prompt: s.deps.Persona().PlanApplyPrompt(plan, action), Dir: wt.Path, Sandbox: WorkspaceWrite, SchemaFile: s.deps.SchemaFile}, false)
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
	return strings.TrimSpace(line), nil
}
