// Command pouple-core is the engine behind the Pouple app: it serves the interface and the API on the loopback
// address and runs the assistant. The app starts it, gives it a token, and reads the chosen port from its first
// line of output.
package main

import (
	"bufio"
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
	"sync"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/api"
	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/asana"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/claude"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/codex"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/gitlab"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slack"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
	"github.com/Arthur-MONNET/pouple/core/internal/ui"
	"github.com/Arthur-MONNET/pouple/core/internal/updates"
)

const keychainService = "app.pouple"

func main() {
	if err := run(); err != nil {
		log.Fatalf("pouple-core: %v", err)
	}
}

func supportDir() (string, error) {
	if dir := os.Getenv("POUPLE_HOME"); dir != "" {
		return filepath.Abs(dir)
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

// readShellToken reads the fixed-size startup line without closing the lifetime pipe.
func readShellToken(input *bufio.Reader) (string, error) {
	const tokenLength = 64
	line, err := input.ReadSlice('\n')
	if err != nil || len(line) != tokenLength+1 {
		return "", errors.New("invalid shell token")
	}
	token := string(line[:tokenLength])
	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("invalid shell token")
	}
	return token, nil
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
		// Optional: the assistant reads them as data when they are connected.
		gitlab.New(),
		asana.New(),
		slite.New(),
		claude.New(filepath.Join(dir, "claude")),
	)

	token := os.Getenv("POUPLE_TOKEN")
	// Children (Codex, Claude, npm, brew) must not inherit the API token.
	_ = os.Unsetenv("POUPLE_TOKEN")
	tokenFromShell := os.Getenv("POUPLE_SHELL_INJECTS_TOKEN") == "1"
	input := bufio.NewReaderSize(os.Stdin, 65)
	if tokenFromShell {
		if token, err = readShellToken(input); err != nil {
			return err
		}
	} else if token == "" {
		if token, err = newToken(); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+envOr("POUPLE_PORT", "0"))
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	defer listener.Close()
	var assistantSocket *net.UnixListener
	if tokenFromShell && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
		socket, closeSocket, err := openAssistantSocket(dir)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		if err != nil {
			log.Printf("local assistant API unavailable: %v", err)
		} else {
			defer closeSocket()
			assistantSocket = socket
		}
	}
	service, attachments, err := newAssistant(dir, registry, settings, log.Printf)
	if err != nil {
		return err
	}
	mountAssistant := api.MountAssistant(service, attachments)
	mountSettings := api.MountSettings(settings)
	updateChecker := updates.New(updates.Options{DataDir: dir})
	mountUpdates := api.MountUpdates(updateChecker)
	mountProviders := api.MountProviders(settings, registry, service)
	language := func() string {
		current, err := settings.Load()
		if err != nil || current.UILanguage == "" {
			return "fr"
		}
		return current.UILanguage
	}
	handler := api.New(api.Options{
		Registry: registry, Token: token, UI: ui.Files(), TokenFromShell: tokenFromShell, Language: language,
		Mount: func(mux *http.ServeMux) {
			mountAssistant(mux)
			mountSettings(mux)
			mountProviders(mux)
			mountUpdates(mux)
		},
	}, port)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// No write timeout: event streams stay open until the client disconnects or the core stops.
	newServer := func(handler http.Handler) *http.Server {
		return &http.Server{
			Handler: handler, ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute,
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
	}
	server := newServer(handler)
	var local *http.Server
	if assistantSocket != nil {
		local = newServer(api.LocalAssistant(service, attachments, language))
		go func() {
			if err := local.Serve(assistantListener{assistantSocket}); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("local assistant API: %v", err)
			}
		}()
	}

	// The app reads this line to learn where the core listens.
	ready, _ := json.Marshal(map[string]any{"ready": true, "port": port})
	fmt.Println(string(ready))
	if !tokenFromShell {
		log.Printf("open http://127.0.0.1:%d/ (token generated for this run)", port)
	}

	// The app owns our standard input: when it closes (the app quit or crashed), so do we.
	go func() {
		_, _ = io.Copy(io.Discard, input)
		stop()
	}()
	wake := make(chan struct{}, 1)
	go watchSlackLive(ctx, registry, wake)
	go updateChecker.Run(ctx)
	go runAssistant(ctx, service, settings, wake)
	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var servers sync.WaitGroup
		drain := func(httpServer *http.Server) {
			if err := httpServer.Shutdown(shutdown); err != nil {
				_ = httpServer.Close()
			}
		}
		servers.Go(func() { drain(server) })
		if local != nil {
			servers.Go(func() { drain(local) })
		}
		servers.Wait()
		service.Shutdown()
	}()
	err = server.Serve(listener)
	stop()
	<-shutdownComplete
	if !errors.Is(err, http.ErrServerClosed) {
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
