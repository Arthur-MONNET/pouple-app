package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var optionIDs = []string{"A", "B", "C", "D", "E", "F"}

// analyze is the first investigation of a request.
func (s *Service) analyze(ctx context.Context, id string) error {
	request, err := s.require(id)
	if err != nil {
		return nil
	}
	// What other people send waits while Codex's own limit is reached; what the user asks themselves still goes through.
	s.mu.Lock()
	paused := s.deps.Now().Before(s.pausedUntil)
	s.mu.Unlock()
	if request.Source.Origin != "note-to-self" && paused {
		return ErrLimit
	}
	prompt, images := s.freshPrompt(ctx, request)
	return s.runInvestigation(ctx, id, prompt, "", images)
}

// continueWith carries on the Codex conversation; it starts a fresh one when the thread is gone.
func (s *Service) continueWith(ctx context.Context, id, prompt string, images []string) error {
	request, err := s.require(id)
	if err != nil {
		return nil
	}
	if request.ThreadID != "" {
		return s.runInvestigation(ctx, id, prompt, request.ThreadID, images)
	}
	fresh, first := s.freshPrompt(ctx, request)
	var transcript []string
	for _, entry := range request.Entries {
		if entry.Kind == "text" {
			transcript = append(transcript, entry.Role+": "+entry.Text)
		}
	}
	return s.runInvestigation(ctx, id, fresh+"\n\nConversation so far:\n"+strings.Join(transcript, "\n")+"\n\n"+prompt, "", append(first, images...))
}

// freshPrompt is the full analysis prompt, and the images Codex looks at.
func (s *Service) freshPrompt(ctx context.Context, request Stored) (string, []string) {
	s.collectFiles(ctx, request.ID)
	current, ok := s.deps.Store.Get(request.ID)
	if ok {
		request = current
	}
	files, images := s.describeFiles(request.Attachments)
	persona := s.deps.Persona()
	history := persona.ConversationHistory(s.previousInConversation(request))
	return persona.AnalysisPrompt(request, history, files, ""), images
}

func (s *Service) previousInConversation(request Stored) []Stored {
	var previous []Stored
	for _, other := range s.deps.Store.List() {
		if other.ID != request.ID && other.Source.ChannelID == request.Source.ChannelID && other.ReceivedAt < request.ReceivedAt && other.Source.Origin != "cockpit-action" {
			previous = append(previous, other)
		}
	}
	if len(previous) > historyRequests {
		previous = previous[len(previous)-historyRequests:]
	}
	return previous
}

// collectFiles downloads the files that came with a Slack message, once; whatever cannot be read is said and the
// rest goes on.
func (s *Service) collectFiles(ctx context.Context, id string) {
	request, ok := s.deps.Store.Get(id)
	if !ok || len(request.PendingFiles) == 0 {
		return
	}
	var saved []AttachmentRef
	var notes []Entry
	for i, file := range request.PendingFiles {
		if i >= maxFilesPerMsg {
			break
		}
		ref, err := s.downloadFile(ctx, file)
		if err != nil {
			var access interface{ FilesAccess() bool }
			if errors.As(err, &access) && access.FilesAccess() {
				s.mu.Lock()
				s.fileNotice = "Slack files: add the files:read permission to the Slack app, then reconnect Slack, to let the assistant see attachments."
				s.mu.Unlock()
			}
			notes = append(notes, s.entry("assistant", "step", fmt.Sprintf("Files: %s could not be read (%s)", file.Name, err)))
			continue
		}
		saved = append(saved, ref)
	}
	s.update(id, func(r *Stored) {
		r.PendingFiles = nil
		r.Attachments = append(r.Attachments, saved...)
		r.Entries = append(r.Entries, notes...)
	})
}

func (s *Service) downloadFile(ctx context.Context, file PendingFile) (AttachmentRef, error) {
	if s.deps.Fetch == nil {
		return AttachmentRef{}, errors.New("Slack is not connected")
	}
	if file.Size > MaxAttachmentBytes {
		return AttachmentRef{}, errors.New("larger than 10 MB")
	}
	data, err := s.deps.Fetch(ctx, file)
	if err != nil {
		return AttachmentRef{}, err
	}
	return s.deps.Attachments.Save(file.Name, data)
}

