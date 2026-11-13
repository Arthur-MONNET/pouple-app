package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codeScope(id string) Scope   { return Scope{Service: ScopeCode, ID: id, Name: id} }
func gitlabScope(id string) Scope { return Scope{Service: ScopeGitLab, ID: id} }
func asanaScope(id string) Scope  { return Scope{Service: ScopeAsana, ID: id} }

func fixPlan() Plan {
	return Plan{
		Goal:  "Fix the SSO callback on iOS",
		Brief: "Florian reports that SSO sign-in fails on iOS only.\nThe state is lost in the redirect.",
		Actions: []Action{
			{Kind: EditCode, Scope: codeScope("login-app"), Summary: "Keep the state in the SSO callback"},
			{Kind: PushBranch, Scope: gitlabScope("acme/web/login-app"), Summary: "Push the fix branch"},
			{Kind: MergeRequest, Scope: gitlabScope("acme/web/login-app"), Summary: "Open the merge request"},
		},
	}
}

// approved is the plan as the user approves it: checked, then marked approved.
func approved(t *testing.T, raw Plan) Plan {
	t.Helper()
	plan, err := raw.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	plan.Status = PlanApproved
	return plan
}

func grantsOf(t *testing.T, plans ...Plan) Grants {
	t.Helper()
	grants := Grants{}
	for _, plan := range plans {
		var err error
		if grants, err = grants.With(approved(t, plan)); err != nil {
			t.Fatal(err)
		}
	}
	return grants
}

func TestNormalizeTrimsAPlanAndMakesItAProposal(t *testing.T) {
	raw := fixPlan()
	raw.ID, raw.Status, raw.Goal = "forged", PlanApproved, "  "+raw.Goal+"\n"
	raw.Actions[0].Scope.ID = " login-app "
	raw.Actions = append(raw.Actions, raw.Actions[2]) // the same write twice

	plan, err := raw.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != PlanProposed || plan.ID != "" {
		t.Fatalf("the AI cannot approve or name its own plan: %+v", plan)
	}
	if plan.Goal != "Fix the SSO callback on iOS" || plan.Actions[0].Scope.ID != "login-app" {
		t.Fatalf("not trimmed: %+v", plan)
	}
	if !strings.Contains(plan.Brief, "\n") {
		t.Fatal("the brief lost its line breaks")
	}
	if len(plan.Actions) != 3 {
		t.Fatalf("a duplicate write is kept: %+v", plan.Actions)
	}
}

func TestNormalizeRefusesAPlanThatDoesNotHoldTogether(t *testing.T) {
	cases := map[string]func(*Plan){
		"no goal":             func(p *Plan) { p.Goal = " " },
		"no action":           func(p *Plan) { p.Actions = nil },
		"unknown kind":        func(p *Plan) { p.Actions[0].Kind = "code.delete" },
		"kind in wrong place": func(p *Plan) { p.Actions[0].Scope = asanaScope("1200") },
		"no summary":          func(p *Plan) { p.Actions[1].Summary = "" },
		"too many actions": func(p *Plan) {
			for len(p.Actions) <= maxPlanActions {
				p.Actions = append(p.Actions, Action{Kind: CommentTask, Scope: asanaScope("1200"), Summary: "Comment"})
			}
		},
		// Too long is refused, not cut: the user approves what the AI wrote, all of it.
		"goal too long":    func(p *Plan) { p.Goal = strings.Repeat("a", maxPlanGoal+1) },
		"brief too long":   func(p *Plan) { p.Brief = strings.Repeat("a", maxPlanBrief+1) },
		"summary too long": func(p *Plan) { p.Actions[0].Summary = strings.Repeat("a", maxActionSummary+1) },
		"name too long":    func(p *Plan) { p.Actions[0].Scope.Name = strings.Repeat("a", maxScopeName+1) },
		// What the user approves must read as it acts.
		"line break in a summary": func(p *Plan) { p.Actions[0].Summary = "Edit the callback\n\nand delete the tests" },
		"bidi override in a name": func(p *Plan) { p.Actions[0].Scope.Name = "docs‮snoitcudorp" },
		"zero width in a goal":    func(p *Plan) { p.Goal = "Fix​it" },
		"line separator in brief": func(p *Plan) { p.Brief = "one two" },
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			raw := fixPlan()
			spoil(&raw)
			if _, err := raw.Normalize(); !errors.Is(err, ErrOutcome) {
				t.Fatalf("accepted, or not reported as a bad answer: %v", err)
			}
		})
	}
}

