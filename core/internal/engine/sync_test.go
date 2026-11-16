package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// syncHarness is a planHarness whose project has an origin, where a teammate's branch "feat/offer" and a newer "main"
// live: the project itself has fetched none of it, as when a colleague asks to bring a branch up to date.
func syncHarness(t *testing.T, steps ...func(context.Context, Turn) (TurnResult, error)) (*harness, string, string) {
	t.Helper()
	h, project, _ := planHarness(t, steps...)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, project, "remote", "add", "origin", origin)
	gitIn(t, project, "push", "-q", "origin", "main")

	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", "-q", origin, other)
	gitIn(t, other, "switch", "-q", "-c", "feat/offer")
	commitFile(t, other, "offer.txt", "offer\n", "add the offer")
	gitIn(t, other, "push", "-q", "origin", "feat/offer")
	gitIn(t, other, "switch", "-q", "main")
	commitFile(t, other, "visa.txt", "visa fix\n", "fix visa")
	gitIn(t, other, "push", "-q", "origin", "main")
	return h, project, other
}

func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", message)
}

func syncPlan(branch, base string) map[string]any {
	return map[string]any{
		"outcome": ProposesPlan, "summary": "**Update the branch**",
		"plan": map[string]any{
			"goal": "Update the offer branch with the Visa fix", "brief": "main now holds the Visa fix; feat/offer holds the new offer layout.",
			"actions": []any{map[string]any{"kind": "code.sync", "scope": map[string]any{"service": "code", "id": "login-app", "name": "Login app"}, "summary": "Merge main into " + branch, "branch": branch, "base": base}},
		},
	}
}

func approve(t *testing.T, h *harness) Request {
	t.Helper()
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	return h.svc.Snapshot().Requests[0]
}

// The branch is not in the project, the base is behind: one approval and Pouple fetches, merges and pushes, without the
// AI that writes (nothing conflicts).
func TestABranchAbsentFromTheProjectIsFetchedMergedAndPushed(t *testing.T) {
	h, project, other := syncHarness(t, reply(syncPlan("feat/offer", "main")))
	h.receive(message("please update your branch with main, I merged the Visa fix"))
	final := approve(t, h)

	if final.Status != StatusDone || len(h.codex.turns) != 1 {
		t.Fatalf("status %s, turns %d: %s", final.Status, len(h.codex.turns), journalOf(final))
	}
	gitIn(t, other, "pull", "-q", "origin", "feat/offer")
	if got := gitIn(t, other, "log", "-1", "--format=%P", "origin/feat/offer"); len(strings.Fields(got)) != 2 {
		t.Errorf("the pushed branch ends on a merge commit: %q", got)
	}
	for _, file := range []string{"offer.txt", "visa.txt"} {
		if _, err := os.Stat(filepath.Join(other, file)); err != nil {
			t.Errorf("the pushed branch must hold %s", file)
		}
	}
	if subject := gitIn(t, other, "log", "-1", "--format=%s", "origin/feat/offer"); subject != "Merge branch 'main' into feat/offer" {
		t.Errorf("the merge reads like any merge: %q", subject)
	}
	// The user's checkout and branch are untouched, and nothing is left behind.
	if status := gitIn(t, project, "status", "--porcelain"); status != "" || gitIn(t, project, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Errorf("the checkout must stay as it was: %q", status)
	}
	if all := gitIn(t, project, "branch", "-a"); strings.Contains(all, "pouple") {
		t.Errorf("no branch carries Pouple's name: %q", all)
	}
	if worktrees := gitIn(t, project, "worktree", "list"); strings.Count(worktrees, "\n") != 0 {
		t.Errorf("the worktree must go: %q", worktrees)
	}
	if recap := journalOf(final); !strings.Contains(recap, "feat/offer is up to date with main and pushed") {
		t.Errorf("the conversation ends on a recap: %s", recap)
	}
}

// The user's own copy of the branch moves forward when nothing of theirs is in the way.
func TestTheLocalCopyOfTheBranchIsBroughtForward(t *testing.T) {
	h, project, _ := syncHarness(t, reply(syncPlan("feat/offer", "main")))
	gitIn(t, project, "fetch", "-q", "origin")
	gitIn(t, project, "branch", "feat/offer", "origin/feat/offer")
	h.receive(message("update feat/offer with main"))
	approve(t, h)
	if gitIn(t, project, "rev-parse", "feat/offer") != gitIn(t, project, "rev-parse", "origin/feat/offer") {
		t.Error("the local branch must follow what was pushed")
	}
	if gitIn(t, project, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("their checked out branch must not change")
	}
}

