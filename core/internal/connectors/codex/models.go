package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

const (
	// modelsTTL is how long the catalog is reused: Codex takes seconds to render it, and it rarely changes.
	modelsTTL = 10 * time.Minute
	// modelsTimeout bounds one run of `codex debug models`.
	modelsTimeout = 30 * time.Second
	// maxCatalogBytes bounds what is read from it (the catalog carries long prompts for each model).
	maxCatalogBytes = 16 << 20
)

// Capture runs a script in a login shell and returns what it wrote on its standard output.
type Capture func(ctx context.Context, env []string, script string) ([]byte, error)

func loginShellOutput(ctx context.Context, env []string, script string) ([]byte, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-lc", script)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxCatalogBytes}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// limitedWriter fails once more than left bytes are written, so a runaway output cannot fill the memory.
type limitedWriter struct {
	w    io.Writer
	left int
}

var errTooLarge = errors.New("the output is too large")

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		return 0, errTooLarge
	}
	l.left -= len(p)
	return l.w.Write(p)
}

// modelCache keeps the last catalog read.
type modelCache struct {
	mu     sync.Mutex
	at     time.Time
	models []providers.Suggestion
}

// Models lists the models Codex offers to pick from, in its own order, as its catalog says (`codex debug models`):
// the ones it shows in its own picker, not the hidden ones. The list is kept for a few minutes. An error means the
// catalog could not be read (Codex missing or not signed in): the interface then lets the model be typed.
func (c *Connector) Models(ctx context.Context) ([]providers.Suggestion, error) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	if len(c.cache.models) > 0 && c.now().Sub(c.cache.at) < modelsTTL {
		return append([]providers.Suggestion(nil), c.cache.models...), nil
	}
	capture := c.Capture
	if capture == nil {
		capture = loginShellOutput
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	out, err := capture(ctx, c.env(), c.command()+" debug models")
	if err != nil {
		return nil, err
	}
	models, err := parseCatalog(out)
	if err != nil {
		return nil, err
	}
	c.cache.models, c.cache.at = models, c.now()
	return append([]providers.Suggestion(nil), models...), nil
}

func (c *Connector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// parseCatalog reads the JSON that `codex debug models` prints. A login shell may print something before it (a
// profile's greeting), so the reading starts at the first brace.
func parseCatalog(out []byte) ([]providers.Suggestion, error) {
	start := bytes.IndexByte(out, '{')
	if start < 0 {
		return nil, errors.New("the model catalog is not JSON")
	}
	var catalog struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Visibility  string `json:"visibility"`
			Priority    int    `json:"priority"`
		} `json:"models"`
	}
	if err := json.NewDecoder(bytes.NewReader(out[start:])).Decode(&catalog); err != nil {
		return nil, err
	}
	listed := catalog.Models[:0]
	for _, model := range catalog.Models {
		if model.Visibility == "list" && model.Slug != "" && providers.ValidModel(model.Slug) {
			listed = append(listed, model)
		}
	}
	sort.SliceStable(listed, func(a, b int) bool { return listed[a].Priority < listed[b].Priority })
	suggestions := make([]providers.Suggestion, 0, len(listed))
	for _, model := range listed {
		label := strings.TrimSpace(model.DisplayName)
		if label == "" {
			label = model.Slug
		}
		if description := strings.TrimSpace(model.Description); description != "" {
			label += " — " + description
		}
		suggestions = append(suggestions, providers.Suggestion{ID: model.Slug, Label: label})
	}
	if len(suggestions) == 0 {
		return nil, errors.New("the model catalog is empty")
	}
	return suggestions, nil
}
