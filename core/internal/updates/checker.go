// Package updates tells whether a newer Pouple can be installed from the source checkout the running build came
// from. The installer leaves a record (install.json) of where that checkout lives; the Checker validates it,
// fetches origin/main with a hardened git, and reports how far behind the checkout is and what would change. It
// only reads: the update itself is run by the macOS shell in a Terminal.
package updates

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	checkTimeout   = 30 * time.Second
	minFetchGap    = 30 * time.Second
	initialDelay   = 30 * time.Second
	checkInterval  = 6 * time.Hour
	maxChanges     = 15
	maxSubjectRune = 120
)

// allowedOrigins are the only remotes the updater will fetch from.
var allowedOrigins = map[string]bool{
	"https://github.com/Arthur-MONNET/pouple-app":     true,
	"https://github.com/Arthur-MONNET/pouple-app.git": true,
	"git@github.com:Arthur-MONNET/pouple-app":         true,
	"git@github.com:Arthur-MONNET/pouple-app.git":     true,
}

// gitPrefix hardens every invocation: no fsmonitor or hook program configured in the checkout can run, and git
// does not take optional locks that would fight with the user's own git commands.
var gitPrefix = []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks"}

// gitEnv is the environment every command requires: never prompt, never read the system-wide configuration.
var gitEnv = []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}

// Options configures a Checker; only DataDir is required.
type Options struct {
	// DataDir is the folder that holds install.json (the folder of config.json).
	DataDir string
	// Git runs git; nil uses the real one.
	Git Runner
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
	// InitialDelay and Interval schedule Run; zero means 30 s and 6 h.
	InitialDelay, Interval time.Duration
	// Jitter returns the extra wait added to Interval; nil adds up to 10 %.
	Jitter func(interval time.Duration) time.Duration
}

// Checker keeps the latest result and refreshes it. It is safe for concurrent use.
type Checker struct {
	dir                    string
	git                    Runner
	now                    func() time.Time
	initialDelay, interval time.Duration
	jitter                 func(time.Duration) time.Duration
	timeout                time.Duration

	mu          sync.Mutex
	result      Result
	have        bool
	lastAttempt time.Time
	attempted   bool
	inflight    chan struct{}
}

// New builds a Checker.
func New(opts Options) *Checker {
	c := &Checker{
		dir: opts.DataDir, git: opts.Git, now: opts.Now,
		initialDelay: opts.InitialDelay, interval: opts.Interval, jitter: opts.Jitter, timeout: checkTimeout,
	}
	if c.git == nil {
		c.git = ExecRunner
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.initialDelay <= 0 {
		c.initialDelay = initialDelay
	}
	if c.interval <= 0 {
		c.interval = checkInterval
	}
	if c.jitter == nil {
		c.jitter = func(interval time.Duration) time.Duration {
			return time.Duration(rand.Int64N(int64(interval / 10)))
		}
	}
	return c
}

// Run checks once after the initial delay and then every interval (plus jitter) until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	wait := c.initialDelay
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		c.Check(ctx)
		wait = c.interval + c.jitter(c.interval)
	}
}

