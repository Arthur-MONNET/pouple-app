package screenshot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// A minimal client of the Chrome DevTools Protocol: enough to open a page in a tab, check every request it makes, wait
// until it is quiet and take a picture of it. The protocol is JSON, over a pipe the browser shares with Pouple alone
// (--remote-debugging-pipe): no network port exists that another program could connect to. Messages end with a NUL byte.

type size struct {
	width, height int
	mobile        bool
}

type message struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type connection struct {
	out     io.Writer
	wmu     sync.Mutex
	mu      sync.Mutex
	next    int
	waiting map[int]chan message
	events  chan message
	done    chan struct{}
	failure error
}

func newConnection(in io.Reader, out io.Writer) *connection {
	c := &connection{out: out, waiting: map[int]chan message{}, events: make(chan message, 1024), done: make(chan struct{})}
	go c.read(in)
	return c
}

func (c *connection) read(in io.Reader) {
	defer close(c.done)
	reader := bufio.NewReaderSize(in, 1<<20)
	for {
		data, err := reader.ReadBytes(0)
		if err != nil {
			c.mu.Lock()
			c.failure = err
			c.mu.Unlock()
			return
		}
		var m message
		if json.Unmarshal(data[:len(data)-1], &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			reply := c.waiting[m.ID]
			delete(c.waiting, m.ID)
			c.mu.Unlock()
			if reply != nil {
				reply <- m
			}
			continue
		}
		select {
		case c.events <- m:
		default: // nobody follows events any more: drop them rather than stop the connection
		}
	}
}

// call sends a command and returns its result. sessionID "" addresses the browser itself.
func (c *connection) call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	reply := make(chan message, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	c.waiting[id] = reply
	c.mu.Unlock()
	command, _ := json.Marshal(message{ID: id, Method: method, Params: raw, SessionID: sessionID})
	c.wmu.Lock()
	_, err = c.out.Write(append(command, 0))
	c.wmu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case m := <-reply:
		if m.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return nil, errors.Join(errors.New("the browser closed the connection"), c.failure)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

const (
	// quiet is how long a page must make no request before it counts as loaded.
	quiet = 600 * time.Millisecond
	// settleLimit bounds the wait for a page that never stops requesting (polling, websockets).
	settleLimit = 8 * time.Second
)

// policy decides, for one request of the page, whether the browser may make it; foreign says it goes beyond this
// computer.
type policy func(rawURL, resourceType string) (allow, foreign bool)

// pageState is what the events of a tab tell, shared between the goroutine that follows them and the one that waits.
type pageState struct {
	mu          sync.Mutex
	loaded      bool
	loadedAt    time.Time
	inflight    map[string]bool
	quietSince  time.Time
	blockedSome bool
}

// capture opens address in a new tab of the browser behind c and returns a PNG of it.
func capture(ctx context.Context, c *connection, address string, screen size, observer *Observer, allow policy) ([]byte, error) {
	// Whatever the page tries to download is refused.
	if _, err := c.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}); err != nil {
		return nil, err
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	result, err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil || json.Unmarshal(result, &created) != nil || created.TargetID == "" {
		return nil, errors.Join(errors.New("no tab could be opened"), err)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	result, err = c.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": created.TargetID, "flatten": true})
	if err != nil || json.Unmarshal(result, &attached) != nil || attached.SessionID == "" {
		return nil, errors.Join(errors.New("the tab could not be reached"), err)
	}
	session := attached.SessionID

	state := &pageState{inflight: map[string]bool{}}
	pumpCtx, stopPump := context.WithCancel(ctx)
	defer stopPump()
	go c.pump(pumpCtx, session, state, observer, allow)

	steps := []struct {
		method string
		params any
	}{
		{"Page.enable", map[string]any{}},
		{"Network.enable", map[string]any{}},
		// Every request of the page, redirects and frames included, is looked at before it leaves.
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*"}}}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": screen.width, "height": screen.height, "deviceScaleFactor": scale(screen), "mobile": screen.mobile}},
	}
	if screen.mobile {
		steps = append(steps, struct {
			method string
			params any
		}{"Emulation.setUserAgentOverride", map[string]any{"userAgent": mobileAgent}})
	}
	for _, step := range steps {
		if _, err := c.call(ctx, session, step.method, step.params); err != nil {
			return nil, err
		}
	}
	// The browser draws what it shows to whoever watches, as it goes.
	if observer != nil && observer.Frame != nil {
		if _, err := c.call(ctx, session, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 55, "everyNthFrame": 1}); err != nil {
			return nil, err
		}
	}
	observer.step("Opening the page")
	if _, err := c.call(ctx, session, "Page.navigate", map[string]any{"url": address}); err != nil {
		return nil, err
	}
	if err := state.settle(ctx, c, observer); err != nil {
		return nil, err
	}
	if observer != nil && observer.Frame != nil {
		_, _ = c.call(ctx, session, "Page.stopScreencast", map[string]any{})
	}
	observer.step("Taking the picture")
	result, err = c.call(ctx, session, "Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	var shot struct {
		Data []byte `json:"data"` // base64 in the JSON: decoded by the encoding
	}
	if err := json.Unmarshal(result, &shot); err != nil {
		return nil, err
	}
	return shot.Data, nil
}

