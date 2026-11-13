package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Screenshotter opens a page in a browser and returns a picture of it. The AI cannot do that from its own sandbox: it
// names a page, Pouple takes the picture. The watch follows the browser as it works.
type Screenshotter interface {
	Take(ctx context.Context, address, viewport string, watch BrowserWatch) ([]byte, error)
}

// BrowserWatch is what the engine learns while a picture is taken; every field may be nil.
type BrowserWatch struct {
	// Step says what the browser is doing.
	Step func(text string)
	// Frame receives what the browser shows (JPEG).
	Frame func(jpeg []byte)
	// Foreign is called when the page reaches beyond this computer: what comes back is written by others.
	Foreign func()
}

// DevServers starts the development server of a project and waits until it answers. The assistant cannot: its sandbox
// forbids running a server, and it would have to ask the user to start it and to say when it is ready.
type DevServers interface {
	// Serve starts what the assistant asked for (a script name, a command, or "" for the project's usual script). A command
	// that is not one of the usual ways is run only when allowed is set; otherwise the error is an *ApprovalNeeded.
	Serve(ctx context.Context, project, asked string, allowed bool, say func(text string)) (ServedServer, error)
}

// ApprovalNeeded says the user must allow the command before it runs.
type ApprovalNeeded struct {
	// Command is exactly what would run, and Why what makes it unusual.
	Command, Why string
}

func (e *ApprovalNeeded) Error() string {
	return "the user must allow this command first: " + e.Command + " (" + e.Why + ")"
}

// ServedServer is a development server that answers.
type ServedServer struct {
	// URL is the address of its root, such as http://localhost:8001.
	URL string
	// Command is what was started, for the journal.
	Command string
	Reused  bool
}

// BrowserView is what the interface shows of the browser while it works for a conversation.
type BrowserView struct {
	// URL is the page being opened.
	URL string `json:"url"`
	// Step is what the browser is doing, in the user's language.
	Step string `json:"step"`
}

// liveBrowser is the browser at work for one conversation, in memory only.
type liveBrowser struct {
	url   string
	step  i18n.Message
	frame []byte
}

// maxScreenshotsPerRound is how many pictures one answer of the AI may ask for.
const maxScreenshotsPerRound = 3

// IsScreenshot reports a lookup that asks for a picture of a page instead of data from a service.
func (l Lookup) IsScreenshot() bool { return l.Service == "screenshot" }

// BrowserFrame is the last picture (JPEG) the browser drew for a conversation; false when none is open.
func (s *Service) BrowserFrame(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := s.browsers[id]
	if live == nil || len(live.frame) == 0 {
		return nil, false
	}
	return live.frame, true
}

// browserView is the browser of a conversation as the interface shows it; nil when none is open.
func (s *Service) browserViewLocked(id string, lang i18n.Lang) *BrowserView {
	live := s.browsers[id]
	if live == nil {
		return nil
	}
	return &BrowserView{URL: live.url, Step: live.step.Text(lang)}
}

func (s *Service) openBrowser(id, address string) {
	s.mu.Lock()
	if s.browsers == nil {
		s.browsers = map[string]*liveBrowser{}
	}
	s.browsers[id] = &liveBrowser{url: address, step: i18n.NewMessage("Starting the browser")}
	s.mu.Unlock()
	s.broadcast()
}

func (s *Service) browserStep(id, step string) {
	s.mu.Lock()
	if live := s.browsers[id]; live != nil {
		live.step = i18n.NewMessage(step)
	}
	s.mu.Unlock()
	s.broadcast()
}

// browserFrame keeps the latest picture only: the interface asks for it as often as it wants to redraw, so nothing is
// pushed for each frame.
func (s *Service) browserFrame(id string, jpeg []byte) {
	s.mu.Lock()
	if live := s.browsers[id]; live != nil {
		live.frame = jpeg
	}
	s.mu.Unlock()
}

func (s *Service) closeBrowser(id string) {
	s.mu.Lock()
	delete(s.browsers, id)
	s.mu.Unlock()
	s.broadcast()
}

