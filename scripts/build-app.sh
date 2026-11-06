#!/bin/bash
# Builds build/Pouple.app: the Swift shell with the Go core bundled in Contents/Resources.
#
#   scripts/build-app.sh [--core <path to pouple-core>] [--universal]
#
# --core       the core binary to bundle (default: build/pouple-core, else core/pouple-core).
# --universal  build the shell for arm64 and x86_64 (the core you pass must be universal too, see `lipo -info`).
#
# Environment (all optional):
#   PROUPLE_SIGN_IDENTITY   a "Developer ID Application: ..." identity: sign with it, hardened runtime, timestamp.
#   PROUPLE_NOTARY_PROFILE  a notarytool keychain profile (xcrun notarytool store-credentials): submit and staple.
#   PROUPLE_VERSION         the marketing version (default 0.1.0).
#   PROUPLE_BUILD           the build number (default: number of git commits, else 1).
#
# About Gatekeeper and signing:
#   * Ad-hoc signature (the default, `codesign --sign -`): it only seals the bundle so macOS accepts to run it on
#     Apple Silicon (unsigned arm64 code does not run at all). It names no developer and no Apple service vouches
#     for it. An app downloaded from the Internet carries the quarantine flag, so Gatekeeper refuses to open it
#     with "cannot be opened because the developer cannot be verified"; the user must right-click > Open once
#     (or allow it in System Settings > Privacy & Security). SMAppService (start at login) may also refuse an
#     ad-hoc app: the menu then shows the error instead of failing silently.
#   * Developer ID + notarization (PROUPLE_SIGN_IDENTITY and PROUPLE_NOTARY_PROFILE set): Apple scanned the
#     build and a ticket is stapled to it, so it opens with a double click, offline included.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/build"
app="$out/Pouple.app"
version="${PROUPLE_VERSION:-0.1.0}"
build="${PROUPLE_BUILD:-$(git -C "$root" rev-list --count HEAD 2>/dev/null || echo 1)}"
core=""
universal=0

while [ $# -gt 0 ]; do
  case "$1" in
    --core) core="${2:?--core needs a path}"; shift 2 ;;
    --universal) universal=1; shift ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$core" ]; then
  for candidate in "$out/pouple-core" "$root/core/pouple-core"; do
    [ -x "$candidate" ] && core="$candidate" && break
  done
fi
if [ -z "$core" ] || [ ! -x "$core" ]; then
  echo "no core binary: build it (cd core && go build -o ../build/pouple-core ./cmd/pouple-core) or pass --core" >&2
  exit 1
fi

archs=()
if [ "$universal" = 1 ]; then archs=(--arch arm64 --arch x86_64); fi

echo "==> swift build (release${archs:+, universal})"
swift build -c release --package-path "$root/app" ${archs[@]+"${archs[@]}"}
bin_dir="$(swift build -c release --package-path "$root/app" ${archs[@]+"${archs[@]}"} --show-bin-path)"

echo "==> assembling $app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin_dir/Pouple" "$app/Contents/MacOS/Pouple"
cp "$core" "$app/Contents/Resources/pouple-core"
chmod 755 "$app/Contents/MacOS/Pouple" "$app/Contents/Resources/pouple-core"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleExecutable</key><string>Pouple</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleIdentifier</key><string>app.pouple.Pouple</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Pouple</string>
  <key>CFBundleDisplayName</key><string>Pouple</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$build</string>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.productivity</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © $(date +%Y) Arthur Monnet. All rights reserved.</string>
  <key>NSPrincipalClass</key><string>NSApplication</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
printf 'APPL????' > "$app/Contents/PkgInfo"

echo "==> icon"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
swift "$root/scripts/make-icon.swift" "$work/icon-1024.png"
iconset="$work/AppIcon.iconset"
mkdir "$iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$work/icon-1024.png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
  sips -z "$((size * 2))" "$((size * 2))" "$work/icon-1024.png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

echo "==> signing"
if [ -n "${PROUPLE_SIGN_IDENTITY:-}" ]; then
  # Inside-out: the helper binary first, then the app, both with the hardened runtime and a secure timestamp.
  codesign --force --options runtime --timestamp --sign "$PROUPLE_SIGN_IDENTITY" "$app/Contents/Resources/pouple-core"
  codesign --force --options runtime --timestamp --sign "$PROUPLE_SIGN_IDENTITY" "$app"
else
  echo "    ad-hoc (set PROUPLE_SIGN_IDENTITY for a Developer ID signature)"
  codesign --force --deep --sign - "$app"
fi
codesign --verify --deep --strict "$app"

if [ -n "${PROUPLE_SIGN_IDENTITY:-}" ] && [ -n "${PROUPLE_NOTARY_PROFILE:-}" ]; then
  echo "==> notarizing"
  zip="$work/Pouple.zip"
  ditto -c -k --keepParent "$app" "$zip"
  xcrun notarytool submit "$zip" --keychain-profile "$PROUPLE_NOTARY_PROFILE" --wait
  xcrun stapler staple "$app"
fi

echo "built $app ($version, build $build)"
