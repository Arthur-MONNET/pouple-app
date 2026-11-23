# Pouple phone access through an ngrok tunnel — TLS inside a WebSocket

Status: the Mac side is implemented (`core/internal/remote/tunnel*.go`); the phone side is specified below. Read with
`docs/REMOTE-API.md`, which holds the settings, the payloads and the limits. No custom cryptography: this reuses the
TLS 1.3 listener, the pinned self-signed certificate, the device tokens and the step-up signatures that the LAN access
already has.

## Why

The free ngrok plan only offers an HTTPS tunnel with a fixed `*.ngrok-free.app` name, and ngrok terminates that TLS,
so it would see every HTTP request. Instead of sending HTTP through the tunnel, the phone and the Mac send *raw TLS
records* through it, carried by a WebSocket. The TLS session is between the phone and the Mac, authenticated by the
Mac's certificate whose SHA-256 fingerprint `fp` the phone pinned at pairing. ngrok only ever carries ciphertext: it
cannot read, forge or replay anything, it can only drop or delay.

## Mac side

- ngrok runs **in-process** through ngrok's official Go SDK, `golang.ngrok.com/ngrok/v2` (pinned at v2.6.0; the older
  `golang.ngrok.com/ngrok` v1 is the previous major): `agent.Listen(ctx, ngrok.WithURL("https://<domain>"))`, with the
  authtoken from the Keychain (`secrets.Store`, name `ngrok-authtoken`). No `ngrok` binary is needed, and nothing
  stopped the SDK from serving a free account, so the supervised-binary fallback was not needed. The SDK reconnects and
  binds the endpoint again by itself; Pouple reports its losses and recoveries in `tunnelStatus`/`tunnelProblem`. An error
  that only the user can fix (authtoken, domain, account) stops the SDK's attempts, which would otherwise repeat forever.
  The SDK sits behind a small interface (`TunnelProvider`), so tests stand a local listener in for ngrok.
- The tunnel's HTTP handler serves exactly one path, `GET /tunnel/v1`, a WebSocket upgrade (binary messages only).
  Anything else answers 404 with no body. `Origin` headers are refused (no browser).
- Each accepted WebSocket becomes a `net.Conn` (a maintained library provides it, `github.com/coder/websocket`'s
  `NetConn`, ISC) and is handed, as an accepted connection, to the **same** `tls.Server` / `http.Server` as the LAN
  listener (a channel-backed `net.Listener`). So TLS 1.3, the certificate, the handler, the allowlist, the throttle,
  the connection caps and every timeout are shared, not duplicated.
- The connection's `RemoteAddr` is the address ngrok reports for it, **not** `X-Forwarded-For` (a deviation from the
  first draft, which said the first value): the SDK sets each connection's address from the client address in the proxy
  header of ngrok's own stream (`ClientAddr`), which a client cannot touch, whereas `X-Forwarded-For` is a header the
  client may fill and ngrok only appends to. No forwarded header is read. A connection with no readable address shares
  one bucket, like in the throttle. The throttle, the caps and the logs then see the real external source.
- A tunnel connection is **never local**: its requests carry a mark, so they get the external limits (6 failures, not 30)
  and their wrong pairing codes never count toward closing the pairing window, even when the address is private, or on
  the Mac's own IPv6 network.
- The upgrade budget per source is a token bucket (30 at once, one more every 20 s, then 429), counted before the upgrade.
  A request that is not a WebSocket upgrade is a 404 with no body that closes the connection and costs nothing. A
  connection that completes an authenticated request gives its upgrade back, and the 16-per-source cap on connections
  does not apply to a source with an authenticated connection open: a paired phone behind a carrier NAT is not locked out
  by a scanner sharing it. The total caps always apply.
- The server that reads the upgrade requests is not the LAN's: 10 s for headers, 15 s to read and write, 30 s idle,
  16 KiB of headers, and its own caps (256, 16 per source) on connections that have not upgraded. Its timeouts are
  cleared when a connection is taken over by the WebSocket.
- The tunnel is reachable from the Internet whatever `externalEnabled` says: the source filter belongs to the LAN
  listeners. See "Access through a tunnel" in `REMOTE-API.md`.
