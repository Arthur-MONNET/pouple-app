package engine

import (
	"slices"
	"strings"
	"unicode"
)

// Topic is a subject several conversations are about, whatever the channel, the person or the tool they come from. The AI
// makes them, merges them and renames them as the conversations evolve; the interface groups the list by topic.
type Topic struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// CreatedAt is when the first conversation of the subject was sorted; UpdatedAt when one was last added or the topic
	// renamed.
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// maxTopicTitleRunes bounds a title: it is one short line in a list.
const maxTopicTitleRunes = 80

// cleanTitle makes what the AI wrote a title: one line, no quotes around it and no final period, short.
func cleanTitle(text string) string {
	// Whitespace is made a single space first; what remains of control and format characters (a right-to-left override, a
	// zero-width space) would make a title read differently from what it says.
	line := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.Join(strings.Fields(text), " "))
	line = strings.TrimRight(line, ".")
	line = strings.Trim(line, " \"'`«»“”")
	return clipRunes(strings.TrimSpace(line), maxTopicTitleRunes)
}

// organization is what the store remembers of the last time the topics were looked at as a whole.
type organization struct {
	// At is when the topics were last reorganized successfully; "" before the first time.
	At string `json:"at,omitempty"`
	// Placed counts the conversations sorted into a topic since then.
	Placed int `json:"placed,omitempty"`
}

// Topics returns the topics, oldest first.
func (s *Store) Topics() []Topic {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.state.Topics)
}

// AddTopic keeps a new topic.
func (s *Store) AddTopic(topic Topic) {
	s.mu.Lock()
	s.state.Topics = append(s.state.Topics, topic)
	s.changed()
}

func (s *Store) topicIndexLocked(id string) int {
	return slices.IndexFunc(s.state.Topics, func(t Topic) bool { return t.ID == id })
}

// PlaceInTopic puts a request in a topic that exists. Sorting is not activity: the request keeps its update time. It
// reports whether both exist.
func (s *Store) PlaceInTopic(requestID, topicID, now string) bool {
	s.mu.Lock()
	topic := s.topicIndexLocked(topicID)
	request := slices.IndexFunc(s.state.Requests, func(r Stored) bool { return r.ID == requestID })
	if topic < 0 || request < 0 {
		s.mu.Unlock()
		return false
	}
	s.state.Requests[request].TopicID = topicID
	s.state.Topics[topic].UpdatedAt = now
	s.state.Organization.Placed++
	s.changed()
	return true
}

// RetitleTopic renames a topic and reports whether it exists. Naming the same again changes nothing.
func (s *Store) RetitleTopic(id, title, now string) bool {
	s.mu.Lock()
	topic := s.topicIndexLocked(id)
	if topic < 0 {
		s.mu.Unlock()
		return false
	}
	if s.state.Topics[topic].Title == title {
		s.mu.Unlock()
		return true
	}
	s.state.Topics[topic].Title, s.state.Topics[topic].UpdatedAt = title, now
	s.changed()
	return true
}

// MergeTopics moves the conversations of one topic into another, which keeps the older start, and removes the first.
// It reports whether both exist and differ.
func (s *Store) MergeTopics(from, into, now string) bool {
	s.mu.Lock()
	source, target := s.topicIndexLocked(from), s.topicIndexLocked(into)
	if source < 0 || target < 0 || source == target {
		s.mu.Unlock()
		return false
	}
	for i := range s.state.Requests {
		if s.state.Requests[i].TopicID == from {
			s.state.Requests[i].TopicID = into
		}
	}
	if s.state.Topics[source].CreatedAt < s.state.Topics[target].CreatedAt {
		s.state.Topics[target].CreatedAt = s.state.Topics[source].CreatedAt
	}
	s.state.Topics[target].UpdatedAt = now
	s.state.Topics = slices.Delete(s.state.Topics, source, source+1)
	s.changed()
	return true
}

// dropEmptyTopicsLocked removes the topics no conversation belongs to, and puts back in the list the conversations
// whose topic is gone.
func (s *Store) dropEmptyTopicsLocked() {
	used := map[string]bool{}
	for _, request := range s.state.Requests {
		used[request.TopicID] = true
	}
	s.state.Topics = slices.DeleteFunc(s.state.Topics, func(t Topic) bool { return !used[t.ID] })
	known := map[string]bool{}
	for _, topic := range s.state.Topics {
		known[topic.ID] = true
	}
	for i := range s.state.Requests {
		if !known[s.state.Requests[i].TopicID] {
			s.state.Requests[i].TopicID = ""
		}
	}
}

// DropEmptyTopics removes the topics left with no conversation.
func (s *Store) DropEmptyTopics() {
	s.mu.Lock()
	s.dropEmptyTopicsLocked()
	s.changed()
}

// Organization is what was last remembered of the organization of the topics.
func (s *Store) Organization() organization {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Organization
}

// MemoryReviewAt is when an alert last asked the user to review their memory; "" when none ever did.
func (s *Store) MemoryReviewAt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.MemoryReviewAt
}

// MemoryReviewed records that an alert asked the user to review their memory.
func (s *Store) MemoryReviewed(now string) {
	s.mu.Lock()
	s.state.MemoryReviewAt = now
	s.changed()
}

// Organized records that the topics were looked at as a whole.
func (s *Store) Organized(now string) {
	s.mu.Lock()
	s.state.Organization = organization{At: now}
	s.changed()
}
