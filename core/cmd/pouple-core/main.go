// Command pouple-core is the engine behind the Pouple app: it serves the interface and the API on the loopback
// address and runs the assistant. The app starts it, gives it a token, and reads the chosen port from its first
// line of output.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/api"
	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/codex"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slack"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
	"github.com/Arthur-MONNET/pouple/core/internal/ui"
)

const keychainService = "app.pouple"

func main() {
	if err := run(); err != nil {
		log.Fatalf("pouple-core: %v", err)
	}
}

func supportDir() (string, error) {
	if dir := os.Getenv("POUPLE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Pouple"), nil
}

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func run() error {
	dir, err := supportDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	var vault secrets.Store = secrets.NewMemory()
	if runtime.GOOS == "darwin" {
		vault = secrets.NewKeychain(keychainService)
	} else {
		log.Print("no keychain on this system: secrets are kept in memory only")
	}

	settings := config.Open(filepath.Join(dir, "config.json"))
	registry := connector.NewRegistry(vault, config.Connectors{File: settings},
		slack.New(),
		codex.New(filepath.Join(dir, "codex")),
	)

	token := os.Getenv("POUPLE_TOKEN")
	// Children (Codex, npm, brew) must not inherit the API token.
	_ = os.Unsetenv("POUPLE_TOKEN")
	tokenFromShell := os.Getenv("POUPLE_SHELL_INJECTS_TOKEN") == "1"
	if token == "" {
		if token, err = newToken(); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+envOr("POUPLE_PORT", "0"))
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	service, attachments, err := newAssistant(dir, registry, settings, log.Printf)
	if err != nil {
		return err
	}
	mountAssistant := api.MountAssistant(service, attachments)
	mountSettings := api.MountSettings(settings)
	handler := api.New(api.Options{
		Registry: registry, Token: token, UI: ui.Files(), TokenFromShell: tokenFromShell,
		Language: func() string {
			current, err := settings.Load()
			if err != nil || current.UILanguage == "" {
				return "fr"
			}
			return current.UILanguage
		},
		Mount: func(mux *http.ServeMux) { mountAssistant(mux); mountSettings(mux) },
	}, port)
	// No write timeout: the event stream stays open on purpose.
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute}

	// The app reads this line to learn where the core listens.
	ready, _ := json.Marshal(map[string]any{"ready": true, "port": port})
	fmt.Println(string(ready))
	if !tokenFromShell {
		log.Printf("open http://127.0.0.1:%d/ (token generated for this run)", port)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The app owns our standard input: when it closes (the app quit or crashed), so do we.
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		stop()
	}()
	go runAssistant(ctx, service, settings)
	go func() {
		<-ctx.Done()
		service.Shutdown()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
