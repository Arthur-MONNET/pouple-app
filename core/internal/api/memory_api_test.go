package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type fakeMemory struct {
	view     engine.MemoryView
	calls    []string
	err      error
	revealed []string
}

func (f *fakeMemory) MemoryView(context.Context) (engine.MemoryView, error) { return f.view, nil }
func (f *fakeMemory) ReviewMemory(note, line string, keep bool) error {
	f.calls = append(f.calls, strings.Join([]string{"review", note, line, map[bool]string{true: "keep", false: "drop"}[keep]}, " "))
	return f.err
}
func (f *fakeMemory) TidyMemory() error {
	f.calls = append(f.calls, "tidy")
	return f.err
}

func memoryServer(t *testing.T, memory *fakeMemory) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	file := config.Open(filepath.Join(t.TempDir(), "config.json"))
	registry := connector.NewRegistry(secrets.NewMemory(), config.Connectors{File: file})
	reveal := func(path string) error {
		memory.revealed = append(memory.revealed, path)
		return nil
	}
	return New(Options{Registry: registry, Token: "secret-token", Mount: MountMemory(memory, MemoryOptions{Home: home, Reveal: reveal})}, port), home
}

func TestTheMemoryPageShowsTheViewAndTheTools(t *testing.T) {
	memory := &fakeMemory{view: engine.MemoryView{Enabled: true, Folder: "/brain", Exists: true}}
	s, home := memoryServer(t, memory)
	r := call(s, "GET", "/api/memory", "", nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"folder":"/brain"`) || !strings.Contains(r.Body.String(), `"id":"claude"`) ||
		!strings.Contains(r.Body.String(), `"file":"`+filepath.Join(home, ".codex", "AGENTS.md")+`"`) {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	for _, c := range []struct {
		method, path, body string
		code               int
		call               string
	}{
		{"POST", "/api/memory/review", `{"note":"a.md","line":"- x","decision":"keep"}`, 200, "review a.md - x keep"},
		{"POST", "/api/memory/review", `{"note":"a.md","line":"- x","decision":"drop"}`, 200, "review a.md - x drop"},
		{"POST", "/api/memory/review", `{"note":"a.md","line":"- x","decision":"later"}`, 400, ""},
		{"POST", "/api/memory/tidy", ``, 202, "tidy"},
	} {
		memory.calls = nil
		if r := call(s, c.method, c.path, c.body, nil); r.Code != c.code || (c.call != "" && (len(memory.calls) != 1 || memory.calls[0] != c.call)) {
			t.Errorf("%s %s: %d %s, calls %q", c.method, c.body, r.Code, r.Body, memory.calls)
		}
	}
	if r := call(s, "POST", "/api/memory/reveal", "", nil); r.Code != 204 || len(memory.revealed) != 1 || memory.revealed[0] != "/brain" {
		t.Fatalf("reveal: %d, %q", r.Code, memory.revealed)
	}
	memory.view.Exists = false
	if r := call(s, "POST", "/api/memory/reveal", "", nil); r.Code != 404 {
		t.Fatalf("a folder still to make cannot be shown: %d", r.Code)
	}
}

func TestToolsAreConnectedOnlyWhileTheMemoryIsOn(t *testing.T) {
	memory := &fakeMemory{view: engine.MemoryView{Enabled: false, Folder: "/brain"}}
	s, home := memoryServer(t, memory)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	if r := call(s, "POST", "/api/memory/clis/claude", `{"connected":true}`, nil); r.Code != 409 {
		t.Fatalf("connecting while off: %d %s", r.Code, r.Body)
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) {
		t.Fatal("a file was written while the memory is off")
	}
	memory.view.Enabled = true
	if r := call(s, "POST", "/api/memory/clis/claude", `{"connected":true}`, nil); r.Code != 200 || !strings.Contains(r.Body.String(), `"connected":true`) {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if raw, _ := os.ReadFile(claude); !strings.Contains(string(raw), "@/brain/INDEX.md") {
		t.Fatalf("file:\n%s", raw)
	}
	memory.view.Enabled = false
	if r := call(s, "POST", "/api/memory/clis/claude", `{"connected":false}`, nil); r.Code != 200 {
		t.Fatalf("disconnecting is always possible: %d %s", r.Code, r.Body)
	}
	if raw, _ := os.ReadFile(claude); len(raw) != 0 {
		t.Fatalf("file after disconnecting: %q", raw)
	}
	for body, code := range map[string]int{`{}`: 400, `{"connected":"yes"}`: 400} {
		if r := call(s, "POST", "/api/memory/clis/claude", body, nil); r.Code != code {
			t.Errorf("%s: %d", body, r.Code)
		}
	}
	if r := call(s, "POST", "/api/memory/clis/cursor", `{"connected":false}`, nil); r.Code != 404 {
		t.Errorf("an unknown tool: %d", r.Code)
	}
}
