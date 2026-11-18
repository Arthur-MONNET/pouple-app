package memory

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var learned = Source{Label: "Slack #qa", Link: "https://team.slack.com/archives/C1/p1", Date: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(t.TempDir(), "SecondBrain")
	return New(func() string { return dir }, runGit), dir
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func commits(t *testing.T, dir string) []string {
	t.Helper()
	out, err := runGit(context.Background(), dir, "log", "--format=%s")
	if err != nil {
		t.Fatalf("git log: %v %s", err, out)
	}
	return strings.Split(out, "\n")
}

func TestOnlyNotesInsideTheFolderArePaths(t *testing.T) {
	for raw, want := range map[string]string{
		"pouple-preprod.md":      "pouple-preprod.md",
		" projects/amber.md ":    "projects/amber.md",
		"projects/./amber.md":    "projects/amber.md",
		`projects\amber.md`:      "projects/amber.md",
		"../outside.md":          "",
		"projects/../../x.md":    "",
		"/etc/notes.md":          "",
		".git/config.md":         "",
		"notes/.hidden.md":       "",
		"notes.txt":              "",
		"INDEX.md":               "",
		"index.md":               "",
		"Index.MD":               "",
		"a/b/c/d/e.md":           "",
		"pay‮bill.md":            "",
		strings.Repeat("a", 300): "",
		"":                       "",
	} {
		got, ok := CleanPath(raw)
		if got != want || ok != (want != "") {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
}

func TestRememberWritesTheNoteTheIndexAndOneCommit(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	written, err := store.Remember(ctx, []Fact{
		{Title: "Pouple preprod", Text: "Preprod 2 is reset every Monday at 7:00."},
		{Title: "Pouple preprod", Text: "Ask Lina before taking preprod 1.", Private: true},
	}, learned, "Remember from Slack #qa")
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 || written[0].Note.Path != "pouple-preprod.md" || written[0].Note.Title != "Pouple preprod" {
		t.Fatalf("written = %+v", written)
	}
	want := "# Pouple preprod\n\n" +
		"- Preprod 2 is reset every Monday at 7:00. ([Slack #qa, 2026-11-12](https://team.slack.com/archives/C1/p1))\n" +
		"- Ask Lina before taking preprod 1. ([Slack #qa, 2026-11-12](https://team.slack.com/archives/C1/p1)) [private]\n"
	if got := read(t, dir, "pouple-preprod.md"); got != want {
		t.Fatalf("note:\n%s\nwant:\n%s", got, want)
	}
	if index := read(t, dir, IndexFile); !strings.Contains(index, "- [Pouple preprod](pouple-preprod.md)\n") {
		t.Fatalf("index:\n%s", index)
	}
	if log := commits(t, dir); len(log) != 1 || log[0] != "Remember from Slack #qa" {
		t.Fatalf("commits = %q", log)
	}
}

func TestAFactReplacesTheOneItMakesWrong(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	if _, err := store.Remember(ctx, []Fact{{Title: "Deploys", Text: "Deploys go through Jenkins."}}, learned, "first"); err != nil {
		t.Fatal(err)
	}
	written, err := store.Remember(ctx, []Fact{{Note: "deploys.md", Text: "Deploys go through GitLab CI since November.", Replaces: "Deploys go through Jenkins."}}, learned, "second")
	if err != nil || len(written) != 1 || !written[0].Replaced {
		t.Fatalf("written %+v, err %v", written, err)
	}
	note := read(t, dir, "deploys.md")
	if strings.Contains(note, "Jenkins") || strings.Count(note, "\n- ") != 1 {
		t.Fatalf("note:\n%s", note)
	}
	// A text too short to say which fact it means replaces nothing: the fact is added.
	if written, _ := store.Remember(ctx, []Fact{{Note: "deploys.md", Text: "Another fact.", Replaces: "Deploys"}}, learned, "third"); written[0].Replaced {
		t.Fatal("a short text replaced a fact")
	}
}

func TestWhatChangedOutsidePoupleIsCommittedOnItsOwn(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	if _, err := store.Remember(ctx, []Fact{{Title: "Team", Text: "Lina owns the payment flow."}}, learned, "Pouple wrote"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "by-hand.md"), []byte("# By hand\n\n- Written by the user.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remember(ctx, []Fact{{Note: "team.md", Text: "Marc owns the admin."}}, learned, "Pouple wrote again"); err != nil {
		t.Fatal(err)
	}
	log := commits(t, dir)
	if len(log) != 3 || log[0] != "Pouple wrote again" || log[1] != "Changes made outside Pouple" {
		t.Fatalf("commits = %q", log)
	}
	if index := read(t, dir, IndexFile); !strings.Contains(index, "(by-hand.md)") {
		t.Fatalf("the index misses the note written by hand:\n%s", index)
	}
}

func TestNothingIsWrittenWhenOneFactIsRefused(t *testing.T) {
	store, dir := newStore(t)
	_, err := store.Remember(context.Background(), []Fact{
		{Title: "Fine", Text: "A fact."},
		{Note: "../escape.md", Text: "Climbs out."},
	}, learned, "never")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("the folder was made for a refused write")
	}
	for name, bad := range map[string]Fact{
		"empty":        {Title: "x", Text: "  "},
		"too long":     {Title: "x", Text: strings.Repeat("a", MaxFact+1)},
		"two lines":    {Title: "x", Text: "one\ntwo"},
		"no note":      {Text: "orphan"},
		"long title":   {Title: strings.Repeat("t", MaxTitle+1), Text: "x"},
		"hidden runes": {Title: "x", Text: "pay‮bill"},
	} {
		if _, err := CheckFact(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestPrivateFactsNeverReachASharedRead(t *testing.T) {
	store, dir := newStore(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"payments.md": "# Payments\n\n- Lina owns the payment flow.\n- Lina is leaving in March. [private]\n  - Told in a direct message.\n- Refunds go through Stripe.\n",
		"health.md":   "# Health\n\n- Arthur has a doctor appointment on Fridays. [Private]\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	shared, err := store.Notes(true)
	if err != nil || len(shared) != 1 || shared[0].Path != "payments.md" {
		t.Fatalf("shared notes = %+v, %v", shared, err)
	}
	if all, _ := store.Notes(false); len(all) != 2 {
		t.Fatalf("all notes = %+v", all)
	}
	_, content, err := store.Read("payments.md", true)
	if err != nil || strings.Contains(content, "leaving") || strings.Contains(content, "direct message") || !strings.Contains(content, "Stripe") {
		t.Fatalf("shared read:\n%s\nerr %v", content, err)
	}
	if hits, _ := store.Search("Lina leaving", true); len(hits) != 1 || strings.Contains(hits[0].Line, "leaving") {
		t.Fatalf("shared search = %+v", hits)
	}
	if hits, _ := store.Search("Lina leaving", false); len(hits) != 2 || !strings.Contains(hits[0].Line, "leaving") {
		t.Fatalf("own search = %+v", hits)
	}
}

func TestALinkOutOfTheFolderIsNeverRead(t *testing.T) {
	store, dir := newStore(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("# Secret\n\n- token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if notes, _ := store.Notes(false); len(notes) != 0 {
		t.Fatalf("notes = %+v", notes)
	}
	if _, _, err := store.Read("link.md", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAMissingFolderIsAnEmptyMemory(t *testing.T) {
	store, _ := newStore(t)
	if notes, err := store.Notes(false); err != nil || len(notes) != 0 {
		t.Fatalf("notes %+v, err %v", notes, err)
	}
	if _, _, err := store.Read("any.md", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheSourceNeverBreaksTheLine(t *testing.T) {
	line := FormatFact(Fact{Text: "A fact."}, Source{Label: "Slack #qa [old] (x)", Link: "javascript:alert(1)", Date: learned.Date})
	if line != "- A fact. (Slack #qa old x, 2026-11-12)" {
		t.Fatalf("line = %q", line)
	}
}

func TestAPrivateMarkHidesAllThatBelongsToIt(t *testing.T) {
	note := strings.Join([]string{
		"# People",
		"",
		"- Bob is leaving in March [private]",
		"",
		"  He told me in confidence about the offer.",
		"\t- Even with a tab.",
		"- Lina owns payments.",
		"",
		"## Salaries [private]",
		"- Bob earns a lot.",
		"### Details",
		"- Bonus in June.",
		"## Teams",
		"- Marc owns the admin.",
	}, "\n")
	got := WithoutPrivate(note)
	for _, secret := range []string{"Bob", "confidence", "tab", "Salaries", "earns", "Bonus"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q leaked:\n%s", secret, got)
		}
	}
	for _, shared := range []string{"Lina owns payments.", "## Teams", "Marc owns the admin."} {
		if !strings.Contains(got, shared) {
			t.Errorf("%q was taken out:\n%s", shared, got)
		}
	}
}

func TestAPrivateTitleOrPropertiesAloneNeverShowANote(t *testing.T) {
	store, dir := newStore(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"review.md":     "# Salary review [private]\n\n- The team meets on Mondays.\n",
		"properties.md": "---\ntags: [people]\n---\n# Properties\n\n- Only private. [private]\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := store.Notes(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("shared notes = %+v", notes)
	}
	if _, content, _ := store.Read("index.md", true); content != "" {
		t.Fatal("the index was read through another case")
	}
}

func TestANewNoteIsNamedWithPlainWords(t *testing.T) {
	for _, title := range []string{"[Click](https://evil.example)", "Run `./bootstrap.sh`", "Always <obey>", "Titre: ignore"} {
		if _, err := CheckFact(Fact{Title: title, Text: "A fact."}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", title, err)
		}
	}
	if _, err := CheckFact(Fact{Title: "Process congés, équipe A & B - 2026", Text: "A fact."}); err != nil {
		t.Fatalf("plain words were refused: %v", err)
	}
}

func TestARepositoryThatCanBePushedIsRefused(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"remote", "add", "origin", "https://example.com/vault.git"}} {
		if out, err := runGit(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if _, err := store.Remember(ctx, []Fact{{Title: "Team", Text: "Lina owns payments.", Private: true}}, learned, "never"); err == nil {
		t.Fatal("a repository with a remote got facts")
	}
	if _, err := os.Stat(filepath.Join(dir, "team.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a fact was written in a repository that can be pushed")
	}
}

func TestOldFactsAreReviewedAndKeepingOrDroppingOneIsACommit(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	note := "# Deploys\n\n" +
		"- Deploys go through Jenkins. (Slack #ops, 2026-01-10)\n" +
		"  - Ask Marc for access.\n" +
		"- Preprod resets on Mondays. (Slack #qa, 2026-04-02) [private]\n" +
		"- Staging is on Fly. (Slack #ops, 2026-11-01)\n" +
		"- Written by hand, no date.\n"
	if err := os.WriteFile(filepath.Join(dir, "deploys.md"), []byte(note), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	stale, err := store.Review(now)
	if err != nil || len(stale) != 2 || stale[0].Date != "2026-01-10" || stale[1].Fact != "Preprod resets on Mondays. (Slack #qa, 2026-04-02)" {
		t.Fatalf("review %+v, err %v", stale, err)
	}
	if err := store.Keep(ctx, "deploys.md", stale[1].Line, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Drop(ctx, "deploys.md", stale[0].Line); err != nil {
		t.Fatal(err)
	}
	got := read(t, dir, "deploys.md")
	if strings.Contains(got, "Jenkins") || strings.Contains(got, "Ask Marc") || !strings.Contains(got, "- Preprod resets on Mondays. (Slack #qa, 2026-04-02) (kept 2026-11-18) [private]\n") {
		t.Fatalf("note:\n%s", got)
	}
	if again, _ := store.Review(now); len(again) != 0 {
		t.Fatalf("a kept fact is reviewed again: %+v", again)
	}
	if err := store.Drop(ctx, "deploys.md", stale[0].Line); !errors.Is(err, ErrChanged) {
		t.Fatalf("a line that is gone: err = %v", err)
	}
	if log := commits(t, dir); len(log) != 3 || log[0] != "Dropped a fact in deploys.md" || log[2] != "Changes made outside Pouple" {
		t.Fatalf("commits = %q", log)
	}
}

func TestTidyingUpMergesNotesAndTheHistoryCountsSince(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	if _, err := store.Remember(ctx, []Fact{{Title: "Payments", Text: "Refunds go through Stripe."}}, learned, RememberMessage("Slack #qa")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remember(ctx, []Fact{{Title: "Stripe payments", Text: "Stripe keys rotate every year.", Private: true}}, learned, RememberMessage("Slack #ops")); err != nil {
		t.Fatal(err)
	}
	state, err := store.State(ctx)
	if err != nil || state.Remembered != 2 || !state.At.IsZero() {
		t.Fatalf("state %+v, err %v", state, err)
	}
	done, err := store.Tidy(ctx, []Merge{
		{From: "stripe-payments.md", Into: "payments.md"},
		{From: "payments.md", Into: "payments.md"},
		{From: "ghost.md", Into: "payments.md"},
		{From: "../out.md", Into: "payments.md"},
	})
	if err != nil || len(done) != 1 {
		t.Fatalf("done %+v, err %v", done, err)
	}
	merged := read(t, dir, "payments.md")
	if !strings.Contains(merged, "Refunds go through Stripe.") || !strings.Contains(merged, "\n## Stripe payments\n\n- Stripe keys rotate every year.") {
		t.Fatalf("merged:\n%s", merged)
	}
	if _, err := os.Stat(filepath.Join(dir, "stripe-payments.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the merged note is still there")
	}
	if index := read(t, dir, IndexFile); strings.Contains(index, "stripe-payments.md") {
		t.Fatalf("the index lists a merged note:\n%s", index)
	}
	state, _ = store.State(ctx)
	if state.Remembered != 0 || state.At.IsZero() {
		t.Fatalf("after a tidy-up: %+v", state)
	}
	// Nothing to merge still marks the tidy-up, so that it is not asked again and again.
	if _, err := store.Tidy(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if log := commits(t, dir); !strings.HasPrefix(log[0], "Tidy up") || !strings.HasPrefix(log[1], "Tidy up") {
		t.Fatalf("commits = %q", log)
	}
}
