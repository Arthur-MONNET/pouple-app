package updates

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// recordFile is written by scripts/install-local.sh at the end of a successful installation.
const recordFile = "install.json"

const maxRecordSize = 64 << 10

var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// Record is what the installer remembered about the build that is running: where its source lives.
type Record struct {
	SourceRoot  string    `json:"sourceRoot"`
	Commit      string    `json:"commit"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
}

var (
	// errNoRecord means there is no install record: Pouple was not installed by the source installer.
	errNoRecord = errors.New("no install record")
	// errBadRecord means a record exists but cannot be trusted.
	errBadRecord = errors.New("invalid install record")
)

// readRecord loads and validates dir/install.json. The file must be a regular file (a symlink is refused, since
// whoever controls it would control which folder the updater runs git in) of a sane size, and every field is
// checked: the source root must be an absolute, already clean path.
func readRecord(dir string) (Record, error) {
	path := filepath.Join(dir, recordFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, errNoRecord
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRecordSize {
		return Record{}, errBadRecord
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, errBadRecord
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, errBadRecord
	}
	if !validRoot(record.SourceRoot) || !commitPattern.MatchString(record.Commit) ||
		!validVersion(record.Version) || record.InstalledAt.IsZero() {
		return Record{}, errBadRecord
	}
	return record, nil
}

func validRoot(root string) bool {
	return root != "" && !strings.ContainsRune(root, 0) && filepath.IsAbs(root) &&
		filepath.Clean(root) == root && root != string(filepath.Separator)
}

func validVersion(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	for _, r := range version {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