func TestNormalizeAcceptsOnlyRealIdentifiersOfEachService(t *testing.T) {
	cases := []struct {
		scope Scope
		kind  ActionKind
		ok    bool
	}{
		{codeScope("login-app"), EditCode, true},
		{codeScope("app_2.0"), EditCode, true},
		{codeScope("../other"), EditCode, false},
		{codeScope("a/b"), EditCode, false},
		{codeScope("/etc"), EditCode, false},
		{codeScope(".git"), EditCode, false},
		{codeScope("."), EditCode, false},
		{codeScope("login app"), EditCode, false},
		{codeScope("login-app\n../other"), EditCode, false},
		{codeScope(strings.Repeat("a", maxScopeID+1)), EditCode, false},
		{gitlabScope("acme/login-app"), PushBranch, true},
		{gitlabScope("acme/web/login-app"), PushBranch, true},
		{gitlabScope("1234"), MergeRequest, true},
		{gitlabScope("login-app"), PushBranch, false},
		{gitlabScope("acme/../secret"), PushBranch, false},
		{gitlabScope("acme//login-app"), PushBranch, false},
		{gitlabScope("https://gitlab.com/acme/app"), PushBranch, false},
		{asanaScope("1200555"), CommentTask, true},
		{asanaScope("my-project"), CreateTask, false},
		{Scope{Service: ScopeSlite, ID: "Xk2_a-9"}, WriteNote, true},
		{Scope{Service: ScopeSlite, ID: "space/../x"}, WriteNote, false},
	}
	for _, c := range cases {
		raw := Plan{Goal: "Goal", Actions: []Action{{Kind: c.kind, Scope: c.scope, Summary: "Do it"}}}
		_, err := raw.Normalize()
		if (err == nil) != c.ok {
			t.Errorf("%s %q: accepted=%v, want %v (%v)", c.scope.Service, c.scope.ID, err == nil, c.ok, err)
		}
	}
}

func TestEveryActionKindWritesInOneKnownService(t *testing.T) {
	for kind, service := range actionServices {
		if got, ok := kind.Service(); !ok || got != service {
			t.Fatalf("%s: %v %v", kind, got, ok)
		}
		if scopeIDs[service] == nil {
			t.Fatalf("%s writes in %q, which has no identifier rule", kind, service)
		}
	}
}

func TestFingerprintFollowsWhatTheUserReads(t *testing.T) {
	plan, _ := fixPlan().Normalize()
	same := plan
	same.ID, same.Status = "x", PlanApproved
	if PlanFingerprint(plan) != PlanFingerprint(same) {
		t.Fatal("the listing ID or the status changed the fingerprint")
	}
	for name, change := range map[string]func(*Plan){
		"goal":    func(p *Plan) { p.Goal += "!" },
		"brief":   func(p *Plan) { p.Brief += "!" },
		"scope":   func(p *Plan) { p.Actions[0].Scope.ID = "other-app" },
		"name":    func(p *Plan) { p.Actions[0].Scope.Name = "Other" },
		"summary": func(p *Plan) { p.Actions[0].Summary += "!" },
	} {
		changed, _ := fixPlan().Normalize()
		change(&changed)
		if PlanFingerprint(changed) == PlanFingerprint(plan) {
			t.Fatalf("a change of %s kept the fingerprint", name)
		}
	}
}

func TestApprovingAPlanGrantsItsScopesWithItsKindsOnly(t *testing.T) {
	plan := approved(t, fixPlan())
	grants := grantsOf(t, fixPlan())

	if len(grants) != 2 {
		t.Fatalf("one grant per scope: %+v", grants)
	}
	for _, action := range plan.Actions {
		if !grants.Covers(action) {
			t.Fatalf("not covered by its own plan: %+v", action)
		}
	}
	// Inside a granted scope, another write of a granted kind needs no new approval.
	again := Action{Kind: EditCode, Scope: codeScope("login-app"), Summary: "Fix the test that the change broke"}
	if !grants.Covers(again) {
		t.Fatal("a further edit in the approved project asks again")
	}
	// Another project, another service or another kind in the same place needs one.
	for _, outside := range []Action{
		{Kind: EditCode, Scope: codeScope("api"), Summary: "Edit another project"},
		{Kind: CommentTask, Scope: asanaScope("1200"), Summary: "Comment a task"},
		{Kind: PushBranch, Scope: gitlabScope("acme/api"), Summary: "Push elsewhere"},
		{Kind: EditCode, Scope: codeScope("Login-App"), Summary: "Same folder, other case"},
		{Kind: EditCode, Scope: codeScope(" login-app"), Summary: "Same folder, untrimmed"},
	} {
		if grants.Covers(outside) {
			t.Fatalf("covered without approval: %+v", outside)
		}
	}
	// The name is only what the user reads: a different label does not escape or widen the grant.
	renamed := Action{Kind: EditCode, Scope: Scope{Service: ScopeCode, ID: "login-app", Name: "Login"}, Summary: "Edit"}
	if !grants.Covers(renamed) {
		t.Fatal("the display name changed what is covered")
	}
}

