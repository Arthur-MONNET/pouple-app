package connector

import (
	"context"
	"errors"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

type optionalTokenConnector struct{}

func (optionalTokenConnector) Descriptor() Descriptor {
	return Descriptor{ID: "example", Fields: []Field{
		{ID: "token", Secret: true},
		{ID: "appToken", Secret: true, Optional: true},
	}}
}

func (optionalTokenConnector) Validate(_ context.Context, values Values) (Profile, error) {
	return Profile{Name: values["token"]}, nil
}

type memoryPersistence map[string]Saved

func (p memoryPersistence) Load(id string) (Saved, bool, error) {
	saved, ok := p[id]
	return saved, ok, nil
}

func (p memoryPersistence) Save(id string, saved Saved) error {
	p[id] = saved
	return nil
}

func (p memoryPersistence) Remove(id string) error {
	delete(p, id)
	return nil
}

type failingVault struct {
	*secrets.Memory
	unreadable string
	reject     string
}

func (v *failingVault) Get(name string) (string, error) {
	if name == v.unreadable {
		return "", errors.New("cannot read this secret")
	}
	return v.Memory.Get(name)
}

func (v *failingVault) Set(name, value string) error {
	if name == v.reject {
		return errors.New("cannot write this secret")
	}
	return v.Memory.Set(name, value)
}

func TestOptionalSecretAndFailedReplacement(t *testing.T) {
	ctx := context.Background()
	vault := &failingVault{Memory: secrets.NewMemory()}
	saved := memoryPersistence{}
	registry := NewRegistry(vault, saved, optionalTokenConnector{})

	if _, err := registry.Save(ctx, "example", Values{"token": "original"}); err != nil {
		t.Fatal(err)
	}
	assertTokens := func(appToken string) {
		t.Helper()
		values, err := registry.Values("example")
		if err != nil || values["token"] != "original" || values["appToken"] != appToken {
			t.Fatalf("expected the original connection, got %v, %v", values, err)
		}
	}
	assertTokens("")
	vault.reject = "example.appToken"
	if _, err := registry.Save(ctx, "example", Values{"token": "replacement", "appToken": "live"}); err == nil {
		t.Fatal("failed secret write was reported as successful")
	}
	assertTokens("")
	if saved["example"].Profile.Name != "original" {
		t.Fatal("failed replacement changed the saved profile")
	}
	vault.reject = ""
	if _, err := registry.Save(ctx, "example", Values{"token": "original", "appToken": "live"}); err != nil {
		t.Fatal(err)
	}
	assertTokens("live")
	vault.unreadable = "example.appToken"
	assertTokens("")
	vault.unreadable = ""
	if _, err := registry.Save(ctx, "example", Values{"token": "original"}); err != nil {
		t.Fatal(err)
	}
	assertTokens("")
}
