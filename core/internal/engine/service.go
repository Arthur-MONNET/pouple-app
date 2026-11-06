package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Errors the API maps to HTTP statuses.
var (
	ErrNotFound = errors.New("request not found")
	ErrState    = errors.New("not possible in the current state")
	ErrInput    = errors.New("invalid input")
	ErrInactive = errors.New("the assistant is not running")
)

// Inbox finds the Slack messages that would notify the user.
type Inbox interface {
	Poll(ctx context.Context, cursor *Cursor) (PollResult, error)
}

// Incoming is a message the inbox found.
type Incoming struct {
	Source  Source
	Text    string
	Context string
	Files   []PendingFile
}

// PollResult is what one poll found.
type PollResult struct {
	Incoming []Incoming
	Cursor   Cursor
	Notices  []string
}

// Deps is everything the service needs from the outside.
type Deps struct {
	Store       *Store
	Inbox       Inbox
	Codex       Codex
	Attachments *Attachments
	// Fetch downloads a Slack file; nil when Slack is not connected.
	Fetch func(ctx context.Context, file PendingFile) ([]byte, error)
	// Persona says who the assistant works for; read at each prompt, so a change applies at once.
	Persona func() Persona
	// ProjectsRoot is the folder holding one folder per project; "" means none is chosen yet.
	ProjectsRoot func() string
	// WorkDir is where read-only investigations run (usually ProjectsRoot, or a scratch folder).
	WorkDir    func() string
	SchemaFile string
	Notify     func(title, body string)
	Redact     func() Redactor
	Usage      func() Usage
	// Inactive returns why the assistant cannot run ("" when it can).
	Inactive func() string
	Now      func() time.Time
	// SettleDelay is how long a request from someone else waits before its turn, so a burst of messages is one request.
	SettleDelay         time.Duration
	AutoApproveOwnNotes func() bool
	// ChangeSummary says what git sees changed in a folder.
	ChangeSummary func(ctx context.Context, dir string) (string, error)
	OnError       func(error)
}

const (
	maxQueued         = 20
	mergeWindow       = 3 * time.Minute
	maxMergedChars    = 4000
	staleAfter        = 7 * 24 * time.Hour
	limitPause        = 30 * time.Minute
	historyRequests   = 5
	maxFilesPerMsg    = 6
	maxAttachmentText = 8000
	maxMessageChars   = 4000
)

// Service is the assistant.
type Service struct {
	deps  Deps
	queue *Queue

	mu           sync.Mutex
	notices      []string
	lastPollAt   *string
	pausedUntil  time.Time
	loginProblem string
	fileNotice   string
	running      map[string]context.CancelFunc
	stops        map[string]int
	subscribers  map[chan struct{}]struct{}
	closing      bool
}

func NewService(deps Deps) *Service {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Redact == nil {
		deps.Redact = func() Redactor { return NewRedactor() }
	}
	if deps.Persona == nil {
		deps.Persona = func() Persona { return Persona{} }
	}
	if deps.ChangeSummary == nil {
		deps.ChangeSummary = gitChanges
	}
	if deps.OnError == nil {
		deps.OnError = func(error) {}
	}
	s := &Service{deps: deps, queue: NewQueue(), running: map[string]context.CancelFunc{}, stops: map[string]int{}, subscribers: map[chan struct{}]struct{}{}}
	deps.Store.OnChange(s.broadcast)
	return s
}

func gitChanges(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain").Output()
	return strings.TrimSpace(string(out)), err
}

// Idle waits until everything queued so far is done and written (tests and shutdown).
func (s *Service) Idle() {
	s.queue.Wait()
	s.deps.Store.Flush()
}

// Subscribe returns a channel that receives a signal after each change, and a function to stop listening.
func (s *Service) Subscribe() (<-chan struct{}, func()) {
	channel := make(chan struct{}, 1)
	s.mu.Lock()
	s.subscribers[channel] = struct{}{}
	s.mu.Unlock()
	return channel, func() {
		s.mu.Lock()
		delete(s.subscribers, channel)
		s.mu.Unlock()
	}
}

