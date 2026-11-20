# Remote API (phone companion)

This is the contract between the Pouple core on the Mac and the paired phone app. It is off unless the user turns
it on in the settings. The loopback API and the local assistant socket are not changed by it.

Version 1 works on the local network only (or over USB with `adb reverse tcp:<port> tcp:<port>` during development).
There is no cloud relay and no push service.

## Transport

- TLS 1.3 only, on the fixed port shown in the pairing payload (default `47821`).
- The certificate is self-signed (ECDSA P-256). Its private key is in the macOS Keychain. The phone checks no chain,
  host name or date: it **pins** the SHA-256 of the certificate's DER, received in the pairing payload, and refuses
  any other certificate. The certificate stays the same across restarts. If the key is lost, a new certificate is
  made and every phone must pair again.
- The listener binds the addresses of the network interfaces the user chose (for example `en0`), never `0.0.0.0` or
  `::`. It restarts when the settings change or when those addresses change. It closes when the access is turned off
  or the core stops.
- The phone is not a browser. A request with an `Origin` or `Sec-Fetch-Site` header is refused with 403
  `forbidden-origin`. No web interface is served, and no CORS header is ever sent.
- Every response has `Cache-Control: no-store`.

Errors from the remote layer are JSON: `{"error":"<code>"}`. Errors from the assistant routes keep their own shapes
(see [LOCAL-API.md](LOCAL-API.md)) and are written in the app's interface language.

| Status | `error` | Meaning |
|---|---|---|
| 400 | `bad-request` | The body is not valid JSON where JSON is required. |
| 401 | `unauthorized` | Token missing, unknown, revoked, or unused for 30 days. Pair again. |
| 403 | `forbidden-origin` | The request carries `Origin` or `Sec-Fetch-Site`. |
| 403 | `forbidden-route` | The route is not on the allowlist, or the path is not in canonical form. |
| 403 | `step-up-required` | This request needs a step-up and has none. |
| 403 | `step-up-refused` | The step-up nonce is unknown, used, expired or another device's, or the signature does not verify. |
| 403 | `pairing-closed` | No pairing window is open: never opened, expired, cancelled, used, or closed after 5 wrong codes. |
| 403 | `pairing-refused` | Wrong pairing code. |
| 409 | `too-many-devices` | 10 devices are already paired. Revoke one on the Mac. |
| 413 | `body-too-large` | JSON bodies are limited to 1 MiB. |
| 422 | `bad-device-name`, `bad-key` | Invalid pairing fields. |

## Pairing

1. On the Mac, the user turns the access on, chooses the network, and asks to pair a phone. The settings call
   `POST /api/remote/pairing` on the loopback API and show its `payload` as a QR code. The window lasts **2 minutes**.
   The code is 128-bit and works **once**. Five wrong codes close the window.
2. The QR payload is a URL with query parameters in this order. Each value is percent-encoded, so decode it as a
   query string:

   ```text
   pouple-pair:v1?hosts=<host>,<host>…&port=<port>&fp=<sha256 hex>&code=<32 hex>&name=<mac name>
   ```

   - `hosts`: the bound IP addresses, then `<mac name>.local` when the listener is on a network (not loopback only).
     Try them in order. An IPv6 address goes in brackets in a URL.
   - `fp`: SHA-256 of the certificate DER, 64 lowercase hex characters. Pin it.
   - `name`: the Mac's name, for display.
3. The phone creates a P-256 key in the Android Keystore that requires user authentication, with
   `setUserAuthenticationRequired(true)` and biometric authentication. It then sends:

   ```http
   POST /remote/v1/pair
   Content-Type: application/json

   {"code":"<code>","deviceName":"Pixel 8","stepUpPublicKey":"<base64>"}
   ```

   - `stepUpPublicKey` is standard base64 with padding of the key's X.509 SubjectPublicKeyInfo DER, which is
     `PublicKey.getEncoded()` on Android. Only P-256 is accepted.
   - `deviceName` has 1 to 64 characters: letters, digits, punctuation and spaces.
4. The answer is `201 {"deviceId":"<32 hex>","token":"<64 hex>"}`. The token is 256 bits. Only its SHA-256 is kept
   on the Mac (`devices.json`, mode 0600). Store the token in `flutter_secure_storage` and never back it up.

The phone authenticates every other call with `Authorization: Bearer <token>`. A token unused for 30 days expires.
The user can revoke a device on the Mac at any time. Revoking it closes its open requests and streams immediately.

