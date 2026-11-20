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
| `alerts.json` | The upcoming alerts (reminders the user set). |
| `attachments/` | Files that came with messages. |
| `codex/` | Dedicated `CODEX_HOME`: Pouple uses its own Codex login. |
| `install.json` | Written by `scripts/install-local.sh`: where the source checkout is, and which commit was built. |
| `claude/` | Dedicated `CLAUDE_CONFIG_DIR`: Pouple uses its own Claude login and never reads or writes `~/.claude`. |

The memory is not in this folder: it is shared with the user's own Claude Code and Codex, in `~/SecondBrain/` (or the folder
chosen in Settings, `memoryFolder`), a git repository of its own that is never pushed.

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

- **Taking over**: a request stopped by an AI's usage limit keeps which AI stopped it (`Stored.LimitedBy`). When the
  other AI is connected and not at its own limit, the request offers it (`Request.switchTo`, set when listed), and
  `Service.ContinueWith` makes it the AI in use (the same setting as the AI section of the settings) and investigates
  the request again with it. The bubble and the window then show "Continue with Codex" in place of "Investigate again".
- **Usage**: a turn returns the tokens it used (`TurnResult.Tokens`) and the AI's own usage windows when it reported
  them (`TurnResult.Limits`: the current five hours and week, each with its share used and its reset time). Codex gives
  its tokens in the `turn.completed` event of `codex exec --json`, and its windows only in its session file
  (`CODEX_HOME/sessions/…/rollout-…-<thread>.jsonl`, the last `rate_limits` that holds a window, read from the end of the
  file first): the windows are told apart by their length (300 and 10080 minutes), not by their place. Claude Code gives its tokens in the `result` event and its windows in
  `rate_limit_event` (`rate_limit_info`, utilizations from 0 to 1; a value above 1 is taken as a percentage). `Metered` hands both to the `UsageMeter`, which adds
  up the tokens of the day for each AI and keeps each AI's last windows in `usage.json`, and drops a window once its reset time is
  past (or, without one, a whole window after it was reported); the interfaces drop it too when their snapshot is older.
  A failed turn reports no tokens. Each conversation adds up its own tokens (`Request.tokens`), without counting it as activity. The snapshot's
  `status.usage` carries `tokensByAI` and `limits`, both by AI: tokens are never added up across AIs, since each has
  its own plan.

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
| Secrets | deny rules through `--settings`: `.env*`, keys, `.ssh`, `.aws`, `.netrc`, `.npmrc`, Pouple's data folder, the memory folder (`ClaudeOptions.Guarded`, read at each turn) | same, and `.env`/`.env.local` cannot be edited |

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

### Alerts and reminders

- `core/internal/alerts` is the store of upcoming alerts (`alerts.json`, written atomically at each change): an `Alert` is a
  title (120 characters at most), a note (1000), a moment with its zone offset and, when it came from a conversation, that
  conversation's id. It validates (a title is required, no control or format character, a moment strictly in the future when
  an alert is made or moved, 200 alerts at most) and knows nothing of conversations. `ParseWhen` reads a client's moment:
  RFC 3339, or a local `2026-11-20T09:00` read in the zone of the clock the service runs on.
- The AI suggests alerts in the answer's `reminders` field (`{label, note, at}`, `at` being a local date and time, six at most;
  an unreadable one is dropped by `ParseOutcome`). Every investigation turn starts with the current date, time and zone
  (`Persona.clockNote`), so a resumed conversation still knows what "tomorrow" is. `attachReminders` turns them into
  `Request.reminders`, a `ReminderOffer` with a fingerprint id and options `r1`, `r2`… (past and repeated suggestions are
  dropped); a later answer without suggestions removes an offer nobody picked from, never one that was accepted.
- `POST /api/assistant/requests/{id}/reminders` (`{"decision":"approve"|"decline","offerId","items":[{"id","at"}]}`) names the
  offer that was read (409 for another one). `DecideReminders` checks every pick before setting any (unknown or repeated
  option, moment in the past: 400) and sets all of them or none; each alert keeps the conversation it came from.
- Alerts made by hand: `POST /api/alerts` (`{title, note, at}`), `PATCH /api/alerts/{id}` (only the fields present) and
  `DELETE /api/alerts/{id}`, on the web server only (not on the local socket). The list is `alerts` in the assistant snapshot
  and in its stream, so every window sees a change at once.