func (s *Service) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for channel := range s.subscribers {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

func (s *Service) now() string { return stamp(s.deps.Now()) }

func newID() string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

func (s *Service) entry(role, kind, text string) Entry {
	return Entry{ID: newID(), At: s.now(), Role: role, Kind: kind, Text: text}
}

func (s *Service) update(id string, change func(*Stored)) {
	s.deps.Store.Update(id, func(r *Stored) {
		change(r)
		r.UpdatedAt = s.now()
	})
}

func (s *Service) require(id string) (Stored, error) {
	request, ok := s.deps.Store.Get(id)
	if !ok {
		return Stored{}, ErrNotFound
	}
	return request, nil
}

func (s *Service) requireActive() error {
	if reason := s.deps.Inactive(); reason != "" {
		return fmt.Errorf("%w: %s", ErrInactive, reason)
	}
	return nil
}

// Snapshot is the whole state, as the interface reads it.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	notices := append([]string(nil), s.notices...)
	for _, standing := range s.standingNoticesLocked() {
		notices = append(notices, standing)
	}
	lastPoll := s.lastPollAt
	s.mu.Unlock()

	status := StatusInfo{Active: true, Notices: notices, LastPollAt: lastPoll, Usage: s.deps.Usage()}
	if reason := s.deps.Inactive(); reason != "" {
		status.Active, status.Problem = false, ptr(reason)
	}
	stored := s.deps.Store.List()
	requests := make([]Request, len(stored))
	for i, request := range stored {
		view := request.Request
		view.Continuable = request.ThreadID != ""
		requests[i] = view
	}
	if notices == nil {
		status.Notices = []string{}
	}
	return Snapshot{Status: status, Requests: requests}
}

func (s *Service) standingNoticesLocked() []string {
	var out []string
	if s.fileNotice != "" {
		out = append(out, s.fileNotice)
	}
	if s.loginProblem != "" {
		out = append(out, s.loginProblem)
	}
	if s.deps.Now().Before(s.pausedUntil) {
		out = append(out, "Codex's usage limit is reached: new Slack requests wait until "+s.pausedUntil.Local().Format("15:04")+". What you ask yourself still goes through.")
	}
	return out
}

// Run polls Slack every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval func() time.Duration) {
	for {
		s.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval()):
		}
	}
}

// Poll reads Slack once and turns what is new into requests.
func (s *Service) Poll(ctx context.Context) {
	if s.deps.Inactive() != "" || s.deps.Inbox == nil {
		return
	}
	result, err := s.deps.Inbox.Poll(ctx, s.deps.Store.Cursor())
	if err != nil {
		s.mu.Lock()
		s.notices = []string{"Slack could not be read: " + err.Error()}
		s.mu.Unlock()
		s.broadcast()
		return
	}
	s.mu.Lock()
	s.notices = result.Notices
	s.lastPollAt = ptr(s.now())
	s.mu.Unlock()
	s.deps.Store.SetCursor(result.Cursor)
	for _, incoming := range result.Incoming {
		s.receive(incoming)
	}
	s.expireStale()
}

func (s *Service) receive(in Incoming) {
	if s.mergeIntoWaiting(in) {
		return
	}
	received := s.now()
	request := Stored{Request: Request{
		ID: newID(), Source: in.Source, Text: in.Text, Context: in.Context, ReceivedAt: received, UpdatedAt: received,
		Status: StatusQueued, Questions: []Question{}, Entries: []Entry{},
	}, PendingFiles: in.Files}
	ownNote := in.Source.Origin == "note-to-self"
	skip := func(reason string) {
		request.Status = StatusIgnored
		request.Entries = []Entry{s.entry("system", "text", reason)}
		s.deps.Store.Add(request)
	}
	if !ownNote && len(in.Files) == 0 && IsSmallTalk(in.Text) {
		skip("Skipped: nothing to answer.")
		return
	}
	waiting := 0
	for _, other := range s.deps.Store.List() {
		if other.Status == StatusQueued {
			waiting++
		}
	}
	if waiting >= maxQueued {
		skip("Skipped: too many requests were waiting.")
		return
	}
	s.deps.Store.Add(request)
	// A note to oneself is answered at once: the user is waiting for it. Others wait a moment, to see if more follows.
	priority, delay := Background, s.deps.SettleDelay
	if ownNote {
		priority, delay = Interactive, 0
	}
	start := func() {
		if current, ok := s.deps.Store.Get(request.ID); ok && current.Status == StatusQueued {
			s.enqueue(request.ID, priority, func(ctx context.Context) error { return s.analyze(ctx, request.ID) })
		}
	}
	if delay > 0 {
		time.AfterFunc(delay, start)
	} else {
		start()
	}
}

