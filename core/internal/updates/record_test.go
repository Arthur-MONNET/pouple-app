package updates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadRecordAcceptsTheInstallersOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, recordFile), `{
  "sourceRoot": "/Users/a/pouple-app",
  "commit": "0123456789abcdef0123456789abcdef01234567",
  "version": "0.1.0",
  "installedAt": "2026-11-06T10:00:00Z",
  "futureField": true
}
`)
	record, err := readRecord(dir)
	if err != nil {
		t.Fatal(err)
	}
	if record.SourceRoot != "/Users/a/pouple-app" || record.Version != "0.1.0" || !record.InstalledAt.Equal(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("record: %+v", record)
	}
}

func TestReadRecordRefusesAnythingThatIsNotAWellFormedRecord(t *testing.T) {
	const good = `"commit":"0123456789abcdef0123456789abcdef01234567","version":"0.1.0","installedAt":"2026-11-06T10:00:00Z"`
	cases := map[string]string{
		"not json":          `nope`,
		"empty":             ``,
		"array":             `[]`,
		"relative root":     `{"sourceRoot":"pouple-app",` + good + `}`,
		"dot-dot root":      `{"sourceRoot":"/Users/a/../b",` + good + `}`,
		"trailing slash":    `{"sourceRoot":"/Users/a/pouple-app/",` + good + `}`,
		"double slash":      `{"sourceRoot":"/Users//a",` + good + `}`,
		"empty root":        `{"sourceRoot":"",` + good + `}`,
		"filesystem root":   `{"sourceRoot":"/",` + good + `}`,
		"NUL in root":       `{"sourceRoot":"/Users/a\u0000/b",` + good + `}`,
		"tilde root":        `{"sourceRoot":"~/pouple-app",` + good + `}`,
		"missing commit":    `{"sourceRoot":"/Users/a","version":"0.1.0","installedAt":"2026-11-06T10:00:00Z"}`,
		"non-hex commit":    `{"sourceRoot":"/Users/a","commit":"--upload-pack=x","version":"0.1.0","installedAt":"2026-11-06T10:00:00Z"}`,
		"short commit":      `{"sourceRoot":"/Users/a","commit":"abc","version":"0.1.0","installedAt":"2026-11-06T10:00:00Z"}`,
		"missing version":   `{"sourceRoot":"/Users/a","commit":"0123456789abcdef","installedAt":"2026-11-06T10:00:00Z"}`,
		"spaced version":    `{"sourceRoot":"/Users/a","commit":"0123456789abcdef","version":"0.1 0","installedAt":"2026-11-06T10:00:00Z"}`,
		"bad date":          `{"sourceRoot":"/Users/a","commit":"0123456789abcdef","version":"0.1.0","installedAt":"yesterday"}`,
		"missing date":      `{"sourceRoot":"/Users/a","commit":"0123456789abcdef","version":"0.1.0"}`,
		"wrong field types": `{"sourceRoot":5,` + good + `}`,
		"too large":         `{"sourceRoot":"/Users/a",` + good + `,"pad":"` + strings.Repeat("x", maxRecordSize) + `"}`,
	}
	for name, content := range cases {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, recordFile), content)
		if _, err := readRecord(dir); err != errBadRecord {
			t.Errorf("%s: got %v, want errBadRecord", name, err)
		}
	}
}

func TestReadRecordTellsAbsentFromUnusable(t *testing.T) {
	dir := t.TempDir()
	if _, err := readRecord(dir); err != errNoRecord {
		t.Errorf("absent: %v", err)
	}
	if _, err := readRecord(filepath.Join(dir, "no-such-folder")); err != errNoRecord {
		t.Errorf("absent folder: %v", err)
	}
}

func TestReadRecordRefusesSymlinksAndFolders(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(t.TempDir(), "elsewhere.json")
	writeFile(t, real, `{"sourceRoot":"/Users/a","commit":"0123456789abcdef","version":"0.1.0","installedAt":"2026-11-06T10:00:00Z"}`)
	if err := os.Symlink(real, filepath.Join(dir, recordFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecord(dir); err != errBadRecord {
		t.Errorf("symlinked record: %v", err)
	}
	folder := t.TempDir()
	if err := os.Mkdir(filepath.Join(folder, recordFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecord(folder); err != errBadRecord {
		t.Errorf("folder as record: %v", err)
	}
}
