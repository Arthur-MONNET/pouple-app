package codex

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func makeReleaseArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	files := tar.NewWriter(compressed)
	if err := files.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestInstallAssetPublishesOnlyTheVerifiedExecutable(t *testing.T) {
	const name = "codex-test-apple-darwin"
	archive := makeReleaseArchive(t, name, "verified executable")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	home := t.TempDir()
	if command := Command(home); command != "codex" {
		t.Fatalf("before install: %q", command)
	}
	destination := managedPath(home)
	asset := releaseAsset{name: name, digest: fmt.Sprintf("%x", sha256.Sum256(archive))}
	if err := installAsset(context.Background(), server.Client(), server.URL, asset, destination); err != nil {
		t.Fatal(err)
	}
	if command := Command(home); command != destination {
		t.Fatalf("after install: %q", command)
	}

	badAssets := []releaseAsset{
		{name: name, digest: "incorrect"},
		{name: "../codex", digest: asset.digest},
	}
	for _, bad := range badAssets {
		if err := installAsset(context.Background(), server.Client(), server.URL, bad, destination); err == nil {
			t.Fatal("invalid archive was accepted")
		}
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "verified executable" {
		t.Fatalf("verified executable was replaced: %q, %v", content, err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files were not removed: %v, %v", entries, err)
	}
}

func TestEnsureHostInstallsTheHelperBesideAManagedCodexOnly(t *testing.T) {
	const name = "codex-code-mode-host-test"
	archive := makeReleaseArchive(t, name, "helper")
	asset := releaseAsset{name: name, digest: fmt.Sprintf("%x", sha256.Sum256(archive))}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	defer server.Close()

	// A Codex of the PATH brings its own helper: nothing is downloaded for it.
	empty := t.TempDir()
	if err := EnsureHost(context.Background(), empty); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hostPath(empty)); err == nil {
		t.Fatal("a helper was installed without a managed Codex")
	}

	// The helper lands where Codex looks for it, executable.
	home := t.TempDir()
	if err := installAsset(context.Background(), server.Client(), server.URL, asset, hostPath(home)); err != nil {
		t.Fatal(err)
	}
	if !isExecutable(hostPath(home)) || filepath.Dir(hostPath(home)) != filepath.Dir(managedPath(home)) {
		t.Fatalf("helper is not executable beside codex: %s", hostPath(home))
	}
}
