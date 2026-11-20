package remote

import (
	"crypto/subtle"
	"errors"
	"sync"
	"time"
)

const (
	// PairingWindow is how long a pairing code can be used.
	PairingWindow = 2 * time.Minute
	// maxPairingAttempts closes the window after this many wrong codes.
	maxPairingAttempts = 5
)

var (
	// ErrPairingClosed is a pairing attempt with no open window: never opened, expired, cancelled or used.
	ErrPairingClosed = errors.New("pairing closed")
	// ErrPairingRefused is a wrong code.
	ErrPairingRefused = errors.New("pairing refused")
)

// Pairing is the one pairing window that can be open at a time: a 128-bit code, valid PairingWindow, used once.
type Pairing struct {
	now func() time.Time

	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
}

func NewPairing(now func() time.Time) *Pairing {
	if now == nil {
		now = time.Now
	}
	return &Pairing{now: now}
}

// Open starts a new window, replacing any earlier one, and returns its code and end.
func (p *Pairing) Open() (string, time.Time, error) {
	code, err := randomHex(16)
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code, p.expires, p.attempts = code, p.now().Add(PairingWindow), 0
	return code, p.expires, nil
}

// Cancel closes the window.
func (p *Pairing) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code = ""
}

// Consume checks a code and closes the window when it matches: a code pairs one device only.
func (p *Pairing) Consume(code string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" || !p.now().Before(p.expires) {
		p.code = ""
		return ErrPairingClosed
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		p.attempts++
		if p.attempts >= maxPairingAttempts {
			p.code = ""
		}
		return ErrPairingRefused
	}
	p.code = ""
	return nil
}
