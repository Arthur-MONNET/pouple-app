#!/bin/bash
# Copies the notices for the bundled web assets and Go code into build/third-party-licenses.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
licenses="$root/build/third-party-licenses"
cd "$root/core"
goroot="$(go env GOROOT)"
go_license="$goroot/LICENSE"
if [ ! -f "$go_license" ]; then
  # Homebrew stores the license at the package root, above libexec (GOROOT).
  go_license="$goroot/../LICENSE"
fi
websocket_module="$(go list -m -f '{{.Dir}}' github.com/coder/websocket)"
node_modules="$root/web/node_modules"

mkdir -p "$licenses"
cp "$root/web/dist/ThirdPartyWebLicenses.md" "$licenses/Web.md"
cp "$node_modules/@fontsource-variable/geist/LICENSE" "$licenses/Geist.txt"
cp "$node_modules/@fontsource-variable/geist-mono/LICENSE" "$licenses/GeistMono.txt"
cp "$node_modules/@fontsource/fredoka/LICENSE" "$licenses/Fredoka.txt"
cp "$node_modules/tailwindcss/LICENSE" "$licenses/TailwindCSS.txt"
cp "$go_license" "$licenses/Go.txt"
cp "$goroot/PATENTS" "$licenses/GoPatents.txt"
cp "$websocket_module/LICENSE.txt" "$licenses/CoderWebSocket.txt"
