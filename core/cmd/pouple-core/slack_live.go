package main

import (
	"context"
	"log"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slack"
	"github.com/Arthur-MONNET/pouple/core/internal/slackinbox"
	"github.com/Arthur-MONNET/pouple/core/internal/slackrt"
)

// watchSlackLive follows the saved Slack connection so setup and token replacement take effect without a restart.
func watchSlackLive(ctx context.Context, registry *connector.Registry, wake chan<- struct{}, hints *slackinbox.ThreadHints) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var connectedAt *time.Time
	var stopSession func()
	stop := func() {
		if stopSession != nil {
			stopSession()
			stopSession = nil
		}
	}
	defer stop()
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	for ctx.Err() == nil {
		status, err := registry.Status(slack.ID)
		switch {
		case err == nil && status.ConnectedAt == nil:
			stop()
			connectedAt = nil
		case err == nil && (connectedAt == nil || !status.ConnectedAt.Equal(*connectedAt)):
			stop()
			values, err := registry.Values(slack.ID)
			if err == nil {
				connectedAt = status.ConnectedAt
				signal()
				if token := values[slack.AppTokenField]; token != "" {
					stopSession = startSlackLive(ctx, token, signal, hints)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func startSlackLive(parent context.Context, token string, signal func(), hints *slackinbox.ThreadHints) func() {
	client := slackrt.New(token)
	client.OnThread = hints.Add
	client.OnEvent = signal
	client.OnState = func(connected bool, _ string) {
		if connected {
			signal()
		}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := client.Run(ctx); err != nil {
			log.Print("Slack live updates stopped: check the app-level token. Polling remains active.")
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
