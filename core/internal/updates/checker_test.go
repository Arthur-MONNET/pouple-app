package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

func status(r Result) Status { return r.Status(i18n.EN) }

func TestNotInstalledFromSource(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(filepath.Join(fx.dir, recordFile)); err != nil {
		t.Fatal(err)
	}
	c := fx.checker()
	for name, got := range map[string]Result{"current": c.Current(context.Background()), "check": c.Check(context.Background())} {
		s := status(got)
		if s.Supported || s.Reason != ReasonNotFromSource || s.Message == "" || s.Installed != nil || s.Available {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if n := len(fx.git.keys()); n != 0 {
		t.Errorf("git ran %d times without an install record", n)
	}
}

func TestUnusableRecordsAreInvalidNotMissing(t *testing.T) {
	fx := newFixture(t)
	writeFile(t, filepath.Join(fx.dir, recordFile), `{"sourceRoot":"relative/path","commit":"0123456789abcdef","version":"0.1.0","installedAt":"2026-11-06T10:00:00Z"}`)
	s := status(fx.checker().Check(context.Background()))
	if s.Supported || s.Reason != ReasonCheckoutBad {
		t.Errorf("%+v", s)
	}
	if len(fx.git.keys()) != 0 {
		t.Error("git must not run for an unusable record")
	}
}

func TestCheckoutMissing(t *testing.T) {
	fx := newFixture(t)
	if err := os.RemoveAll(fx.root); err != nil {
		t.Fatal(err)
	}
	s := status(fx.checker().Check(context.Background()))
	if s.Supported || s.Reason != ReasonCheckoutMissing || s.Message == "" {
		t.Errorf("%+v", s)
	}
	if s.Installed == nil || s.Installed.Version != "0.1.0" || s.Installed.Commit != "1111111" {
		t.Errorf("the installed build is still known: %+v", s.Installed)
	}
}

func TestInvalidCheckouts(t *testing.T) {
	cases := map[string]func(t *testing.T, fx *fixture){
		"a file in place of the folder": func(t *testing.T, fx *fixture) {
			os.RemoveAll(fx.root)
			writeFile(t, fx.root, "x")
		},
		"no installer script": func(t *testing.T, fx *fixture) {
			os.Remove(filepath.Join(fx.root, "scripts", "install-local.sh"))
		},
		"no go.mod": func(t *testing.T, fx *fixture) { os.Remove(filepath.Join(fx.root, "core", "go.mod")) },
		"go.mod is a symlink": func(t *testing.T, fx *fixture) {
			path := filepath.Join(fx.root, "core", "go.mod")
			os.Remove(path)
			if err := os.Symlink(filepath.Join(fx.dir, "elsewhere"), path); err != nil {
				t.Fatal(err)
			}
		},
		"git top level is another folder": func(t *testing.T, fx *fixture) {
			fx.git.set("rev-parse --show-toplevel", filepath.Dir(fx.root)+"\n", nil)
		},
		"not a git repository": func(t *testing.T, fx *fixture) {
			fx.git.set("rev-parse --show-toplevel", "", &CommandError{ExitCode: 128, Stderr: "fatal: not a git repository"})
		},
		"no origin": func(t *testing.T, fx *fixture) {
			fx.git.set("remote get-url origin", "", &CommandError{ExitCode: 2, Stderr: "error: No such remote 'origin'"})
		},
	}
	for name, mutate := range cases {
		fx := newFixture(t)
		mutate(t, fx)
		s := status(fx.checker().Check(context.Background()))
		if s.Supported || s.Reason != ReasonCheckoutBad {
			t.Errorf("%s: %+v", name, s)
		}
		if fx.git.count("fetch") != 0 {
			t.Errorf("%s: fetched from an invalid checkout", name)
		}
	}
}

func TestSymlinkedCheckoutRootsAreRefused(t *testing.T) {
	fx := newFixture(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(fx.root, link); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, fx.dir, Record{SourceRoot: link, Commit: headSHA, Version: "0.1.0", InstalledAt: fx.now})
	fx.git.set("rev-parse --show-toplevel", link+"\n", nil)
	if s := status(fx.checker().Check(context.Background())); s.Supported || s.Reason != ReasonCheckoutBad {
		t.Errorf("symlinked root: %+v", s)
	}
	// A symlinked parent is a symlink trick too, even though the final component is a folder.
	linkedParent := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(filepath.Dir(fx.root), linkedParent); err != nil {
		t.Fatal(err)
	}
	through := filepath.Join(linkedParent, filepath.Base(fx.root))
	writeRecord(t, fx.dir, Record{SourceRoot: through, Commit: headSHA, Version: "0.1.0", InstalledAt: fx.now})
	fx.git.set("rev-parse --show-toplevel", through+"\n", nil)
	if s := status(fx.checker().Check(context.Background())); s.Supported || s.Reason != ReasonCheckoutBad {
		t.Errorf("root behind a symlinked parent: %+v", s)
	}
	if fx.git.count("fetch") != 0 {
		t.Error("fetched through a symlink")
	}
}

func TestOnlyThePoupleRepositoryIsAccepted(t *testing.T) {
	for _, origin := range []string{
		"https://github.com/Arthur-MONNET/pouple-app",
		"https://github.com/Arthur-MONNET/pouple-app.git",
		"git@github.com:Arthur-MONNET/pouple-app",
		"git@github.com:Arthur-MONNET/pouple-app.git",
	} {
		fx := newFixture(t)
		fx.git.set("remote get-url origin", origin+"\n", nil)
		if s := status(fx.checker().Check(context.Background())); !s.Supported {
			t.Errorf("%s refused: %+v", origin, s)
		}
	}
	for _, origin := range []string{
		"https://github.com/attacker/pouple-app.git",
		"https://github.com/Arthur-MONNET/pouple-app-evil.git",
		"https://github.com/Arthur-MONNET/pouple-app.git/",
		"https://github.com/arthur-monnet/pouple-app.git",
		"http://github.com/Arthur-MONNET/pouple-app.git",
		"https://user@github.com/Arthur-MONNET/pouple-app.git",
		"https://github.com.evil.example/Arthur-MONNET/pouple-app.git",
		"ssh://git@github.com/Arthur-MONNET/pouple-app.git",
		"git@github.com:attacker/pouple-app.git",
		"git@evil.example:Arthur-MONNET/pouple-app.git",
		"/Users/mallory/pouple-app",
		"file:///Users/mallory/pouple-app",
		"ext::sh -c evil",
		"",
	} {
		fx := newFixture(t)
		fx.git.set("remote get-url origin", origin+"\n", nil)
		s := status(fx.checker().Check(context.Background()))
		if s.Supported || s.Reason != ReasonCheckoutBad {
			t.Errorf("%q accepted: %+v", origin, s)
		}
		if fx.git.count("fetch") != 0 {
			t.Errorf("%q: fetched from a foreign remote", origin)
		}
	}
}

func TestUpToDate(t *testing.T) {
	fx := newFixture(t)
	s := status(fx.checker().Check(context.Background()))
	if !s.Supported || s.Reason != "" || s.Message != "" || s.Available || s.Behind != 0 || s.Error != nil || s.Checking {
		t.Fatalf("%+v", s)
	}
	if s.Installed == nil || *s.Installed != (Installed{Version: "0.1.0", Commit: "1111111", InstalledAt: "2026-11-01T09:30:00Z"}) {
		t.Errorf("installed: %+v", s.Installed)
	}
	if s.Latest == nil || s.Latest.Commit != "def5678" {
		t.Errorf("latest: %+v", s.Latest)
	}
	if s.CheckedAt == nil || *s.CheckedAt != "2026-11-06T12:00:00Z" {
		t.Errorf("checkedAt: %v", s.CheckedAt)
	}
	if len(s.Changes) != 0 || len(s.Blockers) != 0 || s.Changes == nil || s.Blockers == nil {
		t.Errorf("changes and blockers are empty lists, never null: %+v %+v", s.Changes, s.Blockers)
	}
	if fx.git.count("log") != 0 {
		t.Error("no log is read when nothing is new")
	}
}

func TestBehindListsWhatChanges(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "3\n", nil)
	long := strings.Repeat("très long ", 40)
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --",
		"feat: add updates\n\n  fix:\tspaces   and\x1b[31m controls\r\n"+long+"\n", nil)
	s := status(fx.checker().Check(context.Background()))
	if !s.Available || s.Behind != 3 {
		t.Fatalf("%+v", s)
	}
	if len(s.Changes) != 3 || s.Changes[0] != "feat: add updates" || s.Changes[1] != "fix: spaces and [31m controls" {
		t.Errorf("changes: %q", s.Changes)
	}
	if r := []rune(s.Changes[2]); len(r) != maxSubjectRune || r[len(r)-1] != '…' {
		t.Errorf("a long subject is truncated to %d characters: %d", maxSubjectRune, len(r))
	}
	for _, change := range s.Changes {
		if strings.ContainsAny(change, "\n\r\t\x1b") {
			t.Errorf("not one clean line: %q", change)
		}
	}
	if len(s.Blockers) != 0 {
		t.Errorf("blockers: %+v", s.Blockers)
	}
}