// Current returns the last result without touching the network. Before any check it only validates the install
// record and the checkout, so the interface can tell at once whether updates are supported.
func (c *Checker) Current(ctx context.Context) Result {
	c.mu.Lock()
	if c.have {
		defer c.mu.Unlock()
		return c.snapshot()
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	inspected, _ := c.inspect(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.have && ctx.Err() == nil && inspected.failure == failureNone {
		c.result, c.have = inspected, true
	}
	if !c.have {
		return inspected
	}
	return c.snapshot()
}

func (c *Checker) snapshot() Result {
	r := c.result
	r.Checking = c.inflight != nil
	return r
}

// Check fetches origin/main and refreshes the result. It does at most one real fetch per 30 seconds: a call that
// comes sooner returns the cached result, and a call made while a check runs waits for that check.
func (c *Checker) Check(ctx context.Context) Result {
	c.mu.Lock()
	if running := c.inflight; running != nil {
		c.mu.Unlock()
		select {
		case <-running:
		case <-ctx.Done():
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.snapshot()
	}
	if c.attempted && c.now().Sub(c.lastAttempt) < minFetchGap && c.have {
		defer c.mu.Unlock()
		return c.snapshot()
	}
	previousAttempt, previouslyAttempted := c.lastAttempt, c.attempted
	c.inflight = make(chan struct{})
	c.lastAttempt, c.attempted = c.now(), true
	previous, havePrevious := c.result, c.have
	c.mu.Unlock()

	checkContext, cancel := context.WithTimeout(ctx, c.timeout)
	result := c.check(checkContext, previous, havePrevious)
	cancelled := ctx.Err() != nil
	cancel()

	c.mu.Lock()
	defer c.mu.Unlock()
	if cancelled {
		// The caller went away (the app is quitting, the request was dropped): learn nothing from it.
		c.lastAttempt, c.attempted = previousAttempt, previouslyAttempted
	} else {
		c.result, c.have = result, true
	}
	close(c.inflight)
	c.inflight = nil
	return c.snapshot()
}

// check validates the checkout, fetches and measures. previous is kept, with an error, when the fetch fails.
func (c *Checker) check(ctx context.Context, previous Result, havePrevious bool) Result {
	base, root := c.inspect(ctx)
	if !base.Supported {
		return base
	}
	problem := base.failure
	base.failure = failureNone
	fail := func(kind failure) Result {
		if havePrevious && previous.Supported {
			previous.Installed = base.Installed
			previous.failure = kind
			return previous
		}
		base.failure = kind
		return base
	}
	if problem != failureNone {
		return fail(problem)
	}
	if _, err := c.run(ctx, root, "fetch", "--quiet", "origin", "main"); err != nil {
		return fail(classify(ctx, err))
	}
	branch, err := c.run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fail(classify(ctx, err))
	}
	latest, err := c.commit(ctx, root, "origin/main")
	if err != nil {
		return fail(classify(ctx, err))
	}
	behind, err := c.count(ctx, root, "HEAD..origin/main")
	if err != nil {
		return fail(classify(ctx, err))
	}
	ahead, err := c.count(ctx, root, "origin/main..HEAD")
	if err != nil {
		return fail(classify(ctx, err))
	}
	var changes []string
	if behind > 0 {
		log, err := c.run(ctx, root, "log", "--format=%s", "-n", strconv.Itoa(maxChanges), "HEAD..origin/main", "--")
		if err != nil {
			return fail(classify(ctx, err))
		}
		changes = subjects(log)
	}
	dirty, err := c.run(ctx, root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fail(classify(ctx, err))
	}

	base.Latest = short(latest)
	base.Behind = behind
	base.Changes = changes
	base.CheckedAt = c.now().UTC()
	if strings.TrimSpace(dirty) != "" {
		base.Blockers = append(base.Blockers, BlockerLocalChanges)
	}
	if strings.TrimSpace(branch) != "main" {
		base.Blockers = append(base.Blockers, BlockerNotOnMain)
	}
	if ahead > 0 {
		base.Blockers = append(base.Blockers, BlockerDiverged)
	}
	return base
}

// inspect validates the install record and the checkout it names, without any network access.
func (c *Checker) inspect(ctx context.Context) (Result, string) {
	record, err := readRecord(c.dir)
	if errors.Is(err, errNoRecord) {
		return Result{Reason: ReasonNotFromSource}, ""
	}
	if err != nil {
		return Result{Reason: ReasonCheckoutBad}, ""
	}
	installed := &Installed{
		Version: record.Version, Commit: short(record.Commit),
		InstalledAt: record.InstalledAt.UTC().Format(time.RFC3339),
	}
	reason, problem := c.validateCheckout(ctx, record.SourceRoot)
	if reason != ReasonSupported {
		return Result{Reason: reason, Installed: installed}, ""
	}
	// problem is set when git could not answer (timeout, git missing): the checkout is not judged invalid.
	return Result{Supported: true, Installed: installed, Changes: []string{}, Blockers: []BlockerCode{}, failure: problem}, record.SourceRoot
}

// validateCheckout makes sure root is a real, unlinked folder holding the Pouple sources whose git repository is
// exactly that folder and whose origin is the Pouple repository. A forged install.json therefore cannot aim the
// updater at somebody else's repository.
func (c *Checker) validateCheckout(ctx context.Context, root string) (Reason, failure) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return ReasonCheckoutMissing, failureNone
	}
	if err != nil || !info.IsDir() {
		return ReasonCheckoutBad, failureNone
	}
	if resolved, err := filepath.EvalSymlinks(root); err != nil || resolved != root {
		return ReasonCheckoutBad, failureNone
	}
	for _, marker := range []string{"scripts/install-local.sh", "core/go.mod"} {
		if info, err := os.Lstat(filepath.Join(root, marker)); err != nil || !info.Mode().IsRegular() {
			return ReasonCheckoutBad, failureNone
		}
	}
	// A command that ran and said no judges the checkout; one that could not answer does not.
	judge := func(err error) (Reason, failure) {
		var failed *CommandError
		if errors.As(err, &failed) {
			return ReasonCheckoutBad, failureNone
		}
		return ReasonSupported, classify(ctx, err)
	}
	top, err := c.run(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return judge(err)
	}
	if strings.TrimSpace(top) != root {
		return ReasonCheckoutBad, failureNone
	}
	origin, err := c.run(ctx, root, "remote", "get-url", "origin")
	if err != nil {
		return judge(err)
	}
	if !allowedOrigins[strings.TrimSpace(origin)] {
		return ReasonCheckoutBad, failureNone
	}
	return ReasonSupported, failureNone
}

