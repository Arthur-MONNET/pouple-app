#!/bin/bash
# Packs build/Pouple.app into build/Pouple.dmg: the app and a shortcut to /Applications, with hdiutil only.
# Run scripts/build-app.sh first. When PROUPLE_SIGN_IDENTITY is set the disk image is signed too, and with
# PROUPLE_NOTARY_PROFILE it is notarized and stapled.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
app="$root/build/Pouple.app"
dmg="$root/build/Pouple.dmg"

[ -d "$app" ] || { echo "$app is missing: run scripts/build-app.sh first" >&2; exit 1; }

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
cp -R "$app" "$stage/Pouple.app"
ln -s /Applications "$stage/Applications"

rm -f "$dmg"
hdiutil create -volname "Pouple" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg" >/dev/null

if [ -n "${PROUPLE_SIGN_IDENTITY:-}" ]; then
  codesign --force --timestamp --sign "$PROUPLE_SIGN_IDENTITY" "$dmg"
  if [ -n "${PROUPLE_NOTARY_PROFILE:-}" ]; then
    xcrun notarytool submit "$dmg" --keychain-profile "$PROUPLE_NOTARY_PROFILE" --wait
    xcrun stapler staple "$dmg"
  fi
fi

echo "built $dmg"