// describeFiles lists the files for the prompt, with the text of text files, and returns the image paths.
func (s *Service) describeFiles(refs []AttachmentRef) (string, []string) {
	var lines, images []string
	for _, ref := range refs {
		switch ref.Kind {
		case "image":
			if _, path, err := s.deps.Attachments.Open(ref.ID); err == nil {
				images = append(images, path)
			}
			lines = append(lines, fmt.Sprintf("- %s (image, attached to this turn)", oneLine(ref.Name, 80)))
		case "text":
			text, err := s.deps.Attachments.ReadText(ref.ID, maxAttachmentText)
			if err != nil {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s (text):\n%s", oneLine(ref.Name, 80), block("file-content", text)))
		default:
			lines = append(lines, fmt.Sprintf("- %s (%s, %d bytes: not readable)", oneLine(ref.Name, 80), ref.Mime, ref.Size))
		}
	}
	return strings.Join(lines, "\n"), images
}

func (s *Service) runInvestigation(ctx context.Context, id, prompt, resume string, images []string) error {
	s.update(id, func(r *Stored) { r.Status, r.Error = StatusAnalyzing, nil })
	result, err := s.runCodex(ctx, id, Turn{Prompt: prompt, Dir: s.workDir(), Sandbox: ReadOnly, SchemaFile: s.deps.SchemaFile, ResumeThread: resume, Images: images}, true)
	if err != nil {
		return err
	}
	if result.ThreadID != "" {
		s.update(id, func(r *Stored) { r.ThreadID = result.ThreadID })
	}
	if result.FinalMessage == "" {
		return errors.New("Codex finished without an answer")
	}
	outcome, err := ParseOutcome(result.FinalMessage)
	if err != nil {
		return err
	}
	s.applyOutcome(id, outcome.Redact(s.deps.Redact()))
	return nil
}

func (s *Service) applyOutcome(id string, outcome Outcome) {
	summary := s.entry("assistant", "text", outcome.Summary)
	switch outcome.Outcome {
	case NotARequest:
		s.update(id, func(r *Stored) {
			r.Status, r.Summary = StatusIgnored, ptr(outcome.Summary)
			r.Entries = append(r.Entries, summary)
		})
	case Answered:
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.DraftReply = StatusReady, ptr(outcome.Summary), outcome.Reply
			r.Questions, r.Proposal = []Question{}, nil
			r.Entries = append(r.Entries, summary)
		})
		s.alert(id, outcome.Summary)
	case NeedsInput:
		questions := make([]Question, len(outcome.Questions))
		for i, question := range outcome.Questions {
			options := make([]Option, len(question.Options))
			for j, option := range question.Options {
				options[j] = Option{ID: optionIDs[j], Label: option.Label, Description: option.Description}
			}
			questions[i] = Question{ID: fmt.Sprintf("q%d", i+1), Text: question.Text, Options: options}
		}
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.Proposal, r.Questions = StatusAwaitingUser, ptr(outcome.Summary), nil, questions
			if outcome.Reply != nil {
				r.DraftReply = outcome.Reply
			}
			r.Entries = append(r.Entries, summary)
		})
		s.alert(id, "Needs your decision")
	case ProposesChange:
		if outcome.Proposal == nil || s.projectPath(outcome.Proposal.ProjectID) == "" {
			// A change aimed at a project that does not exist cannot be approved: show the analysis only.
			name := "?"
			if outcome.Proposal != nil {
				name = outcome.Proposal.ProjectID
			}
			s.update(id, func(r *Stored) {
				r.Status, r.Summary, r.DraftReply = StatusReady, ptr(outcome.Summary), outcome.Reply
				r.Questions, r.Proposal = []Question{}, nil
				r.Entries = append(r.Entries, summary, s.entry("system", "text", fmt.Sprintf("A change was proposed for “%s”, which is not a project in your projects folder.", name)))
			})
			s.alert(id, outcome.Summary)
			return
		}
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.DraftReply = StatusAwaitingUser, ptr(outcome.Summary), outcome.Reply
			r.Questions, r.Proposal = []Question{}, outcome.Proposal
			r.Entries = append(r.Entries, summary)
		})
		if s.deps.AutoApproveOwnNotes != nil && s.deps.AutoApproveOwnNotes() {
			if request, ok := s.deps.Store.Get(id); ok && request.Source.Origin == "note-to-self" {
				// They asked for it themselves, in writing: the approval step would only repeat their request.
				_ = s.Decide(id, true)
				return
			}
		}
		s.alert(id, "Proposed change: "+outcome.Proposal.Title)
	}
}

