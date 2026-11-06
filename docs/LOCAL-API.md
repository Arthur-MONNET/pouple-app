# Local Assistant API

Pouple exposes its conversations to local tools while the macOS app is running. Connect with HTTP over a Unix
domain socket. The integration socket has no TCP listener and is accessible to processes running under the same
macOS user account. The API allows those processes to act on conversations, including approving changes.

## Connect

The default socket path is `~/Library/Application Support/Pouple/ipc/assistant.sock`. For development launches
with `POUPLE_HOME`, use `$POUPLE_HOME/ipc/assistant.sock`. Launch Pouple before connecting.

```sh
socket="$HOME/Library/Application Support/Pouple/ipc/assistant.sock"
curl --unix-socket "$socket" http://localhost/api/assistant
curl -N --unix-socket "$socket" http://localhost/api/assistant/stream
```

Use `localhost` as the HTTP host. Pouple checks the connecting process's user ID, so clients do not need a token.
Browser pages should call their own local backend, which can open the socket. Keep that backend local and apply
its own request protections, since the socket trusts the macOS user. Requests with browser origin headers are rejected.

## Read and interact

The snapshot contains `status` and `requests`. The stream sends an initial snapshot and then updated snapshots
as server-sent events with JSON in `data:` lines. Reconnect after the app restarts.

Paths below are relative to `/api/assistant`. URL-encode IDs from the snapshot when constructing paths.

| Method | Path | Body |
|---|---|---|
| GET | `/` (use `/api/assistant` without a trailing slash) | |
| GET | `/stream` | |
| POST | `/requests/{id}/messages` | `{"text":"Tell me more","attachmentIds":[]}` |
| POST | `/requests/{id}/answers` | `{"answers":[{"questionId":"…","optionId":"…"}]}`. For a free answer, use `text` in place of `optionId`. |
| POST | `/requests/{id}/proposal` | `{"decision":"approve","proposalId":"…"}` or `decline`. Use the proposal ID shown to the user. |
| POST | `/requests/{id}/retry`, `/stop`, or `/ignore` under that request | |
| DELETE | `/requests/{id}` | |
| POST | `/requests/{id}/attachments?name=example.txt` | Raw file bytes. The returned ID can be included in a message. |
| GET | `/requests/{id}/attachments/{attachmentId}` | |

Use `Content-Type: application/json` for JSON bodies. Action responses return 202 or 204. A missing request
returns 404, an invalid body returns 400, and a stale proposal or invalid action for the current state returns
409. On 409, read the new snapshot before showing the action again. Use the proposal action for decisions.
