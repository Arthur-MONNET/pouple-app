# Pouple

A macOS assistant that watches the Slack messages that would notify you, has your local Codex look into them, and
shows the answer as floating bubbles in a screen corner and as conversations in a window. You only take the
decisions: nothing is changed in your code before you approve it, and a Stop button ends any run.

## Install

Download [Pouple.zip](https://arthur-monnet.github.io/pouple-app/), open it, then drag Pouple to Applications.
Pouple supports macOS 14 or later on Intel and Apple Silicon.

The first launch guides you through two connections, with a confirmation card showing whose account you connected:

1. **Slack**: one click creates the Slack app with every permission already filled in; paste the token it gives you.
2. **Codex**: If the CLI is missing, Pouple downloads a verified OpenAI release into its own application folder.
   Sign in with ChatGPT in the browser. An existing CLI on your PATH is also supported.

More connectors can be added later from the Connectors page.

Slack can wake Pouple as soon as an event arrives. In the Slack setup, add an optional app-level token
(`xapp-`) with `connections:write`. The generated manifest enables Socket Mode and subscribes to user
message events. For an existing Slack app, follow the Socket Mode instructions in Pouple’s Slack setup and
save the connection again. Pouple still checks Slack periodically to catch messages missed while disconnected.

## Updates

Open **Pouple → Check for Updates…**. If a newer release is available, download its ZIP, quit Pouple, and move the
new app into Applications. Choose **Replace** when Finder asks. Settings and conversations live in Application
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
| `site/` | The download page (GitHub Pages). |
| `scripts/` | App bundle and disk image. |

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design.

## Develop

```bash
cd core && go test -race ./...
cd web && pnpm install && pnpm test && pnpm build:embed
cd core && go build -o /tmp/pouple-core ./cmd/pouple-core
scripts/build-app.sh --core /tmp/pouple-core    # build/Pouple.app
scripts/make-dmg.sh                              # build/Pouple.dmg
```

Run the core alone with a fixed port and token for web development:
`POUPLE_PORT=4400 POUPLE_TOKEN=dev POUPLE_HOME=$(mktemp -d) go run ./cmd/pouple-core`.

## Release

Run the **Release** workflow manually to build a universal test ZIP without publishing a release. The
`Pouple-validation` artifact contains `Pouple.zip`. The default build is ad hoc signed. Gatekeeper may require
manual approval in **System Settings → Privacy & Security** on the test Mac. Enable `sign_and_notarize` to
rehearse the Apple signing process with the test ZIP.

Push a version tag such as `v0.1.0` to build, sign, notarize, and publish both `Pouple.zip` and `Pouple.dmg`.
Configure these GitHub Actions secrets first:

| Secret | Value |
|---|---|
| `CERTIFICATE_P12` | Base64-encoded Developer ID Application certificate and private key in PKCS#12 format |
| `CERTIFICATE_PASSWORD` | Password for the PKCS#12 file |
| `SIGN_IDENTITY` | Full Developer ID Application signing identity |
| `APPLE_ID` | Apple Account used by the notary service |
| `APPLE_TEAM_ID` | Apple Developer Team ID |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for the Apple Account |

Enable GitHub Pages with **GitHub Actions** as its source and run the **Site** workflow to publish the download
page. The page and the in-app update check use the public GitHub Releases channel.
