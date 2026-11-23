package remote

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
)

var _ TunnelProvider = NgrokTunnel()

func TestNgrokErrorsAreClassifiedByTheirCode(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    TunnelFault
	}{
		{"authentication failed: The authtoken you specified does not look like a proper ngrok authtoken.\n\nERR_NGROK_105", FaultToken},
		{"ERR_NGROK_106", FaultToken},
		{"ERR_NGROK_107", FaultToken},
		{"ERR_NGROK_300", FaultToken},
		{"failed to start tunnel: ERR_NGROK_320: domain reserved for another account", FaultDomain},
		{"ERR_NGROK_319", FaultDomain},
		{"ERR_NGROK_309", FaultDomain},
		{"ERR_NGROK_314", FaultDomain},
		{"ERR_NGROK_108", FaultLimit},
		{"ERR_NGROK_334", FaultLimit},
		{"ERR_NGROK_324", FaultLimit},
		{"ERR_NGROK_103", FaultAccount},
		{"ERR_NGROK_337", FaultAccount},
		{"ERR_NGROK_123", FaultAccount},
		{"ERR_NGROK_9999", FaultOther},
		{"dial tcp: lookup connect.ngrok-agent.com: no such host", FaultNetwork},
		{"read tcp 192.168.1.2:5000->3.0.0.1:443: i/o timeout", FaultNetwork},
		{"EOF", FaultNetwork},
	} {
		if got := classifyNgrok(errors.New(tc.message)); got != tc.want {
			t.Errorf("%q: %d, want %d", tc.message, got, tc.want)
		}
	}
	// Only what the user must fix stops the attempts; a lost network or a limit is retried by the SDK.
	for fault, want := range map[TunnelFault]bool{FaultToken: true, FaultDomain: true, FaultAccount: true, faultKeychain: true, FaultLimit: false, FaultNetwork: false, FaultOther: false, faultStopped: false} {
		if fault.userFixable() != want {
			t.Errorf("fault %d: userFixable %v", fault, !want)
		}
	}
}

func TestNgrokAuthtokenNeverReachesAnErrorOrTheLog(t *testing.T) {
	const token = "2abcDEF_ghiJKL0123456789"
	err := withoutToken(errors.New("failed to authenticate with "+token+" on ngrok"), token)
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[authtoken]") {
		t.Fatalf("%v", err)
	}
	original := errors.New("no token in this one")
	if withoutToken(original, token) != original || withoutToken(nil, token) != nil {
		t.Fatal("an error without the token is returned as it is")
	}

	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	logger := ngrokLogger(token)
	logger.Info("connected", "detail", "not shown at this level")
	logger.Warn("reconnecting", "err", "dial failed for "+token)
	logger.Error("gave up", "auth", token)
	text := out.String()
	if strings.Contains(text, token) {
		t.Fatalf("the log holds the authtoken: %s", text)
	}
	if !strings.Contains(text, "ngrok: ") || !strings.Contains(text, "reconnecting") || !strings.Contains(text, "gave up") || strings.Contains(text, "connected") {
		t.Fatalf("warnings and errors only, prefixed: %q", text)
	}
	if strings.Contains(text, "time=") {
		t.Fatalf("the log adds its own time: %q", text)
	}
}

func TestTheTunnelLogLineHasNoAuthtoken(t *testing.T) {
	const token = "ngrok_test_0123456789abcdef"
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	(&Service{}).logTunnel(tunnelConfig{On: true, Domain: "a.ngrok.app", Authtoken: token}, errors.New("refused "+token))
	if strings.Contains(out.String(), token) || !strings.Contains(out.String(), "a.ngrok.app") {
		t.Fatalf("%q", out.String())
	}
}

// ngrok's text about a refused authtoken can repeat what was sent, in any form: only its code is kept and logged.
func TestATokenFaultKeepsAndLogsOnlyTheNgrokCode(t *testing.T) {
	const token = "2abcDEF_ghiJKL0123456789"
	text := "authentication failed: The authtoken you specified (2abcDEF_ghi...) does not look like a proper ngrok authtoken. " + token[:10] + "\n\nERR_NGROK_105"
	err := errors.New(text)
	fault := classifyNgrok(err)
	if fault != FaultToken {
		t.Fatalf("%d", fault)
	}
	safe := safeNgrokError(fault, err, token)
	if safe.Error() != "authtoken refused (ERR_NGROK_105)" {
		t.Fatalf("%q", safe)
	}
	if got := safeNgrokError(FaultToken, errors.New("no code in this one "+token), token); got.Error() != "authtoken refused" {
		t.Fatalf("%q", got)
	}
	// Other faults keep their text, minus the token.
	if got := safeNgrokError(FaultDomain, errors.New("ERR_NGROK_320 "+token), token); strings.Contains(got.Error(), token) || !strings.Contains(got.Error(), "ERR_NGROK_320") {
		t.Fatalf("%q", got)
	}

	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	(&Service{}).logTunnel(tunnelConfig{On: true, Domain: "a.ngrok.app", Authtoken: token}, &TunnelError{Fault: FaultToken, Err: err})
	if strings.Contains(out.String(), token[:8]) || strings.Contains(out.String(), "proper ngrok") || !strings.Contains(out.String(), "ERR_NGROK_105") {
		t.Fatalf("only the code is logged: %q", out.String())
	}
	// Even a TunnelError built elsewhere, with the whole text, is cut down to the code.
	out.Reset()
	(&Service{}).logTunnel(tunnelConfig{On: true, Domain: "a.ngrok.app", Authtoken: token}, &TunnelError{Fault: FaultToken, Err: errors.New("raw " + text)})
	if strings.Contains(out.String(), "raw") || strings.Contains(out.String(), token[:8]) {
		t.Fatalf("%q", out.String())
	}

	// The SDK's own log lines about it are cut down too, whatever form of the token they hold.
	out.Reset()
	ngrokLogger(token).Warn("failed to reconnect session", "err", text)
	ngrokLogger(token).Warn("other", "err", "dial tcp: no route")
	got := out.String()
	if strings.Contains(got, "2abcDEF") || strings.Contains(got, "proper ngrok") || !strings.Contains(got, "authtoken refused (ERR_NGROK_105)") || !strings.Contains(got, "no route") {
		t.Fatalf("%q", got)
	}
}
