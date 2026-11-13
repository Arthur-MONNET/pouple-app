package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

var optionIDs = []string{"A", "B", "C", "D", "E", "F"}

// analyze is the first investigation of a request.
func (s *Service) analyze(ctx context.Context, id string) error {
	request, err := s.require(id)
	if err != nil {
		return nil
	}
	// What other people send waits while the limit of the AI that would run is reached; what the user asks themselves
	// still goes through. The other AI is not affected.
	active := s.deps.Selection().Provider
	if request.Source.Origin != "note-to-self" && s.pausedFor(active) {
		return &providerFailure{active, ErrLimit}
	}
	prompt, images := s.freshPrompt(ctx, request)
	// What a note to oneself says is the user's own request; a message from someone else is not.
	return s.runInvestigation(ctx, id, prompt, "", images, request.Source.Origin == "note-to-self")
}

// continueWith carries on the AI's conversation; it starts a fresh one, with the transcript so far, when the thread is
// gone or belongs to the other AI (the choice of provider changed since).
func (s *Service) continueWith(ctx context.Context, id, prompt string, images []string) error {
	request, err := s.require(id)
	if err != nil {
		return nil
	}
	if request.ThreadID != "" && request.threadOwner() == s.deps.Selection().Provider {
		return s.runInvestigation(ctx, id, prompt, request.ThreadID, images, true)
	}
	fresh, first := s.freshPrompt(ctx, request)
	var transcript []string
	for _, entry := range request.Entries {
		if entry.Kind == "text" {
			transcript = append(transcript, entry.Role+": "+entry.Text)
		}
	}
	return s.runInvestigation(ctx, id, fresh+"\n\nConversation so far (earlier messages, data):\n"+block("conversation", strings.Join(transcript, "\n"))+"\n\n"+prompt, "", append(first, images...), true)
}

// freshPrompt is the full analysis prompt, and the images the AI looks at.
func (s *Service) freshPrompt(ctx context.Context, request Stored) (string, []string) {
	s.collectFiles(ctx, request.ID)
	current, ok := s.deps.Store.Get(request.ID)
	if ok {
		request = current
	}
	files, images := s.describeFiles(request.Attachments)
	persona := s.deps.Persona()
	history := persona.ConversationHistory(s.previousInConversation(request))
	// Read last: a request that was already stopped does not wait for the services.
	services := s.gatherServices(ctx, request)
	return persona.AnalysisPrompt(request, history, files, "", services), images
}

