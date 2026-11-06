package connector

import (
	"context"
	"errors"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
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

type defaultedConnector struct{}

func (defaultedConnector) Descriptor() Descriptor {
	return Descriptor{ID: "hosted", Fields: []Field{
		{ID: "url", Default: "https://gitlab.example.com"},
		{ID: "token", Secret: true},
	}}
}

func (defaultedConnector) Validate(_ context.Context, values Values) (Profile, error) {
	return Profile{Name: values["url"]}, nil
}

func TestEmptyFieldsTakeTheirDefault(t *testing.T) {
	saved := memoryPersistence{}
	registry := NewRegistry(secrets.NewMemory(), saved, defaultedConnector{})
	input := Values{"url": "  ", "token": "t"}
	profile, err := registry.Save(context.Background(), "hosted", input)
	if err != nil || profile.Name != "https://gitlab.example.com" {
		t.Fatalf("got %+v, %v", profile, err)
	}
	if saved["hosted"].Values["url"] != "https://gitlab.example.com" || input["url"] != "  " {
		t.Fatalf("the default must be stored, and the caller's map left alone: %v %v", saved["hosted"].Values, input)
	}
}

func TestTokenExpiryDateIsLocalized(t *testing.T) {
	profile := Profile{Facts: []Fact{{Label: "Token expires", Value: "1 Mar 2027"}, {Label: "Open tasks", Value: "3"}}, Warnings: []Warning{{Title: "Token expires soon", Detail: "Create a new token before it ends, then connect GitLab again."}}}
	french := profile.Localized(i18n.FR, "gitlab")
	if french.Facts[0].Label != "Le jeton expire le" || french.Facts[0].Value != "1 mars 2027" || french.Facts[1].Label != "Tâches ouvertes" || french.Warnings[0].Title != "Le jeton expire bientôt" {
		t.Fatalf("got %+v", french)
	}
	if english := profile.Localized(i18n.EN, "gitlab"); english.Facts[0].Value != "1 Mar 2027" {
		t.Fatalf("got %+v", english)
	}
}
