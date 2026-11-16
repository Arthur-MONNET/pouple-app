#!/bin/bash
# Checks scripts/next-version.sh, the rule the Version workflow tags main with.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd -P)"
failures=0
expect() {
  local want="$1" last="$2" subjects="$3" got
  got="$(printf '%b' "$subjects" | bash "$here/next-version.sh" "$last" 2>/dev/null)"
  if [[ "$got" == "$want" ]]; then printf 'ok: %s + [%s] -> %s\n' "${last:-none}" "${subjects//\\n/, }" "$got"
  else printf 'FAIL: %s + [%s] -> %s, want %s\n' "${last:-none}" "${subjects//\\n/, }" "$got" "$want" >&2; failures=$((failures + 1)); fi
}

expect v0.1.3 v0.1.2 'fix: a\nchore: b'
expect v0.2.0 v0.1.2 'fix: a\nfeat: b'
expect v0.2.0 v0.1.2 'feat(app): scoped'
expect v1.4.0 v1.3.9 'feat!: breaking stays minor'
expect v0.1.3 v0.1.2 'docs: features are described\nrefactor: feature flag'
expect v0.1.0 '' 'feat: first'
if printf 'fix: a\n' | bash "$here/next-version.sh" v1.2 2>/dev/null; then
  printf 'FAIL: a malformed last tag must be refused\n' >&2; failures=$((failures + 1))
else
  printf 'ok: a malformed last tag is refused\n'
fi

[[ "$failures" == 0 ]] || { printf '%d failure(s)\n' "$failures" >&2; exit 1; }
printf 'All next-version checks passed.\n'