// mergeIntoWaiting joins a message to the request of the same person, in the same place, that still waits.
func (s *Service) mergeIntoWaiting(in Incoming) bool {
	if in.Source.Origin == "note-to-self" {
		return false
	}
	now := s.deps.Now()
	for _, candidate := range s.deps.Store.List() {
		received, err := time.Parse(time.RFC3339, candidate.ReceivedAt)
		if candidate.Status != StatusQueued || err != nil || now.Sub(received) >= mergeWindow {
			continue
		}
		if candidate.Source.ChannelID != in.Source.ChannelID || candidate.Source.SenderID != in.Source.SenderID || candidate.Source.Origin == "cockpit-action" {
			continue
		}
		s.update(candidate.ID, func(r *Stored) {
			text := r.Text + "\n" + in.Text
			if len(text) > maxMergedChars {
				text = text[:maxMergedChars]
			}
			r.Text = text
			r.PendingFiles = append(r.PendingFiles, in.Files...)
		})
		return true
	}
	return false
}

func (s *Service) expireStale() {
	limit := s.deps.Now().Add(-staleAfter)
	for _, request := range s.deps.Store.List() {
		updated, err := time.Parse(time.RFC3339, request.UpdatedAt)
		if request.Status == StatusAwaitingUser && err == nil && updated.Before(limit) {
			s.update(request.ID, func(r *Stored) {
				r.Status, r.Questions, r.Proposal = StatusIgnored, []Question{}, nil
				r.Entries = append(r.Entries, s.entry("system", "text", "Expired: no answer for 7 days."))
			})
		}
	}
}

// enqueue runs a job in the Codex queue, with a context that Stop can cancel.
func (s *Service) enqueue(id string, priority Priority, job func(ctx context.Context) error) {
	s.mu.Lock()
	seen := s.stops[id]
	s.mu.Unlock()
	s.queue.Add(priority, func() {
		s.mu.Lock()
		if s.stops[id] != seen || s.closing {
			s.mu.Unlock()
			return // stopped while it waited: it never starts
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.running[id] = cancel
		s.mu.Unlock()
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.running, id)
			s.mu.Unlock()
		}()
		if err := job(ctx); err != nil {
			s.fail(id, err)
		}
	})
}

// Stop ends what the assistant is doing for a request: waiting, investigating or writing.
func (s *Service) Stop(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if !request.Status.Pending() {
		return fmt.Errorf("%w: nothing is running for this request", ErrState)
	}
	s.mu.Lock()
	s.stops[id]++
	cancel := s.running[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel() // the Codex process ends; its turn then fails with ErrCancelled, which settles the request
		return nil
	}
	s.update(id, func(r *Stored) {
		r.Status = StatusIgnored
		r.Entries = append(r.Entries, s.entry("system", "text", "Stopped before it started."))
	})
	return nil
}

// Shutdown ends the Codex turns that are running and writes everything down. What was in progress stays as it is,
// so Recover picks it up at the next start.
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.closing = true
	for _, cancel := range s.running {
		cancel()
	}
	s.mu.Unlock()
	s.queue.Wait()
	s.deps.Store.Flush()
}

