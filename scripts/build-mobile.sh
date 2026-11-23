#!/bin/bash
# Builds the signed Android release of the phone app (mobile/), with a version that always goes up, and optionally
# installs it on one phone. The build number is the number of commits on the current branch, so every build of a newer
# commit can be installed over the previous one (`adb install -r` refuses a lower versionCode); the version name is
# the one of mobile/pubspec.yaml, with the short commit when the tree is not clean or not on a tag.
#
#   scripts/build-mobile.sh [--install <adb serial>] [--dry-run]
#
# The release keystore stays outside the repository: ~/.pouple/companion-key.properties is copied to the gitignored
# mobile/android/key.properties. Nothing secret is printed.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
min_free_gb=4
serial="" dry_run=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --install) serial="${2:?--install needs an adb serial}"; shift 2 ;;
    --dry-run) dry_run=1; shift ;;
    *) printf 'build-mobile: unknown option %s\n' "$1" >&2; exit 2 ;;
  esac
done

cd "$root"
base="$(sed -n 's/^version: *\([0-9][0-9.]*\).*/\1/p' mobile/pubspec.yaml | head -1)"
[[ -n "$base" ]] || { printf 'build-mobile: no version in mobile/pubspec.yaml\n' >&2; exit 1; }
number="$(git rev-list --count HEAD)"
commit="$(git rev-parse --short HEAD)"
name="$base"
if [[ -n "$(git status --porcelain -- mobile)" ]]; then name="$base+$commit-dirty"; fi
printf 'build-mobile: version %s (build %s, commit %s)\n' "$name" "$number" "$commit"

free_gb="$(df -g /System/Volumes/Data | awk 'NR==2 {print $4}')"
if (( free_gb < min_free_gb )); then
  printf 'build-mobile: only %s GB free, %s needed (free some space: Xcode DerivedData, Go and Gradle caches)\n' "$free_gb" "$min_free_gb" >&2
  exit 1
fi
keys="$HOME/.pouple/companion-key.properties"
[[ -f "$keys" ]] || { printf 'build-mobile: %s is missing (the release keystore settings)\n' "$keys" >&2; exit 1; }
if (( dry_run )); then
  printf 'build-mobile: dry run, nothing built\n'
  exit 0
fi

install -m 600 "$keys" mobile/android/key.properties
trap 'rm -f "$root/mobile/android/key.properties"' EXIT
(cd mobile && fvm flutter build apk --release --split-per-abi --build-name "${name%%+*}" --build-number "$number")

apk="mobile/build/app/outputs/flutter-apk/app-arm64-v8a-release.apk"
[[ -f "$apk" ]] || { printf 'build-mobile: %s was not produced\n' "$apk" >&2; exit 1; }
mkdir -p "$HOME/.pouple"
cp "$apk" "$HOME/.pouple/pouple-companion-arm64.apk"
printf 'build-mobile: %s (%s)\n' "$HOME/.pouple/pouple-companion-arm64.apk" "$(du -h "$apk" | cut -f1)"

if [[ -n "$serial" ]]; then
  adb -s "$serial" install -r "$apk"
  printf 'build-mobile: installed on %s\n' "$serial"
fi