func TestChangesAreCappedAtFifteen(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "40\n", nil)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("commit %d", i))
	}
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --", strings.Join(lines, "\n")+"\n", nil)
	s := status(fx.checker().Check(context.Background()))
	if s.Behind != 40 || len(s.Changes) != 15 {
		t.Errorf("behind %d, %d changes", s.Behind, len(s.Changes))
	}
}

func TestBlockersAreReportedAndDoNotHideTheUpdate(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "2\n", nil)
	fx.git.set("rev-list --count origin/main..HEAD --", "1\n", nil)
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --", "one\ntwo\n", nil)
	fx.git.set("rev-parse --abbrev-ref HEAD", "feature\n", nil)
	fx.git.set("status --porcelain", " M core/go.mod\n?? notes.txt\n", nil)
	s := status(fx.checker().Check(context.Background()))
	if !s.Available || s.Behind != 2 || len(s.Changes) != 2 {
		t.Errorf("the update stays visible: %+v", s)
	}
	var codes []BlockerCode
	for _, b := range s.Blockers {
		codes = append(codes, b.Code)
		if b.Message == "" {
			t.Errorf("blocker %s has no message", b.Code)
		}
	}
	if !reflect.DeepEqual(codes, []BlockerCode{BlockerLocalChanges, BlockerNotOnMain, BlockerDiverged}) {
		t.Errorf("blockers: %v", codes)
	}
}

