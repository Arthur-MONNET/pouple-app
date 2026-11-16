package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The text of a reply is written in the user's own style, learned from their past Slack messages: tone, length,
// tutoiement. What is learned is a short description, kept on this computer only; the user can switch it off and make
// Pouple forget it. Replies are never sent by Pouple: the user copies and posts them.

const (
	// styleMessages is how many of the user's messages are read to learn it, and styleMinimum how few is too few.
	styleMessages = 80
	styleMinimum  = 15
	// styleRefresh is how old a learned style gets before it is learned again.
	styleRefresh = 14 * 24 * time.Hour
	// styleRetry is how long to wait after a failed attempt.
	styleRetry    = 6 * time.Hour
	maxStyleChars = 1200
)

// StyleInfo is what is known of the learned style, without the style itself.
type StyleInfo struct {
	Learned   bool   `json:"learned"`
	LearnedAt string `json:"learnedAt,omitempty"`
	// From is how many messages it was learned from.
	From int `json:"from,omitempty"`
}

type styleFile struct {
	Profile   string `json:"profile"`
	LearnedAt string `json:"learnedAt"`
	From      int    `json:"from"`
}

// StyleStore keeps the learned style in a small file.
type StyleStore struct {
	path string
	mu   sync.Mutex
	data styleFile
}

// NewStyleStore opens the file; a missing or unreadable one is no style.
func NewStyleStore(path string) *StyleStore {
	store := &StyleStore{path: path}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &store.data)
	}
	return store
}

// Profile is the learned description of the user's style; "" when none.
func (s *StyleStore) Profile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Profile
}

// Info says whether a style is known, and since when.
func (s *StyleStore) Info() StyleInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StyleInfo{Learned: s.data.Profile != "", LearnedAt: s.data.LearnedAt, From: s.data.From}
}

func (s *StyleStore) learnedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, _ := time.Parse(time.RFC3339, s.data.LearnedAt)
	return at
}

// Set keeps a newly learned style.
func (s *StyleStore) Set(profile string, from int, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = styleFile{Profile: strings.TrimSpace(profile), LearnedAt: now.UTC().Format(time.RFC3339), From: from}
	return s.saveLocked()
}

// Forget removes what was learned.
func (s *StyleStore) Forget() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = styleFile{}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *StyleStore) saveLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

// StyleLearner learns the user's style from their own Slack messages, at first and then every few weeks.
type StyleLearner struct {
	Store *StyleStore
	// Messages returns the user's latest messages, newest first.
	Messages func(ctx context.Context, limit int) ([]string, error)
	// Ask runs one read-only turn of the AI and returns its answer.
	Ask func(ctx context.Context, prompt string) (string, error)
	// Enabled says whether the user wants their style learned; Connected whether Slack and an AI are ready.
	Enabled, Connected func() bool
	Persona            func() Persona
	Now                func() time.Time
	// OnError is told when an attempt fails.
	OnError func(error)
}

// Run checks every hour whether the style must be learned, until ctx ends.
func (l *StyleLearner) Run(ctx context.Context) {
	for ctx.Err() == nil {
		wait := time.Hour
		if l.due() {
			if err := l.Learn(ctx); err != nil {
				wait = styleRetry
				if l.OnError != nil && ctx.Err() == nil {
					l.OnError(err)
				}
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func (l *StyleLearner) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *StyleLearner) due() bool {
	if !l.Enabled() || !l.Connected() {
		return false
	}
	at := l.Store.learnedAt()
	return l.Store.Profile() == "" || at.IsZero() || l.now().Sub(at) > styleRefresh
}

// Learn reads the user's messages and keeps a description of how they write. Too few messages learn nothing.
func (l *StyleLearner) Learn(ctx context.Context) error {
	messages, err := l.Messages(ctx, styleMessages)
	if err != nil {
		return err
	}
	if len(messages) < styleMinimum {
		return nil
	}
	answer, err := l.Ask(ctx, l.Persona().StylePrompt(messages))
	if err != nil {
		return err
	}
	profile := strings.TrimSpace(answer)
	if profile == "" {
		return nil
	}
	if runes := []rune(profile); len(runes) > maxStyleChars {
		profile = string(runes[:maxStyleChars])
	}
	return l.Store.Set(profile, len(messages), l.now())
}
