#!/bin/bash
# Dry run of the install record written by scripts/install-local.sh. It sources the installer (which then only
# defines its functions), points POUPLE_HOME at a temporary folder and never builds or installs anything.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd -P)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/pouple-record-test.XXXXXX")"
trap 'rm -rf -- "$tmp"' EXIT
failures=0
fail() { printf 'FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }
ok() { printf 'ok: %s\n' "$*"; }

# shellcheck source=install-local.sh
source "$here/install-local.sh"
set +e

# A checkout whose path holds characters that must be escaped in JSON.
checkout="$tmp/check \"out\" \\ dir"
mkdir -p "$checkout"
git -C "$checkout" init -q -b main
git -C "$checkout" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m first
checkout="$(cd "$checkout" && pwd -P)"
head="$(git -C "$checkout" rev-parse HEAD)"

field() { node -e 'const r = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")); console.log(r[process.argv[2]])' "$1" "$2"; }
mode() { stat -f '%Lp' "$1"; }

# 1. A fresh data folder is created 0700 and the record 0600 with the four fields.
export POUPLE_HOME="$tmp/support/Pouple"
write_install_record "$checkout" 0.1.0 || fail "write failed"
record="$POUPLE_HOME/install.json"
[[ "$(mode "$POUPLE_HOME")" == 700 ]] && ok "folder is 0700" || fail "folder mode $(mode "$POUPLE_HOME")"
[[ "$(mode "$record")" == 600 ]] && ok "record is 0600" || fail "record mode $(mode "$record")"
[[ "$(field "$record" sourceRoot)" == "$checkout" ]] && ok "sourceRoot survives quotes and backslashes" || fail "sourceRoot"
[[ "$(field "$record" commit)" == "$head" ]] && ok "commit is HEAD" || fail "commit"
[[ "$(field "$record" version)" == 0.1.0 ]] && ok "version" || fail "version"
[[ "$(field "$record" installedAt)" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] && ok "installedAt is RFC3339 UTC" || fail "installedAt"
leftovers="$(find "$POUPLE_HOME" -name '.install.json.*' | wc -l | tr -d ' ')"
[[ "$leftovers" == 0 ]] && ok "no temporary file left" || fail "$leftovers temporary files left"

# 2. Other files of an existing data folder are untouched, and its mode is left alone.
chmod 750 "$POUPLE_HOME"
printf 'precious' >"$POUPLE_HOME/config.json"
write_install_record "$checkout" 0.2.0 || fail "second write failed"
[[ "$(cat "$POUPLE_HOME/config.json")" == precious ]] && ok "config.json untouched" || fail "config.json changed"
[[ "$(mode "$POUPLE_HOME")" == 750 ]] && ok "existing folder mode untouched" || fail "folder mode changed"
[[ "$(field "$record" version)" == 0.2.0 ]] && ok "record replaced" || fail "record not replaced"

# 3. A symlink at the destination is replaced, never followed.
printf 'victim' >"$tmp/victim"
rm -f "$record"
ln -s "$tmp/victim" "$record"
write_install_record "$checkout" 0.3.0 || fail "write over symlink failed"
[[ "$(cat "$tmp/victim")" == victim && ! -L "$record" ]] && ok "symlink replaced, target untouched" || fail "symlink followed"

# 4. Failures warn and return non-zero instead of aborting the installer.
export POUPLE_HOME="$tmp/not-a-folder"
printf 'x' >"$POUPLE_HOME"
write_install_record "$checkout" 0.1.0 2>/dev/null && fail "a file in place of the folder must fail" || ok "unusable data folder reports failure"
export POUPLE_HOME="$tmp/support/Pouple"
mkdir -p "$tmp/plain"
write_install_record "$tmp/plain" 0.1.0 2>/dev/null && fail "a non-git folder must fail" || ok "non-git folder reports failure"
export POUPLE_HOME="relative/path"
write_install_record "$checkout" 0.1.0 2>/dev/null && fail "a relative data folder must fail" || ok "relative data folder refused"

# 5. The installer never names the keychain or Application Support, apart from the documented default folder.
if grep -nE 'security |find-generic|add-generic|delete-generic|Keychain' "$here/install-local.sh" | grep -v '^[0-9]*:#'; then
  fail "the installer mentions the keychain"
else
  ok "no keychain command in the installer"
fi

[[ "$failures" == 0 ]] || { printf '%d failure(s)\n' "$failures" >&2; exit 1; }
printf 'All install record checks passed.\n'
