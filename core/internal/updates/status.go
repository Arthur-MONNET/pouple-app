package updates

import (
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Reason tells why updates are not offered.
type Reason string

const (
	ReasonSupported       Reason = ""
	ReasonNotFromSource   Reason = "not-installed-from-source"
	ReasonCheckoutMissing Reason = "checkout-missing"
	ReasonCheckoutBad     Reason = "checkout-invalid"
)

// BlockerCode names something in the source checkout that would stop the update from going through.
type BlockerCode string

const (
	BlockerLocalChanges BlockerCode = "local-changes"
	BlockerNotOnMain    BlockerCode = "not-on-main"
	BlockerDiverged     BlockerCode = "diverged"
)

// failure is why the last check could not finish.
type failure string

const (
	failureNone    failure = ""
	failureOffline failure = "offline"
	failureAuth    failure = "auth"
	failureTimeout failure = "timeout"
	failureGit     failure = "git"
)

// Result is what the last check found, before it is worded for a language.
type Result struct {
	Supported bool
	Reason    Reason
	Installed *Installed
	// Latest is the short commit of origin/main, empty before the first successful check.
	Latest string
	// Behind is how many commits of origin/main the running build does not have.
	Behind    int
	Changes   []string
	Blockers  []BlockerCode
	CheckedAt time.Time
	Checking  bool
	failure   failure
	// buildCommit is the full commit of the running build, from the install record.
	buildCommit string
}

// Installed describes the build that is running.
type Installed struct {
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	InstalledAt string `json:"installedAt"`
}

// Latest describes the newest build available.
type Latest struct {
	Commit string `json:"commit"`
}

// Blocker is a worded blocker.
type Blocker struct {
	Code    BlockerCode `json:"code"`
	Message string      `json:"message"`
}

// Status is the JSON of GET /api/update and POST /api/update/check.
type Status struct {
	Supported bool       `json:"supported"`
	Reason    Reason     `json:"reason"`
	Message   string     `json:"message"`
	Installed *Installed `json:"installed"`
	Latest    *Latest    `json:"latest"`
	Available bool       `json:"available"`
	Behind    int        `json:"behind"`
	Changes   []string   `json:"changes"`
	Blockers  []Blocker  `json:"blockers"`
	CheckedAt *string    `json:"checkedAt"`
	Checking  bool       `json:"checking"`
	Error     *string    `json:"error"`
}

// Status words the result in a language.
func (r Result) Status(lang i18n.Lang) Status {
	status := Status{
		Supported: r.Supported, Reason: r.Reason, Installed: r.Installed,
		Available: r.Supported && r.Behind > 0, Behind: r.Behind,
		Changes: append([]string{}, r.Changes...), Blockers: []Blocker{}, Checking: r.Checking,
	}
	if !r.Supported {
		status.Message = i18n.T(lang, unsupportedText(r.Reason))
		status.Changes = []string{}
		status.Behind = 0
		return status
	}
	if r.Latest != "" {
		status.Latest = &Latest{Commit: r.Latest}
	}
	for _, code := range r.Blockers {
		status.Blockers = append(status.Blockers, Blocker{Code: code, Message: i18n.T(lang, blockerText(code))})
	}
	if !r.CheckedAt.IsZero() {
		checked := r.CheckedAt.UTC().Format(time.RFC3339)
		status.CheckedAt = &checked
	}
	if r.failure != failureNone {
		text := i18n.T(lang, failureText(r.failure))
		status.Error = &text
	}
	return status
}

func unsupportedText(reason Reason) string {
	switch reason {
	case ReasonCheckoutMissing:
		return "The source folder this copy of Pouple was built from can no longer be found. Clone Pouple again and run scripts/install-local.sh from the new folder."
	case ReasonCheckoutBad:
		return "The source folder recorded when Pouple was installed is not a valid Pouple checkout, so Pouple will not update from it. Reinstall with scripts/install-local.sh."
	default:
		return "Updates from the app are available only when Pouple was installed from its source with scripts/install-local.sh."
	}
}

func blockerText(code BlockerCode) string {
	switch code {
	case BlockerLocalChanges:
		return "The source folder has local changes. Commit or discard them before updating."
	case BlockerNotOnMain:
		return "The source folder is not on the main branch. Switch it to main before updating."
	default:
		return "The source folder has commits that are not on origin/main, so it cannot be fast-forwarded. Push or move them first."
	}
}

func failureText(f failure) string {
	switch f {
	case failureOffline:
		return "GitHub could not be reached. Check your connection and try again."
	case failureAuth:
		return "GitHub refused access to the Pouple repository. Check the SSH key or credentials your Mac uses for it."
	case failureTimeout:
		return "Checking for updates took too long. Try again later."
	default:
		return "Pouple could not check for updates. Try again later."
	}
}
