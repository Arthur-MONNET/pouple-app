# Pouple phone access through an ngrok tunnel — TLS inside a WebSocket

Status: specification (v2). Read with `docs/REMOTE-API.md`. No custom cryptography: this reuses the TLS 1.3 listener,
the pinned self-signed certificate, the device tokens and the step-up signatures that the LAN access already has.

## Why

The free ngrok plan only offers an HTTPS tunnel with a fixed `*.ngrok-free.app` name, and ngrok terminates that TLS,
so it would see every HTTP request. Instead of sending HTTP through the tunnel, the phone and the Mac send *raw TLS
records* through it, carried by a WebSocket. The TLS session is between the phone and the Mac, authenticated by the
Mac's certificate whose SHA-256 fingerprint `fp` the phone pinned at pairing. ngrok only ever carries ciphertext: it
cannot read, forge or replay anything, it can only drop or delay.

## Mac side

- ngrok runs **in-process** through ngrok's official Go SDK (an HTTPS endpoint on the user's static domain, auth token
  from the Keychain); no `ngrok` binary is needed. The SDK is reconnecting by nature; Pouple reports its state. If the
  SDK cannot be used for a reason found during implementation, the fallback is to supervise the `ngrok` binary
  (token in `NGROK_AUTHTOKEN`, never on the command line) forwarding to a loopback listener.
- The tunnel's HTTP handler serves exactly one path, `GET /tunnel/v1`, a WebSocket upgrade (binary messages only).
  Anything else answers 404 with no body. `Origin` headers are refused (no browser).
- Each accepted WebSocket becomes a `net.Conn` (a maintained library provides it, `github.com/coder/websocket`'s
  `NetConn`, ISC) and is handed, as an accepted connection, to the **same** `tls.Server` / `http.Server` as the LAN
  listener (a channel-backed `net.Listener`). So TLS 1.3, the certificate, the handler, the allowlist, the throttle,
  the connection caps and every timeout are shared, not duplicated.
- The connection's `RemoteAddr` is the first `X-Forwarded-For` value that ngrok set on the upgrade request, so the
  source filter, the per-source throttle and the logs see the real external source. Upgrades are counted against the
  throttle per source before the upgrade (30 per 10 minutes, then 429).
- WebSocket read limit 1 MiB per message, ping every 20 s, idle close after 2 minutes without traffic.

## Phone side

- To use the tunnel the app opens a **local loopback forwarder**: a `ServerSocket` bound to `127.0.0.1` on an ephemeral
  port. For every TCP connection its HTTP client makes to it, the forwarder opens one WebSocket to
  `wss://<domain>/tunnel/v1` (outer TLS validated with the system roots; header `ngrok-skip-browser-warning: 1`) and
  pipes bytes both ways. The existing pinned HTTP client then talks to `127.0.0.1:<port>`: the app's pinning code
  (SHA-256 of the DER certificate compared to `fp`, no CA, no host name) is reused as is, for REST and SSE.
- A pin failure on the tunnel endpoint follows the existing rule (hard `certificateMismatch` when the external
  endpoint fails the pin; skip-and-continue for a home address). The tunnel counts as an external endpoint.
- Endpoint order: home addresses, `ext`, then `tun`; the last route that worked is tried first.

## Pairing and address payloads

The QR gains one optional parameter, appended after `ext`: `tun=<domain>` (DNS name, no scheme, port or path; the phone
uses `wss://<domain>:443`). Everything else is unchanged. The address-update payload (`pouple-address:v1`) also accepts
an optional `tun`. `GET /remote/v1/endpoint` returns `{"ext":…,"tun":"<domain>"|null}`.

## Settings (loopback admin API, `/api/remote/settings`)

New fields: `tunnelEnabled` (bool, default false), `tunnelDomain` (the user's ngrok static domain, strict DNS-name
validation), `tunnelAuthtoken` (write-only; stored in the Keychain through `secrets.Store`; never returned, never
logged), read-only `tunnelAuthtokenSet` (bool), `tunnelStatus` (`off|starting|running|error`) and `tunnelProblem`
(a human sentence, empty when fine: bad token, domain not owned by the account, limit reached, no network...).
The tunnel starts with Pouple when enabled and stops when Pouple stops or the setting is turned off.

`enabled` with an empty `interfaces` list now means "automatic": every interface that is up and has a private
(RFC 1918 / ULA) address, never loopback unless chosen explicitly. This is what the simplified settings screen sends.

## Not covered

That a Pouple Mac exists behind a domain, traffic volume and timing, ngrok dropping the connection, and the plan's
own limits (bandwidth, connections).