func (s *Service) previousInConversation(request Stored) []Stored {
	// Events of different projects share one group but are not one conversation.
	if IsEvent(request.Source.Origin) {
		return nil
	}
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
			notes = append(notes, s.entry("assistant", "step", s.text("Files: %s could not be read (%s)", file.Name, err)))
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
		return AttachmentRef{}, i18n.NewError("Slack is not connected")
	}
	if file.Size > MaxAttachmentBytes {
		return AttachmentRef{}, i18n.NewError("larger than 10 MB")
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

// maxLookupRounds is how many times the AI may ask a connected service for more before it must answer.
const maxLookupRounds = 3

// runInvestigation runs the AI until it gives an answer. When it asks for lookups instead, they are run here (the
// credentials never leave this process) and the results go back into the same conversation.
//
// canAct says whether the user asked for this turn themselves (typed in Pouple, or a note to themselves): only then may
// a lookup write something (post a comment). A request that came from someone else's Slack message can only read.
func (s *Service) runInvestigation(ctx context.Context, id, prompt, resume string, images []string, canAct bool) error {
	asks, writes := 0, 0
	for round := 0; ; round++ {
		web := s.webAllowed()
		s.update(id, func(r *Stored) {
			r.Status, r.Error = StatusAnalyzing, nil
			r.WebUsed = r.WebUsed || web
		})
		result, provider, err := s.runProvider(ctx, id, Turn{Prompt: prompt, Dir: s.workDir(), Sandbox: ReadOnly, SchemaFile: s.deps.SchemaFile, ResumeThread: resume, Images: images, Web: web}, true)
		if err != nil {
			return err
		}
		if result.FinalMessage == "" {
			return i18n.NewError("%s finished without an answer", provider.Name())
		}
		outcome, err := ParseOutcome(result.FinalMessage)
		if err != nil {
			return err
		}
		outcome = outcome.Redact(s.deps.Redact())
		if outcome.Outcome != LookupOutcome {
			s.noteServiceSources(id, outcome)
			s.applyOutcome(id, outcome)
			return nil
		}
		if round == maxLookupRounds {
			return i18n.Wrap(ErrOutcome, "The AI kept asking for lookups instead of answering")
		}
		thread := result.ThreadID
		if thread == "" {
			if current, ok := s.deps.Store.Get(id); ok {
				thread = current.ThreadID
			}
		}
		if thread == "" {
			return i18n.NewError("%s finished without an answer", provider.Name())
		}
		results := s.runLookups(ctx, id, limitAsks(outcome.Lookups, &asks), canAct, &writes)
		prompt, resume, images = s.deps.Persona().LookupResultsPrompt(results, round == maxLookupRounds-1), thread, nil
	}
}

// maxWrites is how many things one request may write in the connected services.
const maxWrites = 2

// runLookups asks the connected services for what the AI wanted and journals it. A lookup that writes is run only for a
// request the user made themselves, and at most maxWrites times; otherwise the AI is told it was not done, so it never
// reports a post that did not happen.
func (s *Service) runLookups(ctx context.Context, id string, lookups []Lookup, canAct bool, writes *int) string {
	if s.deps.Lookups == nil {
		return "Lookups are not available: answer with what you have."
	}
	var allowed []Lookup
	var refused ServiceContext
	for _, lookup := range lookups {
		switch {
		case !lookup.Writes():
			allowed = append(allowed, lookup)
		case !canAct:
			refused.Text += "Asana, comment not posted: this request came from a message written by someone else, and Pouple only posts something the user asked for themselves in Pouple. Give the proposed comment in your summary instead; the user can ask you to post it by writing to you in Pouple.\n"
			refused.Steps = append(refused.Steps, ServiceStep{Source: "asana", Message: i18n.NewMessage("Asana: nothing was posted, the request did not come from you"), Failed: true})
		case *writes == maxWrites:
			refused.Text += "Asana, comment not posted: the limit of comments for one request is reached.\n"
			refused.Steps = append(refused.Steps, ServiceStep{Source: "asana", Message: i18n.NewMessage("Asana: nothing was posted, the limit for one request is reached"), Failed: true})
		default:
			*writes++
			allowed = append(allowed, lookup)
		}
	}
	var result ServiceContext
	if len(allowed) > 0 {
		result = s.deps.Lookups(ctx, allowed)
	}
	result.Text = strings.TrimSpace(result.Text + "\n" + refused.Text)
	result.Steps = append(result.Steps, refused.Steps...)
	return s.recordServices(id, result)
}

// maxAsks is how many questions to Slite's own assistant one request may put: each is slow and approximate.
const maxAsks = 2

// limitAsks drops the "ask" lookups beyond maxAsks for the whole request; asked counts those already made.
func limitAsks(lookups []Lookup, asked *int) []Lookup {
	kept := make([]Lookup, 0, len(lookups))
	for _, lookup := range lookups {
		if lookup.Action == "ask" {
			if *asked == maxAsks {
				continue
			}
			*asked++
		}
		kept = append(kept, lookup)
	}
	return kept
}

// webAllowed says whether this request's turns may use the web: whenever the user turned it on. Nothing the AI finds
// can change anything by itself: a proposal waits for the user, and a proposal made after a web turn is never applied
// automatically (WebUsed).
func (s *Service) webAllowed() bool { return s.deps.WebAccess != nil && s.deps.WebAccess() }

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
			questions[i] = Question{ID: fmt.Sprintf("q%d", i+1), Text: question.Text, Options: options, Multiple: question.Multiple}
		}
		s.update(id, func(r *Stored) {
			r.Status, r.Summary, r.Proposal, r.Questions = StatusAwaitingUser, ptr(outcome.Summary), nil, questions
			if outcome.Reply != nil {
				r.DraftReply = outcome.Reply
			}
			r.Entries = append(r.Entries, summary)
		})
		s.alert(id, s.text("Needs your decision"))
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
				r.Entries = append(r.Entries, summary, s.entry("system", "text", s.text("A change was proposed for “%s”, which is not a project in your projects folder.", name)))
			})
			s.alert(id, outcome.Summary)
			return
		}
		// They asked for it themselves, in writing: the approval step would only repeat their request. Never when other
		// people's text (their tasks, notes, merge requests) or the web went into the analysis: a person must read it.
		request, _ := s.deps.Store.Get(id)
		auto := s.deps.AutoApproveAll != nil && s.deps.AutoApproveAll()
		auto = auto || (s.deps.AutoApproveOwnNotes != nil && s.deps.AutoApproveOwnNotes() &&
			request.Source.Origin == "note-to-self" && !request.ServicesUsed && !request.WebUsed)
		write := s.update
		if auto {
			// No question is put to the user, so the work stays silent until the change is made.
			write = s.quietUpdate
		}
		write(id, func(r *Stored) {
			r.Status, r.Summary, r.DraftReply = StatusAwaitingUser, ptr(outcome.Summary), outcome.Reply
			r.Questions, r.Proposal = []Question{}, outcome.Proposal
			r.Entries = append(r.Entries, summary)
		})
		if auto {
			_ = s.decide(id, true, "", false)
			return
		}
		s.alert(id, s.text("Proposed change: %s", outcome.Proposal.Title))
	}
}