func TestDetachedHeadIsNotOnMain(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-parse --abbrev-ref HEAD", "HEAD\n", nil)
	s := status(fx.checker().Check(context.Background()))
	if len(s.Blockers) != 1 || s.Blockers[0].Code != BlockerNotOnMain {
		t.Errorf("%+v", s.Blockers)
	}
}

func TestGitFailuresBecomeLocalizedErrorsWithoutLeakingOutput(t *testing.T) {
	secret := "/Users/someone-else/.ssh/id_ed25519 and https://token@github.com"
	cases := map[string]struct {
		err  error
		want failure
		fr   string
	}{
		"offline dns":   {&CommandError{ExitCode: 128, Stderr: "fatal: unable to access 'https://github.com/x/': Could not resolve host: github.com " + secret}, failureOffline, "GitHub est injoignable"},
		"offline ssh":   {&CommandError{ExitCode: 128, Stderr: "ssh: Could not resolve hostname github.com: nodename nor servname provided " + secret}, failureOffline, "GitHub est injoignable"},
		"auth ssh":      {&CommandError{ExitCode: 128, Stderr: "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository. " + secret}, failureAuth, "GitHub a refusé l’accès"},
		"auth https":    {&CommandError{ExitCode: 128, Stderr: "fatal: could not read Username for 'https://github.com': terminal prompts disabled " + secret}, failureAuth, "GitHub a refusé l’accès"},
		"something odd": {&CommandError{ExitCode: 1, Stderr: "fatal: " + secret}, failureGit, "n’a pas pu rechercher"},
		"git missing":   {errors.New("exec: git: executable file not found " + secret), failureGit, "n’a pas pu rechercher"},
	}
	for name, tc := range cases {
		fx := newFixture(t)
		fx.git.set("fetch --quiet origin main", "", tc.err)
		got := fx.checker().Check(context.Background())
		if got.failure != tc.want {
			t.Errorf("%s: kind %q, want %q", name, got.failure, tc.want)
		}
		s := got.Status(i18n.FR)
		if s.Error == nil || !strings.Contains(*s.Error, tc.fr) {
			t.Errorf("%s: error %v", name, s.Error)
		}
		raw, _ := json.Marshal(got.Status(i18n.EN))
		raw2, _ := json.Marshal(s)
		for _, leak := range []string{"someone-else", "id_ed25519", "token@", "fatal", "ssh:", "Permission denied"} {
			if strings.Contains(string(raw), leak) || strings.Contains(string(raw2), leak) {
				t.Errorf("%s: the status leaks %q: %s", name, leak, raw)
			}
		}
		if !s.Supported || s.Available || s.CheckedAt != nil {
			t.Errorf("%s: a failed first check still says supported with no data: %+v", name, s)
		}
	}
}

