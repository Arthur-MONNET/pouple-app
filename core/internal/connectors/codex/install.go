package codex

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	codexRelease   = "rust-v0.167.0"
	releaseURL     = "https://github.com/openai/codex/releases/download/" + codexRelease + "/"
	maxArchiveSize = 200 << 20
	maxBinarySize  = 400 << 20
)

type releaseAsset struct {
	name   string
	digest string
}

// The digests are those published on the official GitHub release for the pinned archives.
var macOSAssets = map[string]releaseAsset{
	"arm64": {"codex-aarch64-apple-darwin", "4b22a2a971be2791c3e31f912f3e77a88ebd64a65812a7289ccba65dd6425fe9"},
	"amd64": {"codex-x86_64-apple-darwin", "d8eb4a865a0b17319cde10347523e5bab0978d01725b39f4a639f40905e01221"},
}

// Since 0.167 Codex runs every command of its shell tool through a helper program that must sit beside it. Without
// it the AI cannot read a single file, and says so in its own words ("the project is inaccessible").
var macOSHostAssets = map[string]releaseAsset{
	"arm64": {"codex-code-mode-host-aarch64-apple-darwin", "876a3e263348ef0edaeae5cf5bc52a5c121141cefd15157ab0eeca305f3eb563"},
	"amd64": {"codex-code-mode-host-x86_64-apple-darwin", "612d0dc2c7316fbf05ee922796f4d740a285b76a55d4c78e1b946ba621bdcc91"},
}

func managedPath(home string) string { return filepath.Join(home, "bin", "codex") }

// hostPath is where Codex looks for its helper: next to its own executable.
func hostPath(home string) string { return filepath.Join(home, "bin", "codex-code-mode-host") }

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o100 != 0
}

// EnsureHost installs the helper of Pouple's own Codex when it is missing, which is the case of every Codex installed
// by an earlier Pouple. A Codex found on the PATH brings its own helper: nothing to do for it.
func EnsureHost(ctx context.Context, home string) error {
	if !isExecutable(managedPath(home)) || isExecutable(hostPath(home)) {
		return nil
	}
	asset, supported := macOSHostAssets[runtime.GOARCH]
	if runtime.GOOS != "darwin" || !supported {
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	return installAsset(ctx, client, releaseURL+asset.name+".tar.gz", asset, hostPath(home))
}

// Command uses Pouple's installation when present and keeps existing installations on PATH working.
func Command(home string) string {
	path := managedPath(home)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o100 != 0 {
		return path
	}
	return "codex"
}

func (c *Connector) install(ctx context.Context, emit func(connector.Event)) error {
	asset, supported := macOSAssets[runtime.GOARCH]
	if runtime.GOOS != "darwin" || !supported {
		return i18n.NewError("Automatic Codex installation is only available on supported Macs.")
	}
	log := func(message string) {
		emit(connector.Event{Type: "log", Text: i18n.T(i18n.FromContext(ctx), message)})
	}
	log("Downloading Codex from OpenAI…")
	client := &http.Client{Timeout: 5 * time.Minute}
	if err := installAsset(ctx, client, releaseURL+asset.name+".tar.gz", asset, managedPath(c.Home)); err != nil {
		return err
	}
	if err := EnsureHost(ctx, c.Home); err != nil {
		return err
	}
	log("Codex is installed. You can now sign in.")
	return nil
}

func installAsset(ctx context.Context, client *http.Client, url string, asset releaseAsset, destination string) error {
	const downloadFailure = "Codex download failed. Check your internet connection and try again."
	const integrityFailure = "The downloaded Codex archive could not be verified."
	const saveFailure = "Codex could not be saved in Pouple’s application folder."

	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	archive, err := os.CreateTemp(dir, ".codex-archive-*")
	if err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return i18n.Wrap(err, downloadFailure)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		return i18n.Wrap(err, downloadFailure)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return i18n.NewError(downloadFailure)
	}
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(archive, digest), io.LimitReader(response.Body, maxArchiveSize+1))
	if err != nil {
		return i18n.Wrap(err, downloadFailure)
	}
	if size > maxArchiveSize || hex.EncodeToString(digest.Sum(nil)) != asset.digest {
		return i18n.NewError(integrityFailure)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	unpacked, err := gzip.NewReader(archive)
	if err != nil {
		return i18n.Wrap(err, integrityFailure)
	}
	defer unpacked.Close()
	files := tar.NewReader(unpacked)
	header, err := files.Next()
	if err != nil || header.Name != asset.name || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > maxBinarySize {
		return i18n.NewError(integrityFailure)
	}

	binary, err := os.CreateTemp(dir, ".codex-binary-*")
	if err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	defer os.Remove(binary.Name())
	defer binary.Close()
	if _, err := io.Copy(binary, files); err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	if err := binary.Chmod(0o700); err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	if err := binary.Close(); err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(binary.Name(), destination); err != nil {
		return i18n.Wrap(err, saveFailure)
	}
	return nil
}
