# Pouple

A macOS assistant that watches the Slack messages that would notify you, has your local Codex look into them, and
shows the answer as floating bubbles in a screen corner and as conversations in a window. You only take the
decisions: nothing is changed in your code before you approve it, and a Stop button ends any run.

## Install

Pouple builds locally on macOS 14 or later, on Intel and Apple Silicon. Install the Xcode Command Line Tools,
Go 1.27.3 or later, Node.js 22.12 or later, and pnpm 10. Run `xcode-select --install` if you need the Xcode tools.

```sh
git clone https://github.com/Arthur-MONNET/pouple-app.git
cd pouple-app
bash scripts/install-local.sh
```

If you use NVM, run `nvm install` from the checkout before running the installer. The `.nvmrc` selects Node.js 24.
If pnpm is missing after switching Node versions, run `npm install --global pnpm@10`.

The script builds and verifies the app, installs it at `~/Applications/Pouple.app`, and launches it. It preserves
application data and the macOS security settings.

The first launch guides you through two connections, with a confirmation card showing whose account you connected:

1. **Slack**: one click creates the Slack app with every permission already filled in; paste the token it gives you.
2. **Codex**: If the CLI is missing, Pouple downloads a verified OpenAI release into its own application folder.
   Sign in with ChatGPT in the browser. An existing CLI on your PATH is also supported.

More connectors can be added later from the Connectors page. GitLab, Asana and Slite are optional and read-only:
once connected, the assistant is given your open tasks and merge requests and the notes that match each Slack
message, as data it can read but never obey. Their tokens live in the Keychain like the Slack token.

Local tools can read and interact with Pouple conversations through the [local Assistant API](docs/LOCAL-API.md).

Slack can wake Pouple as soon as an event arrives. In the Slack setup, add an optional app-level token
(`xapp-`) with `connections:write`. The generated manifest enables Socket Mode and subscribes to user
message events. For an existing Slack app, follow the Socket Mode instructions in Pouple’s Slack setup and
save the connection again. Pouple still checks Slack periodically to catch messages missed while disconnected.

## Updates

Open **Pouple → Check for Updates…**. Quit Pouple before updating, then run the following from the source directory:

```sh
git pull --ff-only
bash scripts/install-local.sh
```

The installer replaces the previous build at the same location. Settings and conversations live in Application
Support, and Slack tokens are stored in Keychain.

## Security

- Slack tokens live in the macOS Keychain and are write-only in the interface. Codex stores its login in
  Pouple’s dedicated Codex home.
- The core listens on `127.0.0.1` only, behind a per-launch bearer token, a Host check and a cross-site check.
  The macOS shell passes the token to the core through its startup pipe and injects it into the web view only
  for the core’s exact origin.
- Slack text is always handed to Codex as data between delimiters; Codex works read-only until you approve a change,
  and the writing turn never sees the Slack text.

## Layout

| Folder | What |
|---|---|
| `core/` | Go: connectors, Slack inbox, Codex runner, assistant engine, local API. One static binary. |
| `web/` | Vue 3 + Tailwind: setup wizard, conversations, connectors, settings. Embedded in the core. |
| `app/` | Swift: the macOS shell (window, menu bar, floating bubbles). |
| `site/` | The installation page (GitHub Pages). |
| `scripts/` | Local installation and app bundling. |

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design.

## Develop

```bash
cd core && go test -race ./...
cd web && pnpm install && pnpm test && pnpm build:embed
cd core && go build -o /tmp/pouple-core ./cmd/pouple-core
scripts/build-app.sh --core /tmp/pouple-core    # build/Pouple.app
```

Run the core alone with a fixed port and token for web development:
`POUPLE_PORT=4400 POUPLE_TOKEN=dev POUPLE_HOME=$(mktemp -d) go run ./cmd/pouple-core`.

## Release

Push a version tag such as `v0.1.1` to the public repository. The **Release** workflow publishes its source
release, which the in-app update check uses to announce new versions. GitHub Pages hosts the
[installation instructions](https://arthur-monnet.github.io/pouple-app/#install).