func TestFailedRefreshKeepsWhatWasKnown(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "2\n", nil)
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --", "one\ntwo\n", nil)
	c := fx.checker()
	first := status(c.Check(context.Background()))
	fx.advance(time.Minute)
	fx.git.set("fetch --quiet origin main", "", &CommandError{ExitCode: 128, Stderr: "Could not resolve host"})
	second := status(c.Check(context.Background()))
	if second.Error == nil || !second.Available || second.Behind != 2 || len(second.Changes) != 2 || second.Latest == nil {
		t.Errorf("an offline refresh must keep the last good answer: %+v", second)
	}
	if *second.CheckedAt != *first.CheckedAt {
		t.Errorf("checkedAt is the last successful check: %s vs %s", *second.CheckedAt, *first.CheckedAt)
	}
	fx.advance(time.Minute)
	fx.git.set("fetch --quiet origin main", "", nil)
	if third := status(c.Check(context.Background())); third.Error != nil || *third.CheckedAt == *first.CheckedAt {
		t.Errorf("recovery clears the error: %+v", third)
	}
}

func TestTimeout(t *testing.T) {
	fx := newFixture(t)
	fx.git.hook = func(ctx context.Context, cmd Command) {
		if cmd.Args[len(gitPrefix)] == "fetch" {
			<-ctx.Done()
		}
	}
	fx.git.set("fetch --quiet origin main", "", context.DeadlineExceeded)
	c := fx.checker()
	c.timeout = 50 * time.Millisecond
	started := time.Now()
	got := c.Check(context.Background())
	if time.Since(started) > 5*time.Second {
		t.Fatal("the check did not stop at its timeout")
	}
	if got.failure != failureTimeout || !strings.Contains(*got.Status(i18n.FR).Error, "trop de temps") {
		t.Errorf("%+v", got.failure)
	}
}

func TestTimeoutWhileValidatingDoesNotCondemnTheCheckout(t *testing.T) {
	fx := newFixture(t)
	fx.git.hook = func(ctx context.Context, cmd Command) { <-ctx.Done() }
	fx.git.set("rev-parse --show-toplevel", "", context.DeadlineExceeded)
	c := fx.checker()
	c.timeout = 30 * time.Millisecond
	got := c.Check(context.Background())
	if !got.Supported || got.failure != failureTimeout {
		t.Errorf("a slow git is not an invalid checkout: %+v", got)
	}
	// The slow answer is not remembered as the verdict.
	fx.git.hook = nil
	fx.git.set("rev-parse --show-toplevel", fx.root+"\n", nil)
	fx.advance(time.Minute)
	if again := c.Check(context.Background()); !again.Supported || again.failure != failureNone {
		t.Errorf("%+v", again)
	}
}

func TestTheRealTimeoutIsThirtySeconds(t *testing.T) {
	fx := newFixture(t)
	var deadline time.Time
	var seen sync.Once
	fx.git.hook = func(ctx context.Context, cmd Command) {
		seen.Do(func() { deadline, _ = ctx.Deadline() })
	}
	fx.checker().Check(context.Background())
	if left := time.Until(deadline); left < 28*time.Second || left > 30*time.Second {
		t.Errorf("deadline in %s", left)
	}
}

