// Package memory keeps what lasts beyond a conversation: facts about the user's projects, team and decisions, as a
// folder of Markdown notes on their computer. The same folder is read by the Claude Code and Codex the user runs outside
// Pouple, so it is plain text anyone can read and edit: one note per subject, one fact per list item, and an INDEX.md that
// lists the notes. The folder is a git repository of its own, never pushed: every change Pouple makes is one commit.
//
// The package knows nothing of conversations. The assistant engine decides which facts to offer and who may read what;
// here are only the rules of the folder: which paths are notes, what a private fact is, how a fact is written.
package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Errors the callers map to their own statuses.
var (
	ErrInvalid  = i18n.NewError("invalid memory fact")
	ErrNotFound = i18n.NewError("this note is not in the memory")
)

const (
	// IndexFile lists the notes; Pouple writes it again after each of its changes.
	IndexFile = "INDEX.md"
	// PrivateMark ends a fact that must never reach a request of someone else.
	PrivateMark = "[private]"
	// MaxFact bounds one fact: it is one line, read at a glance.
	MaxFact = 300
	// MaxTitle bounds the title of a note.
	MaxTitle = 80
	// maxPath bounds the path of a note, maxNoteBytes the size of a note that is read, and maxNotes how many notes are
	// listed: a folder that grew by hand cannot make one prompt huge.
	maxPath      = 200
	maxNoteBytes = 256 << 10
	maxNotes     = 500
	maxDepth     = 4
)

// Git runs git in a folder and returns what it printed.
type Git func(ctx context.Context, dir string, args ...string) (string, error)

// Store reads and writes the memory folder. The folder is read at each call: Settings may move it at any time.
type Store struct {
	dir func() string
	git Git
	// mu keeps two writes from interleaving their commits; reads take no lock, since a note is replaced in one rename.
	mu sync.Mutex
}

// New returns a store on the folder dir returns ("" when the memory has no folder). git commits each change.
func New(dir func() string, git Git) *Store {
	return &Store{dir: dir, git: git}
}

// Dir is the folder of the memory now; "" when there is none.
func (s *Store) Dir() string { return s.dir() }

// Note is one subject of the memory.
type Note struct {
	// Path is relative to the folder, with forward slashes ("pouple-preprod.md", "projects/amber.md").
	Path  string `json:"path"`
	Title string `json:"title"`
	// Facts counts its list items, as listed (without the private ones for a shared reader).
	Facts int `json:"facts"`
}

// root opens the folder of the memory, so that no path read from a note or from the AI can leave it, through ".." or a
// symbolic link. ok is false when the folder does not exist yet: the memory is then empty.
func (s *Store) root() (*os.Root, bool, error) {
	dir := s.dir()
	if dir == "" {
		return nil, false, nil
	}
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, i18n.Wrap(err, "The memory folder cannot be read")
	}
	return root, true, nil
}

// CleanPath checks the path of a note as the AI or a client gives it and returns it in its canonical form: relative,
// forward slashes, ending in .md, no hidden folder (.git) and nothing that climbs out of the folder.
func CleanPath(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || len(raw) > maxPath || strings.HasPrefix(raw, "/") || hasHiddenRune(raw) {
		return "", false
	}
	clean := path.Clean(raw)
	// The index is never a note: on a disk that ignores case, "index.md" opens it too, and it lists every note.
	if !strings.HasSuffix(strings.ToLower(clean), ".md") || strings.EqualFold(clean, IndexFile) {
		return "", false
	}
	parts := strings.Split(clean, "/")
	if len(parts) > maxDepth {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == ".." || strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	return clean, true
}

// Notes lists the notes of the memory, sorted by path. With shared set, a note that holds only private facts is left
// out: even its title could tell what it is about.
func (s *Store) Notes(shared bool) ([]Note, error) {
	root, ok, err := s.root()
	if err != nil || !ok {
		return nil, err
	}
	defer root.Close()
	var notes []Note
	walkErr := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A folder that cannot be read is left out, not the whole memory.
			return nil
		}
		if entry.IsDir() {
			if name != "." && (strings.HasPrefix(entry.Name(), ".") || strings.Count(name, "/") >= maxDepth-1) {
				return fs.SkipDir
			}
			return nil
		}
		clean, valid := CleanPath(name)
		if !valid || !entry.Type().IsRegular() {
			return nil
		}
		content, err := readNote(root, clean)
		if err != nil {
			return nil
		}
		if shared {
			if content = WithoutPrivate(content); !hasSharedFact(content) {
				return nil
			}
		}
		notes = append(notes, Note{Path: clean, Title: titleOf(clean, content), Facts: CountFacts(content)})
		if len(notes) == maxNotes {
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return nil, i18n.Wrap(walkErr, "The memory folder cannot be read")
	}
	slices.SortFunc(notes, func(a, b Note) int { return strings.Compare(a.Path, b.Path) })
	return notes, nil
}

