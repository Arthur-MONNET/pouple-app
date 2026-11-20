package engine

import (
	"strings"
	"testing"
)

func terminalPlan() map[string]any {
	return map[string]any{
		"outcome": ProposesPlan, "summary": "**Inventory lab and start the tunnel**",
		"plan": map[string]any{
			"goal": "Inventory lab and start ngrok", "brief": "The user opened ssh lab in the terminal of this conversation.",
			"actions": []any{map[string]any{"kind": "terminal.run", "scope": map[string]any{"service": "terminal", "id": ".", "name": "ssh lab"}, "summary": "List the home folders and docker containers, then start ngrok on the n8n port", "branch": "", "base": ""}},
		},
	}
}

// A request the user wrote can reach the terminal through a plan: the AI is told so instead of being left to say it is
// blocked. Once approved, the plan is carried out in a turn of its own that sees the plan only (never the request or the
// investigation's thread), read-only, through the conversation's terminal; and the approval opens no direct work.
func TestAnApprovedTerminalPlanRunsInATurnOfItsOwn(t *testing.T) {
	h := newHarness(t, reply(terminalPlan()), reply(map[string]any{"summary": "**Tunnel started**"}))
	h.svc.deps.InteractiveTerminal = true
	h.receive(noteToSelf("look at how lab is organised and start my ngrok for n8n"))
	if first := h.codex.turns[0]; !strings.Contains(first.Prompt, terminalPlanHint) || first.Sandbox != ReadOnly {
		t.Fatalf("the first turn must offer a terminal plan, read-only: %s\n%s", first.Sandbox, first.Prompt)
	}
	got := waitingPlan(t, h)
	if len(h.codex.turns) != 1 || h.svc.trusted(got.ID) {
		t.Fatal("nothing runs and nothing is granted before the approval")
	}

	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()

	if len(h.codex.turns) != 2 {
		t.Fatalf("the approval must run one turn, got %d", len(h.codex.turns))
	}
	work := h.codex.turns[1]
	if work.Sandbox != ReadOnly || work.ResumeThread != "" || work.sshAgent != "" {
		t.Fatalf("the plan's turn is fresh, read-only and without the agent: %s %q %q", work.Sandbox, work.ResumeThread, work.sshAgent)
	}
	for _, want := range []string{"approved the plan below for the interactive terminal", "start ngrok on the n8n port", "use outcome lookup with service terminal"} {
		if !strings.Contains(work.Prompt, want) {
			t.Errorf("the turn must carry %q:\n%s", want, work.Prompt)
		}
	}
	for _, unread := range []string{"look at how lab is organised", "The user opened ssh lab"} {
		if strings.Contains(work.Prompt, unread) {
			t.Errorf("the plan's turn must not see the request or the brief (%q)", unread)
		}
	}
	final := h.svc.Snapshot().Requests[0]
	if final.Status != StatusReady || !strings.Contains(journalOf(final), "Working in the terminal") || !strings.Contains(journalOf(final), "Tunnel started") {
		t.Fatalf("status %s journal %s", final.Status, journalOf(final))
	}
	if h.svc.trusted(final.ID) {
		t.Error("a terminal approval must not open direct work for the follow-ups")
	}
}

