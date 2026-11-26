#!/bin/bash
# Run the contributor checks for modules changed since the branch's merge base.
# Usage: bash scripts/check-changed.sh [ref] (default: origin/develop).
set -euo pipefail

if [[ $# -gt 1 || ${1:-} == -* ]]; then
  printf 'Usage: bash scripts/check-changed.sh [ref]\n' >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
ref="${1:-origin/develop}"
if ! ref_commit="$(git rev-parse --verify --end-of-options "$ref^{commit}" 2>/dev/null)"; then
  printf 'check-changed: invalid comparison ref %s. Fetch origin or pass an existing commit ref.\n' "$ref" >&2
  exit 2
fi
if ! base="$(git merge-base HEAD "$ref_commit")"; then
  printf 'check-changed: no merge base between HEAD and %s. Fetch more history or pass a related ref.\n' "$ref" >&2
  exit 2
fi

changes="$(mktemp "${TMPDIR:-/tmp}/pouple-changed.XXXXXX")"
trap 'rm -f -- "$changes"' EXIT
# Separate diffs retain changes cancelled out by another layer. Disabling rename
# detection includes both sides of moves, even when they cross module boundaries.
{
  git diff --no-ext-diff --no-renames --name-only -z "$base" HEAD --
  git diff --no-ext-diff --no-renames --name-only -z --cached HEAD --
  git diff --no-ext-diff --no-renames --name-only -z --
  git ls-files --others --exclude-standard -z
} > "$changes"

core=0 web=0 app=0 mobile=0 remote=0
install_test=0 version_test=0 changed_test=0
shell_files=()
while IFS= read -r -d '' file; do
  case "$file" in
    *.md|*.mdx|docs/*) continue ;;
  esac
  case "$file" in
    core/*) core=1 ;;
    web/*|.nvmrc) web=1 ;;
    app/*|scripts/make-icon.swift) app=1 ;;
    mobile/*) mobile=1 ;;
    .github/workflows/ci.yml) core=1; web=1; app=1; mobile=1; remote=1
      install_test=1; version_test=1; changed_test=1 ;;
  esac
  case "$file" in
    core/internal/remote/*) remote=1 ;;
    scripts/install-local.sh|scripts/test-install-record.sh) install_test=1 ;;
    scripts/next-version.sh|scripts/test-next-version.sh) version_test=1 ;;
    scripts/check-changed.sh|scripts/test-check-changed.sh) changed_test=1 ;;
  esac
  case "$file" in
    scripts/*.sh) [[ ! -f "$file" ]] || shell_files+=("$file") ;;
  esac
done < "$changes"

run_in() (
  local directory="$1"
  shift
  cd "$root/$directory"
  printf 'check-changed: %s:' "$directory"
  printf ' %q' "$@"
  printf '\n'
  "$@"
)

printf 'check-changed: comparing with merge base %s (%s).\n' "$base" "$ref"
if [[ $((core + web + app + mobile)) -gt 0 ]]; then
  disk_path=/System/Volumes/Data
  [[ -d "$disk_path" ]] || disk_path=/
  df -h "$disk_path"
fi
if [[ ${#shell_files[@]} -gt 0 ]]; then
  for file in "${shell_files[@]}"; do run_in . bash -n "$file"; done
fi
if [[ $install_test == 1 ]]; then run_in . bash scripts/test-install-record.sh; fi
if [[ $version_test == 1 ]]; then run_in . bash scripts/test-next-version.sh; fi
if [[ $changed_test == 1 ]]; then run_in . bash scripts/test-check-changed.sh; fi
if [[ $core == 1 ]]; then
  run_in core go vet ./...
  run_in core go test -race ./...
  if [[ $remote == 1 ]]; then run_in core go test -race -count=5 ./internal/remote; fi
fi
if [[ $web == 1 ]]; then
  run_in web pnpm typecheck
  run_in web pnpm test
  run_in web pnpm knip
  run_in web pnpm build
fi
if [[ $app == 1 ]]; then run_in app swift test; fi
if [[ $mobile == 1 ]]; then
  run_in mobile fvm flutter analyze
  run_in mobile fvm flutter test
fi
if [[ $((core + web + app + mobile + install_test + version_test + changed_test + ${#shell_files[@]})) == 0 ]]; then
  printf 'check-changed: no applicable checks (documentation or other non-module files only).\n'
else
  printf 'check-changed: all selected checks passed.\n'
fi
