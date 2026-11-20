package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Bringing a branch up to date is one action of a plan (SyncBranch) that Pouple carries out from end to end: it fetches the
// remote, merges the other branch into the branch in a worktree of its own, has the AI resolve the conflicts when there are
// some, commits, and pushes the branch (never forced). Without conflicts
// no AI is involved at all. The user's checkout is only moved forward when that cannot touch their work.

const origin = "origin"

// runBranchSync carries out one SyncBranch action. It returns a line saying what was done, or "" when the branch had
// nothing to receive (the request then stays open, saying so).
func (s *Service) runBranchSync(ctx context.Context, id string, plan Plan, action Action) (string, error) {
	project, branch, base := action.Scope.ID, action.Branch, action.Base
	directory := s.projectPath(project)
	if directory == "" {
		return "", i18n.NewError("project “%s” no longer exists", project)
	}
	if !s.canWriteCode(ctx, project) {
		return "", i18n.NewError("“%s” is not a git repository with a commit, so its code cannot be written on a branch", project)
	}
	step := func(text string) {
		s.update(id, func(r *Stored) { r.Entries = append(r.Entries, s.entry("assistant", "step", text)) })
	}

	step(s.text("Fetching what is new from %s", origin))
	if out, err := s.git(ctx, directory, "fetch", "--quiet", origin); err != nil {
		return "", i18n.NewError("Nothing could be fetched from %s: %s", origin, clipRunes(out, 300))
	}
	baseSHA := firstNonEmpty(s.commitOf(ctx, directory, "refs/remotes/"+origin+"/"+base), s.commitOf(ctx, directory, "refs/heads/"+base))
	if baseSHA == "" {
		return "", i18n.NewError("The branch %s exists neither in “%s” nor on %s", base, project, origin)
	}
	remoteSHA, localSHA := s.commitOf(ctx, directory, "refs/remotes/"+origin+"/"+branch), s.commitOf(ctx, directory, "refs/heads/"+branch)
	start, err := s.syncStart(ctx, directory, branch, remoteSHA, localSHA)
	if err != nil {
		return "", err
	}

	// The work is done on a detached head: no branch carries Pouple's name, and the branch is only ever written by the push.
	wt, err := s.addWorktree(ctx, directory, project, "", s.freeFolder(project, PlanFingerprint(plan)[:6]), start)
	if err != nil {
		return "", err
	}
	step(s.text("Merging %s into %s", base, branch))
	var conflicted []string
	if out, err := s.git(ctx, wt.Path, wt.gitArgs("merge", "--no-edit", "-m", fmt.Sprintf("Merge branch '%s' into %s", base, branch), baseSHA)...); err != nil {
		if conflicted = s.unmergedFiles(ctx, wt); len(conflicted) == 0 {
			s.discardWorktree(ctx, wt)
			return "", i18n.NewError("%s could not be merged into %s: %s", base, branch, clipRunes(out, 300))
		}
		if err := s.resolveConflicts(ctx, id, plan, action, wt, conflicted, step); err != nil {
			s.discardWorktree(ctx, wt)
			return "", err
		}
	}

	head, err := s.git(ctx, wt.Path, wt.gitArgs("rev-parse", "HEAD")...)
	if err != nil || head == "" {
		s.discardWorktree(ctx, wt)
		return "", i18n.NewError("The result of the merge could not be read: %s", clipRunes(head, 300))
	}
	if head == remoteSHA {
		s.discardWorktree(ctx, wt)
		recap := s.text("**%s already has everything from %s.**", branch, base)
		s.update(id, func(r *Stored) {
			if !plan.hasSlitePages() {
				r.Status = StatusReady
			}
			r.Summary = ptr(recap)
			r.Entries = append(r.Entries, s.entry("assistant", "text", recap), s.entry("system", "text", s.text("Nothing was changed.")))
		})
		s.alert(id, recap)
		return "", nil
	}

	step(s.text("Pushing %s to %s", branch, origin))
	if out, err := s.git(ctx, wt.Path, wt.gitArgs("push", origin, "HEAD:refs/heads/"+branch)...); err != nil {
		// The merge is done and committed: it is kept on a branch named after the two it joins, so nothing is lost.
		kept := s.keepMerge(ctx, directory, branch, base, head)
		s.discardWorktree(ctx, wt)
		return "", i18n.NewError("%s was brought up to date with %s but could not be pushed (%s). The merge is on the branch %s of “%s”", branch, base, clipRunes(out, 300), kept, project)
	}
	subject, _ := s.git(ctx, directory, "log", "-1", "--format=%h %s", head)
	note := s.moveLocalBranch(ctx, directory, branch, localSHA, head)
	s.discardWorktree(ctx, wt)

	recap := s.text("**%s is up to date with %s and pushed.**", branch, base) + "\n- " + s.text("Last commit: %s", subject)
	if len(conflicted) > 0 {
		recap += "\n- " + s.text("Conflicts resolved in: %s", strings.Join(conflicted, ", "))
	}
	if note != "" {
		recap += "\n- " + note
	}
	s.update(id, func(r *Stored) {
		r.Summary = ptr(recap)
		r.Entries = append(r.Entries, s.entry("assistant", "text", recap))
	})
	return s.text("Branch %s updated and pushed: %s", branch, plan.Goal), nil
}

