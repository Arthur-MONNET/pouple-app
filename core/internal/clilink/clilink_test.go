package clilink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func status(t *testing.T, home, folder, id string) Status {
	t.Helper()
	for _, s := range List(home, folder) {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no %s in the list", id)
	return Status{}
}

func TestConnectingAddsOnlyTheBlockAndDisconnectingGivesTheFileBack(t *testing.T) {
	home := t.TempDir()
	folder := filepath.Join(home, "SecondBrain")
	file := filepath.Join(home, ".claude", "CLAUDE.md")
	original := "# My rules\n\n- Write in French.\n"
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if status(t, home, folder, "claude").Connected {
		t.Fatal("connected before anything was written")
	}
	if err := Connect(home, "claude", folder); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	text := string(raw)
	if !strings.HasPrefix(text, original+"\n"+startMark) || !strings.Contains(text, "\n@~/SecondBrain/INDEX.md\n") || !strings.HasSuffix(text, endMark+"\n") {
		t.Fatalf("file:\n%s", text)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o640 {
		t.Fatalf("the mode changed: %v", info.Mode())
	}
	if !status(t, home, folder, "claude").Connected {
		t.Fatal("not connected after connecting")
	}
	// Connecting again replaces the block, never adds a second one.
	if err := Connect(home, "claude", folder); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(file); strings.Count(string(raw), startMark) != 1 {
		t.Fatalf("two blocks:\n%s", raw)
	}
	if err := Disconnect(home, "claude"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(file); string(raw) != original {
		t.Fatalf("the file did not come back as it was:\n%q", raw)
	}
}

func TestABlockForAnotherFolderIsNotConnectedAndIsReplaced(t *testing.T) {
	home := t.TempDir()
	if err := Connect(home, "codex", filepath.Join(home, "Old")); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(home, "Vault")
	codex := status(t, home, folder, "codex")
	if codex.Connected || !strings.Contains(codex.Lines, "~/Vault") || codex.File != filepath.Join(home, ".codex", "AGENTS.md") {
		t.Fatalf("status %+v", codex)
	}
	if err := Connect(home, "codex", folder); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(codex.File)
	if strings.Contains(string(raw), "~/Old") || !status(t, home, folder, "codex").Connected {
		t.Fatalf("file:\n%s", raw)
	}
	if err := Disconnect(home, "codex"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(codex.File); len(raw) != 0 {
		t.Fatalf("a file made by Pouple holds only its block: %q", raw)
	}
}

func TestAFileThatIsALinkOrAnUnknownToolIsNeverWritten(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "dotfiles-CLAUDE.md")
	if err := os.WriteFile(elsewhere, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := Connect(home, "claude", filepath.Join(home, "SecondBrain")); err == nil {
		t.Fatal("wrote through a link")
	}
	if raw, _ := os.ReadFile(elsewhere); string(raw) != "mine\n" {
		t.Fatalf("the linked file changed: %q", raw)
	}
	if err := Connect(home, "cursor", "/x"); err != ErrUnknown {
		t.Fatalf("err = %v", err)
	}
}

func TestAStartMarkWithoutItsEndIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := "rules\n" + startMark + "\nhalf a block\n"
	if err := os.WriteFile(file, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Disconnect(home, "codex"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(file); string(raw) != broken {
		t.Fatalf("a block Pouple cannot tell is its own was changed: %q", raw)
	}
}