// runScreenshots takes the pictures the AI asked for, shows them in the conversation, and returns what the AI is told
// and the pictures themselves, so it can look at them in its next turn.
func (s *Service) runScreenshots(ctx context.Context, id string, lookups []Lookup) (string, []string) {
	var told []string
	var images []string
	pictures := 0
	for _, lookup := range lookups {
		if lookup.Action == "serve" {
			told = append(told, s.serveProject(ctx, id, lookup))
			continue
		}
		pictures++
		if pictures == maxScreenshotsPerRound+1 {
			told = append(told, "Screenshot of "+lookup.Target+" not taken: too many pictures asked at once.")
			continue
		}
		if s.deps.Screenshots == nil || s.deps.Attachments == nil {
			told = append(told, "Screenshots are not available.")
			break
		}
		address := lookup.Target
		s.openBrowser(id, address)
		reachedOut := false
		data, err := s.deps.Screenshots.Take(ctx, address, lookup.Text, BrowserWatch{
			Step:    func(step string) { s.browserStep(id, step) },
			Frame:   func(jpeg []byte) { s.browserFrame(id, jpeg) },
			Foreign: func() { reachedOut = true },
		})
		s.closeBrowser(id)
		if err != nil {
			s.update(id, func(r *Stored) {
				r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Screenshot of %s failed: %s", address, i18n.ErrorText(s.deps.Language(), err))))
			})
			told = append(told, fmt.Sprintf("Screenshot of %s failed: %s", address, i18n.ErrorText(i18n.EN, err)))
			continue
		}
		ref, err := s.deps.Attachments.Save("screenshot.png", data)
		if err != nil {
			told = append(told, fmt.Sprintf("Screenshot of %s could not be kept: %s", address, i18n.ErrorText(i18n.EN, err)))
			continue
		}
		_, path, _ := s.deps.Attachments.Open(ref.ID)
		images = append(images, path)
		entry := s.entry("assistant", "text", s.text("Screenshot of %s", address))
		entry.Attachments = []AttachmentRef{ref}
		// What a site other than the user's own computer shows is written by strangers, like the web.
		foreign := reachedOut || !isLocalAddress(address)
		s.update(id, func(r *Stored) {
			r.Entries = append(r.Entries, entry)
			r.WebUsed = r.WebUsed || foreign
		})
		told = append(told, fmt.Sprintf("Screenshot of %s taken (%s). The picture is attached to this message.", address, firstNonEmpty(lookup.Text, "desktop")))
	}
	return strings.Join(told, "\n"), images
}

// serveProject starts the dev server of a project for the assistant and tells it where the server answers. The wait is
// Pouple's, so the assistant never has to ask the user to start something or to say when it is ready.
func (s *Service) serveProject(ctx context.Context, id string, lookup Lookup) string {
	project := lookup.Target
	if s.deps.DevServers == nil {
		return "Starting a server is not available."
	}
	request, _ := s.deps.Store.Get(id)
	allowed := slices.Contains(request.ApprovedCommands, approvalKey(project, lookup.Text))
	served, err := s.deps.DevServers.Serve(ctx, project, lookup.Text, allowed, func(text string) {
		s.update(id, func(r *Stored) {
			r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Starting the server of %s", text)))
		})
	})
	var approval *ApprovalNeeded
	if errors.As(err, &approval) {
		s.update(id, func(r *Stored) {
			r.PendingCommand = &PendingCommand{Project: project, Asked: lookup.Text, Command: approval.Command, Why: approval.Why}
		})
		return "The user is being asked to allow this command, so it has not run: " + approval.Command + ". Your turn ends here; you will be told what they decided."
	}
	if err != nil {
		// What the server wrote may hold a secret of the project's environment: it goes through the redactor.
		reason := i18n.ErrorText(i18n.EN, err)
		if s.deps.Redact != nil {
			reason = s.deps.Redact()(reason)
		}
		shown := i18n.ErrorText(s.deps.Language(), err)
		if s.deps.Redact != nil {
			shown = s.deps.Redact()(shown)
		}
		s.update(id, func(r *Stored) {
			r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("The server of %s did not start: %s", project, shown)))
		})
		return fmt.Sprintf("The server of %s did not start: %s", project, reason)
	}
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Server of %s answers at %s", project, served.URL)))
	})
	return fmt.Sprintf("The server of %s (%s) is running and answers at %s. Open its pages with that address, for example %s/ followed by the path of the page; read the router or the Vite config of the project (its base path included) to find the path. You do not need to wait or to ask the user for anything: capture the page now.", project, served.Command, served.URL, served.URL)
}

func isLocalAddress(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// askToAllowCommand asks the user to allow a command the assistant wants Pouple to run, when it asked for one that is
// not one of the usual ways. It returns whether it asked, in which case the analysis stops here and goes on with their
// answer.
func (s *Service) askToAllowCommand(id string) bool {
	request, ok := s.deps.Store.Get(id)
	if !ok || request.PendingCommand == nil {
		return false
	}
	pending := request.PendingCommand
	question := Question{
		ID:   commandQuestionID,
		Text: s.text("To start the server of %s, Pouple would run this command, which is not one of the usual ways: %s (%s). Allow it?", pending.Project, pending.Command, s.text(pending.Why)),
		Options: []Option{
			{ID: "allow", Label: s.text("Allow it")},
			{ID: "refuse", Label: s.text("Do not run it")},
		},
	}
	s.update(id, func(r *Stored) {
		r.Status, r.Proposal, r.Questions, r.Engaged = StatusAwaitingUser, nil, []Question{question}, true
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Waiting for you to allow a command")))
	})
	s.alert(id, s.text("Needs your decision"))
	return true
}

// commandQuestionID marks the question that asks to allow a command: its answer is not only text for the assistant.
const commandQuestionID = "run-command"
