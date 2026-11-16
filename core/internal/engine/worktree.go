package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// Code that Pouple writes is written in a git worktree of its own, on a branch of its own: the user's checkout, their
// branch and their unsaved files are never touched. The worktree holds the project as of its last commit; what the AI
// writes there is committed by Pouple, so the user reads it as one reviewable commit.

// remoteTimeout bounds one git command that talks to a remote.
const remoteTimeout = 3 * time.Minute

// runGit runs git in dir without anything that could run code of the repository (hooks, the filesystem monitor), never
// asking for a password, and returns what it printed. Nothing of the caller's GIT_* environment reaches it: a variable
// such as GIT_DIR or GIT_EXTERNAL_DIFF would change what git runs.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	if talksToRemote(args) {
		// A remote that does not answer must not hold the conversation forever.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, remoteTimeout)
		defer cancel()
	}
	full := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "core.editor=true", "-C", dir}, args...)
	command := exec.CommandContext(ctx, "git", full...)
	var environment []string
	for _, entry := range providers.ChildEnv(os.Environ()) {
		if !strings.HasPrefix(entry, "GIT_") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0")
	// Talking to a remote needs the machine's own git configuration (its credential helper), which no repository can
	// change; everything else ignores it so that it runs the same everywhere.
	if !talksToRemote(args) {
		environment = append(environment, "GIT_CONFIG_NOSYSTEM=1")
	}
	command.Env = environment
	if talksToRemote(args) {
		// git's children (ssh, the credential helper) can outlive it and hold its output open: stop waiting for them.
		command.WaitDelay = 5 * time.Second
	}
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// talksToRemote says whether a git command (its first word that is not an option) reaches another computer.
func talksToRemote(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg == "fetch" || arg == "push" || arg == "ls-remote"
	}
	return false
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
	// start is the commit the work started from.
	start string
}

// pointerIntact says whether the `.git` file of the worktree is still what git wrote.
func (w Worktree) pointerIntact() bool {
	content, err := os.ReadFile(filepath.Join(w.Path, ".git"))
	return err == nil && string(content) == w.gitFile
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

// hasOrigin says whether the project has the remote its branches are pushed to.
func (s *Service) hasOrigin(ctx context.Context, directory string) bool {
	_, err := s.git(ctx, directory, "remote", "get-url", origin)
	return err == nil
}

// prepareWorktree makes the place the code of an action is written in: a worktree on the new branch the plan names,
// started from the branch it names (the project's current one by default), as it is on the remote when there is one.
func (s *Service) prepareWorktree(ctx context.Context, plan Plan, action Action) (Worktree, error) {
	project := action.Scope.ID
	directory := s.projectPath(project)
	if directory == "" {
		return Worktree{}, i18n.NewError("project “%s” no longer exists", project)
	}
	if !s.canWriteCode(ctx, project) {
		return Worktree{}, i18n.NewError("“%s” is not a git repository with a commit, so its code cannot be written on a branch", project)
	}
	start := "HEAD"
	if s.hasOrigin(ctx, directory) {
		// What is on the remote now: the base to start from, and the names that are already taken.
		if out, err := s.git(ctx, directory, "fetch", "--quiet", origin); err != nil {
			return Worktree{}, i18n.NewError("Nothing could be fetched from %s: %s", origin, clipRunes(out, 300))
		}
	}
	if action.Base != "" {
		if start = firstNonEmpty(s.commitOf(ctx, directory, "refs/remotes/"+origin+"/"+action.Base), s.commitOf(ctx, directory, "refs/heads/"+action.Base)); start == "" {
			return Worktree{}, i18n.NewError("The branch %s exists neither in “%s” nor on %s", action.Base, project, origin)
		}
	}
	branch, path := s.freeSpot(ctx, directory, project, action.Branch, PlanFingerprint(plan)[:6])
	return s.addWorktree(ctx, directory, project, branch, path, start)
}

// freeSpot picks the branch and the folder of a worktree: a name that is not taken, here or on the remote, so that the
// work never lands on a branch that exists, and a folder of its own for each plan.
func (s *Service) freeSpot(ctx context.Context, directory, project, wanted, short string) (branch, path string) {
	root := s.deps.WorktreesRoot()
	branch, path = wanted, filepath.Join(root, project+"-"+short)
	for n := 2; pathExists(path) || s.branchTaken(ctx, directory, branch); n++ {
		branch = wanted + "-" + strconv.Itoa(n)
		path = filepath.Join(root, project+"-"+short+"-"+strconv.Itoa(n))
	}
	return branch, path
}

// freeFolder picks the folder of a worktree that needs no branch of its own.
func (s *Service) freeFolder(project, short string) string {
	root := s.deps.WorktreesRoot()
	path := filepath.Join(root, project+"-"+short)
	for n := 2; pathExists(path); n++ {
		path = filepath.Join(root, project+"-"+short+"-"+strconv.Itoa(n))
	}
	return path
}

func (s *Service) branchTaken(ctx context.Context, directory, branch string) bool {
	return s.branchExists(ctx, directory, branch) || s.commitOf(ctx, directory, "refs/remotes/"+origin+"/"+branch) != ""
}

// addWorktree makes a worktree of a project at path, on a new branch that starts at start; with no branch it is detached
// at start, so that nothing is named after it.
func (s *Service) addWorktree(ctx context.Context, directory, project, branch, path, start string) (Worktree, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Worktree{}, err
	}
	add := []string{"worktree", "add", "-q", "--detach", path, start}
	if branch != "" {
		add = []string{"worktree", "add", "-q", "-b", branch, path, start}
	}
	if out, err := s.git(ctx, directory, add...); err != nil {
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
	// Where the work starts: what the AI commits itself (it may) counts as written too.
	w.start, _ = s.git(ctx, path, w.gitArgs("rev-parse", "HEAD")...)
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
	if !w.pointerIntact() {
		return "", i18n.NewError("The work area was changed in a way Pouple does not commit from")
	}
	changes, err := s.git(ctx, w.Path, w.gitArgs(append([]string{"status", "--porcelain"}, withoutDependencies...)...)...)
	if err != nil {
		return "", i18n.NewError("What was written could not be read: %s", clipRunes(changes, 300))
	}
	head, _ := s.git(ctx, w.Path, w.gitArgs("rev-parse", "HEAD")...)
	if changes == "" && head == w.start {
		s.discardWorktree(ctx, w)
		return "", nil
	}
	if changes != "" {
		if out, err := s.git(ctx, w.Path, w.gitArgs(append([]string{"add", "-A"}, withoutDependencies...)...)...); err != nil {
			return "", i18n.NewError("What was written could not be prepared for a commit: %s", clipRunes(out, 300))
		}
		message := clipRunes(oneLine(goal, 72), 72) + "\n\nWritten by Pouple from a plan the user approved."
		if out, err := s.git(ctx, w.Path, w.gitArgs("commit", "-q", "-m", message)...); err != nil {
			return "", i18n.NewError("The commit could not be made: %s", clipRunes(out, 300))
		}
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
	// Cleaning up is wanted most when the work was stopped: the stop must not stop it too.
	ctx = context.WithoutCancel(ctx)
	_, _ = s.git(ctx, directory, "worktree", "remove", "--force", w.Path)
	if w.Branch != "" {
		_, _ = s.git(ctx, directory, "branch", "-D", w.Branch)
	}
}
