package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Cursor is what the inbox remembers between two polls.
type Cursor struct {
	Since int64    `json:"since"`
	Seen  []string `json:"seen"`
}

type state struct {
	Cursor *Cursor `json:"cursor"`
	// PipelineCursor is what the pipeline source remembers between two polls; apart from Cursor so that neither
	// source can lose the other's place.
	PipelineCursor *Cursor `json:"pipelineCursor,omitempty"`
	// EventCursors are what the other event sources remember between two polls, by source name.
	EventCursors map[string]*Cursor `json:"eventCursors,omitempty"`
	Requests     []Stored           `json:"requests"`
}

// Store keeps the conversations in memory and on disk. Writes are coalesced and happen in the background; Flush
// waits for them.
type Store struct {
	path     string
	mu       sync.Mutex
	state    state
	dirty    bool
	writing  bool
	done     *sync.Cond
	onError  func(error)
	onChange func()
}

func OpenStore(path string, onError func(error)) (*Store, error) {
	s := &Store{path: path, onError: onError}
	s.done = sync.NewCond(&s.mu)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, err
	}
	return s, nil
}

// OnChange registers the function called after every change (it must not block).
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// List returns the requests, oldest first.
func (s *Store) List() []Stored {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Stored, len(s.state.Requests))
	for i, r := range s.state.Requests {
		out[i] = clone(r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReceivedAt < out[j].ReceivedAt })
	return out
}

func (s *Store) Get(id string) (Stored, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.state.Requests {
		if r.ID == id {
			return clone(r), true
		}
	}
	return Stored{}, false
}

func (s *Store) Add(r Stored) {
	s.mu.Lock()
	s.state.Requests = append(s.state.Requests, clone(r))
	s.changed()
}

// Update applies change to a request and reports whether it exists.
func (s *Store) Update(id string, change func(*Stored)) bool {
	s.mu.Lock()
	for i := range s.state.Requests {
		if s.state.Requests[i].ID == id {
			updated := clone(s.state.Requests[i])
			change(&updated)
			s.state.Requests[i] = updated
			s.changed()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

// Transition applies change only if the request is in the expected status, atomically. It reports whether it did.
func (s *Store) Transition(id string, from Status, change func(*Stored) bool) bool {
	s.mu.Lock()
	for i := range s.state.Requests {
		if s.state.Requests[i].ID == id && s.state.Requests[i].Status == from {
			updated := clone(s.state.Requests[i])
			if !change(&updated) {
				s.mu.Unlock()
				return false
			}
			s.state.Requests[i] = updated
			s.changed()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

func (s *Store) Remove(id string) {
	s.mu.Lock()
	kept := s.state.Requests[:0:0]
	for _, r := range s.state.Requests {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	s.state.Requests = kept
	s.changed()
}

func (s *Store) Cursor() *Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Cursor == nil {
		return nil
	}
	c := *s.state.Cursor
	c.Seen = append([]string(nil), c.Seen...)
	return &c
}

func (s *Store) SetCursor(c Cursor) {
	s.mu.Lock()
	s.state.Cursor = &c
	s.changed()
}

func (s *Store) PipelineCursor() *Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.PipelineCursor == nil {
		return nil
	}
	c := *s.state.PipelineCursor
	c.Seen = append([]string(nil), c.Seen...)
	return &c
}

func (s *Store) SetPipelineCursor(c Cursor) {
	s.mu.Lock()
	s.state.PipelineCursor = &c
	s.changed()
}

// EventCursor is what an event source remembered at its last poll; nil before its first look.
func (s *Store) EventCursor(source string) *Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.state.EventCursors[source]
	if stored == nil {
		return nil
	}
	c := *stored
	c.Seen = append([]string(nil), c.Seen...)
	return &c
}

func (s *Store) SetEventCursor(source string, c Cursor) {
	s.mu.Lock()
	if s.state.EventCursors == nil {
		s.state.EventCursors = map[string]*Cursor{}
	}
	s.state.EventCursors[source] = &c
	s.changed()
}

// changed marks the state dirty and starts a write; it releases the lock.
func (s *Store) changed() {
	s.dirty = true
	notify := s.onChange
	start := !s.writing
	if start {
		s.writing = true
	}
	s.mu.Unlock()
	if notify != nil {
		notify()
	}
	if start {
		go s.writeLoop()
	}
}

func (s *Store) writeLoop() {
	for {
		s.mu.Lock()
		if !s.dirty {
			s.writing = false
			s.done.Broadcast()
			s.mu.Unlock()
			return
		}
		s.dirty = false
		data, err := json.Marshal(s.state)
		s.mu.Unlock()
		if err == nil {
			err = writeAtomic(s.path, data)
		}
		if err != nil && s.onError != nil {
			s.onError(err)
		}
	}
}

// Flush waits until everything changed so far is on disk.
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.writing {
		s.done.Wait()
	}
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// clone copies a request deeply enough that callers cannot change the stored one through slices.
func clone(r Stored) Stored {
	data, _ := json.Marshal(r)
	var out Stored
	_ = json.Unmarshal(data, &out)
	return out
}
