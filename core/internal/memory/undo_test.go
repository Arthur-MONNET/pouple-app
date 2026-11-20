package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeNote(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUndoTakesBackAWriteTheReviewMovedOnFrom(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	writeNote(t, dir, "deploys.md", "# Deploys\n\n"+
		"- Deploys go through Jenkins. (Slack #ops, 2026-01-10)\n"+
		"  - Ask Marc for access.\n"+
		"- Staging is on Heroku. (Slack #ops, 2026-02-01)\n"+
		"- Preprod resets on Mondays. (Slack #qa, 2026-04-02)\n")
	change, err := store.Remember(ctx, []Fact{
		{Note: "deploys.md", Text: "Deploys go through GitLab CI.", Replaces: "Deploys go through Jenkins."},
		{Note: "deploys.md", Remove: true, Replaces: "Staging is on Heroku."},
		{Note: "deploys.md", Text: "Releases ship on Thursdays.", Private: true},
		{Note: "deploys.md", Text: "Hotfixes skip the freeze."},
	}, learned, RememberMessage("Slack #qa"))
	if err != nil || len(change.Written) != 4 {
		t.Fatalf("change %+v, err %v", change, err)
	}
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	// After the write, the review keeps one of its facts, shares another and drops a third: a revert would conflict.
	if err := store.Keep(ctx, "deploys.md", change.Written[0].Line, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetShared(ctx, "deploys.md", change.Written[2].Line, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Drop(ctx, "deploys.md", change.Written[3].Line); err != nil {
		t.Fatal(err)
	}
	before := len(commits(t, dir))
	if err := store.Undo(ctx, change.Commit, change.Written); err != nil {
		t.Fatalf("undo: %v", err)
	}
	want := "# Deploys\n\n" +
		"- Deploys go through Jenkins. (Slack #ops, 2026-01-10)\n" +
		"  - Ask Marc for access.\n" +
		"- Preprod resets on Mondays. (Slack #qa, 2026-04-02)\n" +
		"- Staging is on Heroku. (Slack #ops, 2026-02-01)\n"
	if got := read(t, dir, "deploys.md"); got != want {
		t.Fatalf("note:\n%s\nwant:\n%s", got, want)
	}
	log := commits(t, dir)
	if len(log) != before+1 || log[0] != "Undid Remember from Slack #qa ("+change.Commit[:7]+")" {
		t.Fatalf("an undo is one commit: %q", log)
	}
}

func TestUndoRefusesAFactChangedByHandAndWritesNothing(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	change, err := store.Remember(ctx, []Fact{
		{Title: "Team", Text: "Lina owns the payment flow."},
		{Title: "Team", Text: "Marc owns the admin."},
	}, learned, RememberMessage("Slack #qa"))
	if err != nil {
		t.Fatal(err)
	}
	note := read(t, dir, "team.md")
	writeNote(t, dir, "team.md", strings.Replace(note, "Marc owns the admin.", "Marc and Paul own the admin.", 1))
	err = store.Undo(ctx, change.Commit, change.Written)
	if !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), "“Marc owns the admin.” was changed by hand in team.md") {
		t.Fatalf("err = %v, want ErrChanged naming the fact", err)
	}
	if got := read(t, dir, "team.md"); !strings.Contains(got, "Lina owns the payment flow.") || !strings.Contains(got, "Marc and Paul own the admin.") {
		t.Fatalf("a refused undo changed the note:\n%s", got)
	}
	if log := commits(t, dir); log[0] != "Changes made outside Pouple" {
		t.Fatalf("a refused undo committed: %q", log)
	}
}

func TestUndoFollowsANoteATidyUpMerged(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	if _, err := store.Remember(ctx, []Fact{{Title: "Payments", Text: "Refunds go through Stripe."}}, learned, RememberMessage("Slack #qa")); err != nil {
		t.Fatal(err)
	}
	change, err := store.Remember(ctx, []Fact{{Title: "Stripe payments", Text: "Stripe keys rotate every year."}}, learned, RememberMessage("Slack #ops"))
	if err != nil {
		t.Fatal(err)
	}
	if done, err := store.Tidy(ctx, []Merge{{From: "stripe-payments.md", Into: "payments.md"}}); err != nil || len(done) != 1 {
		t.Fatalf("tidy %+v, err %v", done, err)
	}
	if err := store.Undo(ctx, change.Commit, change.Written); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "payments.md"); strings.Contains(got, "Stripe keys") || !strings.Contains(got, "Refunds go through Stripe.") {
		t.Fatalf("the fact was not taken out of the note it was merged into:\n%s", got)
	}
}

func TestUndoOfANewNoteRemovesItAndOnlyAWriteOfFactsIsUndone(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	change, err := store.Remember(ctx, []Fact{{Title: "Payments", Text: "Refunds go through Stripe."}}, learned, RememberMessage("Slack #qa"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Undo(ctx, "not-a-commit", change.Written); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a bad id: %v", err)
	}
	if err := store.Undo(ctx, change.Commit, change.Written); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "payments.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("undoing the write of a new note leaves the note")
	}
	if index := read(t, dir, IndexFile); strings.Contains(index, "payments.md") {
		t.Fatalf("the index lists a note that is gone:\n%s", index)
	}
	head, _ := runGit(ctx, dir, "rev-parse", "HEAD")
	if err := store.Undo(ctx, head, change.Written); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a commit that is not a write of facts: %v", err)
	}
}

func TestANoteLeftWithoutFactsIsRemoved(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()
	writeNote(t, dir, "lfs.md", "# Git LFS\n\n- LFS is on for the brand assets. (Slack #ops, 2026-01-10)\n")
	writeNote(t, dir, "xp4.md", "# XP4\n\n## Setup\n\n- XP4 runs on lab. (Slack #ops, 2026-01-10)\n")
	writeNote(t, dir, "prose.md", "# Prose\n\nWritten by hand, kept.\n\n- One fact. (Slack #ops, 2026-01-10)\n")
	if err := store.Drop(ctx, "lfs.md", "- LFS is on for the brand assets. (Slack #ops, 2026-01-10)"); err != nil {
		t.Fatal(err)
	}
	change, err := store.Remember(ctx, []Fact{
		{Note: "xp4.md", Remove: true, Replaces: "XP4 runs on lab."},
		{Note: "prose.md", Remove: true, Replaces: "One fact."},
	}, learned, RememberMessage("Slack #qa"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lfs.md", "xp4.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s holds no fact any more and is still there", name)
		}
	}
	if got := read(t, dir, "prose.md"); !strings.HasPrefix(got, "# Prose\n\nWritten by hand, kept.\n") || strings.Contains(got, "One fact") {
		t.Fatalf("a note with prose is kept:\n%q", got)
	}
	if index := read(t, dir, IndexFile); strings.Contains(index, "lfs.md") || strings.Contains(index, "xp4.md") || !strings.Contains(index, "prose.md") {
		t.Fatalf("index:\n%s", index)
	}
	// Undoing the write brings the facts back, and the note that went with its last fact.
	if err := store.Undo(ctx, change.Commit, change.Written); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "xp4.md"); got != "# XP4\n\n- XP4 runs on lab. (Slack #ops, 2026-01-10)\n" {
		t.Fatalf("xp4.md:\n%q", got)
	}
	if got := read(t, dir, "prose.md"); !strings.Contains(got, "- One fact. (Slack #ops, 2026-01-10)\n") {
		t.Fatalf("prose.md:\n%q", got)
	}
	if index := read(t, dir, IndexFile); !strings.Contains(index, "(xp4.md)") {
		t.Fatalf("index:\n%s", index)
	}
}
