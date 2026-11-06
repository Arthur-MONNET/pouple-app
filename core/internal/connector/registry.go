package connector

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

// ErrUnknown is returned for a connector id nobody registered.
var ErrUnknown = i18n.NewError("unknown connector")

// Saved is what the registry remembers about a connected service; a Persistence implementation stores it.
type Saved struct {
	Values      map[string]string
	Profile     Profile
	ConnectedAt time.Time
}

// Persistence stores the non-secret part of a connection.
type Persistence interface {
	Load(id string) (Saved, bool, error)
	Save(id string, saved Saved) error
	Remove(id string) error
}

// Registry holds the connectors and ties validation, secrets and persistence together.
type Registry struct {
	mu         sync.Mutex
	connectors map[string]Connector
	secrets    secrets.Store
	store      Persistence
	now        func() time.Time
}

func NewRegistry(secretStore secrets.Store, store Persistence, connectors ...Connector) *Registry {
	registry := &Registry{connectors: map[string]Connector{}, secrets: secretStore, store: store, now: time.Now}
	for _, c := range connectors {
		registry.connectors[c.Descriptor().ID] = c
	}
	return registry
}

// Status is a connector with what is known about its connection.
type Status struct {
	Descriptor  Descriptor `json:"descriptor"`
	Connected   bool       `json:"connected"`
	Profile     *Profile   `json:"profile,omitempty"`
	ConnectedAt *time.Time `json:"connectedAt,omitempty"`
}

func (r *Registry) Get(id string) (Connector, error) {
	c, ok := r.connectors[id]
	if !ok {
		return nil, i18n.Wrap(ErrUnknown, "%s: %s", ErrUnknown, id)
	}
	return c, nil
}

// List returns every connector in setup order.
func (r *Registry) List() ([]Status, error) {
	statuses := make([]Status, 0, len(r.connectors))
	for _, c := range r.connectors {
		status, err := r.Status(c.Descriptor().ID)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Descriptor.Order < statuses[j].Descriptor.Order })
	return statuses, nil
}

func (r *Registry) Status(id string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, err := r.Get(id)
	if err != nil {
		return Status{}, err
	}
	status := Status{Descriptor: c.Descriptor()}
	saved, ok, err := r.store.Load(id)
	if err != nil {
		return status, err
	}
	if ok {
		status.Connected, status.Profile, status.ConnectedAt = true, &saved.Profile, &saved.ConnectedAt
	}
	return status, nil
}

// withDefaults fills the empty fields that have a default. The caller's map is left as it is.
func withDefaults(descriptor Descriptor, values Values) Values {
	filled := Values{}
	for key, value := range values {
		filled[key] = value
	}
	for _, field := range descriptor.Fields {
		if field.Default != "" && strings.TrimSpace(filled[field.ID]) == "" {
			filled[field.ID] = field.Default
		}
	}
	return filled
}

// checkFields refuses what the form itself can already tell is wrong, before the service is called.
func checkFields(descriptor Descriptor, values Values) error {
	for _, field := range descriptor.Fields {
		value := values[field.ID]
		if value == "" {
			if field.Optional {
				continue
			}
			return Invalid(field.ID, "%s is required", i18n.NewMessage(field.Label))
		}
		if field.Pattern != "" && !regexp.MustCompile(field.Pattern).MatchString(value) {
			hint := field.PatternHint
			if hint == "" {
				hint = "does not look right"
			}
			return Invalid(field.ID, "%s %s", i18n.NewMessage(field.Label), i18n.NewMessage(hint))
		}
	}
	return nil
}

// Validate checks the values and returns the profile they connect to. Nothing is saved.
func (r *Registry) Validate(ctx context.Context, id string, values Values) (Profile, error) {
	c, err := r.Get(id)
	if err != nil {
		return Profile{}, err
	}
	values = withDefaults(c.Descriptor(), values)
	if err := checkFields(c.Descriptor(), values); err != nil {
		return Profile{}, err
	}
	return c.Validate(ctx, values)
}

// Save validates, then stores the secrets in the keychain and the rest in the settings.
func (r *Registry) Save(ctx context.Context, id string, values Values) (_ Profile, saveErr error) {
	if c, err := r.Get(id); err == nil {
		values = withDefaults(c.Descriptor(), values)
	}
	profile, err := r.Validate(ctx, id, values)
	if err != nil {
		return Profile{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	type secretChange struct{ name, before string }
	var written []secretChange
	defer func() {
		if saveErr != nil {
			for index := len(written) - 1; index >= 0; index-- {
				change := written[index]
				if err := r.writeSecret(change.name, change.before); err != nil {
					saveErr = errors.Join(saveErr, fmt.Errorf("could not restore the previous connector secret: %w", err))
				}
			}
		}
	}()
	descriptor := r.connectors[id].Descriptor()
	plain := map[string]string{}
	for _, field := range descriptor.Fields {
		if field.Secret {
			name := SecretName(id, field.ID)
			before, err := r.secrets.Get(name)
			if err != nil && !errors.Is(err, secrets.ErrNotFound) {
				return Profile{}, i18n.Wrap(err, "could not save %s in the keychain: %s", i18n.NewMessage(field.Label), err)
			}
			after := values[field.ID]
			if before == after {
				continue
			}
			if err := r.writeSecret(name, after); err != nil {
				return Profile{}, i18n.Wrap(err, "could not save %s in the keychain: %s", i18n.NewMessage(field.Label), err)
			}
			written = append(written, secretChange{name, before})
		} else {
			plain[field.ID] = values[field.ID]
		}
	}
	if err := r.store.Save(id, Saved{Values: plain, Profile: profile, ConnectedAt: r.now()}); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (r *Registry) writeSecret(name, value string) error {
	if value == "" {
		return r.secrets.Delete(name)
	}
	return r.secrets.Set(name, value)
}

// Remove forgets a connection and its secrets.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, err := r.Get(id)
	if err != nil {
		return err
	}
	for _, field := range c.Descriptor().Fields {
		if field.Secret {
			if err := r.secrets.Delete(SecretName(id, field.ID)); err != nil {
				return err
			}
		}
	}
	return r.store.Remove(id)
}

// Values returns what a connected service was set up with, secrets included: for the engine, never for the API.
func (r *Registry) Values(id string) (Values, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	saved, ok, err := r.store.Load(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, i18n.NewError("%s is not connected", id)
	}
	values := Values{}
	for key, value := range saved.Values {
		values[key] = value
	}
	for _, field := range c.Descriptor().Fields {
		if !field.Secret {
			continue
		}
		secret, err := r.secrets.Get(SecretName(id, field.ID))
		// An unavailable optional token cannot disable the required connection.
		if field.Optional && err != nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field.Label, err)
		}
		values[field.ID] = secret
	}
	return values, nil
}

// SecretName is the keychain name of a connector field.
func SecretName(connectorID, fieldID string) string { return connectorID + "." + fieldID }
