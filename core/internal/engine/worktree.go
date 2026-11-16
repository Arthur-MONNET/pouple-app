package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// Code that Pouple writes is written in a git worktree of its own, on a branch of its own: the user's checkout, their
// branch and their unsaved files are never touched. The worktree holds the project as of its last commit; what the AI
// writes there is committed by Pouple, so the user reads it as one reviewable commit.

// runGit runs git in dir without anything that could run code of the repository (hooks, the filesystem monitor), never
// asking for a password, and returns what it printed. Nothing of the caller's GIT_* environment reaches it: a variable
// such as GIT_DIR or GIT_EXTERNAL_DIFF would change what git runs.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "core.editor=true", "-C", dir}, args...)
	command := exec.CommandContext(ctx, "git", full...)
	var environment []string
	for _, entry := range providers.ChildEnv(os.Environ()) {
		if !strings.HasPrefix(entry, "GIT_") {
			environment = append(environment, entry)
		}
	}
	command.Env = append(environment, "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (s *Service) git(ctx context.Context, dir string, args ...string) (string, error) {
	if s.deps.Git != nil {
		return s.deps.Git(ctx, dir, args...)
	}
	return runGit(ctx, dir, args...)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// branchSlug turns a goal into the words of a branch name.
func branchSlug(goal string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(goal), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "change"
	}
	return slug
}

// Worktree is where the code of a plan is written.
type Worktree struct {
	// Path is the folder the AI works in; Branch is the branch Pouple commits to; Project is the project folder it
	// belongs to.
	Path, Branch, Project string
	// gitDir is the worktree's git folder, taken from what git created, and gitFile the content of the worktree's `.git`
	// file at that time. The AI that writes can edit `.git` (it is a file inside its folder): Pouple never reads the git
	// folder from it, and refuses to commit if it changed.
	gitDir, gitFile string
}

// gitArgs addresses the worktree through its own git folder, not through the `.git` file the AI could have rewritten.
func (w Worktree) gitArgs(args ...string) []string {
	return append([]string{"--git-dir=" + w.gitDir, "--work-tree=" + w.Path}, args...)
}

// canWriteCode says whether a project can have its code written on a branch: a git repository with a commit, and a place
// for the worktrees. Anything else is not written, never written in place.
func (s *Service) canWriteCode(ctx context.Context, project string) bool {
	directory := s.projectPath(project)
	if directory == "" || s.deps.WorktreesRoot == nil || s.deps.WorktreesRoot() == "" {
		return false
	}
	_, err := s.git(ctx, directory, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// prepareWorktree makes the place the code of a plan is written in: a worktree on a new branch.
func (s *Service) prepareWorktree(ctx context.Context, plan Plan, project string) (Worktree, error) {
	directory := s.projectPath(project)
	if directory == "" {
		return Worktree{}, i18n.NewError("project “%s” no longer exists", project)
	}
	if !s.canWriteCode(ctx, project) {
		return Worktree{}, i18n.NewError("“%s” is not a git repository with a commit, so its code cannot be written on a branch", project)
	}
	root := s.deps.WorktreesRoot()
	// Each plan has its own branch and folder, whatever else was written in this conversation or before.
	short := PlanFingerprint(plan)[:6]
	branch := "pouple/" + branchSlug(plan.Goal) + "-" + short
	path := filepath.Join(root, project+"-"+short)
	for n := 2; pathExists(path) || s.branchExists(ctx, directory, branch); n++ {
		branch = "pouple/" + branchSlug(plan.Goal) + "-" + short + "-" + strconv.Itoa(n)
		path = filepath.Join(root, project+"-"+short+"-"+strconv.Itoa(n))
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Worktree{}, err
	}
	if out, err := s.git(ctx, directory, "worktree", "add", "-q", "-b", branch, path, "HEAD"); err != nil {
		return Worktree{}, i18n.NewError("The branch for this work could not be created: %s", clipRunes(out, 300))
	}
	w := Worktree{Path: path, Branch: branch, Project: project}
	// What git just wrote in the new `.git` file is where its git folder is; it is read now, before the AI can touch it.
	content, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil || !strings.HasPrefix(string(content), "gitdir: ") {
		s.discardWorktree(ctx, w)
		return Worktree{}, i18n.NewError("The branch for this work could not be created: %s", "no git folder")
	}
	w.gitFile = string(content)
	w.gitDir = strings.TrimSpace(strings.TrimPrefix(w.gitFile, "gitdir: "))
	// The project's dependencies are read from the project itself: a link, never committed (see commitWorktree) and never
	// written through (the AI's sandbox forbids writing outside its folder).
	if info, err := os.Stat(filepath.Join(directory, "node_modules")); err == nil && info.IsDir() {
		_ = os.Symlink(filepath.Join(directory, "node_modules"), filepath.Join(path, "node_modules"))
	}
	return w, nil
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (s *Service) branchExists(ctx context.Context, directory, branch string) bool {
	_, err := s.git(ctx, directory, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// withoutDependencies leaves the link to the project's node_modules out of what is looked at and committed.
var withoutDependencies = []string{"--", ".", ":(exclude)node_modules"}

// commitWorktree commits what was written and returns a line saying what was committed; "" when nothing changed, in
// which case the worktree and its branch are removed.
func (s *Service) commitWorktree(ctx context.Context, w Worktree, goal string) (string, error) {
	// The AI wrote in this folder: if it changed what points to the git folder, nothing is committed from it.
	if content, err := os.ReadFile(filepath.Join(w.Path, ".git")); err != nil || string(content) != w.gitFile {
		return "", i18n.NewError("The work area was changed in a way Pouple does not commit from")
	}
	changes, err := s.git(ctx, w.Path, w.gitArgs(append([]string{"status", "--porcelain"}, withoutDependencies...)...)...)
	if err != nil {
		return "", i18n.NewError("What was written could not be read: %s", clipRunes(changes, 300))
	}
	if changes == "" {
		s.discardWorktree(ctx, w)
		return "", nil
	}
	if out, err := s.git(ctx, w.Path, w.gitArgs(append([]string{"add", "-A"}, withoutDependencies...)...)...); err != nil {
		return "", i18n.NewError("What was written could not be prepared for a commit: %s", clipRunes(out, 300))
	}
	message := "pouple: " + clipRunes(oneLine(goal, 72), 72) + "\n\nWritten by Pouple from a plan the user approved."
	if out, err := s.git(ctx, w.Path, w.gitArgs("commit", "-q", "-m", message)...); err != nil {
		return "", i18n.NewError("The commit could not be made: %s", clipRunes(out, 300))
	}
	summary, _ := s.git(ctx, w.Path, w.gitArgs("show", "--stat", "--format=%h %s", "HEAD")...)
	return summary, nil
}

// discardWorktree removes a worktree and its branch, when nothing was written in it.
func (s *Service) discardWorktree(ctx context.Context, w Worktree) {
	directory := s.projectPath(w.Project)
	if directory == "" {
		return
	}
	_, _ = s.git(ctx, directory, "worktree", "remove", "--force", w.Path)
	_, _ = s.git(ctx, directory, "branch", "-D", w.Branch)
}
