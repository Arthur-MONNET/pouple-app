package secrets

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const keychainBinary = "/usr/bin/security"

// Keychain stores secrets as generic passwords in the user's login keychain.
//
// The value goes to `security` through stdin (its interactive mode), never through the argument list, which any
// local process can read with `ps`. It is base64-encoded first, so no character of a token can end a command.
type Keychain struct {
	Service string
	// run is replaced in tests.
	run func(stdin string, args ...string) (string, error)
}

func NewKeychain(service string) *Keychain {
	return &Keychain{Service: service, run: runSecurity}
}

func runSecurity(stdin string, args ...string) (string, error) {
	cmd := exec.Command(keychainBinary, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		// `security -i` may echo the command (which carries the encoded secret): only the lookups keep its message.
		if len(args) > 0 && args[0] == "-i" {
			return "", fmt.Errorf("the keychain refused the write (%w)", err)
		}
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

// validName keeps a name safe to place in a `security -i` command line.
func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (k *Keychain) Get(name string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("invalid secret name %q", name)
	}
	out, err := k.run("", "find-generic-password", "-s", k.Service, "-a", name, "-w")
	if err != nil {
		// Exit code 44 is "item not found".
		if strings.Contains(err.Error(), "could not be found") || strings.Contains(err.Error(), "exit status 44") {
			return "", ErrNotFound
		}
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil {
		return "", errors.New("the keychain item is not one Pouple wrote")
	}
	return string(decoded), nil
}

func (k *Keychain) Set(name, value string) error {
	if !validName(name) || !validName(k.Service) {
		return fmt.Errorf("invalid secret name %q", name)
	}
	command := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", k.Service, name, base64.StdEncoding.EncodeToString([]byte(value)))
	_, err := k.run(command, "-i")
	return err
}

func (k *Keychain) Delete(name string) error {
	if !validName(name) {
		return fmt.Errorf("invalid secret name %q", name)
	}
	_, err := k.run("", "delete-generic-password", "-s", k.Service, "-a", name)
	if err != nil && (strings.Contains(err.Error(), "could not be found") || strings.Contains(err.Error(), "exit status 44")) {
		return nil
	}
	return err
}
