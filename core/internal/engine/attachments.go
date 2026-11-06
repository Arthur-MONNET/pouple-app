package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// MaxAttachmentBytes is the size limit of one file.
const MaxAttachmentBytes = 10 << 20

var imageTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

var textExtensions = map[string]bool{".txt": true, ".md": true, ".json": true, ".log": true, ".csv": true, ".yml": true, ".yaml": true, ".xml": true}

// Attachments keeps the files that came with messages, on disk, by id.
type Attachments struct{ dir string }

func NewAttachments(dir string) *Attachments { return &Attachments{dir: dir} }

var nameCleaner = regexp.MustCompile(`[^\w.\- ]+`)

func cleanName(name string) string {
	name = nameCleaner.ReplaceAllString(filepath.Base(name), "_")
	if name == "" || name == "." {
		return "file"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// kindOf decides from the bytes, not from what the sender declared: a "png" that is not one is not an image.
func kindOf(name string, data []byte) (kind, mime string) {
	detected := http.DetectContentType(data)
	if _, ok := imageTypes[detected]; ok {
		return "image", detected
	}
	if textExtensions[strings.ToLower(filepath.Ext(name))] && utf8.Valid(data) {
		return "text", "text/plain"
	}
	return "other", "application/octet-stream"
}

// Save stores a file and returns its reference.
func (a *Attachments) Save(name string, data []byte) (AttachmentRef, error) {
	if len(data) == 0 {
		return AttachmentRef{}, i18n.NewError("The file is empty")
	}
	if len(data) > MaxAttachmentBytes {
		return AttachmentRef{}, i18n.NewError("The file is larger than %d MB", MaxAttachmentBytes>>20)
	}
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return AttachmentRef{}, err
	}
	kind, mime := kindOf(name, data)
	ref := AttachmentRef{ID: hex.EncodeToString(raw), Name: cleanName(name), Mime: mime, Size: int64(len(data)), Kind: kind}
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return AttachmentRef{}, err
	}
	if err := os.WriteFile(filepath.Join(a.dir, ref.ID), data, 0o600); err != nil {
		return AttachmentRef{}, err
	}
	meta, _ := json.Marshal(ref)
	return ref, os.WriteFile(filepath.Join(a.dir, ref.ID+".json"), meta, 0o600)
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// Open returns a stored file's reference and path.
func (a *Attachments) Open(id string) (AttachmentRef, string, error) {
	var ref AttachmentRef
	if !idPattern.MatchString(id) {
		return ref, "", os.ErrNotExist
	}
	meta, err := os.ReadFile(filepath.Join(a.dir, id+".json"))
	if err != nil {
		return ref, "", err
	}
	if err := json.Unmarshal(meta, &ref); err != nil {
		return ref, "", err
	}
	return ref, filepath.Join(a.dir, id), nil
}

// Read returns the bytes of a text file, capped.
func (a *Attachments) ReadText(id string, max int) (string, error) {
	_, path, err := a.Open(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > max {
		data = data[:max]
	}
	return string(data), nil
}
