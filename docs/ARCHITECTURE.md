# Pouple — architecture

Pouple is a stand-alone macOS assistant: it listens to the Slack messages that would notify you, has the local
Codex CLI investigate them, and shows the result as floating bubbles in a screen corner and as conversations in a
main window. It is extracted from Cockpit's assistant but shares nothing with Cockpit's task manager.

## Goals

1. **Bubbles first**: the native corner pop-ups stay the main surface.
2. **A real app**: a window with every conversation, a menu-bar icon, "open at login".
3. **Install and connect in minutes**: download, open, a guided setup (Slack, then Codex), more connectors later
   from the app. Nobody edits a file or a config by hand.
4. **Secrets are never in a file in the repo, a log, a URL or the web UI after they are saved.**

## Components

```
Pouple.app
├── Contents/MacOS/Pouple            Swift: menu-bar item, main window (WKWebView), bubbles, supervises the core
└── Contents/Resources/
    └── pouple-core                  Go: HTTP API + SSE + embedded web UI + assistant engine (one static binary)
```

| Part | Language | Role |
|---|---|---|
| `core/` | Go | Config, secrets, connectors, Slack inbox, Codex runner, assistant engine, HTTP API, SSE. Serves the built web UI (`go:embed`). |
| `web/` | Vue 3, Tailwind 4 | Setup wizard, conversations, connectors page. Talks to the core only through the API. |
| `app/` | Swift (SwiftPM, macOS 14+) | Shell: starts/stops the core, menu-bar item, window, floating bubbles (ported from Cockpit's overlay). |
| `site/` | static HTML | GitHub Pages landing page with the Download button (resolves the latest release). |

The core listens on `127.0.0.1` only, on a port it chooses and writes to `state/port`. Every API call must carry a
per-launch bearer token (passed to the shell on start, injected into the web UI page): another local process or a
web page cannot drive the assistant.

## Where things live

`~/Library/Application Support/Pouple/`

| Path | Content |
|---|---|
| `config.json` | Non-secret settings (poll interval, daily limit, which connectors are on). |
| `requests.json` | Conversations. |
| `attachments/` | Files that came with messages. |
| `codex/` | Dedicated `CODEX_HOME`: Pouple uses its own Codex login. |

**Secrets** (Slack token, other connectors' keys) are stored in the **macOS Keychain** (service `app.pouple`),
through `/usr/bin/security`, never in a dotfile. They are write-only for the UI: the API returns whether a secret
exists and where it came from, never its value. They reach the engine in memory only.

## Connectors

A connector is a Go value with a declarative description, so the setup UI is generated, not hand-written per service:

```go
type Connector interface {
    Descriptor() Descriptor                       // id, name, required, order, guide steps, fields
    Validate(ctx context.Context, values map[string]string) (Profile, error)
}
```

- `Descriptor.Fields` lists the inputs (label, help, `secret: true` for tokens, a pattern for early validation).
- `Descriptor.Guide` lists the steps shown beside the form: text and "open this link" buttons.
- `Validate` talks to the service and returns a **Profile** (display name, avatar, a few facts: workspace, e-mail,
  plan, granted scopes…). The UI shows it as the "is this the right account?" confirmation. Nothing is saved before
  the user confirms it.
- Saving a connector stores secrets in the Keychain and the rest in `config.json`, then (re)starts what depends on it.

Required, in order: **1. Slack**, **2. Codex**. Optional, added later from the app: Slite, Asana, GitLab (ported
from Cockpit as the engine needs them).

### Slack

Slack asks for a *user* token (the assistant must read what you can read). Pouple makes it a three-click step:

1. "Create the Slack app": opens `https://api.slack.com/apps?new_app=1&manifest_json=…` with a manifest that already
   lists every scope. Nobody types a scope.
2. In Slack: *Install to Workspace*, then copy the *User OAuth Token*.
3. Paste it in Pouple. `auth.test` + `users.info` + `team.info` give the profile card (avatar, name, workspace,
   e-mail). The granted scopes (`x-oauth-scopes`) are compared to what Pouple needs and each missing one is
   explained (what stops working without it) instead of failing later.

### Codex

1. Detect the `codex` CLI through a login shell. Missing: offer a one-click install (`npm i -g @openai/codex` or
   `brew install codex`, whichever exists) with the output streamed.
2. Log in with Pouple's own `CODEX_HOME`: Pouple runs `codex login`, shows the sign-in page it prints, and waits for
   `auth.json` to appear. Nobody pastes anything.
3. Profile card from the login: e-mail and plan decoded from the ID token (no network call, nothing sent anywhere),
   plus the CLI version.

## Assistant engine

Ported from Cockpit (same behavior, the Cockpit-only parts left out):

- Slack inbox: what would notify you (mentions, DMs, notes to self, group chats, thread replies, keywords).
- Request state machine: queued, analyzing, awaiting-user, ready, applying, done, ignored, failed.
- Codex runner: read-only by default, structured outcome (JSON schema), untrusted text always wrapped as data,
  writes only after approval and only in the chosen project, stop at any time, daily limit, pause on Codex limit.
- Concise answers, small-talk skipping, burst merging, attachments (photos analysed), linked docs.

## Distribution

- GitHub Actions on a macOS runner: build the core for arm64 and amd64 and `lipo` them, build the Swift app
  universal, assemble `Pouple.app`, sign (ad-hoc, or Developer ID + notarization when the secrets are set),
  package `Pouple.dmg`, attach it to the GitHub Release.
- GitHub Pages serves `site/`: one page, a Download button pointing at the latest release asset.
- The repository is **private** until the owner makes it public; Pages and public downloads need that switch.

## Milestones

1. Core: config, Keychain secrets, connector framework, Slack and Codex connectors with profiles, API, tests.
2. Web: setup wizard (Slack, then Codex), connectors page, conversations view.
3. Engine port: inbox, runner, service, store, SSE.
4. Swift shell: menu-bar item, window, core supervision, bubbles.
5. Release pipeline and landing page.
