package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func question(text string, multiple bool) map[string]any {
	return map[string]any{"text": text, "multiple": multiple, "options": []any{
		map[string]any{"label": "Reply to Estelle", "description": nil},
		map[string]any{"label": "Comment the task", "description": nil},
		map[string]any{"label": "Open a merge request", "description": nil},
	}}
}

func needsInput(multiple bool) func(context.Context, Turn) (TurnResult, error) {
	return reply(map[string]any{"outcome": NeedsInput, "questions": []any{question("What should I do?", multiple)}})
}

func only(t *testing.T, h *harness) Request {
	t.Helper()
	requests := h.svc.Snapshot().Requests
	if len(requests) != 1 {
		t.Fatalf("requests %+v", requests)
	}
	return requests[0]
}

func TestAMessageThatIsNotARequestNeverReachesTheUser(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": NotARequest}))
	h.receive(message("thanks!"))
	if request := only(t, h); request.Status != StatusIgnored || request.Engaged {
		t.Fatalf("a message that needs nothing engaged the user: %+v", request)
	}
}

func TestARequestEngagesTheUserAtItsFirstQuestionAndStaysEngagedWhileItWorks(t *testing.T) {
	block := make(chan struct{})
	h := newHarness(t, needsInput(false), func(ctx context.Context, turn Turn) (TurnResult, error) {
		<-block
		return reply(map[string]any{"outcome": Answered, "reply": "Done"})(ctx, turn)
	})
	h.receive(message("Can you check the retests?"))
	request := only(t, h)
	if request.Status != StatusAwaitingUser || !request.Engaged || request.Closed {
		t.Fatalf("the first question did not engage the request: %+v", request)
	}

	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionID: "A"}}); err != nil {
		t.Fatal(err)
	}
	// While the assistant works on the answer, the request stays engaged: its bubble shows the work.
	if working := only(t, h); !working.Status.Pending() || !working.Engaged {
		t.Fatalf("the work after an answer is not shown: %+v", working)
	}
	close(block)
	h.svc.Idle()
	if done := only(t, h); done.Status != StatusReady || !done.Engaged {
		t.Fatalf("answer %+v", done)
	}
}

func TestClosingHidesTheBubbleUntilTheRequestNeedsTheUserAgain(t *testing.T) {
	h := newHarness(t, needsInput(false), needsInput(false))
	h.receive(message("Can you check the retests?"))
	request := only(t, h)

	h.now = h.now.Add(time.Minute)
	if err := h.svc.Close(request.ID, ""); err != nil {
		t.Fatal(err)
	}
	closed := only(t, h)
	if !closed.Closed || closed.UpdatedAt != request.UpdatedAt {
		t.Fatalf("closing is not recorded, or counts as activity: %+v", closed)
	}

	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionID: "B"}}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if again := only(t, h); again.Status != StatusAwaitingUser || again.Closed {
		t.Fatalf("a new question did not open the bubble again: %+v", again)
	}
	if err := h.svc.Close("missing", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("closing an unknown request: %v", err)
	}
}

func TestDecliningAChangeEndsTheConversation(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": ProposesChange, "proposal": map[string]any{"title": "Fix", "description": "d", "projectId": "login-app"}}))
	h.receive(message("Can you fix the login?"))
	request := only(t, h)
	if err := h.svc.Decide(request.ID, false); err != nil {
		t.Fatal(err)
	}
	if declined := only(t, h); declined.Status != StatusReady || !declined.Closed {
		t.Fatalf("a declined change still asks for attention: %+v", declined)
	}
}

func TestAQuestionCanAllowSeveralOptions(t *testing.T) {
	h := newHarness(t, needsInput(true), reply(map[string]any{"outcome": Answered}))
	h.receive(message("Can you check the retests?"))
	request := only(t, h)
	if !request.Questions[0].Multiple {
		t.Fatalf("the question lost its multiple choice: %+v", request.Questions)
	}

	invalid := map[string][]Answer{
		"an unknown option":        {{QuestionID: "q1", OptionIDs: []string{"A", "Z"}}},
		"the same option twice":    {{QuestionID: "q1", OptionIDs: []string{"A", "A"}}},
		"nothing picked":           {{QuestionID: "q1", OptionIDs: []string{}}},
		"single and multiple same": {{QuestionID: "q1", OptionID: "A", OptionIDs: []string{"A"}}},
	}
	for name, answers := range invalid {
		if err := h.svc.Answer(request.ID, answers); !errors.Is(err, ErrInput) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}

	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionIDs: []string{"C", "A"}, Text: "and tell Florian"}}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	entries := only(t, h).Entries
	var said string
	for _, entry := range entries {
		if entry.Role == "user" {
			said = entry.Text
		}
	}
	if said != "Reply to Estelle, Open a merge request, and tell Florian" {
		t.Fatalf("the answer reads %q", said)
	}
	prompt := h.codex.turns[len(h.codex.turns)-1].Prompt
	if want := "Reply to Estelle, Open a merge request, and tell Florian"; !strings.Contains(prompt, want) {
		t.Fatalf("the AI did not get the picked options: %s", prompt)
	}
}

