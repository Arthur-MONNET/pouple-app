// Package i18n translates what the core writes itself (connector guides, validation messages, notices, the journal
// of a conversation). The English text is the key: a call reads T(lang, "Stopped."), and the French table gives the
// translation. A missing French entry falls back to English.
package i18n

import (
	"context"
	"fmt"
	"time"
)

// Lang is an interface language.
type Lang string

const (
	FR Lang = "fr"
	EN Lang = "en"
)

// Parse reads a language code; anything but "en" is French, the default of the app.
func Parse(code string) Lang {
	if code == "en" {
		return EN
	}
	return FR
}

// T returns the text in the language, formatted with args when there are some.
func T(lang Lang, key string, args ...any) string {
	text := key
	if lang != EN {
		if french, ok := fr[key]; ok {
			text = french
		}
	}
	if len(args) == 0 {
		return text
	}
	localized := make([]any, len(args))
	for i, arg := range args {
		switch value := arg.(type) {
		case Message:
			localized[i] = value.Text(lang)
		case error:
			localized[i] = ErrorText(lang, value)
		default:
			localized[i] = arg
		}
	}
	return fmt.Sprintf(text, localized...)
}

// Message carries app copy and its arguments until it is ready to display.
type Message struct {
	key  string
	args []any
}

func NewMessage(key string, args ...any) Message {
	return Message{key: key, args: args}
}

func (m Message) Text(lang Lang) string { return T(lang, m.key, m.args...) }

type localizedError struct {
	message Message
	cause   error
}

func (e *localizedError) Error() string              { return e.message.Text(EN) }
func (e *localizedError) ErrorText(lang Lang) string { return e.message.Text(lang) }
func (e *localizedError) Unwrap() error              { return e.cause }

func NewError(key string, args ...any) error {
	return Wrap(nil, key, args...)
}

// Wrap associates display text with a cause without changing errors.Is or errors.As.
func Wrap(cause error, key string, args ...any) error {
	return &localizedError{message: NewMessage(key, args...), cause: cause}
}

// ErrorText translates app-authored errors and keeps third-party diagnostics intact.
func ErrorText(lang Lang, err error) string {
	if localized, ok := err.(interface{ ErrorText(Lang) string }); ok {
		return localized.ErrorText(lang)
	}
	return err.Error()
}

type contextKey struct{}

func WithLanguage(ctx context.Context, lang Lang) context.Context {
	return context.WithValue(ctx, contextKey{}, lang)
}

func FromContext(ctx context.Context) Lang {
	lang, _ := ctx.Value(contextKey{}).(Lang)
	return Parse(string(lang))
}

var monthsFR = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}

// Date formats a day the way people read it in the language ("5 novembre 2026", "5 Nov 2026").
func Date(lang Lang, t time.Time) string {
	if lang != EN {
		return fmt.Sprintf("%d %s %d", t.Day(), monthsFR[t.Month()-1], t.Year())
	}
	return t.Format("2 Jan 2006")
}

// Time formats a time of day ("14:30" in both languages).
func Time(t time.Time) string { return t.Format("15:04") }