// Answer carries the user's answers back to Codex.
func (s *Service) Answer(id string, answers []Answer) error {
	if err := s.requireActive(); err != nil {
		return err
	}
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status != StatusAwaitingUser || len(request.Questions) == 0 {
		return fmt.Errorf("%w: no question is waiting for an answer", ErrState)
	}
	for _, question := range request.Questions {
		found := false
		for _, answer := range answers {
			if answer.QuestionID != question.ID {
				continue
			}
			valid := strings.TrimSpace(answer.Text) != ""
			for _, option := range question.Options {
				valid = valid || option.ID == answer.OptionID
			}
			if !valid || len(answer.Text) > maxMessageChars {
				return fmt.Errorf("%w: answer to %s is not valid", ErrInput, question.ID)
			}
			found = true
		}
		if !found {
			return fmt.Errorf("%w: question %s has no answer", ErrInput, question.ID)
		}
	}
	prompt := s.deps.Persona().AnswersPrompt(request.Questions, answers)
	s.update(id, func(r *Stored) {
		r.Status, r.Questions = StatusAnalyzing, []Question{}
		r.Entries = append(r.Entries, s.entry("user", "text", answersText(request.Questions, answers)))
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.continueWith(ctx, id, prompt, nil) })
	return nil
}

func answersText(questions []Question, answers []Answer) string {
	var parts []string
	for _, question := range questions {
		for _, answer := range answers {
			if answer.QuestionID != question.ID {
				continue
			}
			text := strings.TrimSpace(answer.Text)
			for _, option := range question.Options {
				if option.ID == answer.OptionID {
					text = option.Label
				}
			}
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " · ")
}

// Message handles something the user typed: a go-ahead for a waiting proposal, or a follow-up for Codex.
func (s *Service) Message(id, text string, attachmentIDs []string) error {
	text = strings.TrimSpace(text)
	if text == "" && len(attachmentIDs) == 0 {
		return fmt.Errorf("%w: write something or add a file", ErrInput)
	}
	if len(text) > maxMessageChars {
		return fmt.Errorf("%w: the message is longer than %d characters", ErrInput, maxMessageChars)
	}
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status.Pending() {
		return fmt.Errorf("%w: the assistant is already working on this request", ErrState)
	}
	if request.Status == StatusAwaitingUser && request.Proposal != nil && len(attachmentIDs) == 0 {
		// Typed by the user in the app, a short "yes" is as much their decision as a click on the button.
		switch ConfirmationOf(text) {
		case Approve:
			return s.Decide(id, true)
		case Decline:
			return s.Decide(id, false)
		}
	}
	if err := s.requireActive(); err != nil {
		return err
	}
	var refs []AttachmentRef
	for _, attachmentID := range attachmentIDs {
		ref, _, err := s.deps.Attachments.Open(attachmentID)
		if err != nil {
			return fmt.Errorf("%w: unknown attachment", ErrInput)
		}
		refs = append(refs, ref)
	}
	files, images := s.describeFiles(refs)
	prompt := s.deps.Persona().FollowUpPrompt(text, files, "")
	entry := s.entry("user", "text", text)
	entry.Attachments = refs
	s.update(id, func(r *Stored) {
		r.Status, r.Error, r.Questions, r.Proposal = StatusAnalyzing, nil, []Question{}, nil
		r.Entries = append(r.Entries, entry)
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.continueWith(ctx, id, prompt, images) })
	return nil
}

// Decide approves or declines the proposal that waits for an answer.
func (s *Service) Decide(id string, approve bool) error {
	if approve {
		if err := s.requireActive(); err != nil {
			return err
		}
	}
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status != StatusAwaitingUser || request.Proposal == nil {
		return fmt.Errorf("%w: no change is waiting for a decision", ErrState)
	}
	proposal := *request.Proposal
	if !approve {
		s.update(id, func(r *Stored) {
			r.Status, r.Proposal = StatusReady, nil
			r.Entries = append(r.Entries, s.entry("user", "text", "Declined: "+proposal.Title))
		})
		return nil
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Proposal = StatusApplying, nil
		r.Entries = append(r.Entries, s.entry("user", "text", "Approved: "+proposal.Title))
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.apply(ctx, id, proposal) })
	return nil
}

