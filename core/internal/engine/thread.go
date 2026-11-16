package engine

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// A message that arrives in the Slack thread of a conversation that is still open (an answer waiting, a question
// waiting) is not a new request: the assistant reads it quietly, in the conversation it already has, and the user is told
// only when it changes what the assistant said or proposed.

// silentKey marks a context whose investigation must leave the state of the conversation as it is until it has
// something to tell.
type silentKey struct{}

func withSilence(ctx context.Context) context.Context {
	return context.WithValue(ctx, silentKey{}, true)
}

func isSilent(ctx context.Context) bool { silent, _ := ctx.Value(silentKey{}).(bool); return silent }

// threadKeyOf names the Slack thread a message belongs to: the thread it replies to, or itself when it starts one.
// "" for what is not a Slack conversation (notes to oneself, events, actions).
func threadKeyOf(source Source) string {
	switch source.Origin {
	case "", "note-to-self", "cockpit-action", "pouple-action":
		return ""
	}
	if IsEvent(source.Origin) || source.ChannelID == "" {
		return ""
	}
	thread := source.TS
	if source.Permalink != nil {
		if parsed, err := url.Parse(*source.Permalink); err == nil {
			if value := parsed.Query().Get("thread_ts"); value != "" {
				thread = value
			}
		}
	}
	if thread == "" {
		return ""
	}
	return source.ChannelID + ":" + thread
}

// newerThan says whether the Slack timestamp a is after b.
func newerThan(a, b string) bool {
	left, err1 := strconv.ParseFloat(a, 64)
	right, err2 := strconv.ParseFloat(b, 64)
	return err1 == nil && err2 == nil && left > right
}

// threadMessage is a message that arrived in the thread of an open conversation.
type threadMessage struct{ Sender, Text string }

// followThread joins a message to the open conversation of its thread, when there is one the assistant can still
// reconsider. It returns whether it took the message.
func (s *Service) followThread(in Incoming) bool {
	key := threadKeyOf(in.Source)
	if key == "" {
		return false
	}
	for _, candidate := range s.deps.Store.List() {
		open := candidate.Status == StatusReady || candidate.Status == StatusAwaitingUser
		if !open || candidate.ThreadID == "" || threadKeyOf(candidate.Source) != key || !newerThan(in.Source.TS, candidate.Source.TS) {
			continue
		}
		seen := false
		for _, ts := range candidate.ThreadSeen {
			seen = seen || ts == in.Source.TS
		}
		if seen {
			return true
		}
		id := candidate.ID
		s.mu.Lock()
		if s.threadPending == nil {
			s.threadPending = map[string][]Incoming{}
		}
		s.threadPending[id] = append(s.threadPending[id], in)
		scheduled := s.threadScheduled[id]
		if s.threadScheduled == nil {
			s.threadScheduled = map[string]bool{}
		}
		s.threadScheduled[id] = true
		s.mu.Unlock()
		// Written down without reaching the user: they see it in the conversation when they open it.
		s.quietUpdate(id, func(r *Stored) {
			r.ThreadSeen = append(r.ThreadSeen, in.Source.TS)
			r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("New message in the thread from %s: %s", oneLine(in.Source.SenderName, 60), clipRunes(oneLine(in.Text, 300), 120))))
		})
		if !scheduled {
			// A burst of messages is read in one turn: the read waits a moment, like a request from someone else does.
			start := func() {
				s.enqueue(id, Background, func(ctx context.Context) error { return s.reconsiderThread(ctx, id) })
			}
			if s.deps.SettleDelay > 0 {
				time.AfterFunc(s.deps.SettleDelay, start)
			} else {
				start()
			}
		}
		return true
	}
	return false
}

// reconsiderThread lets the assistant read the new messages of a thread in the conversation it already has. Whatever
// it concludes that changes nothing leaves the conversation exactly as it was.
func (s *Service) reconsiderThread(ctx context.Context, id string) error {
	s.mu.Lock()
	pending := s.threadPending[id]
	delete(s.threadPending, id)
	delete(s.threadScheduled, id)
	s.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	request, err := s.require(id)
	if err != nil {
		return nil
	}
	// The user acted on the conversation meanwhile (answered, started a change, it is being worked on again): the
	// messages are not lost, they become requests of their own as before.
	if request.Status != StatusReady && request.Status != StatusAwaitingUser {
		for _, in := range pending {
			s.receiveNew(in)
		}
		return nil
	}
	messages := make([]threadMessage, len(pending))
	for i, in := range pending {
		messages[i] = threadMessage{Sender: in.Source.SenderName, Text: in.Text}
	}
	return s.continueWith(withSilence(ctx), id, s.deps.Persona().ThreadUpdatePrompt(messages), nil)
}

// noteThreadChecked journals that new messages changed nothing, without reaching the user.
func (s *Service) noteThreadChecked(id string) {
	s.quietUpdate(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry("assistant", "step", s.text("Checked: the new messages change nothing")))
	})
}
