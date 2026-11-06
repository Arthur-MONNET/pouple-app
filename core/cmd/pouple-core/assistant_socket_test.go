package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssistantSocketIsPrivateAndHasOneOwner(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "pouple-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	socket, closeSocket, err := openAssistantSocket(home)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSocket()
	for path, want := range map[string]os.FileMode{filepath.Join(home, "ipc"): 0o700, socket.Addr().String(): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("wrong permissions on %s: %v, %v", path, info, err)
		}
	}
	if _, cleanup, err := openAssistantSocket(home); err == nil {
		cleanup()
		t.Fatal("a second core acquired the same socket")
	}
	closeSocket()
	if _, err := os.Lstat(socket.Addr().String()); !os.IsNotExist(err) {
		t.Fatalf("socket remains after shutdown: %v", err)
	}
}