func TestAConflictIsSettledByTheAIThatWritesThenPushed(t *testing.T) {
	settle := func(ctx context.Context, turn Turn) (TurnResult, error) {
		if !strings.Contains(turn.Prompt, "conflict.txt") || turn.Sandbox != FullAccess {
			t.Errorf("the writing turn names the files in conflict:\n%s", turn.Prompt)
		}
		if err := os.WriteFile(filepath.Join(turn.Dir, "conflict.txt"), []byte("both\n"), 0o644); err != nil {
			return TurnResult{}, err
		}
		return reply(map[string]any{"summary": "**Kept both**"})(ctx, turn)
	}
	h, project, other := syncHarness(t, reply(syncPlan("feat/offer", "main")), settle)
	// The same line is changed on both sides.
	gitIn(t, other, "switch", "-q", "feat/offer")
	commitFile(t, other, "conflict.txt", "offer side\n", "offer side")
	gitIn(t, other, "push", "-q", "origin", "feat/offer")
	gitIn(t, other, "switch", "-q", "main")
	commitFile(t, other, "conflict.txt", "main side\n", "main side")
	gitIn(t, other, "push", "-q", "origin", "main")

	h.receive(message("update feat/offer with main"))
	final := approve(t, h)
	if final.Status != StatusDone || len(h.codex.turns) != 2 {
		t.Fatalf("status %s, turns %d: %s", final.Status, len(h.codex.turns), journalOf(final))
	}
	gitIn(t, project, "fetch", "-q", "origin")
	if got := gitIn(t, project, "show", "origin/feat/offer:conflict.txt"); got != "both" {
		t.Errorf("the settled file is pushed: %q", got)
	}
	if !strings.Contains(journalOf(final), "Conflicts resolved in: conflict.txt") {
		t.Errorf("the recap names the files: %s", journalOf(final))
	}
}

func TestAConflictLeftWithItsMarkersIsNeverPushed(t *testing.T) {
	keep := func(ctx context.Context, turn Turn) (TurnResult, error) {
		return reply(map[string]any{"summary": "**Could not**"})(ctx, turn)
	}
	h, project, other := syncHarness(t, reply(syncPlan("feat/offer", "main")), keep)
	gitIn(t, other, "switch", "-q", "feat/offer")
	commitFile(t, other, "conflict.txt", "offer side\n", "offer side")
	gitIn(t, other, "push", "-q", "origin", "feat/offer")
	gitIn(t, other, "switch", "-q", "main")
	commitFile(t, other, "conflict.txt", "main side\n", "main side")
	gitIn(t, other, "push", "-q", "origin", "main")

	before := gitIn(t, other, "rev-parse", "origin/feat/offer")
	h.receive(message("update feat/offer with main"))
	final := approve(t, h)
	if final.Status == StatusDone {
		t.Fatalf("an unsettled conflict is not a success: %s", journalOf(final))
	}
	gitIn(t, project, "fetch", "-q", "origin")
	if gitIn(t, project, "rev-parse", "origin/feat/offer") != before {
		t.Error("nothing may be pushed")
	}
	if left := gitIn(t, project, "branch", "--list", "pouple/*"); left != "" {
		t.Errorf("nothing is left behind: %q", left)
	}
}

