package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	// NonceTTL is how long a step-up nonce can be used.
	NonceTTL = 60 * time.Second
	// maxNoncesPerDevice bounds the nonces a device can hold at once.
	maxNoncesPerDevice = 8
	// maxSignatureLength is far above a DER P-256 signature (at most 72 bytes, 96 in base64).
	maxSignatureLength = 256
)

var (
	// ErrBadKey is a step-up public key that is not a P-256 key in X.509 SubjectPublicKeyInfo form.
	ErrBadKey = errors.New("the step-up key must be a P-256 public key (base64 of its SubjectPublicKeyInfo DER)")
	// ErrBadSignature is a step-up that does not verify: no nonce, a used or expired one, or a wrong signature.
	ErrBadSignature = errors.New("step-up signature refused")
)

// ParsePublicKey reads a device's step-up key: base64 (standard, padded) of a P-256 SubjectPublicKeyInfo DER, as
// Android's KeyStore gives it with PublicKey.getEncoded().
func ParsePublicKey(encoded string) (*ecdsa.PublicKey, error) {
	if len(encoded) > 512 {
		return nil, ErrBadKey
	}
	der, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrBadKey
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, ErrBadKey
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, ErrBadKey
	}
	return key, nil
}

// Nonces are the one-time challenges a step-up signs. Each one belongs to the device that asked for it, lasts
// NonceTTL and is spent by its first use, whether the signature then verifies or not.
type Nonces struct {
	now func() time.Time

	mu     sync.Mutex
	issued map[string]issuedNonce
}

type issuedNonce struct {
	device  string
	expires time.Time
}

func NewNonces(now func() time.Time) *Nonces {
	if now == nil {
		now = time.Now
	}
	return &Nonces{now: now, issued: map[string]issuedNonce{}}
}

// Issue returns a new nonce for a device. The oldest of the device's nonces is dropped when it holds too many.
func (n *Nonces) Issue(device string) (string, time.Time, error) {
	nonce, err := randomHex(32)
	if err != nil {
		return "", time.Time{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	now := n.now()
	held, oldest, oldestAt := 0, "", time.Time{}
	for value, issued := range n.issued {
		if !now.Before(issued.expires) {
			delete(n.issued, value)
			continue
		}
		if issued.device == device {
			held++
			if oldest == "" || issued.expires.Before(oldestAt) {
				oldest, oldestAt = value, issued.expires
			}
		}
	}
	if held >= maxNoncesPerDevice {
		delete(n.issued, oldest)
	}
	expires := now.Add(NonceTTL)
	n.issued[nonce] = issuedNonce{device: device, expires: expires}
	return nonce, expires, nil
}

// spend removes a nonce and says whether it was the device's and still valid.
func (n *Nonces) spend(nonce, device string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	issued, ok := n.issued[nonce]
	if !ok {
		return false
	}
	delete(n.issued, nonce)
	return issued.device == device && n.now().Before(issued.expires)
}

// StepUp is what a step-up signature covers. Its canonical form is the exact bytes the phone signs: see
// docs/REMOTE-API.md, "Step-up".
type StepUp struct {
	Nonce     string
	DeviceID  string
	Route     string
	RequestID string
	PlanID    string
	// ProposalID is the proposal approved; empty for any other route.
	ProposalID string
	// Actions are the plan actions ticked; nil when the body has none (every action), and for any other route.
	Actions *[]int
	// BodySHA256 is the SHA-256 of the raw request body, in lowercase hex.
	BodySHA256 string
}

// Canonical is the JSON the signature covers: the fields always present, in this order, without spaces. Every string
// is checked to be made of [A-Za-z0-9._:-] (or, for the route, of the routes' own characters), so no escaping can
// make two encoders disagree.
func (s StepUp) Canonical() ([]byte, error) {
	for _, value := range []string{s.Nonce, s.DeviceID, s.RequestID, s.PlanID, s.ProposalID, s.BodySHA256} {
		if !canonicalValue(value) {
			return nil, ErrBadSignature
		}
	}
	if !canonicalRoute(s.Route) {
		return nil, ErrBadSignature
	}
	return json.Marshal(struct {
		V          int    `json:"v"`
		Nonce      string `json:"nonce"`
		DeviceID   string `json:"deviceId"`
		Route      string `json:"route"`
		RequestID  string `json:"requestId"`
		PlanID     string `json:"planId"`
		ProposalID string `json:"proposalId"`
		Actions    *[]int `json:"actions"`
		BodySHA256 string `json:"bodySha256"`
	}{1, s.Nonce, s.DeviceID, s.Route, s.RequestID, s.PlanID, s.ProposalID, s.Actions, s.BodySHA256})
}

func canonicalValue(value string) bool {
	if len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == ':' || r == '-') {
			return false
		}
	}
	return true
}

func canonicalRoute(route string) bool {
	if route == "" || len(route) > 128 {
		return false
	}
	for _, r := range route {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '/' || r == '{' || r == '}' || r == ' ' || r == '-') {
			return false
		}
	}
	return true
}

// Verify spends the nonce and checks the signature (standard base64 of an ASN.1 DER ECDSA signature, SHA-256 over
// the canonical JSON) with the device's key.
func (n *Nonces) Verify(publicKey string, payload StepUp, signature string) error {
	// Spent first: a nonce is good for one attempt, so a wrong signature cannot be retried with it.
	if payload.Nonce == "" || !n.spend(payload.Nonce, payload.DeviceID) {
		return ErrBadSignature
	}
	key, err := ParsePublicKey(publicKey)
	if err != nil {
		return ErrBadSignature
	}
	if signature == "" || len(signature) > maxSignatureLength {
		return ErrBadSignature
	}
	der, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrBadSignature
	}
	message, err := payload.Canonical()
	if err != nil {
		return ErrBadSignature
	}
	digest := sha256.Sum256(message)
	if !ecdsa.VerifyASN1(key, digest[:], der) {
		return ErrBadSignature
	}
	return nil
}
