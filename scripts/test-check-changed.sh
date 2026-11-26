#!/bin/bash
# Exercise selection with real Git history and stubbed module tools, without builds.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd -P)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/pouple-changed-test.XXXXXX")"
trap 'rm -rf -- "$tmp"' EXIT
repo="$tmp/repo with spaces"
mkdir -p "$tmp/bin"
export CHECK_CHANGED_LOG="$tmp/commands"
export CHECK_CHANGED_FAIL=''
export PATH="$tmp/bin:$PATH"
for tool in go pnpm swift fvm df; do
  cat > "$tmp/bin/$tool" <<'STUB'
#!/bin/bash
set -eu
command="${PWD##*/} ${0##*/} $*"
if [[ ${0##*/} != df ]]; then printf '%s\n' "$command" >> "$CHECK_CHANGED_LOG"; fi
if [[ "$command" == "$CHECK_CHANGED_FAIL" ]]; then exit 37; fi
STUB
  chmod +x "$tmp/bin/$tool"
done

new_repo() {
  rm -rf -- "$repo"
  mkdir -p "$repo/scripts" "$repo/core/internal/remote" "$repo/web" "$repo/app" "$repo/mobile" "$repo/docs"
  cp "$here/check-changed.sh" "$repo/scripts/check-changed.sh"
  for script in test-install-record test-next-version test-check-changed; do
    cat > "$repo/scripts/$script.sh" <<'SCRIPT'
#!/bin/bash
name="${0##*/}"
printf 'script %s\n' "${name%.sh}" >> "$CHECK_CHANGED_LOG"
SCRIPT
  done
  printf 'ignored/\n' > "$repo/.gitignore"
  printf 'base\n' > "$repo/core/base.go"
  printf 'base\n' > "$repo/core/internal/remote/base.go"
  printf 'base\n' > "$repo/web/base.ts"
  printf 'base\n' > "$repo/app/base.swift"
  printf 'base\n' > "$repo/mobile/base.dart"
  git -C "$repo" init -q
  git -C "$repo" config user.name Test
  git -C "$repo" config user.email test@example.invalid
  git -C "$repo" config commit.gpgsign false
  git -C "$repo" config core.hooksPath /dev/null
  git -C "$repo" add .
  git -C "$repo" commit -qm base
  git -C "$repo" update-ref refs/remotes/origin/develop HEAD
  : > "$CHECK_CHANGED_LOG"
  CHECK_CHANGED_FAIL=''
}
run_check() {
  local status=0
  (cd "$repo/web" && bash ../scripts/check-changed.sh "$@") > "$tmp/output" 2>&1 || status=$?
  if [[ $status != "${expected_status:-0}" ]]; then
    cat "$tmp/output" >&2
    printf 'FAIL: expected exit %s, got %s\n' "${expected_status:-0}" "$status" >&2
    exit 1
  fi
}
expect_commands() {
  printf '%s' "$1" > "$tmp/expected"
  diff -u "$tmp/expected" "$CHECK_CHANGED_LOG"
  printf 'ok: %s\n' "$2"
}
core_checks=$'core go vet ./...\ncore go test -race ./...\n'
web_checks=$'web pnpm typecheck\nweb pnpm test\nweb pnpm knip\nweb pnpm build\n'
app_checks=$'app swift test\n'
mobile_checks=$'mobile fvm flutter analyze\nmobile fvm flutter test\n'

new_repo
run_check
expect_commands '' 'clean checkout selects no checks'
printf 'documentation\n' > "$repo/docs/new.md"
printf 'documentation\n' > "$repo/web/README.md"
mkdir "$repo/ignored"
printf 'ignored\n' > "$repo/ignored/example.go"
run_check
expect_commands '' 'documentation and ignored files select no checks'

new_repo
printf 'committed\n' >> "$repo/core/base.go"
git -C "$repo" add core/base.go
git -C "$repo" commit -qm feature
printf 'staged\n' >> "$repo/web/base.ts"
git -C "$repo" add web/base.ts
printf 'local\n' >> "$repo/app/base.swift"
printf 'untracked\n' > "$repo/mobile/new file.dart"
run_check
expect_commands "$core_checks$web_checks$app_checks$mobile_checks" 'committed, staged, local and untracked module changes are combined'

new_repo
printf 'staged\n' >> "$repo/core/base.go"
git -C "$repo" add core/base.go
git -C "$repo" show HEAD:core/base.go > "$repo/core/base.go"
run_check
expect_commands "$core_checks" 'unstaged reversions do not hide staged changes'

new_repo
git -C "$repo" rm -q core/internal/remote/base.go
run_check
expect_commands "$core_checks"$'core go test -race -count=5 ./internal/remote\n' 'remote deletions retain the repeated remote check'

new_repo
git -C "$repo" mv core/base.go 'web/renamed file.ts'
run_check
expect_commands "$core_checks$web_checks" 'cross-module renames check both sides'

new_repo
mkdir -p "$repo/web/space tab"$'\t\n''core'
printf 'changed\n' > "$repo/web/space tab"$'\t\n''core/fake.go'
printf 'changed\n' > "$repo/web/-[literal]*.ts"
run_check
expect_commands "$web_checks" 'spaces, tabs, newlines and wildcard characters remain literal paths'

new_repo
printf '22\n' > "$repo/.nvmrc"
run_check
expect_commands "$web_checks" 'root Node configuration selects web checks'

new_repo
mkdir -p "$repo/.github/workflows"
printf 'name: CI\n' > "$repo/.github/workflows/ci.yml"
run_check
expect_commands $'script test-install-record\nscript test-next-version\nscript test-check-changed\n'"$core_checks"$'core go test -race -count=5 ./internal/remote\n'"$web_checks$app_checks$mobile_checks" 'shared CI configuration selects every documented suite'

new_repo
printf '\n' >> "$repo/scripts/test-check-changed.sh"
run_check
expect_commands $'script test-check-changed\n' 'changed shell checks run their targeted regression test'

new_repo
printf 'committed\n' >> "$repo/core/base.go"
git -C "$repo" add core/base.go
git -C "$repo" commit -qm feature
feature="$(git -C "$repo" rev-parse HEAD)"
git -C "$repo" checkout -qb upstream refs/remotes/origin/develop
printf 'upstream\n' >> "$repo/web/base.ts"
git -C "$repo" add web/base.ts
git -C "$repo" commit -qm upstream
git -C "$repo" update-ref refs/remotes/origin/develop HEAD
git -C "$repo" checkout -q "$feature"
run_check
expect_commands "$core_checks" 'diverged upstream changes are excluded by the merge base'
: > "$CHECK_CHANGED_LOG"
run_check HEAD
expect_commands '' 'an explicit comparison ref is accepted'

new_repo
expected_status=2
run_check nonexistent-ref
[[ "$(cat "$tmp/output")" == *'invalid comparison ref'* ]]
expect_commands '' 'an invalid ref fails clearly before running commands'
git -C "$repo" checkout -q --orphan unrelated
git -C "$repo" rm -qrf .
git -C "$repo" commit -qm unrelated --allow-empty
unrelated="$(git -C "$repo" rev-parse HEAD)"
git -C "$repo" checkout -q refs/remotes/origin/develop
run_check "$unrelated"
[[ "$(cat "$tmp/output")" == *'no merge base'* ]]
expect_commands '' 'unrelated history fails clearly before running commands'
unset expected_status

new_repo
printf 'local\n' >> "$repo/core/base.go"
printf 'local\n' >> "$repo/web/base.ts"
CHECK_CHANGED_FAIL='core go vet ./...'
expected_status=37
run_check
expect_commands $'core go vet ./...\n' 'the first failing command preserves its exit code and stops later checks'
unset expected_status

new_repo
printf 'local\n' >> "$repo/web/base.ts"
CHECK_CHANGED_FAIL='web pnpm knip'
expected_status=37
run_check
expect_commands $'web pnpm typecheck\nweb pnpm test\nweb pnpm knip\n' 'a later failure also preserves its exit code'
unset expected_status

new_repo
printf 'true\n' > "$repo/scripts/a-valid.sh"
printf 'if\n' > "$repo/scripts/z-invalid.sh"
expected_status=2
run_check
expect_commands '' 'every changed shell file is syntax checked, including the second file'
printf 'All changed-file checks passed.\n'