| Method | Path | Answer |
|---|---|---|
| GET | `/remote/v1/device` | `{"id":"…","name":"…"}`, to check that the pairing still works. |
| DELETE | `/remote/v1/device` | 204. Unpairs this device: its token stops working. |
| POST | `/remote/v1/challenge` | `{"nonce":"<64 hex>","expiresAt":"<RFC 3339>"}`, for a step-up. |

## Routes and scopes

The phone uses the assistant routes of [LOCAL-API.md](LOCAL-API.md) with the same paths, bodies and answers. The
core serves only the routes in the table below. **Any other path returns 403 `forbidden-route`**: connectors and
secrets, settings, providers, GitLab access, the terminal, updates, the memory, the writing style, and the remote
settings themselves.

A message from the phone has the local API's semantics. A typed "yes" never approves a proposal (the engine answers
409). A proposal decision must name the `proposalId` that was read, and a plan decision must name the `planId`.

| Route (exact pattern, used as `route` in a step-up) | Scope |
|---|---|
| `GET /api/assistant` | read |
| `GET /api/assistant/stream` | read |
| `GET /api/assistant/requests/{id}/attachments/{attachmentId}` | read |
| `GET /api/assistant/requests/{id}/browser` | read |
| `POST /api/assistant/requests/{id}/answers` | interact; **step-up** if the conversation has grants or "apply every change" is on |
| `POST /api/assistant/requests/{id}/messages` | interact; **step-up** if the conversation has grants or "apply every change" is on |
| `POST /api/assistant/requests/{id}/reminders` | interact |
| `POST /api/assistant/requests/{id}/stop` | interact |
| `POST /api/assistant/requests/{id}/retry` | interact |
| `POST /api/assistant/requests/{id}/ignore` | interact |
| `POST /api/assistant/requests/{id}/close` | interact |
| `POST /api/assistant/requests/{id}/continue-with` | interact |
| `POST /api/assistant/requests/{id}/attachments?name=…` | interact (raw bytes) |
| `POST /api/assistant/requests` | interact (new conversation); **step-up** if "apply every change" is on |
| `POST /api/assistant/attachments?name=…` | interact (file for a new conversation) |
| `GET /api/assistant/projects` | interact |
| `POST /api/alerts`, `PATCH /api/alerts/{id}`, `DELETE /api/alerts/{id}` | interact |
| `POST /api/assistant/requests/{id}/proposal` | **step-up** unless `"decision":"decline"` |
| `POST /api/assistant/requests/{id}/plan` | **step-up** unless `"decision":"decline"` |
| `DELETE /api/assistant/requests/{id}` | **step-up** |

A conversation "has grants" when its `grants` array in the snapshot is not empty: the user approved a plan there,
and what is typed there may lead to writes in the approved places without a new approval. The phone should read
`grants` and request a step-up before it sends a message or an answer. If it sends without one, it gets 403
`step-up-required`, and can retry with a step-up.

Use a path exactly as the pattern shows it, with each `{…}` replaced by the URL-encoded id from the snapshot. Do not
use a trailing slash, `//`, or dot segments. The stream sends `event: snapshot` server-sent events and a
`: keep-alive` comment every 15 seconds.

## Step-up

A step-up proves that the person holding the phone authorized this exact request with their fingerprint.

1. `POST /remote/v1/challenge` returns a nonce. It is valid for **60 seconds**, belongs to this device, and is spent
   by its **first use**, even when the signature is wrong. A device holds at most 8 unused nonces. Asking for a
   ninth drops the oldest one.
2. The phone builds the request body bytes it will send, then builds the **canonical JSON** below. It signs the UTF-8
   bytes of the canonical JSON with the Keystore key through a `BiometricPrompt` `CryptoObject`, using
   `SHA256withECDSA`.
3. It sends the request with these headers:

   ```http
   X-Pouple-Nonce: <nonce>
   X-Pouple-Signature: <standard base64, padded, of the ASN.1 DER ECDSA signature>
   ```

### Canonical JSON

All keys are always present, in this order, with no whitespace:

```json
{"v":1,"nonce":"<nonce>","deviceId":"<deviceId>","route":"<pattern>","requestId":"<id>","planId":"<planId>","proposalId":"<proposalId>","actions":<actions>,"bodySha256":"<hex>"}
```

| Key | Value |
|---|---|
| `v` | `1` |
| `nonce` | The nonce from the challenge. |
| `deviceId` | The `deviceId` received at pairing. |
| `route` | The exact pattern from the table, for example `POST /api/assistant/requests/{id}/plan`. Keep `{id}` as it is. |
| `requestId` | The conversation id in the path (the decoded `{id}`). Use `""` for a route without `{id}`. |
| `planId` | The body's `planId`, or `""` when the body has none. |
| `proposalId` | The body's `proposalId`, or `""` when the body has none. |
| `actions` | The body's `actions` as a JSON array of integers with no spaces (for example `[0,2]` or `[]`). Use `null` when the body has no `actions` key or sets it to `null`. |
| `bodySha256` | SHA-256 of the exact body bytes sent, as lowercase hex. An empty body hashes to `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`. |

