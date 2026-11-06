package providers

import (
	"strings"
	"testing"
)

func TestOnlyKnownProvidersParseAndCodexComesFirst(t *testing.T) {
	if all := All(); len(all) != 2 || all[0] != Codex || all[1] != Claude {
		t.Fatalf("preference order: %v", all)
	}
	for value, want := range map[string]bool{"codex": true, "claude": true, "": false, "Codex": false, "gemini": false, " claude": false} {
		if _, ok := Parse(value); ok != want {
			t.Errorf("Parse(%q) = %v", value, ok)
		}
	}
	if Codex.Name() != "Codex" || Claude.Name() != "Claude" {
		t.Fatal("display names")
	}
}

func TestAModelNameIsAShortWordThatCannotBeReadAsAnOption(t *testing.T) {
	for _, valid := range []string{"", "opus", "gpt-5.2-codex", "claude-opus-5-5", "anthropic/claude:latest", "a_b.c-d", strings.Repeat("a", 80)} {
		if !ValidModel(valid) {
			t.Errorf("%q should be valid", valid)
		}
	}
	for _, invalid := range []string{"-x", "--model", "opus --x", "a b", "opus\n", "ça", "sonnet[1m]", "a;b", "$(x)", "a'b", strings.Repeat("a", 81)} {
		if ValidModel(invalid) {
			t.Errorf("%q should be refused", invalid)
		}
	}
}

func TestSuggestionsAreValidModelsAndCodexHasNone(t *testing.T) {
	if len(Suggestions(Codex)) != 0 {
		t.Fatal("Codex cannot list its models offline")
	}
	suggestions := Suggestions(Claude)
	seen := map[string]bool{}
	for _, want := range []string{"opus", "sonnet", "haiku"} {
		for _, s := range suggestions {
			seen[s.ID] = seen[s.ID] || s.ID == want
		}
		if !seen[want] {
			t.Errorf("missing the %s alias", want)
		}
	}
	for _, s := range suggestions {
		if s.ID == "" || !ValidModel(s.ID) || s.Label == "" {
			t.Errorf("bad suggestion %+v", s)
		}
	}
	if ModelNote(Codex) == "" || ModelNote(Claude) == "" {
		t.Fatal("a note explains what can be typed")
	}
}

func TestChildEnvDropsPoupleAndAnthropicVariablesAndAddsWhatIsAsked(t *testing.T) {
	got := ChildEnv([]string{
		"HOME=/Users/a", "PATH=/bin", "POUPLE_TOKEN=x", "POUPLE_PORT=1", "ANTHROPIC_API_KEY=k", "ANTHROPIC_AUTH_TOKEN=t",
		"ANTHROPIC_BASE_URL=u", "CLAUDE_CODE_OAUTH_TOKEN=o", "CLAUDE_CONFIG_DIR=/x", "CLAUDECODE=1", "CLAUDE_CODE_SIMPLE=1",
		"HTTPS_PROXY=p", "MY_CLAUDE_NOTE=kept",
	}, "CLAUDE_CONFIG_DIR=/pouple")
	want := "HOME=/Users/a,PATH=/bin,HTTPS_PROXY=p,MY_CLAUDE_NOTE=kept,CLAUDE_CONFIG_DIR=/pouple"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v", got)
	}
}
