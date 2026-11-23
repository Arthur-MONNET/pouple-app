package remote

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.ngrok.com/ngrok/v2"
)

// NgrokTunnel is the TunnelProvider that runs ngrok in this process, through ngrok's official Go SDK: an HTTPS endpoint
// on the user's static domain, authenticated by their authtoken. No ngrok binary is involved. The SDK reconnects by
// itself and binds the endpoint again; its losses and recoveries are reported to the supervisor.
func NgrokTunnel() TunnelProvider { return ngrokTunnel{} }

type ngrokTunnel struct{}

// ngrokCode is the error code ngrok puts in the messages it sends (https://ngrok.com/docs/errors).
var ngrokCode = regexp.MustCompile(`ERR_NGROK_(\d+)`)

// classifyNgrok says what the user can do about an error of the SDK, by the ngrok error code in it. An error with no
// code comes from the connection itself (no network, a reset, a timeout): the SDK keeps trying.
func classifyNgrok(err error) TunnelFault {
	match := ngrokCode.FindStringSubmatch(err.Error())
	if match == nil {
		return FaultNetwork
	}
	code, _ := strconv.Atoi(match[1])
	switch code {
	case 105, 106, 107, 300: // the authtoken is malformed, of another version, invalid or revoked
		return FaultToken
	case 307, 308, 309, 310, 311, 313, 314, 316, 317, 318, 319, 320, 321, 322, 326, 327, 328, 329, 330, 331:
		// the domain is not reserved by this account, not allowed to it or not valid
		return FaultDomain
	case 108, 324, 334: // too many sessions or endpoints, or the endpoint is already online
		return FaultLimit
	case 102, 103, 122, 123, 337: // the account is unpaid, suspended or unverified
		return FaultAccount
	}
	return FaultOther
}

func (ngrokTunnel) Listen(ctx context.Context, domain, authtoken string, report TunnelReport) (net.Listener, error) {
	// The session lives as long as this context: the SDK closes it when it ends.
	session, cancel := context.WithCancel(ctx)
	var (
		mu       sync.Mutex
		ready    bool
		fatalErr error
	)
	handler := func(event ngrok.Event) {
		switch e := event.(type) {
		case *ngrok.EventAgentConnectSucceeded:
			mu.Lock()
			online := ready
			mu.Unlock()
			if online {
				report(true, nil)
			}
		case *ngrok.EventAgentDisconnected:
			if e.Error == nil || session.Err() != nil {
				return
			}
			fault := classifyNgrok(e.Error)
			err := &TunnelError{Fault: fault, Err: withoutToken(e.Error, authtoken)}
			if fault.userFixable() {
				mu.Lock()
				if fatalErr == nil {
					fatalErr = err
				}
				mu.Unlock()
				report(false, err)
				cancel()
				return
			}
			report(false, err)
		}
	}
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(authtoken),
		ngrok.WithAutoConnect(false),
		ngrok.WithEventHandler(handler),
		ngrok.WithAgentDescription("Pouple phone access"),
		ngrok.WithLogger(ngrokLogger(authtoken)),
	)
	if err != nil {
		cancel()
		return nil, &TunnelError{Fault: FaultOther, Err: withoutToken(err, authtoken)}
	}
	fail := func(err error) (net.Listener, error) {
		mu.Lock()
		stored := fatalErr
		mu.Unlock()
		cancel()
		_ = agent.Disconnect()
		if stored != nil {
			return nil, stored
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &TunnelError{Fault: classifyNgrok(err), Err: withoutToken(err, authtoken)}
	}
	// Connect keeps trying while the network is down (the failures arrive as events), and returns once connected, or
	// when the handler gave up on a fault only the user can fix.
	if err := agent.Connect(session); err != nil {
		return fail(err)
	}
	endpoint, err := agent.Listen(session, ngrok.WithURL("https://"+domain), ngrok.WithDescription("Pouple phone access"))
	if err != nil {
		return fail(err)
	}
	mu.Lock()
	ready = true
	mu.Unlock()
	report(true, nil)
	return &ngrokListener{EndpointListener: endpoint, agent: agent, cancel: cancel}, nil
}

// ngrokListener closes the whole session with its endpoint.
type ngrokListener struct {
	ngrok.EndpointListener
	agent  ngrok.Agent
	cancel context.CancelFunc
}

func (l *ngrokListener) Close() error {
	err := l.EndpointListener.Close()
	_ = l.agent.Disconnect()
	l.cancel()
	return err
}

// withoutToken is an error whose text never holds the authtoken.
func withoutToken(err error, token string) error {
	if err == nil {
		return nil
	}
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "[authtoken]"))
}

// ngrokLogger sends the SDK's warnings and errors to the core's log, with the authtoken cut out of each line.
func ngrokLogger(authtoken string) *slog.Logger {
	return slog.New(slog.NewTextHandler(redactingWriter{token: authtoken}, &slog.HandlerOptions{
		Level: slog.LevelWarn,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	}))
}

type redactingWriter struct{ token string }

func (w redactingWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if w.token != "" {
		line = strings.ReplaceAll(line, w.token, "[authtoken]")
	}
	log.Printf("ngrok: %s", line)
	return len(p), nil
}