func TestEveryCommandIsHardenedAndNeverUsesAShell(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "1\n", nil)
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --", "one\n", nil)
	fx.checker().Check(context.Background())
	wantPrefix := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks"}
	wantEnv := []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}
	if len(fx.git.calls) != 9 {
		t.Fatalf("calls: %v", fx.git.keys())
	}
	for _, call := range fx.git.calls {
		if !reflect.DeepEqual(call.Args[:len(wantPrefix)], wantPrefix) {
			t.Errorf("args %v do not start with the hardening flags", call.Args)
		}
		if !reflect.DeepEqual(call.Env, wantEnv) {
			t.Errorf("env %v", call.Env)
		}
		if call.Dir != fx.root {
			t.Errorf("dir %q", call.Dir)
		}
		// A Command is an argument vector for git itself (no program name, no shell string).
		for _, arg := range call.Args {
			if arg == "sh" || arg == "bash" || arg == "/bin/sh" || arg == "git" {
				t.Errorf("a program name in args: %v", call.Args)
			}
		}
	}
	want := []string{
		"rev-parse --show-toplevel",
		"remote get-url origin",
		"fetch --quiet origin main",
		"rev-parse --abbrev-ref HEAD",
		"rev-parse --verify --quiet origin/main^{commit}",
		"rev-list --count HEAD..origin/main --",
		"rev-list --count origin/main..HEAD --",
		"log --format=%s -n 15 HEAD..origin/main --",
		"status --porcelain",
	}
	if !reflect.DeepEqual(fx.git.keys(), want) {
		t.Errorf("commands:\n got %q\nwant %q", fx.git.keys(), want)
	}
}

func TestManualChecksAreRateLimitedAndCached(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "1\n", nil)
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --", "one\n", nil)
	c := fx.checker()
	first := c.Check(context.Background())
	fetches := func() int { return fx.git.count("fetch") }
	if fetches() != 1 {
		t.Fatalf("fetches: %d", fetches())
	}
	callsAfterFirst := len(fx.git.keys())
	fx.advance(29 * time.Second)
	again := c.Check(context.Background())
	if fetches() != 1 || len(fx.git.keys()) != callsAfterFirst || !reflect.DeepEqual(first, again) {
		t.Errorf("a second check within 30 s must return the cached status without running git")
	}
	if cached := c.Current(context.Background()); !reflect.DeepEqual(first, cached) || len(fx.git.keys()) != callsAfterFirst {
		t.Error("Current is the cache")
	}
	fx.advance(2 * time.Second)
	c.Check(context.Background())
	if fetches() != 2 {
		t.Errorf("after 30 s a check fetches again: %d", fetches())
	}
}

func TestFailedChecksCountAgainstTheRateLimit(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("fetch --quiet origin main", "", &CommandError{ExitCode: 128, Stderr: "Could not resolve host"})
	c := fx.checker()
	c.Check(context.Background())
	c.Check(context.Background())
	if fx.git.count("fetch") != 1 {
		t.Errorf("fetches: %d", fx.git.count("fetch"))
	}
}

func TestCurrentBeforeAnyCheckValidatesWithoutFetching(t *testing.T) {
	fx := newFixture(t)
	c := fx.checker()
	s := status(c.Current(context.Background()))
	if !s.Supported || s.Installed == nil || s.CheckedAt != nil || s.Latest != nil || s.Available {
		t.Errorf("%+v", s)
	}
	if fx.git.count("fetch") != 0 {
		t.Error("GET must never reach the network")
	}
	// The first real check is allowed right after.
	c.Check(context.Background())
	if fx.git.count("fetch") != 1 {
		t.Error("the first manual check must fetch")
	}
}

