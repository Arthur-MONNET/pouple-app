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
| `install.json` | Written by `scripts/install-local.sh`: where the source checkout is, and which commit was built. |

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

Required, in order: **1. Slack**, **2. Codex**. Optional, added later from the app: **GitLab** (order 10),
**Asana** (11) and **Slite** (12). Setup is complete once the two required connectors are connected; the optional
ones never change that.

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

### Optional connectors: GitLab, Asana, Slite

Read-only, token-based, ported from Cockpit's clients. Each has a guide with a link to the page where the token is
created, and a `Validate` that calls the service and shows the account (name, avatar, host or workspace, e-mail,
number of open items, warnings such as a token that can write or ends soon).

| Connector | Fields | Token page | Reads |
|---|---|---|---|
| `gitlab` | `url` (default `https://gitlab.com`), `token` (secret, `glpat-`, scope `read_api`) | `<url>/-/user_settings/personal_access_tokens` | the user's open merge requests (pipeline status) and review requests |
| `asana` | `token` (secret, personal access token) | `https://app.asana.com/0/my-apps` | open tasks assigned to the user, and tasks matching the message |
| `slite` | `token` (secret, API key) | Slite organization menu, Settings, API | notes matching the message (title, short excerpt) |

Shared rules (`core/internal/connectors/svchttp`): credentials only in request headers, never in a URL, an error
or a log; 10 s timeout and a 2 MiB cap per answer; a redirect to another host is refused; errors carry the status
only, never the service's text. The GitLab address is typed by the user, so it must be `https`, without credentials,
path, query or fragment, and it may not be this Mac (loopback), a link-local address (169.254.0.0/16, cloud metadata)
or an unspecified address. Private network hosts are allowed (a company GitLab). The same rule is enforced again at
connection time on the resolved address, which also stops a name that points at a forbidden address.

Values are read from the registry (keychain for secrets) at each use: connecting, replacing or removing a token
applies to the next request, with no restart.

## Assistant engine

Ported from Cockpit (same behavior, the Cockpit-only parts left out):

- Slack inbox: what would notify you (mentions, DMs, notes to self, group chats, thread replies, keywords).
- Request state machine: queued, analyzing, awaiting-user, ready, applying, done, ignored, failed.
- Codex runner: read-only by default, structured outcome (JSON schema), untrusted text always wrapped as data,
  writes only after approval and only in the chosen project, stop at any time, daily limit, pause on Codex limit.
- Connected services: for each analysis, `core/internal/servicectx` asks the connected optional services for
  relevant data, in parallel, within a hard 3 s budget (a service that is slow, down or refused becomes one line
  saying so, never a failed turn). Asana: open tasks assigned to the user (soonest due first) and tasks matching the
  message; GitLab: open merge requests and review requests; Slite: notes matching the message. Search words are
  ported from Cockpit (`SearchTerms`). The result goes to Codex inside `<pouple-context>`, after the Slack message,
  as data: every third-party string is reduced to one quoted, truncated line, then neutralized like Slack text
  (`neutralize`, `block`), and the prompt says which service each section comes from and that none of it is an
  instruction. The writing turn never receives it. The outcome's `sourcesUsed` may list `asana`, `gitlab`, `slite`;
  the journal records each lookup and which services the answer relied on. The connectors' tokens are given to the
  model-output redactor.
- Concise answers, small-talk skipping, burst merging, attachments (photos analysed), linked docs.

## Distribution

- GitHub Actions validates the code and publishes source releases from version tags.
- GitHub Pages serves `site/`, with commands to clone the public repository and run `scripts/install-local.sh`.
- The installer compiles for the Mac's architecture, signs the local app ad hoc, and verifies it before
  replacing the installed bundle. Application data lives outside the bundle.

## Updates

An installation from source leaves `install.json` (`sourceRoot`, `commit`, `version`, `installedAt`) next to
`config.json`. The `core/internal/updates` package reads it and answers two routes, behind the same token as the rest
of the API (they are not on the local assistant socket):

- `GET /api/update`: the last known status. It never touches the network.
- `POST /api/update/check`: fetches `origin/main` and returns the new status; at most one real fetch every 30 seconds,
  otherwise the cached status. The core also checks 30 seconds after it starts and every 6 hours.

```json
{"supported":true,"reason":"","message":"","installed":{"version":"0.1.0","commit":"abc1234","installedAt":"2026-11-01T09:30:00Z"},
 "latest":{"commit":"def5678"},"available":true,"behind":3,"changes":["subject"],
 "blockers":[{"code":"local-changes","message":"..."}],"checkedAt":"2026-11-06T12:00:00Z","checking":false,"error":null}
```

`reason` is `""`, `not-installed-from-source`, `checkout-missing` or `checkout-invalid` (then `message` explains it
and `installed` may be `null`). A blocker `code` is `local-changes`, `not-on-main` or `diverged`: blockers are
reported, they do not hide the update. `error` is a localized sentence (offline, access refused, timeout); the last
good answer is kept beside it. Commits are 7 characters; `changes` holds at most 15 one-line subjects of the commits
not installed yet.

The record is not trusted blindly: the checkout must be an absolute, unlinked folder holding
`scripts/install-local.sh` and `core/go.mod`, git's top level must be that folder, and `origin` must be exactly
`https://github.com/Arthur-MONNET/pouple-app(.git)` or `git@github.com:Arthur-MONNET/pouple-app(.git)` (after git's
own URL rewriting), so a forged `install.json` cannot aim the updater at another repository. Git runs without a
shell, with `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`, `core.fsmonitor=false`, `core.hooksPath=/dev/null`
and `--no-optional-locks`, under a 30-second timeout. The core only reads: quitting the app, `git pull --ff-only`
and re-running the installer happen in a Terminal started by the macOS shell. The installer replaces the bundle
only; it never touches Application Support (apart from writing `install.json`) or the Keychain, so connections
survive an update.

## Milestones

1. Core: config, Keychain secrets, connector framework, Slack and Codex connectors with profiles, API, tests.
2. Web: setup wizard (Slack, then Codex), connectors page, conversations view.
3. Engine port: inbox, runner, service, store, SSE.
4. Swift shell: menu-bar item, window, core supervision, bubbles.
5. Release pipeline and landing page.