`planId`, `proposalId` and `actions` come from the body for every step-up route. Because `bodySha256` binds the whole
body, the decision, the text of a message and the ticked actions are all covered by the signature.

Every string value must contain only `A–Z a–z 0–9 . _ : -`. The route can also contain `/ { }` and spaces. Ids from
the snapshot meet this rule, so no JSON escaping can apply. A value outside it is refused.

Example: approving actions 0 and 2 of plan `9f2c…` in conversation `a1b2c3d4e5f60718`:

```http
POST /api/assistant/requests/a1b2c3d4e5f60718/plan
Authorization: Bearer <token>
Content-Type: application/json
X-Pouple-Nonce: 5d1e…
X-Pouple-Signature: MEUCIQ…

{"decision":"approve","planId":"9f2c4b1a0d3e5f67","actions":[0,2]}
```

The signed bytes are:

```json
{"v":1,"nonce":"5d1e…","deviceId":"<deviceId>","route":"POST /api/assistant/requests/{id}/plan","requestId":"a1b2c3d4e5f60718","planId":"9f2c4b1a0d3e5f67","proposalId":"","actions":[0,2],"bodySha256":"<sha256 of the body above>"}
```

## Mac-side administration (loopback API only)

These routes are on the token-protected loopback API used by the Mac app's settings. The remote listener never
serves them.

| Method | Path | Body / answer |
|---|---|---|
| GET | `/api/remote/settings` | `{enabled, interfaces[], port, available[{name, addresses[], loopback}], listening[], problem, fingerprint}` |
| PUT | `/api/remote/settings` | `{enabled?, interfaces?, port?}`. Turning the access on requires at least one interface. The port must be in 1024–65535. Invalid settings return 422. The listener restarts. |
| POST | `/api/remote/pairing` | 201 `{payload, expiresAt}`. Returns 409 when the access is off or not listening. |
| DELETE | `/api/remote/pairing` | 204. Closes the window. |
| GET | `/api/remote/devices` | `{devices:[{id, name, createdAt, lastSeen, revoked, expired}]}`. Never returns token hashes or keys. |
| DELETE | `/api/remote/devices/{id}` | 204. Revokes the device and closes its streams. Returns 404 for an unknown id. |

The configuration key is `remote` in `config.json`: `{"enabled":false,"interfaces":[],"port":0}`, where `0` means
the default port.

## Threat model

**Assets**: the conversations (private messages, code context), the ability to make Pouple write (code, GitLab,
Asana, Slite), and the secrets of connected services.

| Threat | Mitigation |
|---|---|
| Someone on the same network (café, office Wi-Fi) | Off by default. Binds only the chosen interfaces. TLS 1.3 with a pinned certificate. Every route except pairing needs a 256-bit token. Pairing needs a 128-bit single-use code shown on the Mac's screen for 2 minutes, and 5 wrong guesses close the window. |
| Man in the middle, or a spoofed Mac | The phone pins the certificate fingerprint received in the QR code, scanned in person. It never trusts a CA or a host name. |
| A web page in a browser on the network (CSRF, DNS rebinding) | Requests with `Origin` or `Sec-Fetch-Site` are refused. The certificate is not trusted by browsers. The token is a header, never a cookie. No CORS. |
| Stolen phone token (backup, malware without biometrics) | The token reads and interacts but cannot approve: approving a plan or proposal, deleting a conversation, and typing into a conversation that holds write grants all need a fresh signature by a Keystore key that requires the user's fingerprint. A typed "yes" never approves. The user revokes the device on the Mac, and tokens unused for 30 days expire. |
| Replay of a captured step-up | The nonce is single use, expires after 60 s, and is bound to the device. The signature binds the route, conversation, ids, actions and the SHA-256 of the whole body. |
| Phone reaching what it should not | Explicit allowlist enforced by the remote handler, on a mux of its own. Connectors, secrets, settings, providers, GitLab access, terminal, updates, memory, style and the remote administration are never reachable. |
| "Apply every change" setting | When it is on, text from the phone (new conversation, message, answer) needs a step-up, because it could be applied without anyone reading it. |
| Lost Keychain key | A new certificate is made, its fingerprint changes, and pinned phones refuse it until they pair again. Nothing falls back to an unpinned connection. |

Out of scope for v1: access away from the local network, push notifications (the phone polls locally), and a
compromised Mac user account. That account can already read everything through the local socket.