// Answer carries the user's answers back to the AI.
func (s *Service) Answer(id string, answers []Answer) error {
	if err := s.requireActive(); err != nil {
		return err
	}
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status != StatusAwaitingUser || len(request.Questions) == 0 {
		return i18n.Wrap(ErrState, "no question is waiting for an answer")
	}
	for _, question := range request.Questions {
		found := false
		for _, answer := range answers {
			if answer.QuestionID != question.ID {
				continue
			}
			if !validAnswer(question, answer) {
				return i18n.Wrap(ErrInput, "answer to %s is not valid", question.ID)
			}
			found = true
		}
		if !found {
			return i18n.Wrap(ErrInput, "question %s has no answer", question.ID)
		}
	}
	prompt := s.deps.Persona().AnswersPrompt(request.Questions, answers)
	s.update(id, func(r *Stored) {
		// Answering is being in the conversation, even for a question asked before Pouple kept track of it.
		r.Status, r.Questions, r.Engaged = StatusAnalyzing, []Question{}, true
		r.Entries = append(r.Entries, s.entry("user", "text", answersText(request.Questions, answers)))
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.continueWith(ctx, id, prompt, nil) })
	return nil
}

// validAnswer says whether an answer fits its question: options that exist, each once, several only when the question
// allows it, and at least an option or a typed text.
func validAnswer(question Question, answer Answer) bool {
	chosen := answer.chosen()
	if len(answer.Text) > maxMessageChars || (!question.Multiple && len(chosen) > 1) {
		return false
	}
	for i, id := range chosen {
		exists := slices.ContainsFunc(question.Options, func(option Option) bool { return option.ID == id })
		if !exists || slices.Contains(chosen[:i], id) {
			return false
		}
	}
	return len(chosen) > 0 || strings.TrimSpace(answer.Text) != ""
}

// answerText is an answer as the user would say it: the options they picked, then what they typed.
func answerText(question Question, answer Answer) string {
	var parts []string
	for _, option := range question.Options {
		if slices.Contains(answer.chosen(), option.ID) {
			parts = append(parts, option.Label)
		}
	}
	if text := strings.TrimSpace(answer.Text); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, ", ")
}

func answersText(questions []Question, answers []Answer) string {
	var parts []string
	for _, question := range questions {
		for _, answer := range answers {
			if answer.QuestionID == question.ID {
				parts = append(parts, answerText(question, answer))
			}
		}
	}
	return strings.Join(parts, " · ")
}

// Message handles something the user typed: a go-ahead for a waiting proposal, or a follow-up for the AI.
func (s *Service) Message(id, text string, attachmentIDs []string) error {
	return s.message(id, text, attachmentIDs, true)
}

// MessageFromLocalAPI leaves proposal decisions to the endpoint that requires a fingerprint.
func (s *Service) MessageFromLocalAPI(id, text string, attachmentIDs []string) error {
	return s.message(id, text, attachmentIDs, false)
}

