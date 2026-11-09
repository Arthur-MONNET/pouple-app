package slackinbox

import "sync"

const maxHints = 200

// ThreadHints remembers the threads in which a message was just posted, as Socket Mode reports them. It only keeps
// identifiers: whether the message matters is decided by the inbox, which asks Slack if the user follows the thread.
// A nil *ThreadHints accepts and forgets everything, so the inbox works the same without live events.
type ThreadHints struct {
	mu    sync.Mutex
	order []threadRef
	known map[string]bool
}

func NewThreadHints() *ThreadHints { return &ThreadHints{known: map[string]bool{}} }

// Add records a thread once. When too many pile up, the oldest are forgotten: the searches still cover them.
func (h *ThreadHints) Add(channelID, threadTS string) {
	if h == nil || channelID == "" || threadTS == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := channelID + ":" + threadTS
	if h.known[key] {
		return
	}
	h.known[key] = true
	h.order = append(h.order, threadRef{channelID: channelID, threadTS: threadTS})
	for len(h.order) > maxHints {
		delete(h.known, h.order[0].channelID+":"+h.order[0].threadTS)
		h.order = h.order[1:]
	}
}

// take removes and returns up to limit threads, oldest first.
func (h *ThreadHints) take(limit int) []threadRef {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	count := min(limit, len(h.order))
	taken := append([]threadRef(nil), h.order[:count]...)
	for _, thread := range taken {
		delete(h.known, thread.channelID+":"+thread.threadTS)
	}
	h.order = h.order[count:]
	return taken
}

// putBack returns threads that could not be read, so the next poll tries them again.
func (h *ThreadHints) putBack(threads []threadRef) {
	for _, thread := range threads {
		h.Add(thread.channelID, thread.threadTS)
	}
}
