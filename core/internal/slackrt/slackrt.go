// Package slackrt listens to Slack's Socket Mode, so Pouple learns about a new message the moment it is posted
// instead of at the next poll. It does not read the messages itself: every event only wakes the inbox (and, for a
// reply in a thread, names the thread), which keeps the single place where "would Slack notify the user?" is decided.
package slackrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const apiBase = "https://slack.com/api"

// Client keeps one Socket Mode connection open and calls OnEvent for every message event.
type Client struct {
	// AppToken is the app-level token (xapp-…) with the connections:write scope. It only goes in a header.
	AppToken string
	HTTP     *http.Client
	// Base is the Slack API address; tests point it at a local server.
	Base string
	// OnEvent is called for each message event (it must not block).
	OnEvent func()
	// OnThread is called, before OnEvent, for each message posted in a thread, with the channel and the thread's first
	// message. Only identifiers are passed: deciding whether the message matters stays with the inbox. It must not block.
	OnThread func(channelID, threadTS string)
	// OnState is called when the connection comes up or goes down.
	OnState func(connected bool, reason string)
	// MaxBackoff caps the wait between two reconnection attempts.
	MaxBackoff time.Duration

	connected atomic.Bool
}

func New(appToken string) *Client {
	return &Client{AppToken: appToken, HTTP: &http.Client{Timeout: 15 * time.Second}, Base: apiBase, MaxBackoff: time.Minute}
}

// Connected tells whether events are flowing right now.
func (c *Client) Connected() bool { return c.connected.Load() }

// Check verifies that Slack accepts the token without starting a WebSocket session.
func (c *Client) Check(ctx context.Context) error {
	_, err := c.open(ctx)
	return err
}

type envelope struct {
	Type       string `json:"type"`
	EnvelopeID string `json:"envelope_id"`
	Payload    struct {
		Event struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			Channel  string `json:"channel"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
		} `json:"event"`
	} `json:"payload"`
	Reason string `json:"reason"`
}

// ErrToken is returned when Slack refuses the app-level token: retrying would not help.
var ErrToken = errors.New("slack refused the app-level token")

// Run connects and reconnects until ctx ends. It returns ErrToken when the token is refused.
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		err := c.session(ctx)
		wasConnected := c.Connected()
		c.setState(false, errText(err))
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrToken) {
			return err
		}
		if wasConnected {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > c.MaxBackoff {
			backoff = c.MaxBackoff
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (c *Client) setState(connected bool, reason string) {
	if c.connected.Swap(connected) != connected && c.OnState != nil {
		c.OnState(connected, reason)
	}
}

// open asks Slack for a one-time WebSocket address.
func (c *Client) open(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/apps.connections.open", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+c.AppToken)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("Slack could not be reached: %w", err)
	}
	defer response.Body.Close()
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		URL   string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&body); err != nil {
		return "", fmt.Errorf("Slack answered something unreadable (HTTP %d)", response.StatusCode)
	}
	if !body.OK {
		switch body.Error {
		case "invalid_auth", "not_authed", "token_revoked", "account_inactive", "missing_scope", "not_allowed_token_type", "app_not_socket_mode_enabled":
			return "", fmt.Errorf("%w (%s)", ErrToken, body.Error)
		}
		return "", fmt.Errorf("Slack refused the connection (%s)", body.Error)
	}
	if body.URL == "" {
		return "", errors.New("Slack returned no live connection URL")
	}
	return body.URL, nil
}

// session runs one connection until it drops.
func (c *Client) session(ctx context.Context) error {
	address, err := c.open(ctx)
	if err != nil {
		return err
	}
	socket, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPClient: c.HTTP})
	if err != nil {
		// The dial error may contain the one-time WebSocket URL.
		return errors.New("the live connection could not be opened")
	}
	defer socket.CloseNow()
	socket.SetReadLimit(1 << 20)
	for {
		_, data, err := socket.Read(ctx)
		if err != nil {
			return err
		}
		var message envelope
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		// Every envelope that carries an id must be acknowledged, or Slack sends it again.
		if message.EnvelopeID != "" {
			ack, _ := json.Marshal(map[string]string{"envelope_id": message.EnvelopeID})
			if err := socket.Write(ctx, websocket.MessageText, ack); err != nil {
				return err
			}
		}
		switch message.Type {
		case "hello":
			c.setState(true, "")
		case "disconnect":
			return fmt.Errorf("Slack asked to reconnect (%s)", message.Reason)
		case "events_api":
			event := message.Payload.Event
			if event.Type == "message" && c.OnThread != nil && isThreadReply(event.Channel, event.TS, event.ThreadTS, event.Subtype) {
				c.OnThread(event.Channel, event.ThreadTS)
			}
			if (event.Type == "message" || event.Type == "app_mention") && c.OnEvent != nil {
				c.OnEvent()
			}
		}
	}
}

// isThreadReply tells whether a message event is a new reply in a thread: it has a thread, is not the thread's first
// message, and is not an edit or a deletion of an earlier message.
func isThreadReply(channelID, ts, threadTS, subtype string) bool {
	if channelID == "" || threadTS == "" || threadTS == ts {
		return false
	}
	return subtype != "message_changed" && subtype != "message_deleted"
}