func (s *Service) message(id, text string, attachmentIDs []string, allowConfirmation bool) error {
	text = strings.TrimSpace(text)
	if text == "" && len(attachmentIDs) == 0 {
		return i18n.Wrap(ErrInput, "write something or add a file")
	}
	if len(text) > maxMessageChars {
		return i18n.Wrap(ErrInput, "the message is longer than %d characters", maxMessageChars)
	}
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status.Pending() {
		return i18n.Wrap(ErrState, "the assistant is already working on this request")
	}
	if request.Status == StatusAwaitingUser && request.Proposal != nil && len(attachmentIDs) == 0 {
		// Typed by the user in the app, a short "yes" is as much their decision as a click on the button.
		decision := ConfirmationOf(text)
		if decision == Approve || decision == Decline {
			if !allowConfirmation {
				return i18n.Wrap(ErrState, "use the proposal action to approve or decline this change")
			}
			return s.Decide(id, decision == Approve)
		}
	}
	if err := s.requireActive(); err != nil {
		return err
	}
	var refs []AttachmentRef
	for _, attachmentID := range attachmentIDs {
		ref, _, err := s.deps.Attachments.Open(attachmentID)
		if err != nil {
			return i18n.Wrap(ErrInput, "unknown attachment")
		}
		refs = append(refs, ref)
	}
	files, images := s.describeFiles(refs)
	entry := s.entry("user", "text", text)
	entry.Attachments = refs
	s.update(id, func(r *Stored) {
		r.Status, r.Error, r.Questions, r.Proposal = StatusAnalyzing, nil, []Question{}, nil
		r.Entries = append(r.Entries, entry)
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error {
		// A follow-up often names what to look for (a folder, a title): the services are asked again for this message.
		services := s.gatherServices(ctx, Stored{Request: Request{ID: id, Text: text}})
		return s.continueWith(ctx, id, s.deps.Persona().FollowUpPrompt(text, files, "", services), images)
	})
	return nil
}

// ProposalFingerprint identifies a proposal, so that a decision can name the one the user actually read.
func ProposalFingerprint(p Proposal) string {
	fields, _ := json.Marshal([3]string{p.ProjectID, p.Title, p.Description})
	sum := sha256.Sum256(fields)
	return hex.EncodeToString(sum[:8])
}

// Decide approves or declines the proposal that waits for an answer.
func (s *Service) Decide(id string, approve bool) error { return s.DecideProposal(id, approve, "") }

// DecideProposal is Decide for a given proposal: when wanted is not empty, it must match the proposal waiting now.
func (s *Service) DecideProposal(id string, approve bool, wanted string) error {
	return s.decide(id, approve, wanted, true)
}

// decide approves or declines the waiting proposal. byUser is false for an approval Pouple gives on its own (a note to
// self), which must not show the work in a bubble the user never saw.
func (s *Service) decide(id string, approve bool, wanted string, byUser bool) error {
	if approve {
		if err := s.requireActive(); err != nil {
			return err
		}
	}
	if _, err := s.require(id); err != nil {
		return err
	}
	var proposal Proposal
	var stale bool
	status, label := StatusReady, "Declined: %s"
	if approve {
		status, label = StatusApplying, "Approved: %s"
	}
	started := s.deps.Store.Transition(id, StatusAwaitingUser, func(r *Stored) bool {
		if r.Proposal == nil {
			return false
		}
		proposal = *r.Proposal
		stale = wanted != "" && wanted != ProposalFingerprint(proposal)
		if stale {
			return false
		}
		r.Status, r.Proposal = status, nil
		// Declining ends the conversation: there is nothing left to show in a bubble. Approving is being in it.
		r.Closed = !approve
		r.Engaged = r.Engaged || (approve && byUser)
		r.Entries = append(r.Entries, s.entry("user", "text", s.text(label, proposal.Title)))
		r.UpdatedAt = s.now()
		return true
	})
	if stale {
		return i18n.Wrap(ErrState, "the proposal changed since you read it")
	}
	if !started {
		return i18n.Wrap(ErrState, "no change is waiting for a decision")
	}
	if approve {
		s.enqueue(id, Interactive, func(ctx context.Context) error { return s.apply(ctx, id, proposal) })
	}
	return nil
}

// apply carries out an approved change in the project folder only, in a fresh turn that never sees the Slack text.
func (s *Service) apply(ctx context.Context, id string, proposal Proposal) error {
	directory := s.projectPath(proposal.ProjectID)
	if directory == "" {
		return i18n.NewError("project “%s” no longer exists", proposal.ProjectID)
	}
	// What git says before: a turn that changed nothing must not be reported as a change.
	before, beforeErr := s.deps.ChangeSummary(ctx, directory)
	result, provider, err := s.runProvider(ctx, id, Turn{Prompt: s.deps.Persona().ApplyPrompt(proposal, ""), Dir: directory, Sandbox: WorkspaceWrite, SchemaFile: s.deps.SchemaFile}, false)
	if err != nil {
		return err
	}
	if result.FinalMessage == "" {
		return i18n.NewError("%s finished without an answer", provider.Name())
	}
	outcome, err := ParseOutcome(result.FinalMessage)
	if err != nil {
		return err
	}
	outcome = outcome.Redact(s.deps.Redact())
	after, afterErr := s.deps.ChangeSummary(ctx, directory)
	if beforeErr == nil && afterErr == nil && after == before {
		// The AI stopped on something it lacked: say it plainly and keep the request open.
		s.update(id, func(r *Stored) {
			r.Status = StatusReady
			r.Entries = append(r.Entries, s.entry("assistant", "text", outcome.Summary), s.entry("system", "text", s.text("No file was changed.")))
		})
		s.alert(id, s.text("Nothing was changed: %s", outcome.Summary))
		return nil
	}
	s.update(id, func(r *Stored) {
		r.Status = StatusDone
		r.Entries = append(r.Entries, s.entry("assistant", "text", outcome.Summary))
		if after != "" {
			r.Entries = append(r.Entries, s.entry("system", "step", after))
		}
	})
	s.alert(id, s.text("Change applied in %s", proposal.ProjectID))
	return nil
}

// Ignore dismisses a request that is not being worked on.
func (s *Service) Ignore(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status.Pending() {
		return i18n.Wrap(ErrState, "the assistant is still working on this request")
	}
	s.update(id, func(r *Stored) { r.Status, r.Questions, r.Proposal = StatusIgnored, []Question{}, nil })
	return nil
}

// Close hides the request's bubble until the request needs the user again. The conversation itself is kept, and closing
// it is not activity: the time it was last updated stays. seen is the update time of the request the user closed; when
// the request changed since (a new question came in meanwhile), it stays open, since what changed may need them.
func (s *Service) Close(id, seen string) error {
	stale := false
	found := s.deps.Store.Update(id, func(r *Stored) {
		if seen != "" && !sameInstant(seen, r.UpdatedAt) {
			stale = true
			return
		}
		r.Closed = true
	})
	if !found {
		return ErrNotFound
	}
	if stale {
		return i18n.Wrap(ErrState, "the request changed since you saw it")
	}
	return nil
}

// Retry starts the investigation over.
func (s *Service) Retry(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status != StatusFailed && request.Status != StatusIgnored && request.Status != StatusReady {
		return i18n.Wrap(ErrState, "only a failed, ignored or finished request can be investigated again")
	}
	if err := s.requireActive(); err != nil {
		return err
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Error, r.ThreadID, r.ThreadProvider, r.Questions, r.Proposal = StatusQueued, nil, "", "", []Question{}, nil
		r.LimitedBy = ""
	})
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.analyze(ctx, id) })
	return nil
}

// ContinueWith makes another AI the one Pouple uses, then investigates the request again with it: what a request
// stopped by the usage limit of the AI in use offers. Only the AI the request offers (SwitchTo) is accepted.
func (s *Service) ContinueWith(id string, provider providers.ID) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if offered := s.switchTo(request); offered == "" || offered != provider {
		return i18n.Wrap(ErrState, "%s cannot take over this request", provider.Name())
	}
	if err := s.deps.UseProvider(provider); err != nil {
		return err
	}
	return s.Retry(id)
}

// Remove deletes a request that is not being worked on.
func (s *Service) Remove(id string) error {
	request, err := s.require(id)
	if err != nil {
		return err
	}
	if request.Status.Pending() {
		return i18n.Wrap(ErrState, "the assistant is still working on this request")
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
				r.Entries = append(r.Entries, s.entry("system", "text", s.text("Pouple was closed while the change was being made. Check the project with git status, then ask again if needed.")))
			})
		}
	}
}
