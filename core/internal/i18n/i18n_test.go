package i18n

import (
	"errors"
	"reflect"
	"regexp"
	"testing"
)

func TestFrenchTemplatesPreserveArguments(t *testing.T) {
	placeholder := regexp.MustCompile(`%[a-z]`)
	for english, french := range fr {
		if !reflect.DeepEqual(placeholder.FindAllString(english, -1), placeholder.FindAllString(french, -1)) {
			t.Errorf("placeholders changed in %q: %q", english, french)
		}
	}
}

func TestErrorTextKeepsTheCauseAndExternalText(t *testing.T) {
	cause := errors.New("third-party response %s")
	err := Wrap(cause, "Slack could not be reached: %s", cause)
	if !errors.Is(err, cause) {
		t.Fatal("the cause must still be available to errors.Is")
	}
	if got := ErrorText(Parse(""), err); got != "Slack est injoignable : third-party response %s" {
		t.Errorf("French error: %q", got)
	}
	if got := err.Error(); got != "Slack could not be reached: third-party response %s" {
		t.Errorf("English diagnostic: %q", got)
	}
}
