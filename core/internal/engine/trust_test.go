package engine

import (
	"context"
	"strings"
	"testing"
)

// Full access is the user's own terminal: every tool, no question, no rule on the files the user's tools may need; only
// Pouple's own data folder stays unreadable.
func TestClaudeArgsForFullAccessAreTheUsersOwnTerminal(t *testing.T) {
	r := NewClaudeRunner(ClaudeOptions{ProtectedDirs: []string{"/data/Pouple"}})
	args, err := r.BuildArgs(Turn{Sandbox: FullAccess, Dir: "/projects/app"}, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--permission-mode bypassPermissions", "--dangerously-skip-permissions", "Bash", "WebFetch", "--add-dir /projects/app", "Read(//data/Pouple/**)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	for _, unwanted := range []string{"--disallowedTools", "Read(**/.env)", "Edit(**/.env)"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("a turn of the user's own work is not restricted by %q: %s", unwanted, joined)
		}
	}
}

func TestCodexArgsForFullAccessHaveNoSandboxAndTheWholeEnvironment(t *testing.T) {
	joined := strings.Join(NewRunner(RunnerOptions{}).BuildArgs(Turn{Sandbox: FullAccess}), " ")
	for _, want := range []string{"--sandbox danger-full-access", `sandbox_mode="danger-full-access"`, `shell_environment_policy.inherit="all"`, `web_search="live"`, "mcp_servers={}"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	read := strings.Join(NewRunner(RunnerOptions{}).BuildArgs(Turn{Sandbox: ReadOnly}), " ")
	if !strings.Contains(read, `shell_environment_policy.inherit="core"`) || !strings.Contains(read, `web_search="disabled"`) {
		t.Errorf("what others wrote is still investigated with the strict settings: %s", read)
	}
}

// Level one: what others wrote is investigated read-only, a follow-up included, until the user approves something.
// Level two: once they approved a plan, their own messages are worked on directly, with no restriction.
func TestTheUsersOwnMessagesAreWorkedOnDirectlyOnlyAfterTheyApprovedAPlan(t *testing.T) {
	h, _, _ := planHarness(t,
		reply(samplePlan()), writes("redirect.txt", "keep the page\n"),
		reply(map[string]any{"outcome": Answered, "summary": "**Before approval**"}),
		reply(map[string]any{"outcome": Answered, "summary": "**After approval**"}),
	)
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)

	// Before the approval, a message is carried out read-only: the plan is still waiting, nothing is granted.
	if sandbox := h.codex.turns[0].Sandbox; sandbox != ReadOnly {
		t.Fatalf("what others wrote is read-only: %s", sandbox)
	}
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	before := len(h.codex.turns)
	if err := h.svc.Message(got.ID, "now run the tests and tell me", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if len(h.codex.turns) != before+1 {
		t.Fatalf("one turn for the message: %d -> %d", before, len(h.codex.turns))
	}
	turn := h.codex.turns[before]
	if turn.Sandbox != FullAccess || !strings.Contains(turn.Prompt, "work with you directly") || strings.Contains(turn.Prompt, "read-only as before") {
		t.Fatalf("after the approval the user works directly: %s\n%s", turn.Sandbox, turn.Prompt)
	}
}

func TestStrangersTextTakesTheDirectModeAwayUntilTheNextApproval(t *testing.T) {
	h, _, _ := planHarness(t,
		reply(samplePlan()), writes("redirect.txt", "keep the page\n"),
		reply(map[string]any{"outcome": Answered, "summary": "**Answer**"}),
	)
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	// Someone else writes in the thread afterwards.
	h.svc.deps.Store.Update(got.ID, func(r *Stored) { r.ForeignSinceGrant = true })
	before := len(h.codex.turns)
	if err := h.svc.Message(got.ID, "and then?", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if turn := h.codex.turns[before]; turn.Sandbox != ReadOnly || !strings.Contains(turn.Prompt, "read-only as before") {
		t.Fatalf("after a stranger's text the conversation is read-only again: %s", turn.Sandbox)
	}
}

// The writing turns of an approved plan have full access: they install, use the network and the project's tools.
func TestTheWritingTurnsOfAnApprovedPlanHaveFullAccess(t *testing.T) {
	h, _, _ := planHarness(t, reply(samplePlan()), func(ctx context.Context, turn Turn) (TurnResult, error) {
		if turn.Sandbox != FullAccess || !strings.Contains(turn.Prompt, "install dependencies") || strings.Contains(turn.Prompt, "Do not commit, push, install dependencies or use the network") {
			t.Errorf("the writer is not restricted: %s\n%s", turn.Sandbox, turn.Prompt)
		}
		return writes("redirect.txt", "keep the page\n")(ctx, turn)
	})
	h.receive(message("fix the login redirect"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if final := h.svc.Snapshot().Requests[0]; final.Status != StatusDone {
		t.Fatalf("%s: %s", final.Status, journalOf(final))
	}
}
