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
| POST | `/requests/{id}/answers` | `{"answers":[{"questionId":"…","optionId":"…"}]}`. For a free answer, use `text` in place of `optionId`. For a question with `"multiple": true`, send the picked options as `"optionIds":["A","C"]`, with an optional `text`. |
| POST | `/requests/{id}/proposal` | `{"decision":"approve","proposalId":"…"}` or `decline`. Use the proposal ID shown to the user. |
| POST | `/requests/{id}/retry`, `/stop`, or `/ignore` under that request | |
| POST | `/requests/{id}/memories` | `{"action":"undo","changeId":"…"}` reverts a write of the memory listed in `memoryChanges`; `{"action":"share","changeId":"…","item":"m1","shared":true}` shares one of its facts (or makes it private again with `false`). A change undone already, or that the notes moved on from, returns 409. |
| POST | `/requests/{id}/continue-with` | `{"provider":"codex"}`. For a request that offers `switchTo` (it stopped on an AI's usage limit, and the other AI is connected and not at its own limit): makes that AI the one Pouple uses, and investigates the request again with it. Any other AI returns 409. |
| POST | `/requests/{id}/close` | Optional `{"updatedAt":"…"}`, the `updatedAt` of the request as the user saw it. Hides the request's bubble until it needs the user again (`closed` in the snapshot); the conversation is kept. When the request changed since that time, it stays open and the answer is 409. |
| POST | `/requests/{id}/arrange` | Any of `{"pinned":true}`, `{"archived":true}`, `{"snoozedUntil":"2026-11-27T09:00:00Z"}`; each is undone with `false` (or `""` for the snooze). `pinnedAt`, `archivedAt` and `snoozedUntil` appear in the snapshot. It is not activity: `updatedAt` stays. Archiving closes the bubble, unpins, and cancels a snooze (and the reverse); a request being worked on cannot be archived (409). The snooze must be within a year, or the answer is 400. Whatever reaches the user again (a question, an answer, a failure) clears the archive and the snooze. A conversation done for 7 days archives itself unless it is pinned. This route is not on the phone's allowlist. |
| DELETE | `/requests/{id}` | |
| POST | `/requests/{id}/attachments?name=example.txt` | Raw file bytes. The returned ID can be included in a message. |
| GET | `/requests/{id}/attachments/{attachmentId}` | |

Use `Content-Type: application/json` for JSON bodies. Action responses return 202 or 204. A missing request
returns 404, an invalid body returns 400, and a stale proposal or invalid action for the current state returns
409. On 409, read the new snapshot before showing the action again. Use the proposal action for decisions.

## Origins

Each request's `source.origin` says where it came from. `note-to-self` (a message the user wrote to themselves in
Slack) and `in-app` (a conversation the user started in the Pouple app) are the user's own text and carry the user's
rights. `pipeline`, `asana`, `merge-request`, `pull-request` and `reminder` are events of a service. Every other
origin is a Slack message written by someone else. An `in-app` request has `channelId` `in-app:<id>`, `senderId`
`self`, no `channelName` and no `permalink`; its `label` is the start of its first line, and `projectId` (when
present) is the project folder the user chose.

## Starting a conversation (app only)

The socket cannot start a conversation. A conversation started this way is the user's own request, with the rights of
a note to themselves, so only the app's token-protected HTTP API offers these routes (Bearer token, same-origin
requests only). They are listed here so that the contract is in one place.

| Method | Path | Body |
|---|---|---|
| POST | `/attachments?name=example.txt` | Raw file bytes, at most 10 MB. Returns 201 and the attachment (`id`, `name`, `mime`, `size`, `kind`), to send with the conversation. An empty or too large file returns 400. |
| GET | `/projects` | Returns `{"projects":[{"id":"web","name":"web"}]}`: the folders directly under the projects folder, by name; `[]` when none is chosen. |
| POST | `/requests` | `{"text":"Fix the login redirect","attachmentIds":[],"projectId":"web"}`. `projectId` is optional. Returns 201 and `{"id":"…"}`; the request arrives in the next snapshot, and its analysis starts at once. |

`POST /requests` returns 400 when the text is empty with no file, longer than 4000 characters, names an unknown
attachment or a folder that is not a project, 409 when no AI is connected, and 409 when too many requests are waiting.
A conversation of the app is never skipped silently.
