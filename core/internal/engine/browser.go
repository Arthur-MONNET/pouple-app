package engine

import (
	"context"
	"fmt"
	"net/url"
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
	for i, lookup := range lookups {
		if i == maxScreenshotsPerRound {
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

func isLocalAddress(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
