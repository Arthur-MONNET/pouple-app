package engine

import (
	"path/filepath"
	"testing"
)

func TestStoreSurvivesARestartAndNeverSharesMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	store, err := OpenStore(path, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	store.OnChange(func() { changes++ })
	store.Add(Stored{Request: Request{ID: "a", ReceivedAt: "2", Questions: []Question{}, Entries: []Entry{}}})
	store.Add(Stored{Request: Request{ID: "b", ReceivedAt: "1", Questions: []Question{}, Entries: []Entry{}}})
	store.Update("a", func(r *Stored) { r.Status = StatusDone; r.Entries = append(r.Entries, Entry{Text: "x"}) })
	store.SetCursor(Cursor{Since: 5, Seen: []string{"C:1"}})
	store.Flush()
	if changes != 4 {
		t.Fatalf("changes = %d", changes)
	}

	list := store.List()
	list[0].ID = "mutated"
	if got, _ := store.Get("b"); got.ID != "b" {
		t.Fatal("callers must not change the stored request")
	}

	reopened, err := OpenStore(path, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.List()
	if len(got) != 2 || got[0].ID != "b" || got[1].Status != StatusDone || len(got[1].Entries) != 1 {
		t.Fatalf("reloaded %+v", got)
	}
	if c := reopened.Cursor(); c == nil || c.Since != 5 {
		t.Fatalf("cursor %+v", c)
	}
	reopened.Remove("a")
	reopened.Flush()
	if _, ok := reopened.Get("a"); ok {
		t.Fatal("not removed")
	}
	if reopened.Update("zzz", func(*Stored) {}) {
		t.Fatal("updating a missing request must report false")
	}
}

func TestAttachmentsDecideTheKindFromTheBytes(t *testing.T) {
	a := NewAttachments(t.TempDir())
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	ref, err := a.Save("../../shot.png", png)
	if err != nil || ref.Kind != "image" || ref.Name != "shot.png" {
		t.Fatalf("image: %+v %v", ref, err)
	}
	fake, _ := a.Save("fake.png", []byte("not an image"))
	if fake.Kind != "other" {
		t.Fatalf("a fake png must not be an image: %+v", fake)
	}
	text, _ := a.Save("notes.md", []byte("# hi"))
	if text.Kind != "text" {
		t.Fatalf("text: %+v", text)
	}
	if _, err := a.Save("empty.txt", nil); err == nil {
		t.Fatal("empty file accepted")
	}
	if _, _, err := a.Open("../etc/passwd"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, path, err := a.Open(ref.ID); err != nil || filepath.Dir(path) == "" {
		t.Fatalf("open: %v", err)
	}
}