- `Service.watchAlerts` (started by `Run`) fires the alerts that come due: it looks at the wall clock at least every 20 s
  (timers stop while the Mac sleeps) and at once when the alerts change. A due alert becomes a ready, engaged conversation
  of origin `reminder` (an event: `IsEvent`), id `reminder-<alert id>`, with `Source.relatedRequestId` pointing at the
  conversation it came from, then the alert is removed. The deterministic id means a crash between the two steps cannot open
  a second conversation. The macOS shell shows it as any bubble. A ready answer that carries a proposed `reminders` offer shows it in its bubble too
  (`ReminderPicker`; the ticking and time rules are `ReminderDraft` in `PoupleCore`). `Retry` refuses a reminder.

### Topics

- A `Topic` (`{id, title, createdAt, updatedAt}`) is a subject several conversations are about. They are kept in
  `requests.json` next to the conversations (`topics`, plus `organization`: when the topics were last reorganized and how many
  conversations were sorted since), and every `Request` carries its `topicId` (empty until sorted, and for a message that
  needed nothing). The snapshot (and its stream) has `topics`, each with at least one conversation: `Store.Remove` and the
  merges drop the empty ones.
- Sorting is on only when `Deps.Topics` (a `TopicConfig`: the two schema files and a folder) is set; `cmd/pouple-core` writes
  `TopicSchema` and `OrganizeSchema` next to the outcome schema. `analyze` first runs `sortIntoTopic`: one short read-only turn
  that belongs to no conversation (`Service.ask`, in the empty `topics` folder of the data directory, so it can read no
  project), given the topics with their latest conversations and the new message; it answers `{topicId, title}` (an existing
  id, or "" with a title for a new topic; with an id, a title only to rename). An id the AI made up makes a new topic, a
  missing title falls back on the start of the message, and a turn that fails is reported once and never stops the analysis:
  the conversation waits for the next reorganization. Titles go through `Service.title`: redacted first, then one clean line
  (no control or format character, no final period, 80 characters at most).
- `maybeOrganize` (called by every `Poll`) queues `organize` in the background when conversations wait for a topic (those saved
  before topics, or that could not be sorted), or when five were sorted since the last look and half an hour passed. One turn
  places up to 25 waiting conversations (new topics are named by a key of the AI's own, shared by the conversations placed
  under it), merges topics (`Store.MergeTopics` keeps the older start and moves the conversations) and renames them; only ids
  that exist count. A round that places nothing ends the run, and a failed attempt is retried after ten minutes.
- The analysis of a conversation in a topic reads the others of the topic (`relatedConversationsOf`: at most six, not ignored,
  not being worked on) through `AnalysisPromptWithTopic`. A conversation that came from someone else, whose reply may be sent to
  them, reads only the conversations of its own channel (the others may be private), and so do an Asana comment and a merge
  request review, whose reply goes to their author (`sharedReader`, the rule the memory follows too); a note to oneself, a
  finished pipeline and a reminder read the whole topic. If any conversation read holds text written by other people, the request is flagged
  `ServicesUsed` and `ForeignSinceGrant` like any text of strangers, so nothing runs on its own after it. `ownText` decides: only
  a note to oneself that no service or web page shaped, or a reminder set from such a note, is the user's own.
- Web: `lib/topicGroups.ts` groups by topic (`groupByTopic`, an unknown or missing topic goes to the `unsorted` group) and names
  a row by its source (`sourceLabel`); `lib/topicFolds.ts` and `composables/useTopicFolds.ts` hold the fold state (folded by
  default, open for the selected conversation and up to three topics that wait for the user, explicit choices kept in
  `localStorage`).

### Memory

- `core/internal/memory` is the folder of notes (`memory.Store`, on the folder `config.Settings.MemoryDir` gives at each
  call). It knows nothing of conversations. Every path goes through `CleanPath` (relative, `.md`, no hidden segment, four
  levels at most) and every file through an `os.Root` on the folder, so neither `..` nor a symbolic link leaves it. `Notes`
  lists the notes (title: the first `# ` heading, else the file name), `Read` gives one, `Search` ranks the lines that hold
  the most words of a query. With `shared` set, `WithoutPrivate` takes out every line marked `[private]` with what belongs
  to it (the lines indented under it, blank lines included, a tab counting as four spaces; for a heading, its whole section
  down to the next heading of the same level or higher), titles come from the filtered note, and a note left with nothing
  but headings and front matter is not listed at all. `INDEX.md` is never a note, whatever its case.
