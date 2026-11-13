# Pouple — architecture

Pouple is a stand-alone macOS assistant: it listens to the Slack messages that would notify you, has a local AI CLI
(Codex or Claude Code, your choice) investigate them, and shows the result as floating bubbles in a screen corner and as conversations in a
main window. It is extracted from Cockpit's assistant but shares nothing with Cockpit's task manager.

## Goals

1. **Bubbles first**: the native corner pop-ups stay the main surface.
2. **A real app**: a window with every conversation, a menu-bar icon, "open at login".
3. **Install and connect in minutes**: download, open, a guided setup (Slack, then Codex or Claude), more connectors later
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
| `core/` | Go | Config, secrets, connectors, Slack inbox, Codex and Claude runners, assistant engine, HTTP API, SSE. Serves the built web UI (`go:embed`). |
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
| `claude/` | Dedicated `CLAUDE_CONFIG_DIR`: Pouple uses its own Claude login and never reads or writes `~/.claude`. |

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

Required: **1. Slack**, and **one AI of the two alternatives 2. Codex, 3. Claude**. A connector declares
`Required` (needed on its own) or a `Choice` group (one connected connector of the group is enough): the two AIs
share the group `"ai"` and are `Required: false` individually. `connector.SetupComplete` is the single rule used by
`GET /api/state` (`setupComplete`) and by the engine (`Inactive`): Slack connected **and** (Codex **or** Claude
connected). Optional, added later from the app: **GitLab** (order 10), **Asana** (11) and **Slite** (12); they never change whether setup is complete.

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
### Claude

Claude Code is the second AI. Pouple has its own login, kept apart from the interactive CLI the same way Codex's is:

1. Detect `claude` through a login shell (the native installer's `~/.local/bin` is appended to the PATH). Missing:
   one click runs Anthropic's official installer (`curl -fsSL https://claude.ai/install.sh | bash`, no `sudo`,
   output streamed). Nothing is pinned: the CLI auto-updates itself.
