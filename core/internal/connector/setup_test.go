package connector

import "testing"

func status(id, name string, required bool, choice string, connected bool) Status {
	return Status{Descriptor: Descriptor{ID: id, Name: name, Required: required, Choice: choice}, Connected: connected}
}

func TestSetupNeedsSlackAndOneAI(t *testing.T) {
	slack := func(connected bool) Status { return status("slack", "Slack", true, "", connected) }
	codex := func(connected bool) Status { return status("codex", "Codex", false, "ai", connected) }
	claude := func(connected bool) Status { return status("claude", "Claude", false, "ai", connected) }
	other := status("gitlab", "GitLab", false, "", false)

	cases := []struct {
		name     string
		statuses []Status
		complete bool
		gaps     [][]string
	}{
		{"nothing", []Status{slack(false), codex(false), claude(false), other}, false, [][]string{{"Slack"}, {"Codex", "Claude"}}},
		{"Slack only", []Status{slack(true), codex(false), claude(false), other}, false, [][]string{{"Codex", "Claude"}}},
		{"an AI only", []Status{slack(false), codex(false), claude(true), other}, false, [][]string{{"Slack"}}},
		{"Slack and Codex", []Status{slack(true), codex(true), claude(false), other}, true, nil},
		{"Slack and Claude", []Status{slack(true), codex(false), claude(true), other}, true, nil},
		{"Slack and both", []Status{slack(true), codex(true), claude(true), other}, true, nil},
	}
	for _, c := range cases {
		gaps := SetupGaps(c.statuses)
		if SetupComplete(c.statuses) != c.complete || len(gaps) != len(c.gaps) {
			t.Errorf("%s: complete=%v gaps=%v", c.name, SetupComplete(c.statuses), gaps)
			continue
		}
		for i, gap := range gaps {
			if len(gap.Names) != len(c.gaps[i]) {
				t.Errorf("%s: gap %d is %v, want %v", c.name, i, gap.Names, c.gaps[i])
				continue
			}
			for j, name := range gap.Names {
				if name != c.gaps[i][j] {
					t.Errorf("%s: gap %d is %v, want %v", c.name, i, gap.Names, c.gaps[i])
				}
			}
		}
	}
}