func TestASingleChoiceQuestionRefusesSeveralOptions(t *testing.T) {
	h := newHarness(t, needsInput(false))
	h.receive(message("Can you check the retests?"))
	request := only(t, h)
	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionIDs: []string{"A", "B"}}}); !errors.Is(err, ErrInput) {
		t.Fatalf("two options on a single choice: %v", err)
	}
	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", Text: "  something else  "}}); err != nil {
		t.Fatalf("a typed answer was refused: %v", err)
	}
	// The answer starts a turn in the background: wait for it, or the temp folder cleanup races its writes.
	h.svc.Idle()
}

func TestClosingWhatTheUserNoLongerSeesLeavesTheNewStateOpen(t *testing.T) {
	h := newHarness(t, needsInput(false), needsInput(false))
	h.receive(message("Can you check the retests?"))
	seen := only(t, h).UpdatedAt

	h.now = h.now.Add(time.Minute)
	if err := h.svc.Answer(only(t, h).ID, []Answer{{QuestionID: "q1", OptionID: "A"}}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	// The user closes the bubble they saw, while a new question has come in meanwhile.
	if err := h.svc.Close(only(t, h).ID, seen); !errors.Is(err, ErrState) {
		t.Fatalf("an older state closed the new one: %v", err)
	}
	if only(t, h).Closed {
		t.Fatal("the new question was hidden")
	}
	if err := h.svc.Close(only(t, h).ID, only(t, h).UpdatedAt); err != nil || !only(t, h).Closed {
		t.Fatalf("closing what the user sees: %v", err)
	}
}

func TestStoppingTheWorkClosesTheBubble(t *testing.T) {
	started := make(chan struct{})
	h := newHarness(t, needsInput(false), func(ctx context.Context, _ Turn) (TurnResult, error) {
		close(started)
		<-ctx.Done()
		return TurnResult{}, ErrCancelled
	})
	h.receive(message("Can you check the retests?"))
	request := only(t, h)
	if err := h.svc.Answer(request.ID, []Answer{{QuestionID: "q1", OptionID: "A"}}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := h.svc.Stop(request.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if stopped := only(t, h); stopped.Status != StatusReady || !stopped.Closed {
		t.Fatalf("stopping reopened an answer bubble: %+v", stopped)
	}
}

func TestAChangeApprovedOnItsOwnStaysSilentUntilItIsMade(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := newHarness(t, reply(loginProposal), func(ctx context.Context, turn Turn) (TurnResult, error) {
		close(started)
		<-release
		return reply(map[string]any{"summary": "done"})(ctx, turn)
	})
	h.svc.deps.AutoApproveOwnNotes = func() bool { return true }
	h.svc.Poll(context.Background())
	h.inbox.deliver(message("fix login", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
	h.svc.Poll(context.Background())
	<-started
	if working := only(t, h); working.Status != StatusApplying || working.Engaged {
		t.Fatalf("a change nobody was asked about shows its work: %+v", working)
	}
	close(release)
	h.svc.Idle()
	if done := only(t, h); done.Status != StatusDone || !done.Engaged || done.Closed {
		t.Fatalf("the change made is not confirmed: %+v", done)
	}
}

func TestAnsweringAQuestionAskedBeforeEngagementEngagesTheRequest(t *testing.T) {
	h := newHarness(t, reply(map[string]any{"outcome": Answered}))
	h.svc.Poll(context.Background())
	h.svc.deps.Store.Add(Stored{Request: Request{
		ID: "legacy", Source: message("hi").Source, Status: StatusAwaitingUser, ReceivedAt: h.svc.now(), UpdatedAt: h.svc.now(),
		Questions: []Question{{ID: "q1", Text: "Which?", Options: []Option{{ID: "A", Label: "This one"}}}}, Entries: []Entry{},
	}})
	if err := h.svc.Answer("legacy", []Answer{{QuestionID: "q1", OptionID: "A"}}); err != nil {
		t.Fatal(err)
	}
	if request := only(t, h); !request.Engaged {
		t.Fatalf("the work after the answer is silent: %+v", request)
	}
	h.svc.Idle()
}