- `Remember` checks every fact first (`CheckFact`: one line, 300 characters, a note or the title of a new one, a title
  being plain words since `INDEX.md` may be imported into the user's own `CLAUDE.md`), then, under one lock: `git init`
  when needed (a repository that already has a remote is refused: private facts could be pushed), a commit "Changes made outside Pouple" of what changed by hand, each change made (a fact
  appended; put in the place of the fact whose text starts with `replaces`, keeping that fact's private mark; or, with
  `Remove`, taken out with what is indented under it; `SharedOnly` never touches a private fact), `INDEX.md` written again,
  and one commit, whose id `Change.Commit` names it for `Undo` (`git revert` of a "Remember from" commit; a conflict is
  `ErrChanged`). `SetShared` adds or removes the private mark of one line. Notes are written with
  a rename, so a reader sees the old note or the new one. git is `engine.RunGit` (no hook, no fsmonitor, no signing) with
  Pouple's own identity; a failed last commit returns the facts written with `ErrNotCommitted`.
- The engine (`engine/memory.go`) is on while `Deps.Memory` is set and `Deps.MemoryOn()` says so (`memoryEnabled` in the
  settings, false by default: the memory is opt-in). The first prompt carries `Persona.MemorySection`: the index, the `notes` lookups (`search`, `read`) and the rules
  of the `memories` field of the answer (four changes at most, `{note, title, fact, replaces, remove, lasts}`; `ParseOutcome`
  drops what the memory could not make). `notes` lookups are run by `runNotes`, outside the connected services; what they
  return is the user's own text unless it holds an `[unconfirmed]` fact, which sets `ServicesUsed` and
  `ForeignSinceGrant` like any text of others. A request whose answer may reach someone else (`sharedReader`: all but a
  note to oneself, a finished pipeline and a reminder; an Asana comment and a merge request review get a reply drafted for
  their author) sees no private fact, in the index, the searches or the notes. Claude cannot read the folder with its own
  tools (`Guarded` denies it as written and with its links resolved, `config.ResolvePath`). Known limits: Codex's
  read-only sandbox can read the disk, and a turn with full access has a shell, so either could open a note by path, as
  they could Pouple's data folder; git runs without hooks or fsmonitor, but a filter driver set in the folder's own
  `.git/config` would still run, which needs someone who can already write there.
- `applyMemories` (after `showOutcome`, for an answer that reaches the user) makes the changes at once: a note the reader
  cannot see becomes a new note, a new fact is private, a fact is `[unconfirmed]` unless `ownText` holds for the
  conversation, `lasts` ("weeks", "months", "years") sets its `(review …)` date, a request of others gets `SharedOnly`.
  The write is one commit with the conversation's source (`memorySource`: "Slack #channel", "Note to self"… and its
  permalink) and a 30 s bound, recorded as `Request.memoryChanges` (the last ten; each `MemoryChange` is the commit id and
  its items) and as a step of the journal. `POST /api/assistant/requests/{id}/memories` takes `{"action":"undo","changeId"}`
  (`UndoMemoryChange`) or `{"action":"share","changeId","item","shared"}` (`ShareMemoryFact`). The macOS bubble says the
  last change in one line, with Undo; the window lists it.
- Review and tidy-up (`memory/review.go`, `memory/tidy.go`, `engine/memory_tidy.go`). `Review` lists the facts whose
  `ReviewDate` came (their `(review …)` date, else `StaleAfter`, 183 days, after the last other date of the line; a fact
  written by hand without any date is never due) and every `[unconfirmed]` one; `Keep` takes the unconfirmed mark out and
  writes `(kept <today>) (review …)`, the new review as far ahead as the fact had lived (at least two weeks), `Drop` removes the line and what is indented under it, each
  naming the line read (`ErrChanged` otherwise) and making one commit. The git history is the tidy-up's state: `State` counts
  the "Remember from" commits since the last "Tidy up" one. `maybeTidyMemory` (every `Poll`) queues `tidyMemory` after five
  writes and a day; one read-only turn in the empty topics folder (`Service.ask`, `MemoryTidySchema`) sees each note's path,
  title and first facts and answers `{merges: [{from, into}]}`; `Store.Tidy` makes at most five merges of notes that exist,
  writes the index and commits "Tidy up" even when nothing moved. `maybeAskMemoryReview` (every `Poll`) sets an alert
  "Memory: N facts to review", naming up to eight, when some wait and the last one is a week old (`Store.MemoryReviewAt`).