// apply carries out an approved change in the project folder only, in a fresh turn that never sees the Slack text.
func (s *Service) apply(ctx context.Context, id string, proposal Proposal) error {
	directory := s.projectPath(proposal.ProjectID)
	if directory == "" {
		return fmt.Errorf("project “%s” no longer exists", proposal.ProjectID)
	}
	// What git says before: a turn that changed nothing must not be reported as a change.
	before, beforeErr := s.deps.ChangeSummary(ctx, directory)
	result, err := s.runCodex(ctx, id, Turn{Prompt: s.deps.Persona().ApplyPrompt(proposal, ""), Dir: directory, Sandbox: WorkspaceWrite, SchemaFile: s.deps.SchemaFile}, false)
	if err != nil {
		return err
	}
	if result.FinalMessage == "" {
		return errors.New("Codex finished without an answer")
	}
	outcome, err := ParseOutcome(result.FinalMessage)
	if err != nil {
		return err
	}
	outcome = outcome.Redact(s.deps.Redact())
	after, afterErr := s.deps.ChangeSummary(ctx, directory)
	if beforeErr == nil && afterErr == nil && after == before {
		// Codex stopped on something it lacked: say it plainly and keep the request open.
		s.update(id, func(r *Stored) {
			r.Status = StatusReady
			r.Entries = append(r.Entries, s.entry("assistant", "text", outcome.Summary), s.entry("system", "text", "No file was changed."))
		})
		s.alert(id, "Nothing was changed: "+outcome.Summary)
		return nil
	}
	s.update(id, func(r *Stored) {
		r.Status = StatusDone
		r.Entries = append(r.Entries, s.entry("assistant", "text", outcome.Summary))
		if after != "" {
			r.Entries = append(r.Entries, s.entry("system", "step", after))
		}
	})
	s.alert(id, "Change applied in "+proposal.ProjectID)
	return nil
}

// Ignore dismisses a request that is not being worked on.
func (s *Service) Ignore(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status.Pending() {
		return fmt.Errorf("%w: the assistant is still working on this request", ErrState)
	}
	s.update(id, func(r *Stored) { r.Status, r.Questions, r.Proposal = StatusIgnored, []Question{}, nil })
	return nil
}

// Retry starts the investigation over.
func (s *Service) Retry(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status != StatusFailed && request.Status != StatusIgnored && request.Status != StatusReady {
		return fmt.Errorf("%w: only a failed, ignored or finished request can be investigated again", ErrState)
	}
	if err := s.requireActive(); err != nil {
		return err
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Error, r.ThreadID, r.Questions, r.Proposal = StatusQueued, nil, "", []Question{}, nil
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.analyze(ctx, id) })
	return nil
}

// Remove deletes a request that is not being worked on.
func (s *Service) Remove(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status.Pending() {
		return fmt.Errorf("%w: the assistant is still working on this request", ErrState)
	}
	s.deps.Store.Remove(id)
	return nil
}

// Recover puts back to work what was interrupted when the app stopped.
func (s *Service) Recover() {
	for _, request := range s.deps.Store.List() {
		switch request.Status {
		case StatusQueued, StatusAnalyzing:
			priority := Background
			if request.Source.Origin == "note-to-self" {
				priority = Interactive
			}
			s.update(request.ID, func(r *Stored) { r.Status = StatusQueued })
			id := request.ID
			s.enqueue(id, priority, func(ctx context.Context) error { return s.analyze(ctx, id) })
		case StatusApplying:
			// A write that was cut short is not repeated on its own: the user decides.
			s.update(request.ID, func(r *Stored) {
				r.Status = StatusReady
				r.Entries = append(r.Entries, s.entry("system", "text", "Pouple was closed while the change was being made. Check the project with git status, then ask again if needed."))
			})
		}
	}
}