func TestABranchThatHasEverythingAlreadyIsLeftAlone(t *testing.T) {
	h, project, _ := syncHarness(t, reply(syncPlan("feat/offer", "main")), reply(syncPlan("feat/offer", "main")))
	h.receive(message("update feat/offer with main"))
	approve(t, h)
	// A second time, there is nothing to receive.
	gitIn(t, project, "fetch", "-q", "origin")
	before := gitIn(t, project, "rev-parse", "origin/feat/offer")
	h.receive(message("update feat/offer with main again"))
	got := h.svc.Snapshot().Requests
	last := got[len(got)-1]
	if last.Status != StatusAwaitingUser || last.Plan == nil {
		t.Fatalf("a push is always shown first, even after an approval for the project: %+v", last)
	}
	if err := h.svc.DecidePlan(last.ID, true, last.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if after := gitIn(t, project, "rev-parse", "origin/feat/offer"); after != before {
		t.Error("nothing to push")
	}
	if done := h.svc.Snapshot().Requests; done[len(done)-1].Status != StatusReady || !strings.Contains(journalOf(done[len(done)-1]), "already has everything") {
		t.Errorf("it says so: %s", journalOf(done[len(done)-1]))
	}
}

// The user approves once, at the start: updates in that project are then covered, whichever branch (they are read in
// the plan they validate, and strangers' text revokes the cover: see ForeignSinceGrant).
func TestTheApprovalOfABranchUpdateCoversTheProjectForTheConversation(t *testing.T) {
	action := func(branch string) Action {
		return Action{Kind: SyncBranch, Scope: Scope{Service: ScopeCode, ID: "login-app"}, Summary: "s", Branch: branch, Base: "main"}
	}
	plan := Plan{Goal: "g", Brief: "b", Status: PlanApproved, Actions: []Action{action("feat/offer")}}
	grants, err := Grants{}.With(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !grants.Covers(action("feat/offer")) || !grants.Covers(action("feat/other")) {
		t.Error("the project is covered")
	}
	if grants.Covers(Action{Kind: SyncBranch, Scope: Scope{Service: ScopeCode, ID: "other-app"}, Summary: "s", Branch: "feat/offer", Base: "main"}) {
		t.Error("another project is read first")
	}
}

// After the one question, a second update in the same conversation runs by itself, even though GitLab's text went into
// the conversation (the user read it with the plan).
func TestAfterTheFirstApprovalTheNextUpdateOfThatBranchRunsByItself(t *testing.T) {
	h, project, other := syncHarness(t, reply(syncPlan("feat/offer", "main")), reply(syncPlan("feat/offer", "main")))
	h.receive(message("update feat/offer with main"))
	got := waitingPlan(t, h)
	h.svc.deps.Store.Update(got.ID, func(r *Stored) { r.ServicesUsed = true })
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	gitIn(t, other, "switch", "-q", "main")
	commitFile(t, other, "later.txt", "later\n", "a later fix")
	gitIn(t, other, "push", "-q", "origin", "main")

	if err := h.svc.Message(got.ID, "main moved again, redo it", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusDone {
		t.Fatalf("no second question: %s: %s", final.Status, journalOf(final))
	}
	gitIn(t, project, "fetch", "-q", "origin")
	if gitIn(t, project, "show", "origin/feat/offer:later.txt") != "later" {
		t.Error("the second update is pushed")
	}
}

func TestPlansThatNameBranchesWronglyAreRefused(t *testing.T) {
	base := Plan{Goal: "g", Brief: "b", Status: PlanProposed}
	action := func(kind ActionKind, branch, base string) Action {
		return Action{Kind: kind, Scope: Scope{Service: ScopeCode, ID: "login-app"}, Summary: "s", Branch: branch, Base: base}
	}
	for name, a := range map[string]Action{
		"the same branch":              action(SyncBranch, "feat/x", "feat/x"),
		"an option":                    action(SyncBranch, "--force", "main"),
		"a parent folder":              action(SyncBranch, "feat/../x", "main"),
		"no base":                      action(SyncBranch, "feat/x", ""),
		"an edit starting from itself": action(EditCode, "feat/x", "feat/x"),
		"a lock name":                  action(SyncBranch, "feat/x.lock", "main"),
		"a name starting with a slash": action(SyncBranch, "/feat/x", "main"),
	} {
		plan := base
		plan.Actions = []Action{a}
		if _, err := plan.Normalize(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	// What branch to update is the user's call, read in the plan: main included.
	for _, branch := range []string{"feat/premium-offer-layout", "main", "release/1.2"} {
		plan := base
		plan.Actions = []Action{action(SyncBranch, branch, "develop")}
		if _, err := plan.Normalize(); err != nil {
			t.Errorf("%s is the user's to choose: %v", branch, err)
		}
	}
}

// A line made of "=======" that comes from the base (a title underline) is not a conflict marker, and an accented file
// name in conflict reaches the AI as it is written.
func TestACleanFileWithAnUnderlineIsNotAConflictAndAccentedNamesSurvive(t *testing.T) {
	var prompt string
	settle := func(ctx context.Context, turn Turn) (TurnResult, error) {
		prompt = turn.Prompt
		if err := os.WriteFile(filepath.Join(turn.Dir, "café.txt"), []byte("both\n"), 0o644); err != nil {
			return TurnResult{}, err
		}
		return reply(map[string]any{"summary": "**Kept both**"})(ctx, turn)
	}
	h, project, other := syncHarness(t, reply(syncPlan("feat/offer", "main")), settle)
	gitIn(t, other, "switch", "-q", "feat/offer")
	commitFile(t, other, "café.txt", "offer side\n", "offer side")
	gitIn(t, other, "push", "-q", "origin", "feat/offer")
	gitIn(t, other, "switch", "-q", "main")
	commitFile(t, other, "café.txt", "main side\n", "main side")
	commitFile(t, other, "doc.rst", "Title\n=======\n", "a document")
	gitIn(t, other, "push", "-q", "origin", "main")

	h.receive(message("update feat/offer with main"))
	final := approve(t, h)
	if final.Status != StatusDone || !strings.Contains(prompt, "café.txt") || strings.Contains(prompt, "\\303") {
		t.Fatalf("status %s, prompt %q: %s", final.Status, prompt, journalOf(final))
	}
	gitIn(t, project, "fetch", "-q", "origin")
	if got := gitIn(t, project, "show", "origin/feat/offer:doc.rst"); !strings.Contains(got, "=======") {
		t.Errorf("the document from main must arrive: %q", got)
	}
}

// New work is committed on the branch the plan names, pushed, and the user is told.
func TestNewWorkIsPushedOnABranchWithATypedNameAndTheUserIsTold(t *testing.T) {
	h, project, _ := syncHarness(t, reply(samplePlan()), writes("redirect.txt", "keep the page\n"))
	h.receive(message("fix the login redirect"))
	final := approve(t, h)
	if final.Status != StatusDone {
		t.Fatalf("status %s: %s", final.Status, journalOf(final))
	}
	gitIn(t, project, "fetch", "-q", "origin")
	if got := gitIn(t, project, "show", "origin/fix/login-redirect:redirect.txt"); got != "keep the page" {
		t.Errorf("the branch is on origin with the work: %q", got)
	}
	if left := gitIn(t, project, "branch", "--list", "pouple/*"); left != "" {
		t.Errorf("no branch of Pouple's own: %q", left)
	}
	if upstream := gitIn(t, project, "rev-parse", "--abbrev-ref", "fix/login-redirect@{upstream}"); upstream != "origin/fix/login-redirect" {
		t.Errorf("the branch follows origin: %q", upstream)
	}
	notes := h.notifications()
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1], "fix/login-redirect pushed") {
		t.Errorf("the user is told what was pushed: %v", notes)
	}
}

// A name that is taken, here or on origin, is never reused: the work never lands on someone else's branch.
func TestATakenBranchNameGetsASuffixInsteadOfBeingReused(t *testing.T) {
	h, project, other := syncHarness(t, reply(samplePlan()), writes("redirect.txt", "keep the page\n"))
	gitIn(t, other, "switch", "-q", "-c", "fix/login-redirect")
	commitFile(t, other, "mine.txt", "a colleague's work\n", "colleague")
	gitIn(t, other, "push", "-q", "origin", "fix/login-redirect")
	h.receive(message("fix the login redirect"))
	approve(t, h)
	gitIn(t, project, "fetch", "-q", "origin")
	if got := gitIn(t, project, "show", "origin/fix/login-redirect:mine.txt"); got != "a colleague's work" {
		t.Errorf("their branch is untouched: %q", got)
	}
	if got := gitIn(t, project, "show", "origin/fix/login-redirect-2:redirect.txt"); got != "keep the page" {
		t.Errorf("the work goes to another name: %q", got)
	}
}

func TestNewWorkStartsFromTheBaseTheUserNames(t *testing.T) {
	plan := samplePlan()
	plan["plan"].(map[string]any)["actions"].([]any)[0].(map[string]any)["base"] = "main"
	h, project, _ := syncHarness(t, reply(plan), writes("redirect.txt", "keep the page\n"))
	h.receive(message("fix the login redirect"))
	approve(t, h)
	gitIn(t, project, "fetch", "-q", "origin")
	if got := gitIn(t, project, "show", "origin/fix/login-redirect:visa.txt"); got != "visa fix" {
		t.Errorf("the branch starts from the latest main on origin, which the project had not fetched: %q", got)
	}
}

func TestNewWorkNamedLikeABranchOfPouplesOwnOrWithoutATypeIsRefused(t *testing.T) {
	for _, branch := range []string{"pouple/fix-it", "fix", "Fix/It", "fix/it with space", ""} {
		plan := Plan{Goal: "g", Brief: "b", Actions: []Action{{Kind: EditCode, Scope: Scope{Service: ScopeCode, ID: "login-app"}, Summary: "s", Branch: branch}}}
		if _, err := plan.Normalize(); err == nil {
			t.Errorf("%q must be refused", branch)
		}
	}
}

// The user ticks what they want: only that is carried out and granted, and the rest is dropped from the plan.
func TestOnlyTheTickedActionsOfAPlanAreApprovedAndCarriedOut(t *testing.T) {
	two := syncPlan("feat/offer", "main")
	actions := two["plan"].(map[string]any)["actions"].([]any)
	actions = append(actions, samplePlan()["plan"].(map[string]any)["actions"].([]any)[0])
	two["plan"].(map[string]any)["actions"] = actions
	h, project, _ := syncHarness(t, reply(two))
	h.receive(message("update feat/offer with main, and fix the login redirect"))
	got := waitingPlan(t, h)
	if len(got.Plan.Actions) != 2 {
		t.Fatalf("both actions are proposed: %+v", got.Plan.Actions)
	}
	for name, selection := range map[string][]int{"nothing": {}, "outside": {5}, "twice": {0, 0}} {
		if err := h.svc.DecidePlanActions(got.ID, true, got.Plan.ID, selection); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(h.codex.turns) != 1 || h.svc.Snapshot().Requests[0].Status != StatusAwaitingUser {
		t.Fatal("a refused selection leaves the plan waiting and writes nothing")
	}

	if err := h.svc.DecidePlanActions(got.ID, true, got.Plan.ID, []int{0}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusDone || len(final.Plan.Actions) != 1 || final.Plan.Actions[0].Kind != SyncBranch {
		t.Fatalf("only the ticked action remains: %+v", final.Plan)
	}
	stored, _ := h.svc.deps.Store.Get(final.ID)
	if stored.Grants.Covers(Action{Kind: EditCode, Scope: Scope{Service: ScopeCode, ID: "login-app"}, Summary: "x", Branch: "fix/login-redirect"}) {
		t.Error("what was not ticked is not granted")
	}
	gitIn(t, project, "fetch", "-q", "origin")
	if out, err := exec.Command("git", "-C", project, "rev-parse", "--verify", "--quiet", "origin/fix/login-redirect").CombinedOutput(); err == nil {
		t.Errorf("the unticked action must not run: %s", out)
	}
}

// A push that origin refuses loses nothing: the merge is kept on a branch named after what it joins, never after Pouple.
func TestARefusedPushKeepsTheMergeOnANamedBranch(t *testing.T) {
	h, project, _ := syncHarness(t, reply(syncPlan("feat/offer", "main")))
	origin := gitIn(t, project, "remote", "get-url", "origin")
	hook := filepath.Join(origin, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho refused by the test >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.receive(message("update feat/offer with main"))
	final := approve(t, h)
	if final.Status == StatusDone {
		t.Fatalf("a refused push is not a success: %s", journalOf(final))
	}
	if kept := gitIn(t, project, "branch", "--list", "feat/offer-merge-main"); kept == "" {
		t.Fatalf("the merge must be kept: %s", gitIn(t, project, "branch", "-a"))
	}
	if subject := gitIn(t, project, "log", "-1", "--format=%s", "feat/offer-merge-main"); subject != "Merge branch 'main' into feat/offer" {
		t.Errorf("the kept merge: %q", subject)
	}
	if all := gitIn(t, project, "branch", "-a"); strings.Contains(all, "pouple") {
		t.Errorf("no branch carries Pouple's name: %q", all)
	}
	if worktrees := gitIn(t, project, "worktree", "list"); strings.Count(worktrees, "\n") != 0 {
		t.Errorf("the worktree must go: %q", worktrees)
	}
}
