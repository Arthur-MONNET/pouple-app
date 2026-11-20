package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/secrets"
)

// tlsKeySecret names the remote listener's private key in the secrets store (the Keychain on macOS).
const tlsKeySecret = "remote-tls-key"

// certValidity is long on purpose: the phone pins the certificate's fingerprint, never a chain or a date.
const certValidity = 10 * 365 * 24 * time.Hour

// Certificate is the remote listener's self-signed certificate and the fingerprint the phone pins.
type Certificate struct {
	TLS tls.Certificate
	// Fingerprint is the SHA-256 of the certificate's DER, in lowercase hex.
	Fingerprint string
}

// LoadOrCreateCertificate returns the remote listener's certificate: its private key comes from the secrets store,
// the public certificate from certPath. When either is missing or they do not match, a new pair is made, so the
// fingerprint changes and the phones must pair again.
func LoadOrCreateCertificate(store secrets.Store, certPath string) (Certificate, error) {
	if certificate, err := loadCertificate(store, certPath); err == nil {
		return certificate, nil
	} else if !errors.Is(err, secrets.ErrNotFound) && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errMismatch) {
		// A Keychain that refuses to answer must not make the fingerprint change behind the user's back.
		return Certificate{}, err
	}
	return createCertificate(store, certPath)
}

var errMismatch = errors.New("the stored key and certificate do not match")

func loadCertificate(store secrets.Store, certPath string) (Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return Certificate{}, err
	}
	keyPEM, err := store.Get(tlsKeySecret)
	if err != nil {
		return Certificate{}, err
	}
	pair, err := tls.X509KeyPair(certPEM, []byte(keyPEM))
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: %v", errMismatch, err)
	}
	return certificateOf(pair)
}

func createCertificate(store secrets.Store, certPath string) (Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Pouple remote access"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	// The key goes to the store first: a certificate on disk without its key would be replaced at the next start anyway.
	if err := store.Set(tlsKeySecret, string(keyPEM)); err != nil {
		return Certificate{}, err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return Certificate{}, err
	}
	temporary := certPath + ".tmp"
	if err := os.WriteFile(temporary, certPEM, 0o600); err != nil {
		return Certificate{}, err
	}
	if err := os.Rename(temporary, certPath); err != nil {
		return Certificate{}, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Certificate{}, err
	}
	return certificateOf(pair)
}

func certificateOf(pair tls.Certificate) (Certificate, error) {
	if len(pair.Certificate) == 0 {
		return Certificate{}, errors.New("empty certificate")
	}
	return Certificate{TLS: pair, Fingerprint: Fingerprint(pair.Certificate[0])}, nil
}

// Fingerprint is the SHA-256 of a certificate's DER, in lowercase hex: what the phone compares when it connects.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