// Read returns a note. With shared set, its private facts are taken out first.
func (s *Store) Read(raw string, shared bool) (Note, string, error) {
	clean, valid := CleanPath(raw)
	if !valid {
		return Note{}, "", ErrNotFound
	}
	root, ok, err := s.root()
	if err != nil {
		return Note{}, "", err
	}
	if !ok {
		return Note{}, "", ErrNotFound
	}
	defer root.Close()
	content, err := readNote(root, clean)
	if err != nil {
		return Note{}, "", ErrNotFound
	}
	if shared {
		content = WithoutPrivate(content)
	}
	return Note{Path: clean, Title: titleOf(clean, content)}, content, nil
}

func readNote(root *os.Root, name string) (string, error) {
	info, err := root.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxNoteBytes {
		return "", ErrNotFound
	}
	raw, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n"), nil
}

// titleOf is the first level-one heading of a note, or its file name when it has none.
func titleOf(name, content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if title, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(title) != "" {
			return clipRunes(strings.TrimSpace(title), MaxTitle)
		}
	}
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	return strings.ReplaceAll(base, "-", " ")
}

// IsPrivate says whether a line of a note is a private fact.
func IsPrivate(line string) bool {
	return strings.Contains(strings.ToLower(line), PrivateMark)
}

// WithoutPrivate takes the private facts out of a note. A line marked private goes with everything that belongs to it:
// for a list item or a paragraph, the lines indented under it (blank lines included, which may sit inside one item); for
// a heading, its whole section, up to the next heading of the same level or higher.
func WithoutPrivate(content string) string {
	var kept []string
	dropIndent, dropLevel := -1, 0
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		level := headingLevel(trimmed)
		if dropLevel > 0 {
			if level == 0 || level > dropLevel {
				continue
			}
			dropLevel = 0
		}
		indent := indentWidth(line)
		if dropIndent >= 0 {
			if trimmed == "" || indent > dropIndent {
				continue
			}
			dropIndent = -1
		}
		if IsPrivate(line) {
			if level > 0 {
				dropLevel = level
			} else {
				dropIndent = indent
			}
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// headingLevel is the level of a Markdown heading ("## A" is 2), 0 for any other line.
func headingLevel(trimmed string) int {
	level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
	if level == 0 || level > 6 || (len(trimmed) > level && trimmed[level] != ' ') {
		return 0
	}
	return level
}

// indentWidth counts a tab as four spaces, as Markdown does.
func indentWidth(line string) int {
	width := 0
	for _, r := range line {
		switch r {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width
		}
	}
	return width
}

// hasSharedFact says whether a note holds something besides headings and its front matter (Obsidian's properties): a
// list item, or a line of prose. content is already without its private facts.
func hasSharedFact(content string) bool {
	lines := strings.Split(content, "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && headingLevel(trimmed) == 0 && trimmed != "---" {
			return true
		}
	}
	return false
}

func hasHiddenRune(text string) bool {
	for _, r := range text {
		// The zero-width joiner and non-joiner build emoji and words of some scripts: they change nothing of what is read.
		if r == '‌' || r == '‍' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
