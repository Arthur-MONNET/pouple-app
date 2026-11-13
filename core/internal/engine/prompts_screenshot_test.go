package engine

import (
	"strings"
	"testing"
)

func TestAFollowUpRemindsTheAIThatPoupleTakesScreenshots(t *testing.T) {
	prompt := Persona{Name: "Arthur"}.FollowUpPrompt("fais moi 2 captures avant apres", "", "", "")
	for _, want := range []string{`"service":"screenshot"`, "Never say that screenshots are impossible", "fais moi 2 captures avant apres"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing %q:\n%s", want, prompt)
		}
	}
}
