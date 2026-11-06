package slackrt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeSlack serves apps.connections.open and a WebSocket that plays a script of envelopes.
type fakeSlack struct {
	server    *httptest.Server
	opens     atomic.Int32
	acks      chan string
	script    func(ctx context.Context, socket *websocket.Conn)
	tokenSeen atomic.Value
}

func newFake(t *testing.T, script func(ctx context.Context, socket *websocket.Conn)) *fakeSlack {
	t.Helper()
	f := &fakeSlack{acks: make(chan string, 16), script: script}
	mux := http.NewServeMux()
	mux.HandleFunc("/apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		f.opens.Add(1)
		f.tokenSeen.Store(r.Header.Get("Authorization"))
		if strings.Contains(r.URL.String(), "xapp") {
			t.Errorf("the token must never be in the URL: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer xapp-good" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(f.server.URL, "http") + "/socket"})
	})
	mux.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			for {
				_, data, err := socket.Read(ctx)
				if err != nil {
					return
				}
				var ack struct {
					EnvelopeID string `json:"envelope_id"`
				}
				_ = json.Unmarshal(data, &ack)
				f.acks <- ack.EnvelopeID
			}
		}()
		f.script(ctx, socket)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func send(ctx context.Context, socket *websocket.Conn, v any) {
	data, _ := json.Marshal(v)
	_ = socket.Write(ctx, websocket.MessageText, data)
}

func messageEvent(id string) map[string]any {
	return map[string]any{"type": "events_api", "envelope_id": id, "payload": map[string]any{"event": map[string]any{"type": "message"}}}
}

func TestEventsWakeTheInboxAndAreAcknowledged(t *testing.T) {
	f := newFake(t, func(ctx context.Context, socket *websocket.Conn) {
		send(ctx, socket, map[string]any{"type": "hello"})
		send(ctx, socket, messageEvent("e1"))
		send(ctx, socket, map[string]any{"type": "events_api", "envelope_id": "e2", "payload": map[string]any{"event": map[string]any{"type": "reaction_added"}}})
		<-ctx.Done()
	})
	var wakes atomic.Int32
	states := make(chan bool, 4)
	c := &Client{AppToken: "xapp-good", HTTP: f.server.Client(), Base: f.server.URL, MaxBackoff: 50 * time.Millisecond,
		OnEvent: func() { wakes.Add(1) }, OnState: func(up bool, _ string) { states <- up }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if up := <-states; !up {
		t.Fatal("expected the connection to come up")
	}
	got := map[string]bool{<-f.acks: true, <-f.acks: true}
	if !got["e1"] || !got["e2"] {
		t.Fatalf("every envelope must be acknowledged: %v", got)
	}
	if wakes.Load() != 1 {
		t.Fatalf("only message events wake the inbox, got %d", wakes.Load())
	}
	if !c.Connected() {
		t.Fatal("Connected should be true")
	}
}

func TestReconnectsAfterADisconnectRequest(t *testing.T) {
	var sessions atomic.Int32
	var mu sync.Mutex
	f := newFake(t, func(ctx context.Context, socket *websocket.Conn) {
		mu.Lock()
		n := sessions.Add(1)
		mu.Unlock()
		send(ctx, socket, map[string]any{"type": "hello"})
		if n == 1 {
			send(ctx, socket, map[string]any{"type": "disconnect", "reason": "refresh_requested"})
			return
		}
		send(ctx, socket, messageEvent("again"))
		<-ctx.Done()
	})
	woke := make(chan struct{}, 2)
	c := &Client{AppToken: "xapp-good", HTTP: f.server.Client(), Base: f.server.URL, MaxBackoff: 20 * time.Millisecond, OnEvent: func() { woke <- struct{}{} }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	select {
	case <-woke:
	case <-time.After(5 * time.Second):
		t.Fatal("no event after the reconnection")
	}
	if f.opens.Load() < 2 {
		t.Fatalf("expected a second connection, got %d", f.opens.Load())
	}
}

func TestARefusedTokenStopsInsteadOfRetrying(t *testing.T) {
	f := newFake(t, func(context.Context, *websocket.Conn) {})
	c := &Client{AppToken: "xapp-bad", HTTP: f.server.Client(), Base: f.server.URL, MaxBackoff: 10 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "app-level token") {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run kept retrying a refused token")
	}
	if f.opens.Load() != 1 {
		t.Fatalf("a refused token must not be retried, %d attempts", f.opens.Load())
	}
}

func TestRunEndsWhenTheContextEnds(t *testing.T) {
	f := newFake(t, func(ctx context.Context, socket *websocket.Conn) {
		send(ctx, socket, map[string]any{"type": "hello"})
		<-ctx.Done()
	})
	c := &Client{AppToken: "xapp-good", HTTP: f.server.Client(), Base: f.server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a normal stop is not an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}
