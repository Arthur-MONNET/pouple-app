# Pouple

A macOS assistant that watches the Slack messages that would notify you, has your local Codex look into them, and
shows the answer as floating bubbles in a screen corner and as conversations in a window. You only take the
decisions: nothing is changed in your code before you approve it, and a Stop button ends any run.

## Install

Download `Pouple.dmg` from the [latest release](../../releases/latest) (or the download page), drag Pouple to
Applications and open it. Until the app is notarized, macOS asks you to confirm the first opening: right-click the
app, then Open.

The first launch guides you through two connections, with a confirmation card showing whose account you connected:

1. **Slack**: one click creates the Slack app with every permission already filled in; paste the token it gives you.
2. **Codex**: Pouple installs the Codex CLI if it is missing and signs in with ChatGPT, with its own login.

More connectors can be added later from the Connectors page.

## Security

- Tokens and keys live in the macOS Keychain. They are never written to a file, never sent to the interface once
  saved, and never put in a URL or a command line.
- The core listens on `127.0.0.1` only, behind a per-launch bearer token, a Host check and a cross-site check.
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

Push a tag (`git tag v0.1.0 && git push origin v0.1.0`): the workflow builds a universal app and attaches
`Pouple.dmg` to a GitHub Release. Add the `CERTIFICATE_P12`, `CERTIFICATE_PASSWORD`, `SIGN_IDENTITY` and
`NOTARY_PROFILE` secrets to sign and notarize it. The download page needs the repository to be public.
