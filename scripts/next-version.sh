#!/bin/bash
# Prints the next release tag, from the last one and the subjects of the commits since (read on standard input, one
# per line). A "feat" commit bumps the minor number, anything else the patch number: v0.1.2 + "feat: x" -> v0.2.0,
# v0.1.2 + "fix: y" -> v0.1.3. Without a last tag, the first release is v0.1.0.
set -euo pipefail

last="${1:-}"
if [[ -z "$last" ]]; then
  printf 'v0.1.0\n'
  exit 0
fi
if [[ ! "$last" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  printf 'next-version: "%s" is not a vMAJOR.MINOR.PATCH tag\n' "$last" >&2
  exit 1
fi
major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}"
if grep -qE '^feat(\([^)]*\))?!?:'; then
  printf 'v%d.%d.0\n' "$major" "$((minor + 1))"
else
  printf 'v%d.%d.%d\n' "$major" "$minor" "$((patch + 1))"
fi
