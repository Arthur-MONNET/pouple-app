// Package secrets keeps the tokens and API keys connectors need. Values never go to a file in the project,
// a log or an API response: the macOS Keychain holds them, and tests use an in-memory store.
package secrets

import (
	"errors"
	"sync"
)

// ErrNotFound is returned when no secret is stored under a name.
var ErrNotFound = errors.New("secret not found")

// Store reads and writes named secrets.
type Store interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// Memory is a Store that lives in the process: for tests and for platforms without a keychain.
type Memory struct {
	mu     sync.Mutex
	values map[string]string
}

func NewMemory() *Memory { return &Memory{values: map[string]string{}} }

func (m *Memory) Get(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[name]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (m *Memory) Set(name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[name] = value
	return nil
}

func (m *Memory) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, name)
	return nil
}
