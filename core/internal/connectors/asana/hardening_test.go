package asana

import "testing"

func TestOnlyAsanaLinksReachTheAssistant(t *testing.T) {
	for raw, want := range map[string]string{
		"https://app.asana.com/0/1/2":          "https://app.asana.com/0/1/2",
		"https://evil.example/0/1/2":           "",
		"http://app.asana.com/0/1/2":           "",
		"https://app.asana.com.evil.example/x": "",
	} {
		task, ok := taskPayload{GID: "1", Name: "Task", PermalinkURL: raw}.task()
		if !ok || task.PermalinkURL != want {
			t.Errorf("%q: got %q (ok %v), want %q", raw, task.PermalinkURL, ok, want)
		}
	}
}