func (s *Service) fail(id string, cause error) {
	if errors.Is(cause, ErrCancelled) {
		s.mu.Lock()
		closing := s.closing
		s.mu.Unlock()
		if !closing {
			s.settleStopped(id)
		}
		return
	}
	var quota *QuotaError
	quiet := errors.As(cause, &quota) || errors.Is(cause, ErrLimit)
	message := cause.Error()
	if errors.Is(cause, ErrLimit) {
		s.mu.Lock()
		first := !s.deps.Now().Before(s.pausedUntil)
		if first {
			s.pausedUntil = s.deps.Now().Add(limitPause)
		}
		s.mu.Unlock()
		if first {
			s.notify("Codex limit reached", "New Slack requests wait 30 minutes; what you ask yourself still goes through.")
		}
		message = "Codex's usage limit is reached. Try again later with “Investigate again”."
	}
	login := errors.Is(cause, ErrLogin)
	if login {
		s.mu.Lock()
		first := s.loginProblem == ""
		s.loginProblem = "Codex is not signed in any more: open Connectors and sign in again."
		s.mu.Unlock()
		if first {
			s.notify("Codex needs you to sign in again", "The assistant cannot answer until you do.")
		}
		message = "Codex is not signed in any more. Open Connectors to sign in again."
	}
	if !quiet && !login {
		s.deps.OnError(cause)
	}
	if _, ok := s.deps.Store.Get(id); !ok {
		return
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Error = StatusFailed, ptr(message)
		r.Entries = append(r.Entries, s.entry("system", "text", "Failed: "+message))
	})
	if !quiet && !login {
		s.alert(id, "Failed: "+message)
	}
}

// settleStopped leaves a stopped request open: it can be asked again or continued. A stopped write may have left
// files half changed.
func (s *Service) settleStopped(id string) {
	request, ok := s.deps.Store.Get(id)
	if !ok {
		return
	}
	text := "Stopped."
	if request.Status == StatusApplying {
		text = "Stopped. Some files may be half changed: check the project with git status."
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Error, r.Questions, r.Proposal = StatusReady, nil, []Question{}, nil
		r.Entries = append(r.Entries, s.entry("system", "text", text))
	})
}

func (s *Service) notify(title, body string) {
	if s.deps.Notify != nil {
		s.deps.Notify(title, body)
	}
}

func (s *Service) alert(id, message string) {
	request, ok := s.deps.Store.Get(id)
	if !ok {
		return
	}
	title := "Request from " + request.Source.SenderName
	if request.Source.ChannelName != nil {
		title += " in #" + *request.Source.ChannelName
	}
	if request.Source.Origin == "note-to-self" {
		title = "Note to self"
	}
	message = strings.Join(strings.Fields(strings.ReplaceAll(message, "**", "")), " ")
	if runes := []rune(message); len(runes) > 160 {
		message = string(runes[:160])
	}
	s.notify(title, message)
}

// runCodex runs a turn under the job's context, so that Stop reaches it.
func (s *Service) runCodex(ctx context.Context, id string, turn Turn, trackThread bool) (TurnResult, error) {
	turn.OnEvent = func(event Event) { s.onEvent(id, event, trackThread) }
	result, err := s.deps.Codex.Run(ctx, turn)
	if err == nil {
		// A turn that went through shows Codex works again: its limit and login problems are over.
		s.mu.Lock()
		s.pausedUntil, s.loginProblem = time.Time{}, ""
		s.mu.Unlock()
	}
	return result, err
}

func (s *Service) onEvent(id string, event Event, trackThread bool) {
	switch {
	case event.Type == "thread" && trackThread:
		s.update(id, func(r *Stored) { r.ThreadID = event.ThreadID })
	case event.Type == "step":
		text := s.deps.Redact()(event.Text)
		s.update(id, func(r *Stored) { r.Entries = append(r.Entries, s.entry("assistant", "step", text)) })
	}
}

func (s *Service) workDir() string {
	if s.deps.WorkDir != nil {
		return s.deps.WorkDir()
	}
	return "."
}

var projectID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// projectPath is the folder of a project, only when it really is a folder directly under the projects root.
func (s *Service) projectPath(id string) string {
	root := s.deps.ProjectsRoot()
	if root == "" || !projectID.MatchString(id) || id == "." || id == ".." {
		return ""
	}
	path := filepath.Join(root, id)
	if info, err := statDir(path); err != nil || !info {
		return ""
	}
	return path
}