func (c *Checker) run(ctx context.Context, root string, args ...string) (string, error) {
	full := make([]string, 0, len(gitPrefix)+len(args))
	full = append(append(full, gitPrefix...), args...)
	return c.git(ctx, Command{Dir: root, Env: append([]string(nil), gitEnv...), Args: full})
}

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

func (c *Checker) commit(ctx context.Context, root, ref string) (string, error) {
	out, err := c.run(ctx, root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	if out = strings.TrimSpace(out); !fullCommit.MatchString(out) {
		return "", fmt.Errorf("unexpected rev-parse output")
	}
	return out, nil
}

func (c *Checker) count(ctx context.Context, root, rangeSpec string) (int, error) {
	out, err := c.run(ctx, root, "rev-list", "--count", rangeSpec, "--")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("unexpected rev-list output")
	}
	return n, nil
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// subjects turns git log output into at most maxChanges one-line, bounded subjects.
func subjects(log string) []string {
	out := []string{}
	for _, line := range strings.Split(log, "\n") {
		if line = cleanLine(line); line != "" {
			out = append(out, line)
		}
		if len(out) == maxChanges {
			break
		}
	}
	return out
}

func cleanLine(line string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range line {
		switch {
		case unicode.IsSpace(r) || unicode.IsControl(r):
			pendingSpace = b.Len() > 0
		default:
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		}
	}
	text := b.String()
	if runes := []rune(text); len(runes) > maxSubjectRune {
		text = strings.TrimSpace(string(runes[:maxSubjectRune-1])) + "…"
	}
	return text
}

// classify maps a failed command to a user-facing kind. The command's own text is only inspected, never kept.
func classify(ctx context.Context, err error) failure {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failureTimeout
	}
	var failed *CommandError
	if !errors.As(err, &failed) {
		return failureGit
	}
	text := strings.ToLower(failed.Stderr)
	for _, hint := range []string{"permission denied", "authentication failed", "could not read username", "repository not found", "publickey", "access denied"} {
		if strings.Contains(text, hint) {
			return failureAuth
		}
	}
	for _, hint := range []string{"could not resolve host", "unable to access", "network is unreachable", "connection timed out", "connection refused", "connection reset", "operation timed out", "no route to host", "could not read from remote repository", "temporary failure in name resolution", "ssh: connect to host", "could not resolve hostname"} {
		if strings.Contains(text, hint) {
			return failureOffline
		}
	}
	return failureGit
}