func TestConcurrentChecksShareOneFetch(t *testing.T) {
	fx := newFixture(t)
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	fx.git.hook = func(ctx context.Context, cmd Command) {
		if cmd.Args[len(gitPrefix)] == "fetch" {
			started <- struct{}{}
			<-release
		}
	}
	c := fx.checker()
	var wg sync.WaitGroup
	results := make([]Result, 4)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = c.Check(context.Background()) }()
	}
	<-started
	deadline := time.Now().Add(5 * time.Second)
	for !c.Current(context.Background()).Checking {
		if time.Now().After(deadline) {
			t.Fatal("Current never reported the running check")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if fx.git.count("fetch") != 1 {
		t.Errorf("fetches: %d", fx.git.count("fetch"))
	}
	for i, r := range results {
		if r.Latest != "def5678" || r.Checking {
			t.Errorf("result %d: %+v", i, r)
		}
	}
}

func TestACancelledCallLearnsNothing(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	fx.git.hook = func(c context.Context, cmd Command) {
		if cmd.Args[len(gitPrefix)] == "fetch" {
			cancel()
			<-c.Done()
		}
	}
	fx.git.set("fetch --quiet origin main", "", context.Canceled)
	c := fx.checker()
	c.Check(ctx)
	fx.git.hook = nil
	fx.git.set("fetch --quiet origin main", "", nil)
	got := status(c.Check(context.Background()))
	if got.Error != nil || got.Latest == nil || fx.git.count("fetch") != 2 {
		t.Errorf("a dropped request must neither poison the cache nor use up the rate limit: %+v", got)
	}
}

func TestRunChecksAfterTheDelayAndOnEveryInterval(t *testing.T) {
	fx := newFixture(t)
	var mu sync.Mutex
	ticks := 0
	// Every call of the clock moves one hour on, so the rate limit never holds a scheduled check back.
	c := New(Options{
		DataDir: fx.dir, Git: fx.git.run,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			ticks++
			return fx.now.Add(time.Duration(ticks) * time.Hour)
		},
		InitialDelay: 20 * time.Millisecond, Interval: 20 * time.Millisecond,
		Jitter: func(time.Duration) time.Duration { return 0 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	time.Sleep(10 * time.Millisecond)
	if fx.git.count("fetch") != 0 {
		t.Error("the first check waits for the initial delay")
	}
	deadline := time.Now().Add(5 * time.Second)
	for fx.git.count("fetch") < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
	if fx.git.count("fetch") < 3 {
		t.Errorf("scheduled fetches: %d", fx.git.count("fetch"))
	}
}

func TestDefaultSchedule(t *testing.T) {
	c := New(Options{DataDir: t.TempDir()})
	if c.initialDelay != 30*time.Second || c.interval != 6*time.Hour || c.timeout != 30*time.Second {
		t.Errorf("%v %v %v", c.initialDelay, c.interval, c.timeout)
	}
	for range 200 {
		if j := c.jitter(c.interval); j < 0 || j >= c.interval/10 {
			t.Fatalf("jitter %v", j)
		}
	}
}

func TestStatusJSONShape(t *testing.T) {
	fx := newFixture(t)
	fx.git.set("rev-list --count HEAD..origin/main --", "3\n", nil)
	fx.git.set("log --format=%s -n 15 HEAD..origin/main --", "a\nb\nc\n", nil)
	supported, _ := json.Marshal(status(fx.checker().Check(context.Background())))
	want := `{"supported":true,"reason":"","message":"","installed":{"version":"0.1.0","commit":"1111111","installedAt":"2026-11-01T09:30:00Z"},"latest":{"commit":"def5678"},"available":true,"behind":3,"changes":["a","b","c"],"blockers":[],"checkedAt":"2026-11-06T12:00:00Z","checking":false,"error":null}`
	if string(supported) != want {
		t.Errorf("supported:\n got %s\nwant %s", supported, want)
	}
	other := newFixture(t)
	os.Remove(filepath.Join(other.dir, recordFile))
	unsupported, _ := json.Marshal(other.checker().Check(context.Background()).Status(i18n.EN))
	wantUnsupported := `{"supported":false,"reason":"not-installed-from-source","message":"Updates from the app are available only when Pouple was installed from its source with scripts/install-local.sh.","installed":null,"latest":null,"available":false,"behind":0,"changes":[],"blockers":[],"checkedAt":null,"checking":false,"error":null}`
	if string(unsupported) != wantUnsupported {
		t.Errorf("unsupported:\n got %s\nwant %s", unsupported, wantUnsupported)
	}
}

func TestEveryUserVisibleTextHasAFrenchTranslation(t *testing.T) {
	var texts []string
	for _, reason := range []Reason{ReasonNotFromSource, ReasonCheckoutMissing, ReasonCheckoutBad} {
		texts = append(texts, unsupportedText(reason))
	}
	for _, code := range []BlockerCode{BlockerLocalChanges, BlockerNotOnMain, BlockerDiverged} {
		texts = append(texts, blockerText(code))
	}
	for _, f := range []failure{failureOffline, failureAuth, failureTimeout, failureGit} {
		texts = append(texts, failureText(f))
	}
	seen := map[string]bool{}
	for _, text := range texts {
		if seen[text] {
			t.Errorf("two situations share the text %q", text)
		}
		seen[text] = true
		if !i18n.Has(text) {
			t.Errorf("no French translation for %q", text)
		}
		if i18n.T(i18n.FR, text) == text {
			t.Errorf("the French text equals the English one: %q", text)
		}
	}
}

func TestCleanLine(t *testing.T) {
	for in, want := range map[string]string{
		"  hello   world  ": "hello world",
		"a\tb\x00c":         "a b c",
		"\n\n":              "",
		"émoji 🙂 ok":        "émoji 🙂 ok",
	} {
		if got := cleanLine(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
