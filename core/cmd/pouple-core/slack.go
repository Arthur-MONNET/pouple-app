package main

import (
	"context"
	"sync"

	"github.com/Arthur-MONNET/pouple/core/internal/config"
	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/slackinbox"
)

// slackSource adapts the Slack inbox to the engine. The client is built from the keychain at the time it is needed,
// so a token connected or replaced while Pouple runs takes effect at the next poll.
type slackSource struct {
	registry *connector.Registry
	settings *config.File
	// hints are the threads live events reported; they outlive the inbox, which is rebuilt when the token changes.
	hints *slackinbox.ThreadHints

	mu     sync.Mutex
	token  string
	client *slackinbox.Client
	inbox  *slackinbox.Inbox
}

func (s *slackSource) current() (*slackinbox.Client, *slackinbox.Inbox, error) {
	values, err := s.registry.Values("slack")
	if err != nil {
		return nil, nil, err
	}
	settings, err := s.settings.Load()
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The inbox keeps its own caches, so it is rebuilt only when what it depends on changed.
	s.client = slackinbox.NewClient(values["token"], nil, "")
	s.token = values["token"]
	s.inbox = slackinbox.NewInbox(s.client, slackinbox.Options{IgnoreChannels: settings.IgnoreChannels, Keywords: settings.Keywords, Hints: s.hints})
	return s.client, s.inbox, nil
}

func (s *slackSource) cached() (*slackinbox.Client, *slackinbox.Inbox, error) {
	values, err := s.registry.Values("slack")
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	ready := s.inbox != nil && s.token == values["token"]
	client, inbox := s.client, s.inbox
	s.mu.Unlock()
	if ready {
		return client, inbox, nil
	}
	return s.current()
}

func (s *slackSource) Poll(ctx context.Context, cursor *engine.Cursor) (engine.PollResult, error) {
	_, inbox, err := s.cached()
	if err != nil {
		return engine.PollResult{}, err
	}
	var from *slackinbox.Cursor
	if cursor != nil {
		from = &slackinbox.Cursor{Since: cursor.Since, Seen: cursor.Seen}
	}
	poll, err := inbox.Poll(ctx, from)
	if err != nil {
		return engine.PollResult{}, err
	}
	result := engine.PollResult{Cursor: engine.Cursor{Since: poll.Cursor.Since, Seen: poll.Cursor.Seen}, Notices: poll.Notices}
	for _, in := range poll.Incoming {
		incoming := engine.Incoming{Source: engine.Source{Origin: string(in.Source.Origin), ChannelID: in.Source.ChannelID, ChannelName: in.Source.ChannelName, SenderID: in.Source.SenderID, SenderName: in.Source.SenderName, TS: in.Source.TS, Permalink: in.Source.Permalink}, Text: in.Text, Context: in.Context}
		for _, file := range in.Files {
			incoming.Files = append(incoming.Files, engine.PendingFile{ID: file.ID, Name: file.Name, Mime: file.Mime, URL: file.URL, Size: file.Size})
		}
		result.Incoming = append(result.Incoming, incoming)
	}
	return result, nil
}

func (s *slackSource) Fetch(ctx context.Context, file engine.PendingFile) ([]byte, error) {
	client, _, err := s.cached()
	if err != nil {
		return nil, err
	}
	return client.DownloadFile(ctx, slackinbox.FileRef{ID: file.ID, Name: file.Name, Mime: file.Mime, URL: file.URL, Size: file.Size})
}
