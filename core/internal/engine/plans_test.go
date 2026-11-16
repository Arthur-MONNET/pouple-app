package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.email=a@b.c", "-c", "user.name=Arthur"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// planHarness is a harness whose project is a real git repository, with a worktrees folder of its own.
func planHarness(t *testing.T, steps ...func(context.Context, Turn) (TurnResult, error)) (*harness, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	h := newHarness(t, steps...)
	project := filepath.Join(h.root, "login-app")
	gitIn(t, project, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(project, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, project, "add", "-A")
	gitIn(t, project, "commit", "-qm", "first")
	worktrees := filepath.Join(t.TempDir(), "worktrees")
	h.svc.deps.WorktreesRoot = func() string { return worktrees }
	return h, project, worktrees
}

func samplePlan() map[string]any {
	return map[string]any{
		"outcome": ProposesPlan, "summary": "**A plan**",
		"plan": map[string]any{
			"goal": "Fix the login redirect", "brief": "After login, users land on /home instead of the page they asked for.",
			"actions": []any{map[string]any{"kind": "code.edit", "scope": map[string]any{"service": "code", "id": "login-app", "name": "Login app"}, "summary": "Keep the requested page after login", "branch": "fix/login-redirect", "base": ""}},
		},
	}
}

// writes simulates the AI that writes: it changes a file where it is asked to work.
func writes(file, content string) func(context.Context, Turn) (TurnResult, error) {
	answer := reply(map[string]any{"summary": "**Done**"})
	return func(ctx context.Context, turn Turn) (TurnResult, error) {
		if err := os.WriteFile(filepath.Join(turn.Dir, file), []byte(content), 0o644); err != nil {
			return TurnResult{}, err
		}
		return answer(ctx, turn)
	}
}

func waitingPlan(t *testing.T, h *harness) Request {
	t.Helper()
	got := h.svc.Snapshot().Requests[0]
	if got.Status != StatusAwaitingUser || got.Plan == nil || got.Plan.Status != PlanProposed || got.Plan.ID == "" {
		t.Fatalf("a plan must wait for the user: %+v", got)
	}
	return got
}

func TestAPlanWaitsForApprovalThenIsWrittenOnABranchOfItsOwn(t *testing.T) {
	h, project, worktrees := planHarness(t, reply(samplePlan()), writes("redirect.txt", "keep the page\n"))
	h.receive(message("when I log in from a link I always land on /home, please fix that"))
	got := waitingPlan(t, h)
	if len(h.codex.turns) != 1 {
		t.Fatalf("nothing is written before the approval: %d turns", len(h.codex.turns))
	}
	if h.svc.Snapshot().Requests[0].Grants != nil {
		t.Fatal("nothing is granted before the approval")
	}

	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()

	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusDone || final.Plan == nil || final.Plan.Status != PlanApproved {
		t.Fatalf("status %s plan %+v", final.Status, final.Plan)
	}
	// The AI that wrote saw the plan and never the request's text, and worked in a copy, not in the project.
	write := h.codex.turns[1]
	if !strings.Contains(write.Prompt, "users land on /home") || strings.Contains(write.Prompt, "when I log in from a link") {
		t.Fatalf("the writing turn must get the plan only:\n%s", write.Prompt)
	}
	if write.Sandbox != FullAccess || !strings.HasPrefix(write.Dir, worktrees) || write.Dir == project {
		t.Fatalf("dir %q sandbox %s", write.Dir, write.Sandbox)
	}
	// The user's checkout and branch are untouched; the work is one commit on a branch of its own.
	if _, err := os.Stat(filepath.Join(project, "redirect.txt")); err == nil {
		t.Error("the project folder must not be written")
	}
	if branch := gitIn(t, project, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Errorf("the user's branch must stay: %s", branch)
	}
	if status := gitIn(t, project, "status", "--porcelain"); status != "" {
		t.Errorf("the user's checkout must stay clean: %q", status)
	}
	branches := gitIn(t, project, "branch", "--list", "fix/*")
	if !strings.Contains(branches, "fix/login-redirect") {
		t.Fatalf("branches: %q", branches)
	}
	if subject := gitIn(t, write.Dir, "log", "-1", "--format=%s"); subject != "Fix the login redirect" {
		t.Errorf("commit subject: %q", subject)
	}
	if files := gitIn(t, write.Dir, "show", "--name-only", "--format=", "HEAD"); files != "redirect.txt" {
		t.Errorf("the commit holds only what was written: %q", files)
	}
	// The approval is a grant for this conversation.
	stored, _ := h.svc.deps.Store.Get(final.ID)
	if !stored.Grants.Covers(final.Plan.Actions[0]) {
		t.Errorf("the places of an approved plan are granted: %+v", stored.Grants)
	}
	if !strings.Contains(journalOf(final), "Committed on branch fix/login-redirect") {
		t.Errorf("the conversation says where the work is: %s", journalOf(final))
	}
}

func TestAnApprovalNamesThePlanTheUserRead(t *testing.T) {
	h, _, _ := planHarness(t, reply(samplePlan()))
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, "0000000000000000"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("a plan that is not the one read must be refused: %v", err)
	}
	if len(h.codex.turns) != 1 {
		t.Fatal("nothing may be written")
	}
	// Declining closes it, and grants nothing.
	if err := h.svc.DecidePlan(got.ID, false, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	final := h.svc.Snapshot().Requests[0]
	if final.Plan.Status != PlanDeclined || final.Grants != nil || len(h.codex.turns) != 1 {
		t.Fatalf("%+v", final)
	}
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err == nil {
		t.Error("a decided plan cannot be decided again")
	}
}

func TestInsideAPlaceApprovedInThisConversationLaterWorkRunsWithoutAsking(t *testing.T) {
	second := samplePlan()
	second["plan"].(map[string]any)["goal"] = "Also fix the logout redirect"
	h, _, _ := planHarness(t, reply(samplePlan()), writes("a.txt", "1\n"), reply(second), writes("b.txt", "2\n"))
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if err := h.svc.Message(got.ID, "and the logout one too", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()

	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusDone || len(h.codex.turns) != 4 {
		t.Fatalf("a second plan in the same place needs no new approval: status %s, %d turns", final.Status, len(h.codex.turns))
	}
	if !strings.Contains(journalOf(final), "Inside what you already approved here") {
		t.Errorf("the conversation says why it did not ask: %s", journalOf(final))
	}
}

func TestAPlanThatCannotBeCarriedOutIsRefusedBeforeTheUserIsAsked(t *testing.T) {
	push := samplePlan()
	push["plan"].(map[string]any)["actions"] = []any{map[string]any{"kind": "gitlab.push", "scope": map[string]any{"service": "gitlab", "id": "dating/login-app", "name": ""}, "summary": "Push the branch"}}
	missing := samplePlan()
	missing["plan"].(map[string]any)["actions"].([]any)[0].(map[string]any)["scope"] = map[string]any{"service": "code", "id": "no-such-project", "name": ""}
	hidden := samplePlan()
	hidden["plan"].(map[string]any)["goal"] = "Fix it‮ and more"
	for name, plan := range map[string]map[string]any{"another kind of write": push, "an unknown project": missing, "hidden characters": hidden} {
		h, _, _ := planHarness(t, reply(plan))
		h.receive(message("fix the login redirect"))
		got := h.svc.Snapshot().Requests[0]
		if got.Status != StatusReady || got.Plan != nil || len(h.codex.turns) != 1 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestAPlanThatChangesNothingLeavesNoBranchBehind(t *testing.T) {
	h, project, worktrees := planHarness(t, reply(samplePlan()), reply(map[string]any{"summary": "**Nothing to change**"}))
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusReady || !strings.Contains(journalOf(final), "No file was changed") {
		t.Fatalf("%s %s", final.Status, journalOf(final))
	}
	if branches := gitIn(t, project, "branch", "--list", "fix/*"); branches != "" {
		t.Errorf("no branch for nothing: %q", branches)
	}
	if entries, _ := os.ReadDir(worktrees); len(entries) != 0 {
		t.Errorf("no worktree for nothing: %v", entries)
	}
}

func TestAProjectThatIsNotAGitRepositoryIsNeverWrittenInPlace(t *testing.T) {
	h := newHarness(t, reply(samplePlan()))
	h.svc.deps.WorktreesRoot = func() string { return filepath.Join(t.TempDir(), "worktrees") }
	h.receive(message("fix the login redirect"))
	got := h.svc.Snapshot().Requests[0]
	if got.Status != StatusReady || got.Plan != nil || len(h.codex.turns) != 1 || !strings.Contains(journalOf(got), "not a git repository") {
		t.Fatalf("a plan for code that cannot go on a branch is refused, not written in the folder: %s %+v", got.Status, got.Plan)
	}
}

func TestAnApprovalMustNameThePlanInTheEngineToo(t *testing.T) {
	h, _, _ := planHarness(t, reply(samplePlan()))
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, ""); err == nil {
		t.Fatal("an approval that names no plan must be refused")
	}
	if len(h.codex.turns) != 1 {
		t.Fatal("nothing may be written")
	}
}

// What the AI that writes can edit is its folder, and the `.git` file in it says where the git folder is: Pouple must
// never commit through a `.git` the AI rewrote, since git would then run what that folder's config says.
func TestAWorktreeWhoseGitPointerWasRewrittenIsNotCommittedFrom(t *testing.T) {
	hostile := func(ctx context.Context, turn Turn) (TurnResult, error) {
		if err := os.WriteFile(filepath.Join(turn.Dir, ".git"), []byte("gitdir: /tmp/somewhere-else\n"), 0o644); err != nil {
			return TurnResult{}, err
		}
		if err := os.WriteFile(filepath.Join(turn.Dir, "x.txt"), []byte("x\n"), 0o644); err != nil {
			return TurnResult{}, err
		}
		return reply(map[string]any{"summary": "**Done**"})(ctx, turn)
	}
	h, project, _ := planHarness(t, reply(samplePlan()), hostile)
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusFailed || final.Error == nil || !strings.Contains(*final.Error, "does not commit from") {
		t.Fatalf("status %s error %v", final.Status, final.Error)
	}
	if log := gitIn(t, project, "log", "--oneline", "--all"); strings.Count(log, "\n") != 0 {
		t.Errorf("nothing may be committed: %q", log)
	}
}

// The AI that writes installs what it needs in its own copy: the project's dependencies are never linked to it, and what it
// installed is never committed.
func TestWhatTheWritingAIInstallsStaysInItsOwnCopyAndIsNeverCommitted(t *testing.T) {
	install := func(ctx context.Context, turn Turn) (TurnResult, error) {
		if err := os.MkdirAll(filepath.Join(turn.Dir, "node_modules", "left-pad"), 0o755); err != nil {
			return TurnResult{}, err
		}
		if err := os.WriteFile(filepath.Join(turn.Dir, "node_modules", "left-pad", "index.js"), []byte("module.exports = 1\n"), 0o644); err != nil {
			return TurnResult{}, err
		}
		return writes("redirect.txt", "keep the page\n")(ctx, turn)
	}
	h, project, _ := planHarness(t, reply(samplePlan()), install)
	if err := os.MkdirAll(filepath.Join(project, "node_modules", "left-pad"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	dir := h.codex.turns[1].Dir
	if _, err := os.Readlink(filepath.Join(dir, "node_modules")); err == nil {
		t.Fatal("the project's dependencies must not be linked into the copy: an install would write through the link")
	}
	if files := gitIn(t, dir, "show", "--name-only", "--format=", "HEAD"); files != "redirect.txt" {
		t.Errorf("only what was written is committed: %q", files)
	}
}

// The AI is free to commit itself, like in a terminal: what it committed is kept and pushed, not thrown away as "nothing
// changed".
func TestACommitTheWritingAIMadeItselfIsKept(t *testing.T) {
	commits := func(ctx context.Context, turn Turn) (TurnResult, error) {
		if err := os.WriteFile(filepath.Join(turn.Dir, "redirect.txt"), []byte("keep the page\n"), 0o644); err != nil {
			return TurnResult{}, err
		}
		gitIn(t, turn.Dir, "add", "-A")
		gitIn(t, turn.Dir, "commit", "-qm", "the AI's own commit")
		return reply(map[string]any{"summary": "**Done**"})(ctx, turn)
	}
	h, project, _ := planHarness(t, reply(samplePlan()), commits)
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusDone {
		t.Fatalf("a commit made by the AI is work done: %s: %s", final.Status, journalOf(final))
	}
	if subject := gitIn(t, project, "log", "-1", "--format=%s", "fix/login-redirect"); subject != "the AI's own commit" {
		t.Errorf("the branch keeps it: %q", subject)
	}
}

func TestStrangersTextAfterTheApprovalMeansTheNextPlanIsShownFirst(t *testing.T) {
	second := samplePlan()
	second["plan"].(map[string]any)["goal"] = "Also fix the logout redirect"
	h, _, _ := planHarness(t, reply(samplePlan()), writes("a.txt", "1\n"), reply(second))
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	// Someone else writes in the thread afterwards: what they wrote may shape the next brief.
	h.svc.deps.Store.Update(got.ID, func(r *Stored) { r.ServicesUsed, r.ForeignSinceGrant = true, true })
	if err := h.svc.Message(got.ID, "and the logout one too", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusAwaitingUser || final.Plan == nil || final.Plan.Status != PlanProposed || len(h.codex.turns) != 3 {
		t.Fatalf("a plan made after outside text waits for the user even inside a granted project: %s %d", final.Status, len(h.codex.turns))
	}
}

// The schema offers the AI exactly the kinds of write Pouple can carry out: a kind it could ask for but Pouple cannot do
// would be refused only after the AI spent a turn on it.
func TestTheSchemaOffersOnlyTheKindsOfWriteThatCanBeCarriedOut(t *testing.T) {
	var schema struct {
		Properties struct {
			Plan struct {
				Properties struct {
					Actions struct {
						Items struct {
							Properties struct {
								Kind  struct{ Enum []string } `json:"kind"`
								Scope struct {
									Properties struct {
										Service struct{ Enum []string } `json:"service"`
									} `json:"properties"`
								} `json:"scope"`
							} `json:"properties"`
						} `json:"items"`
					} `json:"actions"`
				} `json:"properties"`
			} `json:"plan"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(OutcomeSchema), &schema); err != nil {
		t.Fatal(err)
	}
	item := schema.Properties.Plan.Properties.Actions.Items.Properties
	if len(item.Kind.Enum) != len(supportedKinds) {
		t.Fatalf("the schema offers %v, Pouple supports %v", item.Kind.Enum, supportedKinds)
	}
	for _, kind := range supportedKinds {
		service, _ := kind.Service()
		if !slices.Contains(item.Kind.Enum, string(kind)) || !slices.Contains(item.Scope.Properties.Service.Enum, string(service)) {
			t.Errorf("%s is supported but not offered", kind)
		}
	}
}

func TestWhenTheUserAllowsEveryChangeAPlanIsApprovedAsItIsMade(t *testing.T) {
	h, project, _ := planHarness(t, reply(samplePlan()), writes("redirect.txt", "keep the page\n"))
	h.svc.deps.AutoApproveAll = func() bool { return true }
	h.receive(message("fix the login redirect"))
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusDone || final.Plan == nil || final.Plan.Status != PlanApproved || len(h.codex.turns) != 2 {
		t.Fatalf("status %s turns %d", final.Status, len(h.codex.turns))
	}
	// Nobody read the plan: it is written without full access, and it grants nothing, so the follow-ups of this
	// conversation are not run with full access either.
	if write := h.codex.turns[1]; write.Sandbox != WorkspaceWrite || strings.Contains(write.Prompt, "full access") || !strings.Contains(write.Prompt, "use the network") {
		t.Errorf("an approval by the setting must not write with full access: %s\n%s", write.Sandbox, write.Prompt)
	}
	if stored, _ := h.svc.deps.Store.Get(final.ID); len(stored.Grants) != 0 || !stored.Plan.Automatic || h.svc.trusted(final.ID) {
		t.Errorf("an approval by the setting grants nothing: %+v", stored.Grants)
	}
	// Even then, the user's checkout is untouched.
	if status := gitIn(t, project, "status", "--porcelain"); status != "" {
		t.Errorf("the checkout must stay clean: %q", status)
	}
}

func (h *harness) projectDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(filepath.Join(h.root, "login-app"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
