package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Errors the API maps to HTTP statuses.
var (
	ErrNotFound = i18n.NewError("request not found")
	ErrState    = i18n.NewError("not possible in the current state")
	ErrInput    = i18n.NewError("invalid input")
	ErrInactive = i18n.NewError("the assistant is not running")
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
	Notices  []i18n.Message
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
	// Language is the interface language, read when displaying current state or writing new journal entries.
	Language func() i18n.Lang
	// ProjectsRoot is the folder holding one folder per project; "" means none is chosen yet.
	ProjectsRoot func() string
	// ProtectedDir is Pouple's data directory, which must stay outside writable projects.
	ProtectedDir string
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
	notices      []i18n.Message
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
	if deps.Language == nil {
		deps.Language = func() i18n.Lang { return i18n.FR }
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
	out, err := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", dir, "status", "--porcelain").Output()
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

func (s *Service) text(key string, args ...any) string {
	return i18n.T(s.deps.Language(), key, args...)
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
		return i18n.Wrap(ErrInactive, "%s: %s", ErrInactive, reason)
	}
	return nil
}

// Snapshot is the whole state, as the interface reads it.
func (s *Service) Snapshot() Snapshot {
	lang := s.deps.Language()
	s.mu.Lock()
	notices := make([]string, 0, len(s.notices))
	for _, notice := range s.notices {
		notices = append(notices, notice.Text(lang))
	}
	notices = append(notices, s.standingNoticesLocked(lang)...)
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
		if view.Proposal != nil {
			shown := *view.Proposal
			shown.ID = ProposalFingerprint(shown)
			view.Proposal = &shown
		}
		requests[i] = view
	}
	return Snapshot{Status: status, Requests: requests}
}

func (s *Service) standingNoticesLocked(lang i18n.Lang) []string {
	var out []string
	if s.fileNotice != "" {
		out = append(out, i18n.T(lang, s.fileNotice))
	}
	if s.loginProblem != "" {
		out = append(out, i18n.T(lang, s.loginProblem))
	}
	if s.deps.Now().Before(s.pausedUntil) {
		out = append(out, i18n.T(lang, "Codex's usage limit is reached: new Slack requests wait until %s. What you ask yourself still goes through.", i18n.Time(s.pausedUntil.Local())))
	}
	return out
}

// Run polls Slack periodically and lets a pending live event bring the next poll forward.
// The cooldown keeps a burst of events from issuing a search for every message.
func (s *Service) Run(ctx context.Context, intervals func() (period, cooldown time.Duration), wake <-chan struct{}) {
	for ctx.Err() == nil {
		s.Poll(ctx)
		period, cooldown := intervals()
		timer := time.NewTimer(period)
		ready := time.NewTimer(cooldown)
		select {
		case <-ctx.Done():
		case <-timer.C:
		case <-ready.C:
			select {
			case <-ctx.Done():
			case <-timer.C:
			case <-wake:
			}
		}
		timer.Stop()
		ready.Stop()
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
		s.notices = []i18n.Message{i18n.NewMessage("Slack could not be read: %s", err)}
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
		request.Entries = []Entry{s.entry("system", "text", s.text(reason))}
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
				r.Entries = append(r.Entries, s.entry("system", "text", s.text("Expired: no answer for 7 days.")))
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
		return i18n.Wrap(ErrState, "nothing is running for this request")
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
		r.Entries = append(r.Entries, s.entry("system", "text", s.text("Stopped before it started.")))
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
	lang := s.deps.Language()
	message := i18n.ErrorText(lang, cause)
	if errors.Is(cause, ErrLimit) {
		s.mu.Lock()
		first := !s.deps.Now().Before(s.pausedUntil)
		if first {
			s.pausedUntil = s.deps.Now().Add(limitPause)
		}
		s.mu.Unlock()
		if first {
			s.notify(i18n.T(lang, "Codex limit reached"), i18n.T(lang, "New Slack requests wait 30 minutes; what you ask yourself still goes through."))
		}
		message = i18n.T(lang, "Codex's usage limit is reached. Try again later with “Investigate again”.")
	}
	login := errors.Is(cause, ErrLogin)
	if login {
		s.mu.Lock()
		first := s.loginProblem == ""
		s.loginProblem = "Codex is not signed in any more: open Connectors and sign in again."
		s.mu.Unlock()
		if first {
			s.notify(i18n.T(lang, "Codex needs you to sign in again"), i18n.T(lang, "The assistant cannot answer until you do."))
		}
		message = i18n.T(lang, "Codex is not signed in any more. Open Connectors to sign in again.")
	}
	if !quiet && !login {
		s.deps.OnError(cause)
	}
	if _, ok := s.deps.Store.Get(id); !ok {
		return
	}
	failure := i18n.T(lang, "Failed: %s", message)
	s.update(id, func(r *Stored) {
		r.Status, r.Error = StatusFailed, ptr(message)
		r.Entries = append(r.Entries, s.entry("system", "text", failure))
	})
	if !quiet && !login {
		s.alert(id, failure)
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
		r.Entries = append(r.Entries, s.entry("system", "text", s.text(text)))
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
	lang := s.deps.Language()
	title := i18n.T(lang, "Request from %s", request.Source.SenderName)
	if request.Source.ChannelName != nil {
		title = i18n.T(lang, "Request from %s in #%s", request.Source.SenderName, *request.Source.ChannelName)
	}
	if request.Source.Origin == "note-to-self" {
		title = i18n.T(lang, "Note to self")
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
	result, err := s.deps.Codex.Run(i18n.WithLanguage(ctx, s.deps.Language()), turn)
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
	// Hidden folders (.ssh, .git, .config…) are never projects.
	if root == "" || !projectID.MatchString(id) || strings.HasPrefix(id, ".") {
		return ""
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, id))
	if err != nil {
		return ""
	}
	// A symlink that leaves the projects folder is not a project.
	if relative, err := filepath.Rel(realRoot, path); err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return ""
	}
	if isDir, err := statDir(path); err != nil || !isDir {
		return ""
	}
	if protected := s.deps.ProtectedDir; protected != "" {
		projectInfo, projectErr := os.Stat(path)
		protectedInfo, protectedErr := os.Stat(protected)
		realProtected, resolveErr := filepath.EvalSymlinks(protected)
		if projectErr != nil || protectedErr != nil || resolveErr != nil ||
			hasAncestor(path, protectedInfo) || hasAncestor(realProtected, projectInfo) {
			return ""
		}
	}
	return path
}

func hasAncestor(path string, ancestor os.FileInfo) bool {
	for {
		info, err := os.Stat(path)
		if err == nil && os.SameFile(info, ancestor) {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}