// commitOf is the commit a reference points to; "" when there is none.
func (s *Service) commitOf(ctx context.Context, directory, ref string) string {
	out, err := s.git(ctx, directory, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return out
}

func (s *Service) isAncestor(ctx context.Context, directory, ancestor, descendant string) bool {
	_, err := s.git(ctx, directory, "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// syncStart is where the update of a branch starts: the branch as it is on the remote, or as it is here when it holds
// commits that were not pushed yet (they are pushed too). A branch that went two ways is not touched: choosing between
// them is the user's.
func (s *Service) syncStart(ctx context.Context, directory, branch, remote, local string) (string, error) {
	switch {
	case remote == "" && local == "":
		return "", i18n.NewError("The branch %s exists neither in the project nor on %s", branch, origin)
	case local == "":
		return remote, nil
	case remote == "" || remote == local || s.isAncestor(ctx, directory, remote, local):
		return local, nil
	case s.isAncestor(ctx, directory, local, remote):
		return remote, nil
	}
	return "", i18n.NewError("The branch %s is not the same here and on %s: each has commits the other lacks", branch, origin)
}

// unmergedFiles lists the files a merge left in conflict.
func (s *Service) unmergedFiles(ctx context.Context, w Worktree) []string {
	// NUL-separated: a name with accents or spaces comes back as it is written.
	out, err := s.git(ctx, w.Path, w.gitArgs("diff", "--name-only", "-z", "--diff-filter=U")...)
	if err != nil || out == "" {
		return nil
	}
	var files []string
	for _, name := range strings.Split(out, "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files
}

// resolveConflicts has the AI that writes settle the conflicts of a merge in the worktree, then commits the merge. It
// fails, leaving nothing behind to clean, when a conflict is left or the merge cannot be committed.
func (s *Service) resolveConflicts(ctx context.Context, id string, plan Plan, action Action, w Worktree, files []string, step func(string)) error {
	step(s.text("Resolving the conflicts in %d file(s)", len(files)))
	result, provider, err := s.runProvider(ctx, id, Turn{Prompt: s.deps.Persona().ConflictPrompt(plan, action, files, !plan.Automatic), Dir: w.Path, Sandbox: writeSandbox(!plan.Automatic), SchemaFile: s.deps.SchemaFile}, false)
	if err != nil {
		return err
	}
	if result.FinalMessage == "" {
		return i18n.NewError("%s finished without an answer", provider.Name())
	}
	if _, err := ParseOutcome(result.FinalMessage); err != nil {
		return err
	}
	if !w.pointerIntact() {
		return i18n.NewError("The work area was changed in a way Pouple does not commit from")
	}
	if out, err := s.git(ctx, w.Path, w.gitArgs(append([]string{"add", "-A"}, withoutDependencies...)...)...); err != nil {
		return i18n.NewError("What was written could not be prepared for a commit: %s", clipRunes(out, 300))
	}
	// A marker left in a file is the one thing that must never be committed.
	// Only the files that were in conflict: a line made of "=======" in a file that merged cleanly is not a marker.
	checked, _ := s.git(ctx, w.Path, w.gitArgs(append([]string{"diff", "--cached", "--check", "--"}, files...)...)...)
	if strings.Contains(checked, "leftover conflict marker") {
		return i18n.NewError("The conflicts could not be resolved in: %s", strings.Join(files, ", "))
	}
	if out, err := s.git(ctx, w.Path, w.gitArgs("commit", "-q", "--no-edit")...); err != nil {
		return i18n.NewError("The commit could not be made: %s", clipRunes(out, 300))
	}
	return nil
}

// moveLocalBranch brings the user's own copy of the branch forward to what was pushed, when that cannot touch their
// work, and says what it did when they should know. old is where their branch was ("" when they have none).
func (s *Service) moveLocalBranch(ctx context.Context, directory, branch, old, head string) string {
	if old == "" {
		return ""
	}
	where, known := s.checkedOutAt(ctx, directory, branch)
	if !known {
		return s.text("Your local branch %s was not moved: update it with git pull.", branch)
	}
	switch {
	case where == "":
		if _, err := s.git(ctx, directory, "update-ref", "refs/heads/"+branch, head, old); err != nil {
			return s.text("Your local branch %s was not moved: update it with git pull.", branch)
		}
	case sameFolder(where, directory):
		// Moving a branch that is checked out rewrites the files of their checkout: only when nothing of theirs is in it.
		if status, err := s.git(ctx, directory, "status", "--porcelain", "--untracked-files=no"); err != nil || status != "" {
			return s.text("Your local checkout of %s has changes of yours: update it with git pull when you are ready.", branch)
		}
		if _, err := s.git(ctx, directory, "merge", "--ff-only", "--quiet", head); err != nil {
			return s.text("Your local branch %s was not moved: update it with git pull.", branch)
		}
	default:
		return s.text("Your local branch %s is checked out in another folder, which was not touched.", branch)
	}
	return s.text("Your local branch %s was brought up to date.", branch)
}

// checkedOutAt is the folder where a branch is checked out; "" when it is checked out nowhere. known is false when git
// could not say, or when the project's own checkout is in the middle of something (a rebase has no branch checked out).
func (s *Service) checkedOutAt(ctx context.Context, directory, branch string) (folder string, known bool) {
	out, err := s.git(ctx, directory, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false
	}
	for _, marker := range []string{"rebase-merge", "rebase-apply"} {
		path, _ := s.git(ctx, directory, "rev-parse", "--git-path", marker)
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		if pathExists(path) {
			return "", false
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			folder = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return folder, true
		}
	}
	return "", true
}

func sameFolder(a, b string) bool {
	resolve := func(path string) string {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return real
		}
		return filepath.Clean(path)
	}
	return resolve(a) == resolve(b)
}

// keepMerge keeps a merge that could not be pushed on a branch named after what it joins ("feat/x-merge-master"), and
// returns that name.
func (s *Service) keepMerge(ctx context.Context, directory, branch, base, head string) string {
	name := branch + "-merge-" + base
	for n := 2; s.branchTaken(ctx, directory, name); n++ {
		name = fmt.Sprintf("%s-merge-%s-%d", branch, base, n)
	}
	_, _ = s.git(ctx, directory, "branch", name, head)
	return name
}
