# Pouple security model

Pouple protects conversations and their attachments, project context, connector credentials, and the authority to
write to a project or external service. The boundaries are the macOS account, the origin and scope of a request,
and the credentials used by a paired phone. The layout of this policy is inspired by the
[T3 Code security policy](https://github.com/pingdotgg/t3code/blob/main/SECURITY.md).

## Actors and authority

| Actor | Authority |
| --- | --- |
| Mac owner using the app | Uses the loopback API with a per-launch bearer token. Can approve scoped work, manage secrets and connectors, and enable access, pair phones, or revoke devices. |
| Process running as the same macOS user | Can use the Unix assistant socket. Its documented routes include conversation actions and approval decisions. The socket checks the peer's user ID. |
| Connector sender or third-party content author | Supplies data for investigation. Content from messages, comments, attachments, or external sources carries no approval or write authority. |
| Agent investigating or carrying out work | Can use the permissions of its current turn. Explicitly approved work is limited to the conversation and the approved scopes. |
| Unpaired network client | Can reach an enabled listener where network settings permit and redeem a valid pairing code during an open window. A client needs a device token for subsequent requests. |
| Paired phone with a valid bearer token | Can read conversations and use the interaction routes on the remote allowlist, including starting a conversation and managing alerts under the documented conditions. |
| Paired phone with a valid bearer token and step-up signature | Can also perform the signed action that requires step-up, subject to the same allowlist and conversation rules. |
| ngrok and network intermediaries | Carry encrypted phone traffic when the optional tunnel is enabled. They can observe connection metadata, traffic size and timing, and can delay or drop traffic. |

In-app requests and genuine Slack notes to self have owner-originated rights. Connector events and other people's
text are untrusted data. Free-form text carries no approval. When new third-party content enters a conversation,
it can require a fresh plan approval before trusted execution resumes. The user can enable limited automatic
application of changes under the product rules. That setting grants no full-access write authority.
[Product rules](docs/PRODUCT.md#writing) · [Architecture](docs/ARCHITECTURE.md) · [Local socket contract](docs/LOCAL-API.md)

## Phone and tunnel boundaries

- Remote access is off by default. Pairing uses a short-lived, single-use code shown on the Mac. The Android app
  pins the Mac's TLS 1.3 certificate fingerprint from the pairing QR code before sending credentials. The Mac
  keeps the TLS private key in Keychain and stores hashes of device tokens.
- Device tokens give the documented read and interaction capabilities. Revoking a device invalidates its token and
  cancels its open requests and streams. Inactive tokens expire. The remote API rejects browser-origin headers
  and has an explicit allowlist for app routes. Connector and secret management, terminal access, and remote
  administration are outside the phone's scope.
- Approving a plan or proposal and deleting a conversation require step-up. Sending messages or answers also
  requires step-up when the conversation has write grants or the user enabled “Apply every change.” Starting a
  conversation requires step-up under that setting as well. The official Android app requires a strong biometric
  for each signing operation. The Mac verifies the registered key's signature against a short-lived, single-use
  challenge bound to the device and the exact request.
- The optional tunnel carries the same pinned TLS session inside a WebSocket. The tunnel can be reached over the
  Internet while enabled, independently of the LAN listener's external-access setting. A direct port forward
  should preserve source addresses so the LAN listener can enforce its network filter and per-source limits.

For route details and operating limits, see the [remote API contract](docs/REMOTE-API.md) and
[tunnel design](docs/REMOTE-E2E.md).

## What to report

The following cases cross a documented security boundary.

- A different local account, an unrelated web origin, or an unpaired network client receiving access without
  the credentials required for the request.
- Third-party content being treated as the owner's approval, or agent work exceeding the approved scope.
- A phone reaching an app route outside its allowlist or using its bearer token alone for an action that
  requires step-up.
- A pairing credential working beyond its window or more than once, a step-up authorizing a different request or
  being accepted again, or a revoked device retaining access.
- A phone sending credentials to an endpoint that fails the expected certificate check, tunnel intermediaries
  gaining access to application plaintext, or secrets leaking into output such as logs or API responses.

## Assumptions and expected exposure

- The macOS account is a trusted boundary. Code already running as that user has the authority exposed by the
  local socket. A different local account and an ordinary web page are separate actors.
- A device bearer token authorizes the documented read and interaction routes. Keep it private and revoke the device
  if the token may have been exposed. Step-up additionally requires control of the paired signing key.
- A reachable tunnel reveals that an endpoint exists. Its provider sees traffic metadata and the endpoint metadata
  sent by the tunnel SDK. Tunnel availability depends on the provider. [Tunnel design](docs/REMOTE-E2E.md#not-covered)

Share potential findings privately with the maintainer. Include the affected version, the actor and access they
already have, the boundary involved, and the observed impact. Omit live credentials and private conversation data.