- `core/internal/clilink` writes and removes the block that connects the user's own CLIs: `~/.claude/CLAUDE.md` imports
  the index (`@~/SecondBrain/INDEX.md`), `~/.codex/AGENTS.md` is told where to read. The block sits between
  `<!-- pouple-memory:start -->` and `<!-- pouple-memory:end -->`; connecting replaces an earlier block, disconnecting gives
  the file back as it was, a start marker without its end is left alone, a symbolic link is never written through, and the
  file keeps its mode. A block for another folder counts as not connected.
- Web routes (`api/memory_api.go`, web server only): `GET /api/memory` (the engine's `MemoryView` and `clis`),
  `POST /api/memory/review` (`{note, line, decision: keep|drop}`), `POST /api/memory/tidy`, `POST /api/memory/reveal`
  (`/usr/bin/open` on the folder) and `POST /api/memory/clis/{id}` (`{connected}`; connecting needs the memory on).

### Plans and grants (being built)

The target is described in [PRODUCT.md](PRODUCT.md), "Writing". The model lives in `core/internal/engine/plan.go`; nothing
creates a plan yet, and the current proposal flow is unchanged.

- A **scope** is one place a write approval covers: a project folder (`code`), a GitLab project (`gitlab`), an Asana project
  (`asana`), a Slite space (`slite`) or the conversation's interactive terminal (`terminal`, whose only ID is `.`: sessions
  come and go, so a grant never names one). Only the service and the ID match; the name is for display.
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
- `terminal.run` is carried out by `runTerminalPlan`, a turn of its own like a code action: `TerminalPlanPrompt` holds the
  goal and the approved summaries only (no request text, no brief, no investigation thread), the process runs `ReadOnly`
  without the ssh-agent, and only its `terminal` lookups are run, through `terminalLookups`, for at most
  `maxLookupRounds`. Only its recap is kept. `Normalize` refuses a plan that mixes it with other kinds; `proposePlan`
  refuses it when the turn that wrote it could not act (the request came from someone else), never treats it as covered by
  an earlier grant, and never lets the "approve every change" setting approve it. Its grant does not count in `trusted()`
  and its approval leaves `ForeignSinceGrant` as it was, so it never opens direct work. A user-written turn that is not
  trusted yet is told, with `terminalPlanHint`, to answer with such a plan instead of a refusal.
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
- `POST /api/update/check`: fetches the branch of the chosen channel (`origin/main`, or `origin/develop` for the beta,
  from the `updateChannel` setting, `betaUpdates` in `/api/settings`) and returns the new status; at most one real fetch
  every 30 seconds for the same channel, otherwise the cached status. A result is never shown for another channel than
  the one it was measured for. The core also checks 30 seconds after it starts and every hour; the window polls the
  cached status every 5 minutes and asks for a check when it comes back after 15 minutes. The menu's **Check for
  Updates…** uses this route too.
- "Behind" is measured from the installed build's commit to the channel's branch. A checkout with commits that are on no
  branch of `origin` is blocked (`diverged`), and so is one on another branch than `main` or `develop`
  (`other-branch`): the update script switches between those two itself (`git switch`), never away from the user's work.

```json
{"channel":"stable","branch":"main","supported":true,"reason":"","message":"","installed":{"version":"0.1.0","commit":"abc1234","installedAt":"2026-11-01T09:30:00Z"},
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


### Authorized merge requests and LFS pushes

`gitlab.merge-request` is an executable plan action. Its normalized GitLab scope, source (`branch`) and target (`base`)
are fingerprinted for approval. Unlike broad code grants, MR grants persist the exact action and branch pair; old
project-wide kind grants do not cover MRs. Automatic local-code approval never authorizes this action. The engine invokes
a typed GitLab callback, wired to the current registry credentials, after preceding code actions succeed. The client
checks for an existing open MR and for the remote source branch before POSTing. On an uncertain POST response it reconciles
with another lookup, without a second POST. It exposes no merge or update operation.

`pushWorktree` is shared by code edit and sync. It checks the trusted worktree pointer, detects LFS in attribute history,
requires git-lfs, transfers the source history with `git lfs push --all origin HEAD`, then pushes the Git ref. Incomplete
LFS uploads are explicitly forbidden. All calls retain `core.hooksPath=/dev/null`; LFS commands use remote timeouts and
machine credentials. Failures leave committed work recoverable and stop subsequent MR creation.
