package codex

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const catalogJSON = `{"models":[
 {"slug":"gpt-6-sol","display_name":"GPT-6-Sol","description":"Previous generation","visibility":"list","priority":4},
 {"slug":"gpt-6.1-sol","display_name":"GPT-6.1-Sol","description":"Latest workhorse","visibility":"list","priority":1},
 {"slug":"gpt-reserve","display_name":"GPT-Reserve","visibility":"hide","priority":5},
 {"slug":"--evil","display_name":"Evil","visibility":"list","priority":2},
 {"slug":"gpt-6-luna","visibility":"list","priority":6}
]}`

func TestModelsListsWhatCodexShowsInItsPickerInItsOrder(t *testing.T) {
	var script string
	c := &Connector{Home: "/home/pouple/codex", Capture: func(_ context.Context, env []string, s string) ([]byte, error) {
		script = s
		if len(env) != 1 || env[0] != "CODEX_HOME=/home/pouple/codex" {
			t.Errorf("Pouple's own Codex home: %v", env)
		}
		return []byte("welcome back\n" + catalogJSON), nil
	}}
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	if strings.Join(ids, ",") != "gpt-6.1-sol,gpt-6-sol,gpt-6-luna" {
		t.Errorf("hidden and unsafe names are left out, the order is Codex's: %v", ids)
	}
	if models[0].Label != "GPT-6.1-Sol — Latest workhorse" || models[2].Label != "gpt-6-luna" {
		t.Errorf("labels: %+v", models)
	}
	if !strings.HasSuffix(script, " debug models") {
		t.Errorf("script: %q", script)
	}
}

func TestModelsAreKeptForAWhileAndAFailureIsNotKept(t *testing.T) {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	runs, fail := 0, true
	c := &Connector{Now: func() time.Time { return now }, Capture: func(context.Context, []string, string) ([]byte, error) {
		runs++
		if fail {
			return nil, errors.New("codex: command not found")
		}
		return []byte(catalogJSON), nil
	}}
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("a Codex that cannot run is an error")
	}
	fail = false
	for range 3 {
		if _, err := c.Models(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 2 {
		t.Fatalf("one failed run, one kept: %d runs", runs)
	}
	now = now.Add(modelsTTL + time.Second)
	_, _ = c.Models(context.Background())
	if runs != 3 {
		t.Fatalf("read again after the delay: %d runs", runs)
	}
}

func TestModelsRefuseWhatIsNotACatalog(t *testing.T) {
	for _, out := range []string{"", "no json here", `{"models":[]}`, `{"models":[{"slug":"x","visibility":"hide"}]}`} {
		c := &Connector{Capture: func(context.Context, []string, string) ([]byte, error) { return []byte(out), nil }}
		if _, err := c.Models(context.Background()); err == nil {
			t.Errorf("%q was taken as a catalog", out)
		}
	}
}
