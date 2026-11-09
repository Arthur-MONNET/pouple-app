package updates

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	goodOrigin = "git@github.com:Arthur-MONNET/pouple-app.git"
	headSHA    = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	latestSHA  = "def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// reply is what the fake git answers to one command.
type reply struct {
	out string
	err error
}

// fakeGit answers commands by their arguments after the hardening prefix, and records every call.
type fakeGit struct {
	mu      sync.Mutex
	replies map[string]reply
	calls   []Command
	// hook runs before the answer; tests use it to block or observe.
	hook func(ctx context.Context, cmd Command)
}

func (f *fakeGit) run(ctx context.Context, cmd Command) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	hook := f.hook
	f.mu.Unlock()
	if hook != nil {
		hook(ctx, cmd)
	}
	key := strings.Join(cmd.Args[len(gitPrefix):], " ")
	f.mu.Lock()
	r, ok := f.replies[key]
	f.mu.Unlock()
	if !ok {
		return "", &CommandError{ExitCode: 128, Stderr: "fatal: unexpected command " + key}
	}
	return r.out, r.err
}

func (f *fakeGit) set(key string, out string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[key] = reply{out, err}
}

func (f *fakeGit) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		keys = append(keys, strings.Join(c.Args[len(gitPrefix):], " "))
	}
	return keys
}

func (f *fakeGit) count(prefix string) int {
	n := 0
	for _, key := range f.keys() {
		if strings.HasPrefix(key, prefix) {
			n++
		}
	}
	return n
}

// fixture is a data folder with an install record and a checkout that looks like Pouple's sources.
type fixture struct {
	dir  string
	root string
	git  *fakeGit
	now  time.Time
	mu   sync.Mutex
}

func (fx *fixture) clock() time.Time {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.now
}

func (fx *fixture) advance(d time.Duration) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.now = fx.now.Add(d)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRecord(t *testing.T, dir string, record Record) {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, recordFile), string(data))
}

// newFixture builds a supported setup whose fake git is up to date on main with a clean tree.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(root, "scripts", "install-local.sh"), "#!/bin/bash\n")
	writeFile(t, filepath.Join(root, "core", "go.mod"), "module x\n")
	writeRecord(t, dir, Record{SourceRoot: root, Commit: headSHA, Version: "0.1.0", InstalledAt: time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)})
	git := &fakeGit{replies: map[string]reply{}}
	git.set("rev-parse --show-toplevel", root+"\n", nil)
	git.set("remote get-url origin", goodOrigin+"\n", nil)
	git.set("fetch --quiet origin main", "", nil)
	git.set("rev-parse --abbrev-ref HEAD", "main\n", nil)
	git.set("rev-parse --verify --quiet origin/main^{commit}", latestSHA+"\n", nil)
	git.set("rev-parse --verify --quiet "+headSHA+"^{commit}", headSHA+"\n", nil)
	git.set("rev-list --count "+headSHA+"..origin/main --", "0\n", nil)
	git.set("rev-list --count origin/main..HEAD --", "0\n", nil)
	git.set("status --porcelain --untracked-files=no", "", nil)
	return &fixture{dir: dir, root: root, git: git, now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
}

func (fx *fixture) checker() *Checker {
	return New(Options{DataDir: fx.dir, Git: fx.git.run, Now: fx.clock})
}