// The plan's turn drives the terminal and nothing else: another lookup is not run, and the turn goes on in its own thread.
func TestATerminalPlanRunsOnlyTerminalLookups(t *testing.T) {
	lookup := reply(map[string]any{"outcome": LookupOutcome, "summary": "searching", "lookups": []any{map[string]any{"service": "slack", "action": "search", "target": "n8n", "text": ""}}})
	h := newHarness(t, reply(terminalPlan()), lookup, reply(map[string]any{"summary": "**Done**"}))
	h.svc.deps.InteractiveTerminal = true
	h.receive(noteToSelf("start my ngrok for n8n on lab"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if len(h.codex.turns) != 3 {
		t.Fatalf("expected the plan's turn and its follow-up, got %d turns", len(h.codex.turns))
	}
	next := h.codex.turns[2]
	if next.ResumeThread != "thread-1" || !strings.Contains(next.Prompt, "Only terminal lookups are carried out") || next.Sandbox != ReadOnly {
		t.Fatalf("thread %q sandbox %s:\n%s", next.ResumeThread, next.Sandbox, next.Prompt)
	}
}

// Every terminal plan is asked: what the user approved before does not cover a session opened since (another server).
func TestEachTerminalPlanIsAskedAgain(t *testing.T) {
	h := newHarness(t, reply(terminalPlan()), reply(map[string]any{"summary": "**Done**"}), reply(terminalPlan()))
	h.svc.deps.InteractiveTerminal = true
	h.receive(noteToSelf("start my ngrok for n8n on lab"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if err := h.svc.Message(got.ID, "now do the same on prod", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if again := waitingPlan(t, h); again.Plan.Status != PlanProposed || len(h.codex.turns) != 3 {
		t.Fatalf("the second terminal plan must wait for the user: %+v, %d turns", again.Plan, len(h.codex.turns))
	}
}

// Approving terminal work does not vouch for what others wrote: a conversation that lost direct work stays without it.
func TestATerminalApprovalDoesNotRestoreDirectWork(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"summary": "**Done**"}))
	plan, err := Plan{Goal: "Start ngrok", Actions: []Action{{Kind: RunTerminal, Scope: Scope{Service: ScopeTerminal, ID: ConversationTerminal}, Summary: "Start ngrok on the n8n port"}}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	h.svc.deps.Store.Add(Stored{
		Request: Request{
			ID: "mixed", Status: StatusAwaitingUser, Plan: &plan,
			Grants: Grants{{Scope: codeScope("login-app"), Kinds: []ActionKind{EditCode}}},
			Source: Source{Origin: "note-to-self", SenderName: "Arthur", TS: "1.1"},
		},
		ForeignSinceGrant: true,
	})
	if err := h.svc.DecidePlan("mixed", true, PlanFingerprint(plan)); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if stored, _ := h.svc.deps.Store.Get("mixed"); !stored.ForeignSinceGrant || h.svc.trusted("mixed") {
		t.Fatalf("direct work must stay closed: foreign=%v trusted=%v", stored.ForeignSinceGrant, h.svc.trusted("mixed"))
	}
}

// What others wrote may shape a plan: it must never put the terminal one click away.
func TestATerminalPlanFromSomeoneElsesMessageIsRefused(t *testing.T) {
	h := newHarness(t, reply(terminalPlan()))
	h.svc.deps.InteractiveTerminal = true
	h.receive(message("can you start ngrok on lab for n8n?"))
	got := h.svc.Snapshot().Requests[0]
	if got.Plan != nil || got.Status == StatusAwaitingUser || !strings.Contains(journalOf(got), "request you wrote yourself") {
		t.Fatalf("a terminal plan from a stranger's message must be refused: %s %+v %s", got.Status, got.Plan, journalOf(got))
	}
	if strings.Contains(h.codex.turns[0].Prompt, terminalPlanHint) {
		t.Error("the hint to plan terminal work is only for requests the user wrote")
	}
}

// The setting that approves every change is for code on a branch of its own, never for the terminal.
func TestATerminalPlanIsNeverApprovedByTheSetting(t *testing.T) {
	h := newHarness(t, reply(terminalPlan()))
	h.svc.deps.InteractiveTerminal = true
	h.svc.deps.AutoApproveAll = func() bool { return true }
	h.receive(noteToSelf("start my ngrok for n8n on lab"))
	got := waitingPlan(t, h)
	if len(h.codex.turns) != 1 || h.svc.trusted(got.ID) {
		t.Fatal("a terminal plan waits for the user even when every change is allowed")
	}
}