2. Log in with `claude auth login --claudeai` and `CLAUDE_CONFIG_DIR` set to Pouple's `claude/` folder. The CLI
   opens the browser and prints the same address, which is forwarded as the fallback `open` event. On macOS Claude
   Code keys its Keychain entry to the configuration folder, so a folder of Pouple's own is a login of Pouple's
   own (`~/.claude` and the user's Keychain entry are never touched). Without the Keychain it falls back to
   `claude/.credentials.json`.
3. Profile card from `claude auth status` (JSON, local, no network): e-mail, plan, organization, plus the CLI
   version. A login that is not a Claude subscription (Console, billed per use) shows a warning.

## AI providers

The engine does not know Codex: it knows a `Provider` (`Run(ctx, Turn) (TurnResult, error)`). Both CLIs implement
it (`engine.Runner` for Codex, `engine.ClaudeRunner` for Claude) and are wrapped by the same `Metered` wrapper, so
the daily quota counts the turns of whichever AI runs them.

- **Chosen at each turn**: `Deps.Selection` reads the settings (`provider`, `models`) when a turn starts, so a change
  applies to the very next turn without restarting anything. `provider` is empty (automatic) or `codex`/`claude`;
  automatic means the connected one, Codex when both are (`config.Settings.ActiveProvider`). `models` holds one model
  name per provider; empty is the CLI's own default. A model name is `[A-Za-z0-9._:/-]{0,80}` and never starts with
  `-`, so it can never be read as an option (`providers.ValidModel`, enforced by the API and again by the runner).
- **Per-provider state**: a usage limit or a login problem is held against the provider that reported it
  (`providerFailure`). The pause, the standing notices, the notification titles and the journal name that provider
  (`%s` is `Codex` or `Claude`, translated around it). The other one keeps working, and a pause only shows while its
  provider is the active one.
- **Conversations**: a session id belongs to the provider that created it (`Stored.ThreadProvider`, empty means
  Codex for data written before Claude existed). After a switch the next turn does not resume it: it starts fresh
  with the transcript so far, exactly as when a thread is lost.
- **Test**: `Service.TestProvider` runs one minimal read-only turn ("reply with ok") with the model to test, through
  the metered provider (one turn of the quota), with a 90 s limit.
- **Prompts** are provider-neutral. The structure is the same; the final answer follows the same JSON schema, enforced
  by `codex exec --output-schema` or by `claude --json-schema` (see below).

### API

- `GET /api/settings`, `PUT /api/settings` (partial): `provider` (`"codex"` | `"claude"`; the effective one on read;
  `""` on write goes back to automatic; a provider that is not connected is refused with 422) and
  `models` (`{"codex": "", "claude": ""}` on read; on write only the keys named change; 422 for an unknown provider
  or an invalid model name).
- `GET /api/providers`: `{"active": "claude", "providers": [{"id", "name", "connected", "active", "model",
  "suggestedModels": [{"id", "label"}], "note"}]}`. Codex has no suggestions (its models cannot be listed offline);
  Claude proposes the aliases `opus`, `sonnet`, `haiku`, `fable` and the current full ids.
- `POST /api/providers/{id}/test` (optional body `{"model": "…"}` to test an unsaved model):
  `{"ok", "latencyMs", "model", "message", "code"}`. A failed test is still HTTP 200 with `ok: false`, a localized
  `message` and a `code` (`not-connected`, `invalid-model`, `login`, `limit`, `quota`, `timeout`, `failed`);
  an unknown provider is 404. One turn of the quota, except `not-connected` and `invalid-model`, which run nothing.
- `GET /api/assistant`: `status.provider` is `{"id", "name", "model"}` of the AI that runs the next turn.
- `GET /api/state`: each step also carries `choice` (`"ai"` for Codex and Claude); `setupComplete` follows the rule
  above.

### How a Claude turn is confined

Codex runs in an operating-system sandbox. Claude Code does not, so a Claude turn is confined by its arguments, and
`ClaudeRunner.BuildArgs` is covered by a test that asserts the exact argument list of both modes.

| | Read-only turn (investigation) | Write turn (after approval) |
|---|---|---|
| Tools (`--tools`) | `Read,Glob,Grep` | `Read,Glob,Grep,Edit,Write` |
| Permission mode | `dontAsk`: whatever is not allowed is refused | `acceptEdits`: edits only inside the working folders |
| Working folder | the projects folder (or Pouple's scratch folder) | the project folder, also given as `--add-dir` |
| Shell, web, MCP, sub-agents | none: not offered (`--tools`) and denied (`--disallowedTools`); `--strict-mcp-config` with no config ignores every MCP source, the project's `.mcp.json` included | same |
| Settings, hooks, plugins | `--setting-sources ""`: no user, project or local settings, so no hook, plugin or rule from outside | same |
| Secrets | deny rules through `--settings`: `.env*`, keys, `.ssh`, `.aws`, `.netrc`, `.npmrc`, Pouple's data folder | same, and `.env`/`.env.local` cannot be edited |

Verified against Claude Code 2.1.272, with the real CLI pointed at a local fake API (no account involved): a model
that asks for Bash gets "No such tool available"; a read outside the working folder is refused; an edit outside the
project, through a symlink, in `.git`/`.claude` or on `.env` is refused (note: a bare `--allowedTools Edit,Write`
would have allowed edits anywhere, which is why the write mode relies on `acceptEdits` and the tool list instead);
without `--setting-sources ""` and `--strict-mcp-config`, a project's SessionStart hook and `.mcp.json` server do run,
with them they do not.

Other rules of the runner:

- The prompt goes through stdin, never through the arguments. Images are copied for the length of the turn into a
  private folder (with the extension the `Read` tool needs), which is the only extra readable folder
  (`--add-dir`), and their paths are appended to the prompt.
- Same login-shell pattern as Codex (`exec "$0" "$@"`), own process group, timeout, and a cancelled context kills the
  group, so Stop works the same way (`ErrCancelled`). The shared machinery is `runProcess` in `process.go`.
- The child environment drops `POUPLE_*`, `ANTHROPIC_*`, `CLAUDE_*` and `CLAUDECODE` and sets
  `CLAUDE_CONFIG_DIR` to Pouple's folder: no variable of the parent can choose another account, a key or a gateway
  (the parent's `CLAUDE_CODE_*` variables would otherwise even write the parent's e-mail into Pouple's folder).
- Streams (`--output-format stream-json --verbose`): `system/init` gives the session id and the resolved model,
  `assistant` messages carry text and `tool_use` blocks (shown as steps: `Read src/a.ts`, `Searched for “login”`,
  `Edited a.ts`), the final `result` carries `result`, `structured_output` and `is_error`. `--json-schema` is
  enforced through a synthetic `StructuredOutput` tool. `--resume <session id>` continues a session.
- Failures are classified like Codex's (`ErrLimit`, `ErrLogin`) from the result text, the exit code and stderr. A
  rejected request is retried by the CLI up to ten times with growing delays, which takes minutes: the runner reads
  the `system/api_retry` events and stops at once when retrying cannot help (login refused twice, organization not
  allowed, billing error, third rate limit).

## Assistant engine

Ported from Cockpit (same behavior, the Cockpit-only parts left out):

- Slack inbox: what would notify you (mentions, DMs, notes to self, group chats, thread replies, keywords).
  Thread replies come from the threads you wrote in, the threads that mention you, and, with live events, any thread
  Slack says you follow (`subscribed` on the first message), the same rule as Slack's Activity view.
- Request state machine: queued, analyzing, awaiting-user, ready, applying, done, ignored, failed. A request is
  `engaged` once its status first changes into one that reaches the user (awaiting-user, ready, failed, done), in
  `Service.update`; the bubbles show the work of an engaged request only, so the first investigation is silent.
  `closed` hides the bubble (Done, the close button, a declined change) and is cleared the next time the request
  reaches the user. Questions may allow several options (`multiple`), answered with `optionIds`. The bubble rules
  themselves are in `app/Sources/PoupleCore/Visibility.swift`.
- AI runner (Codex or Claude, see above): read-only by default, structured outcome (JSON schema), untrusted text
  always wrapped as data, writes only after approval and only in the chosen project, stop at any time, daily limit,
  pause on the limit of the AI that reported it.
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

### Plans and grants (being built)

The target is described in [PRODUCT.md](PRODUCT.md), "Writing". The model lives in `core/internal/engine/plan.go`; nothing
creates a plan yet, and the current proposal flow is unchanged.

- A **scope** is one place a write approval covers: a project folder (`code`), a GitLab project (`gitlab`), an Asana project
  (`asana`) or a Slite space (`slite`). Only the service and the ID match; the name is for display.
- An **action** is one typed write. Each kind writes in one service (`code.edit`; `gitlab.push`,
  `gitlab.merge-request`; `asana.comment`, `asana.create-task`; `slite.write`), so an action can only name a scope of
  that service. The AI never pushes, calls GitLab or writes in Asana or Slite itself: the core carries out typed
  actions, each checked against the grants.
- A **plan** is the investigating AI's goal, a brief restating what the writing needs to know, and its actions.
  `Plan.Normalize` checks a plan written by the AI before it is shown and refuses, as a bad answer (`ErrOutcome`), one
  that does not hold together: an unknown kind, a kind in the wrong service, an identifier that is not one of that
  service (a project folder is a plain folder name directly under the projects root and never hidden, like
  `projectPath`; a GitLab project is a numeric ID or a `group/project` path; an Asana project a numeric gid; a Slite
  space a plain ID), a text that is too long, or a character the user would not see as written (control and format
  characters such as bidirectional overrides; line breaks only in the brief). Nothing is cut: the user approves what
  the AI wrote. The AI that writes receives the plan, never the request's own text.
- Approving a plan **grants** each of its scopes with the kinds of action it asked for there, for the rest of the
  conversation (`Grants.With`, which refuses a plan that is not approved or was not checked). Matching is exact on the
  service and the ID, so a trimming or case difference asks again. `Grants.Covers` never covers an action whose kind
  does not write in its scope's service. `Grants.Uncovered` lists the actions that need a new approval: another scope,
  or another kind in a granted scope. `PlanFingerprint` lets an approval name the plan the user read.
- `Request.plan` and `Request.grants` are omitted from the JSON until they exist, so the interface sees no change, and
  conversations saved before them load as they are.

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

1. Core: config, Keychain secrets, connector framework, Slack, Codex and Claude connectors with profiles, API, tests.
2. Web: setup wizard (Slack, then Codex or Claude), connectors page, conversations view.
3. Engine port: inbox, runner, service, store, SSE.
4. Swift shell: menu-bar item, window, core supervision, bubbles.
5. Release pipeline and landing page.
