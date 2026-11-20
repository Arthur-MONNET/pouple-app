package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
)

func slitePage(parent, title, markdown string) Action {
	return Action{
		Kind: WriteNote, Scope: Scope{Service: ScopeSlite, ID: parent, Name: "Documentation"},
		Summary: "Publish in Slite", Title: title, Markdown: markdown,
	}
}

func slitePlanAnswer(actions ...Action) map[string]any {
	return map[string]any{
		"outcome": ProposesPlan, "summary": "**Publish in Slite**",
		"plan": Plan{Goal: "Publish in Slite", Actions: actions},
	}
}

func TestSlitePublicationRequiresApprovalOfTheExactPages(t *testing.T) {
	first := slitePage("parent-1", "First page", "\n# First\n\n    Indented text\n")
	second := slitePage("parent-2", "Second page", "Two trailing spaces  \n")
	third := slitePage("parent-3", "Third page", "# Third")
	fourth := slitePage("parent-4", "Unselected page", "# Unselected")
	h := newHarness(t, reply(slitePlanAnswer(first, second, third, fourth)), reply(slitePlanAnswer(first)))
	h.svc.deps.AutoApproveAll = func() bool { return true }
	var calls []Action
	h.svc.deps.CreateSlitePage = func(_ context.Context, parentID, title, markdown string) (string, error) {
		calls = append(calls, slitePage(parentID, title, markdown))
		if title == second.Title {
			return "", slite.ErrCreationUnconfirmed
		}
		return "https://team.slite.com/api/s/" + parentID, nil
	}
	h.receive(message("Publish the pages in Slite", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
	got := waitingPlan(t, h)
	if len(calls) != 0 || !slices.Equal(got.Plan.Actions, []Action{first, second, third, fourth}) {
		t.Fatalf("before approval: calls=%+v plan=%+v", calls, got.Plan)
	}
	for _, change := range []func(*Action){
		func(a *Action) { a.Title += " changed" },
		func(a *Action) { a.Markdown += "\n" },
		func(a *Action) { a.Scope.ID = "another-parent" },
	} {
		stale := *got.Plan
		stale.Actions = slices.Clone(stale.Actions)
		change(&stale.Actions[0])
		if err := h.svc.DecidePlan(got.ID, true, PlanFingerprint(stale)); err == nil {
			t.Fatal("the approval matched a different page")
		}
	}
	if err := h.svc.DecidePlanActions(got.ID, true, got.Plan.ID, []int{0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()

	final := only(t, h)
	journal := journalOf(final)
	if !slices.Equal(calls, []Action{first, second, third}) ||
		!strings.Contains(journal, "[First page](https://team.slite.com/api/s/parent-1)") ||
		!strings.Contains(journal, "creation of “Second page” is unconfirmed") ||
		!strings.Contains(journal, "[Third page](https://team.slite.com/api/s/parent-3)") ||
		final.Summary == nil || *final.Summary != "Slite pages created: 2 of 3" ||
		len(final.Grants) != 0 {
		t.Fatalf("calls=%+v request=%+v journal=%s", calls, final, journal)
	}
	if err := h.svc.Message(got.ID, "Publish one more page in Slite", nil); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	waitingPlan(t, h)
	if len(calls) != 3 {
		t.Fatalf("the first approval covered a later plan: %+v", calls)
	}
}

func TestSlitePublicationRejectsForeignRequestsAndMissingParents(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin string
		parent string
	}{
		{"foreign request", "slack", "valid-parent"},
		{"empty parent", "note-to-self", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, reply(slitePlanAnswer(slitePage(tc.parent, "Page", "Content"))))
			h.svc.deps.AutoApproveAll = func() bool { return true }
			var calls int
			h.svc.deps.CreateSlitePage = func(context.Context, string, string, string) (string, error) {
				calls++
				return "", errors.New("unexpected write")
			}
			h.receive(message("Publish in Slite", func(in *Incoming) { in.Source.Origin = tc.origin }))
			got := only(t, h)
			if got.Status != StatusReady || got.Plan != nil || calls != 0 {
				t.Fatalf("publication should be refused: calls=%d request=%+v", calls, got)
			}
		})
	}
}