func scale(screen size) int {
	if screen.mobile {
		return 2
	}
	return 1
}

// pump follows the events of the tab: it answers every request the page makes, passes the frames on, and keeps the
// state that tells when the page is quiet.
func (c *connection) pump(ctx context.Context, session string, state *pageState, observer *Observer, allow policy) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case m := <-c.events:
			if m.SessionID != session {
				continue
			}
			var params struct {
				RequestID    string `json:"requestId"`
				ResourceType string `json:"resourceType"`
				Request      struct {
					URL string `json:"url"`
				} `json:"request"`
				Data  []byte `json:"data"`
				AckID int    `json:"sessionId"`
			}
			_ = json.Unmarshal(m.Params, &params)
			switch m.Method {
			case "Fetch.requestPaused":
				if ok, foreign := allow(params.Request.URL, params.ResourceType); ok {
					if foreign {
						observer.foreign()
					}
					_, _ = c.call(ctx, session, "Fetch.continueRequest", map[string]any{"requestId": params.RequestID})
				} else {
					state.mu.Lock()
					state.blockedSome = true
					state.mu.Unlock()
					_, _ = c.call(ctx, session, "Fetch.failRequest", map[string]any{"requestId": params.RequestID, "errorReason": "BlockedByClient"})
				}
			case "Page.screencastFrame":
				observer.frame(params.Data)
				// The browser sends the next frame only once the last one was acknowledged.
				_, _ = c.call(ctx, session, "Page.screencastFrameAck", map[string]any{"sessionId": params.AckID})
			case "Page.loadEventFired":
				observer.step("Waiting for the page to settle")
				state.mu.Lock()
				state.loaded, state.loadedAt, state.quietSince = true, time.Now(), time.Now()
				state.mu.Unlock()
			case "Network.requestWillBeSent":
				state.mu.Lock()
				state.inflight[params.RequestID] = true
				state.quietSince = time.Now()
				state.mu.Unlock()
			case "Network.loadingFinished", "Network.loadingFailed":
				state.mu.Lock()
				delete(state.inflight, params.RequestID)
				state.quietSince = time.Now()
				state.mu.Unlock()
			}
		}
	}
}

// settle waits for the page to load, then for it to stop requesting things for a moment, so what is drawn is what the
// user would see and not the page half way.
func (s *pageState) settle(ctx context.Context, c *connection, observer *Observer) error {
	deadline := time.After(settleLimit + 15*time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return errors.New("the browser closed the connection")
		case <-deadline:
			return errors.New("the page never finished loading")
		case <-tick.C:
			s.mu.Lock()
			loaded, pending, since, at := s.loaded, len(s.inflight), s.quietSince, s.loadedAt
			s.mu.Unlock()
			if loaded && ((pending == 0 && time.Since(since) >= quiet) || time.Since(at) >= settleLimit) {
				return nil
			}
		}
	}
}
