package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/machine"
)

func TestMRFailureKeepsPushAndCanResumeOnlyMR(t *testing.T) {
	proposed := samplePlan()
	actions := proposed["plan"].(map[string]any)["actions"].([]any)
	proposed["plan"].(map[string]any)["actions"] = append(actions, map[string]any{
		"kind": "gitlab.merge-request", "scope": map[string]any{"service": "gitlab", "id": "acme/app"},
		"summary": "Open MR", "branch": "fix/login-redirect", "base": "main",
	})
	h, project, _ := planHarness(t, reply(proposed), writes("redirect.txt", "fixed\n"))
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, project, "init", "--bare", remote)
	gitIn(t, project, "remote", "add", "origin", remote)
	gitIn(t, project, "push", "origin", "main")
	h.svc.deps.Machine = func() machine.State {
		return machine.State{Disk: &machine.Disk{FreeBytes: 1 << 30, Level: machine.Critical}}
	}
	calls := 0
	h.svc.deps.OpenMergeRequest = func(context.Context, Action, string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("GitLab HTTP 403: access refused")
		}
		return "https://gitlab.example.com/mr/1", nil
	}
	h.receive(message("fix redirect and open a MR"))
	got := waitingPlan(t, h)
	if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	final := h.svc.Snapshot().Requests[0]
	if final.Error == nil || !strings.Contains(*final.Error, "Branch fix/login-redirect pushed") || strings.Contains(*final.Error, "disk") {
		t.Fatalf("partial result: %+v", final.Error)
	}
	if !strings.Contains(journalOf(final), "Branch fix/login-redirect pushed") {
		t.Fatal("push result missing")
	}
	before := gitIn(t, remote, "rev-parse", "refs/heads/fix/login-redirect")
	retry := *got.Plan
	retry.Status = PlanApproved
	retry.Actions = retry.Actions[len(retry.Actions)-1:]
	// A standalone MR action uses the pushed branch without running the code provider again.
	if err := h.svc.executePlan(context.Background(), got.ID, retry); err != nil {
		t.Fatal(err)
	}
	after := gitIn(t, remote, "rev-parse", "refs/heads/fix/login-redirect")
	if calls != 2 || before != after {
		t.Fatal("retry changed the pushed branch")
	}
}
