package updates

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

// fakeGitBinary installs a "git" that records how it was started, and puts it first on PATH.
func fakeGitBinary(t *testing.T, exitCode int, stderr string) (record string) {
	t.Helper()
	bin := t.TempDir()
	record = filepath.Join(t.TempDir(), "record")
	script := "#!/bin/sh\n" +
		"{ echo \"cwd=$PWD\"; for a in \"$@\"; do echo \"arg=$a\"; done; env | sort | sed 's/^/env=/'; } > '" + record + "'\n" +
		"echo '" + stderr + "' >&2\n" +
		"echo out-line\n" +
		"exit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return record
}

func TestExecRunnerStartsGitDirectlyWithTheExactArgumentsAndEnvironment(t *testing.T) {
	record := fakeGitBinary(t, 0, "")
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_SSH_COMMAND", "evil")
	t.Setenv("POUPLE_TOKEN", "secret")
	t.Setenv("LANG", "fr_FR.UTF-8")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	// Shell metacharacters stay literal arguments: nothing interprets them.
	marker := filepath.Join(t.TempDir(), "pwned")
	hostile := "$(touch " + marker + ")`touch " + marker + "`;touch " + marker
	out, err := ExecRunner(context.Background(), Command{
		Dir: dir, Env: gitEnv, Args: append(append([]string{}, gitPrefix...), "rev-parse", hostile),
	})
	if err != nil || strings.TrimSpace(out) != "out-line" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a shell interpreted an argument")
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var args, env []string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "arg="):
			args = append(args, strings.TrimPrefix(line, "arg="))
		case strings.HasPrefix(line, "env="):
			env = append(env, strings.TrimPrefix(line, "env="))
		case strings.HasPrefix(line, "cwd="):
			if got := strings.TrimPrefix(line, "cwd="); got != dir {
				t.Errorf("cwd %q", got)
			}
		}
	}
	wantArgs := append(append([]string{}, gitPrefix...), "rev-parse", hostile)
	if strings.Join(args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Errorf("args %q", args)
	}
	has := func(entry string) bool {
		for _, e := range env {
			if e == entry {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C"} {
		if !has(want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_SSH_COMMAND=") || strings.HasPrefix(e, "POUPLE_") {
			t.Errorf("inherited %s", e)
		}
	}
}

func TestExecRunnerReportsFailuresWithTheirStderrForClassification(t *testing.T) {
	fakeGitBinary(t, 3, "fatal: Could not resolve host")
	_, err := ExecRunner(context.Background(), Command{Dir: t.TempDir(), Args: []string{"fetch"}})
	var failed *CommandError
	if !errors.As(err, &failed) || failed.ExitCode != 3 || !strings.Contains(failed.Stderr, "Could not resolve host") {
		t.Fatalf("%v", err)
	}
	if classify(context.Background(), err) != failureOffline {
		t.Error("classification")
	}
}

func TestExecRunnerStopsAtTheContext(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := ExecRunner(ctx, Command{Dir: t.TempDir(), Args: []string{"fetch"}})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 10*time.Second {
		t.Errorf("%v after %s", err, time.Since(started))
	}
	if classify(ctx, err) != failureTimeout {
		t.Error("a deadline is a timeout")
	}
}

func TestCleanEnvironment(t *testing.T) {
	got := cleanEnvironment([]string{"PATH=/bin", "GIT_DIR=x", "GIT_WORK_TREE=y", "POUPLE_TOKEN=z", "HOME=/h", "LC_ALL=fr", "SSH_AUTH_SOCK=/s"})
	if strings.Join(got, ",") != "PATH=/bin,HOME=/h,SSH_AUTH_SOCK=/s" {
		t.Errorf("%v", got)
	}
}

// A real git, on a real repository: the validation and the measures agree with git itself.
func TestAgainstARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("HOME", t.TempDir()) // the user's own gitconfig must not change what the test sees
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root, _ := filepath.EvalSymlinks(t.TempDir())
	remote := filepath.Join(t.TempDir(), "remote.git")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(cleanEnvironment(os.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(root, "scripts", "install-local.sh"), "#!/bin/bash\n")
	writeFile(t, filepath.Join(root, "core", "go.mod"), "module x\n")
	run(root, "add", ".")
	run(root, "commit", "-q", "-m", "first")
	installed := run(root, "rev-parse", "HEAD")
	run(t.TempDir(), "clone", "-q", "--bare", root, remote)
	// The checkout's origin is a local bare repository, which the allow-list refuses: that is the point.
	run(root, "remote", "add", "origin", remote)
	dir := t.TempDir()
	writeRecord(t, dir, Record{SourceRoot: root, Commit: installed, Version: "0.1.0", InstalledAt: time.Now()})
	c := New(Options{DataDir: dir})
	if s := status(c.Check(context.Background())); s.Supported || s.Reason != ReasonCheckoutBad {
		t.Fatalf("a foreign origin is refused by the real git too: %+v", s)
	}
	// Rewriting the URL through the local configuration must not smuggle a foreign remote past the allow-list:
	// git remote get-url applies insteadOf, which is what fetch would use.
	run(root, "config", "remote.origin.url", "https://github.com/Arthur-MONNET/pouple-app.git")
	run(root, "config", "url."+remote+".insteadOf", "https://github.com/Arthur-MONNET/pouple-app.git")
	c = New(Options{DataDir: dir})
	if s := status(c.Check(context.Background())); s.Supported {
		t.Fatalf("insteadOf redirected the remote and was accepted: %+v", s)
	}
	// Without the rewrite the checkout is valid; the fetch itself then goes to GitHub, so only validation is run.
	run(root, "config", "--unset", "url."+remote+".insteadOf")
	c = New(Options{DataDir: dir})
	if s := status(c.Current(context.Background())); !s.Supported || s.Installed == nil || s.Installed.Commit != installed[:7] {
		t.Fatalf("a genuine checkout: %+v", s)
	}
}