- WebSocket read limit 1 MiB per message, ping every 20 s, idle close after 2 minutes without data.
- `websocket.NetConn` closes the whole connection when a read deadline expires during a read, which `net/http` does
  after every response (it interrupts its background read with a deadline in the past, then clears it): each request
  would end the tunnel. A small adapter (`deadlineConn`) reads the WebSocket in one goroutine and gives `Read` ordinary
  deadlines, so one WebSocket carries a keep-alive connection and the event stream.
- The connection is dropped without the WebSocket closing handshake once TLS has said goodbye: the library would wait up
  to 5 seconds for a peer that may be gone.

## Phone side

- To use the tunnel the app opens a **local forwarder on a Unix domain socket** in a private directory of the app
  (random name, owner-only, removed on close): there is no TCP port, so no other app on the phone can reach it. For
  every connection its HTTP client makes to it, the forwarder opens one WebSocket to `wss://<domain>/tunnel/v1`
  (outer TLS validated with the system roots; header `ngrok-skip-browser-warning: 1`; messages above 1 MiB close the
  link) and pipes bytes both ways. A dedicated HTTP client reaches the socket through `HttpClient.connectionFactory`
  and does the TLS handshake itself with the same pin (SHA-256 of the DER certificate compared to `fp`, no CA, no
  host name; the URL host is the placeholder `mac.invalid`), for REST and SSE.
- A pin failure on the tunnel endpoint follows the existing rule (hard `certificateMismatch` when the external
  endpoint fails the pin; skip-and-continue for a home address). The tunnel counts as an external endpoint. Only a
  certificate that the pin check itself refused is a pin failure; a handshake cut by the peer is just unreachable.
- Endpoint order: home addresses, `ext`, then `tun`; the last route that worked is tried first.

## Pairing and address payloads

The QR gains one optional parameter, appended after `ext`: `tun=<domain>` (DNS name, no scheme, port or path; the
phone uses `wss://<domain>:443`), present while the tunnel is on and configured. Everything else is unchanged. The
address-update payload (`pouple-address:v1?ext=…&fp=…`) also accepts an optional `tun`, last; `ext` stays required there.
`GET /remote/v1/endpoint` returns `{"ext":"<host>:<port>"|null,"tun":"<domain>"|null}` (both keys always present).

## Settings (loopback admin API, `/api/remote/settings`)

New fields: `tunnelEnabled` (bool, default false), `tunnelDomain` (the user's ngrok static domain, strict DNS-name
validation), `tunnelAuthtoken` (write-only; stored in the Keychain through `secrets.Store`; never returned, never
logged), read-only `tunnelAuthtokenSet` (bool), `tunnelStatus` (`off|starting|running|error`) and `tunnelProblem`
(a human sentence in the interface language, empty when fine: bad token, domain not owned by the account, limit
reached, no network...). The exact semantics, with examples, are in `REMOTE-API.md` ("Access through a tunnel").
The tunnel runs while the access (`enabled`) and `tunnelEnabled` are both on: it starts with Pouple, restarts when its
domain or token changes, and stops when Pouple stops or either setting is turned off. It runs apart from the LAN
listeners, so a change of the Mac's addresses never drops its ngrok session, and its connection to ngrok never blocks
the settings.

`enabled` with an empty `interfaces` list now means "automatic": the broadcast interfaces that are up and not
point-to-point, never loopback, never `utun*`, `ipsec*`, `ppp*`, `bridge*`, `vmnet*`, `awdl*`, `llw*` or `anpi*`, and on
them only their private (RFC 1918 / ULA) addresses, never a global IPv6 one. An interface chosen explicitly is taken as it
is. This is what the simplified settings screen sends. The choice is read again at every address check, like an explicit one.

## Not covered

That a Pouple Mac exists behind a domain, traffic volume and timing, ngrok dropping the connection, and the plan's
own limits (bandwidth, connections). Also what the ngrok v2 SDK sends by default about the endpoint: the Mac's host
name, the path of the executable and its process id (`app://<host>/<path>?pid=<n>`). The SDK has no public option to
replace it (`WithForwardsTo` is in its internal package, and `Forward` would need a loopback upstream and loses the
client address), so it stays; ngrok already holds the account, the domain and the traffic.