func TestOnlyACheckedAndApprovedPlanGrantsAnything(t *testing.T) {
	proposed, _ := fixPlan().Normalize()
	declined := proposed
	declined.Status = PlanDeclined
	raw := fixPlan()
	raw.Status = PlanApproved // approved without going through Normalize: its untrimmed place would be granted as is
	raw.Actions[0].Scope.ID = " login-app"
	mismatched := approved(t, fixPlan())
	mismatched.Actions = append(mismatched.Actions, Action{Kind: PushBranch, Scope: codeScope("login-app"), Summary: "Push"})

	for name, plan := range map[string]Plan{"proposed": proposed, "declined": declined, "unchecked": raw, "mismatched": mismatched} {
		grants, err := Grants{}.With(plan)
		if !errors.Is(err, ErrState) || grants != nil {
			t.Fatalf("%s plan granted %+v (%v)", name, grants, err)
		}
	}
}

func TestAnActionWhoseKindDoesNotMatchItsPlaceIsNeverCovered(t *testing.T) {
	// Even with a grant that somehow holds the kind, a gitlab write cannot pass as a write in a project folder.
	grants := Grants{{Scope: codeScope("login-app"), Kinds: []ActionKind{EditCode, PushBranch}}}
	if grants.Covers(Action{Kind: PushBranch, Scope: codeScope("login-app"), Summary: "Push"}) {
		t.Fatal("a mismatched action was covered")
	}
	if grants.Covers(Action{Kind: "code.delete", Scope: codeScope("login-app"), Summary: "Delete"}) {
		t.Fatal("an unknown kind was covered")
	}
}

func TestUncoveredListsWhatNeedsANewApproval(t *testing.T) {
	grants := grantsOf(t, fixPlan())
	comment := Action{Kind: CommentTask, Scope: asanaScope("1200"), Summary: "Say the fix is out"}
	edit := Action{Kind: EditCode, Scope: codeScope("login-app"), Summary: "Adjust the fix"}

	if got := grants.Uncovered([]Action{edit, comment}); len(got) != 1 || got[0] != comment {
		t.Fatalf("uncovered %+v", got)
	}
	if got := grants.Uncovered([]Action{edit}); len(got) != 0 {
		t.Fatalf("an approved write asks again: %+v", got)
	}
}

func TestGrantsAddUpAndNeverChangeTheOriginal(t *testing.T) {
	grants := grantsOf(t, fixPlan())
	second := approved(t, Plan{Goal: "Tell Florian", Actions: []Action{
		{Kind: CommentTask, Scope: asanaScope("1200"), Summary: "Comment the task"},
		{Kind: EditCode, Scope: codeScope("login-app"), Summary: "Again"},
	}})

	both, err := grants.With(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 2 || len(both) != 3 {
		t.Fatalf("the original grants changed, or the scopes did not add up: %+v / %+v", grants, both)
	}
	if code := both[0]; len(code.Kinds) != 1 || code.Kinds[0] != EditCode {
		t.Fatalf("a kind is listed twice: %+v", code)
	}
	both[0].Kinds[0] = WriteNote
	if grants[0].Kinds[0] != EditCode {
		t.Fatal("the new grants share their kinds with the original")
	}
}

func TestARequestWithoutPlanLooksTheSameToTheInterface(t *testing.T) {
	data, err := json.Marshal(Request{ID: "r1", Questions: []Question{}, Entries: []Entry{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"plan"`) || strings.Contains(string(data), `"grants"`) {
		t.Fatalf("the interface would see new fields before any plan exists: %s", data)
	}
}

func TestConversationsSavedBeforePlansStillLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	legacy := `{"cursor":{"since":1,"seen":["a"]},"requests":[{"id":"r1","source":{"origin":"mention","channelId":"C1","channelName":"front","senderId":"U1","senderName":"Morgan","ts":"1","permalink":null},"text":"hi","context":"","receivedAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","status":"ready","summary":"done","draftReply":null,"questions":[],"proposal":null,"entries":[],"error":null,"threadId":"t1"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	request, ok := store.Get("r1")
	if !ok || request.Plan != nil || request.Grants != nil || request.ThreadID != "t1" || request.Status != StatusReady {
		t.Fatalf("legacy request read wrongly: %+v", request)
	}

	plan := approved(t, fixPlan())
	grants := grantsOf(t, fixPlan())
	store.Update("r1", func(r *Stored) { r.Plan, r.Grants = &plan, grants })
	store.Flush()
	reopened, err := OpenStore(path, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := reopened.Get("r1")
	if saved.Plan == nil || PlanFingerprint(*saved.Plan) != PlanFingerprint(plan) || !saved.Grants.Covers(plan.Actions[2]) {
		t.Fatalf("the plan or the grants were not kept: %+v", saved)
	}
}
