// Package remote is the access of the paired phones to this Mac: a TLS listener on the network interfaces the user
// chose, devices paired with a single-use code, and a handler that serves an explicit allowlist of the assistant's
// routes, with a fingerprint-bound signature (step-up) for what writes or approves. It is off unless the user turns
// it on; the loopback API and the unix socket are not touched by it. The contract the phone follows is
// docs/REMOTE-API.md.
package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	// TokenTTL is how long a device token lasts without being used.
	TokenTTL = 30 * 24 * time.Hour
	// maxDevices caps the devices that can be paired at once.
	maxDevices = 10
	// touchEvery is how often a device's last use is written to disk: the expiry counts in days.
	touchEvery = time.Hour
)

var (
	// ErrUnauthorized is a token that is missing, unknown, revoked or expired.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrUnknownDevice is a device id that was never paired.
	ErrUnknownDevice = errors.New("unknown device")
	// ErrTooManyDevices is returned when the device limit is reached.
	ErrTooManyDevices = errors.New("too many devices")
)

// Device is a paired phone as kept on disk. Its token is never stored: only its SHA-256.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"`
	// StepUpPublicKey is the device's P-256 public key, base64 of its X.509 SubjectPublicKeyInfo (DER).
	StepUpPublicKey string    `json:"stepUpPublicKey"`
	CreatedAt       time.Time `json:"createdAt"`
	LastSeen        time.Time `json:"lastSeen"`
	Revoked         bool      `json:"revoked"`
}

// DeviceInfo is what the settings show about a device: never its token hash nor its key.
type DeviceInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
	Revoked   bool      `json:"revoked"`
	// Expired is set when the token went unused for longer than TokenTTL.
	Expired bool `json:"expired"`
}

// Devices are the paired phones, kept in a 0600 file, and the requests each one has open (so a revocation closes
// them, streams included).
type Devices struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	devices []Device
	written map[string]time.Time // last LastSeen written to disk, per device
	open    map[string]map[*openRequest]struct{}
}

type openRequest struct{ cancel context.CancelFunc }

// OpenDevices reads the devices file; a missing file is no device.
func OpenDevices(path string, now func() time.Time) (*Devices, error) {
	if now == nil {
		now = time.Now
	}
	d := &Devices{path: path, now: now, written: map[string]time.Time{}, open: map[string]map[*openRequest]struct{}{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Devices []Device `json:"devices"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		// An unreadable file must not stop the app: it is set aside and no phone is paired (they pair again), which
		// never lets a device in that the user did not pair.
		if renameErr := os.Rename(path, path+".unreadable"); renameErr != nil {
			return nil, fmt.Errorf("%s is unreadable (%v) and could not be set aside: %w", path, err, renameErr)
		}
		log.Printf("remote access: %s was unreadable (%v): it was set aside, and the phones must pair again", path, err)
		return d, nil
	}
	d.devices = file.Devices
	for _, device := range d.devices {
		d.written[device.ID] = device.LastSeen
	}
	return d, nil
}

func (d *Devices) save() error {
	data, err := json.MarshalIndent(struct {
		Devices []Device `json:"devices"`
	}{Devices: d.devices}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return err
	}
	temporary := d.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, d.path)
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// expired says whether a device's token went unused too long.
func (d *Devices) expired(device Device) bool {
	last := device.LastSeen
	if last.IsZero() {
		last = device.CreatedAt
	}
	return d.now().Sub(last) > TokenTTL
}

func (d *Devices) active(device Device) bool { return !device.Revoked && !d.expired(device) }

// Add pairs a device and returns it with its token: a 256-bit secret shown once, to the phone.
func (d *Devices) Add(name, publicKey string) (Device, string, error) {
	if _, err := ParsePublicKey(publicKey); err != nil {
		return Device{}, "", err
	}
	id, err := randomHex(16)
	if err != nil {
		return Device{}, "", err
	}
	token, err := randomHex(32)
	if err != nil {
		return Device{}, "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	active := 0
	for _, device := range d.devices {
		if d.active(device) {
			active++
		}
	}
	if active >= maxDevices {
		return Device{}, "", ErrTooManyDevices
	}
	now := d.now().UTC()
	device := Device{ID: id, Name: name, TokenHash: hex.EncodeToString(hashToken(token)), StepUpPublicKey: publicKey, CreatedAt: now, LastSeen: now}
	d.devices = append(d.devices, device)
	if err := d.save(); err != nil {
		d.devices = d.devices[:len(d.devices)-1]
		return Device{}, "", err
	}
	d.written[id] = now
	return device, token, nil
}

// Authenticate finds the active device of a token and notes its use.
func (d *Devices) Authenticate(token string) (Device, error) {
	if len(token) != 64 {
		return Device{}, ErrUnauthorized
	}
	given := hashToken(token)
	d.mu.Lock()
	defer d.mu.Unlock()
	index := -1
	for i, device := range d.devices {
		stored, err := hex.DecodeString(device.TokenHash)
		if err == nil && subtle.ConstantTimeCompare(given, stored) == 1 {
			index = i
		}
	}
	if index < 0 || !d.active(d.devices[index]) {
		return Device{}, ErrUnauthorized
	}
	now := d.now().UTC()
	d.devices[index].LastSeen = now
	device := d.devices[index]
	if now.Sub(d.written[device.ID]) >= touchEvery {
		// The last use is best effort: a failed write only shortens the token's life by the time since the last one.
		if d.save() == nil {
			d.written[device.ID] = now
		}
	}
	return device, nil
}

// Get returns a device by id, active or not.
func (d *Devices) Get(id string) (Device, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	index := slices.IndexFunc(d.devices, func(device Device) bool { return device.ID == id })
	if index < 0 {
		return Device{}, false
	}
	return d.devices[index], true
}

// List returns what the settings show about each device, the latest paired first.
func (d *Devices) List() []DeviceInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DeviceInfo, 0, len(d.devices))
	for i := len(d.devices) - 1; i >= 0; i-- {
		device := d.devices[i]
		out = append(out, DeviceInfo{ID: device.ID, Name: device.Name, CreatedAt: device.CreatedAt, LastSeen: device.LastSeen, Revoked: device.Revoked, Expired: d.expired(device)})
	}
	return out
}

// Revoke ends a device's access for good, and closes the requests it has open (its streams included).
func (d *Devices) Revoke(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	index := slices.IndexFunc(d.devices, func(device Device) bool { return device.ID == id })
	if index < 0 {
		return ErrUnknownDevice
	}
	// Closed first: even if the file cannot be written, nothing stays open for a device the user revoked.
	for request := range d.open[id] {
		request.cancel()
	}
	delete(d.open, id)
	if d.devices[index].Revoked {
		return nil
	}
	d.devices[index].Revoked = true
	if err := d.save(); err != nil {
		d.devices[index].Revoked = false
		return err
	}
	return nil
}

// track registers a request of a device so Revoke can cancel it; the returned function ends the registration. It
// refuses a device that is no longer active (revoked between authentication and now).
func (d *Devices) track(id string, cancel context.CancelFunc) (func(), bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	index := slices.IndexFunc(d.devices, func(device Device) bool { return device.ID == id })
	if index < 0 || !d.active(d.devices[index]) {
		return nil, false
	}
	request := &openRequest{cancel: cancel}
	if d.open[id] == nil {
		d.open[id] = map[*openRequest]struct{}{}
	}
	d.open[id][request] = struct{}{}
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.open[id], request)
		if len(d.open[id]) == 0 {
			delete(d.open, id)
		}
	}, true
}
