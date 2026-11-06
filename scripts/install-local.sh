#!/bin/bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/install-local.sh [--applications-dir <path>] [--no-launch]

Build Pouple from this checkout, install it in ~/Applications, and open it.
Requires macOS 14+, Xcode Command Line Tools, Node.js 22.12+, pnpm 10, and Go
(see core/go.mod for the required toolchain).
USAGE
}

die() {
  printf 'Pouple: %s\n' "$*" >&2
  exit 1
}

# Records where this checkout lives, so the app can offer to update itself from it. The record is the only thing
# this script ever writes outside ~/Applications and its temporary build folder, and it is a single new file; the
# rest of Pouple's data folder and the Keychain are never read or modified here.
# Never fails an installation: a problem is reported as a warning and the function returns 1.
write_install_record() {
  local root="$1" version="$2" home commit installed_at temporary
  home="${POUPLE_HOME:-${HOME:+$HOME/Library/Application Support/Pouple}}"
  [[ -n "$home" && "$home" == /* ]] || { printf 'Pouple: no data folder, so updates from the app are unavailable.\n' >&2; return 1; }
  commit="$(git -C "$root" rev-parse HEAD 2>/dev/null)" && [[ "$commit" =~ ^[0-9a-f]{40,64}$ ]] \
    || { printf 'Pouple: this folder is not a git checkout, so updates from the app are unavailable.\n' >&2; return 1; }
  installed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || return 1
  if [[ ! -d "$home" ]]; then
    # shellcheck disable=SC2174 # the mode is wanted on the Pouple folder only; its parents already exist or take the default
    mkdir -p -m 700 -- "$home" || { printf 'Pouple: could not create %s, so updates from the app are unavailable.\n' "$home" >&2; return 1; }
  fi
  # mktemp creates the file with mode 0600 and fails rather than reuse an existing name; the rename is atomic and
  # replaces a symlink at the destination instead of following it.
  temporary="$(mktemp "$home/.install.json.XXXXXX")" \
    || { printf 'Pouple: could not write the install record, so updates from the app are unavailable.\n' >&2; return 1; }
  if POUPLE_RECORD_ROOT="$root" POUPLE_RECORD_COMMIT="$commit" POUPLE_RECORD_VERSION="$version" \
      POUPLE_RECORD_TIME="$installed_at" node -e '
        const e = process.env;
        process.stdout.write(JSON.stringify({
          sourceRoot: e.POUPLE_RECORD_ROOT, commit: e.POUPLE_RECORD_COMMIT,
          version: e.POUPLE_RECORD_VERSION, installedAt: e.POUPLE_RECORD_TIME,
        }, null, 2) + "\n");
      ' >"$temporary" && chmod 600 "$temporary" && mv -f -- "$temporary" "$home/install.json"; then
    return 0
  fi
  rm -f -- "$temporary"
  printf 'Pouple: could not write the install record, so updates from the app are unavailable.\n' >&2
  return 1
}

# Sourcing this file (for tests) only defines the functions above.
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return 0; fi

applications="${HOME:+$HOME/Applications}"
launch=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --applications-dir)
      [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || die "$1 needs a directory."
      applications="$2"
      shift 2 ;;
    --no-launch) launch=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown option: $1. See --help." ;;
  esac
done

[[ "$(uname -s)" == Darwin ]] || die "This installer requires macOS."
macos_version="$(sw_vers -productVersion)"
[[ "${macos_version%%.*}" -ge 14 ]] || die "macOS 14 or later is required."
[[ "$EUID" -ne 0 ]] || die "Run this installer as your own user, without sudo."
[[ -n "$applications" ]] || die "Set HOME or provide --applications-dir."
case "$(uname -m)" in
  arm64) go_arch=arm64 ;;
  x86_64) go_arch=amd64 ;;
  *) die "This Mac's processor architecture is not supported." ;;
esac

root="$(cd "$(dirname "$0")/.." && pwd -P)"
[[ -f "$root/core/go.mod" && -f "$root/web/pnpm-lock.yaml" ]] \
  || die "Run this script from a Pouple source checkout."
required_go="$(awk '$1 == "go" { print $2; exit }' "$root/core/go.mod")"
if ! xcode-select -p >/dev/null 2>&1 || ! xcrun --find swift >/dev/null 2>&1; then
  die "Install the Xcode Command Line Tools with xcode-select --install, then try again."
fi
command -v go >/dev/null || die "Install Go $required_go or later and make sure go is on PATH."
(cd "$root/core" && go version) || die "Go could not load the toolchain required by core/go.mod ($required_go)."
command -v node >/dev/null || die "Install Node.js 22.12 or later. If you use NVM, run nvm install from the Pouple checkout, then retry."
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); process.exit(major > 22 || (major === 22 && minor >= 12) ? 0 : 1)' \
  || die "Node.js 22.12 or later is required (found $(node --version))."
pnpm_version="$(pnpm --version 2>/dev/null || true)"
if [[ "${pnpm_version%%.*}" != 10 ]] && command -v brew >/dev/null; then
  pnpm_prefix="$(brew --prefix pnpm@10 2>/dev/null || true)"
  if [[ -n "$pnpm_prefix" && -x "$pnpm_prefix/bin/pnpm" ]]; then
    export PATH="$pnpm_prefix/bin:$PATH"
    pnpm_version="$(pnpm --version)" || die "Could not run Homebrew's pnpm 10."
  fi
fi
[[ -n "$pnpm_version" ]] || die "Install pnpm 10 (for example: npm install --global pnpm@10)."
[[ "${pnpm_version%%.*}" == 10 ]] || die "pnpm 10 is required (found $pnpm_version)."

mkdir -p -- "$applications"
applications="$(cd -- "$applications" && pwd -P)"

check_destination() {
  local directory="$applications" owner mode
  while :; do
    owner="$(stat -f '%u' "$directory")"
    mode="$(stat -f '%p' "$directory")"
    [[ "$owner" == "$UID" || ( "$directory" != "$applications" && "$owner" == 0 ) ]] \
      || die "The installation directory must be owned by you and have trusted parents: $directory"
    # A sticky parent such as /private/tmp protects entries owned by you or root.
    if (( (8#$mode & 8#22) != 0 )) && \
      { [[ "$directory" == "$applications" ]] || (( (8#$mode & 8#1000) == 0 )); }; then
      die "Choose a private installation directory. This directory is writable by other users: $directory"
    fi
    # On macOS, ls -le lists ACL entries independently of the directory mode.
    # shellcheck disable=SC2012
    if ! LC_ALL=C ls -lde "$directory" | awk '/^[[:space:]]*[0-9]+: .* allow / { exit 1 }'; then
      die "Choose a private installation directory without ACLs granting access: $directory"
    fi
    [[ "$directory" != / ]] || break
    directory="${directory%/*}"
    directory="${directory:-/}"
  done
}

check_destination
app="$applications/Pouple.app"
backup="$applications/.pouple-previous.app"
stage="$applications/.pouple-install"
bundle_identifier=app.pouple.Pouple

check_existing_bundle() {
  local bundle="$1"
  if [[ -e "$bundle" || -L "$bundle" ]]; then
    [[ ! -L "$bundle" && -d "$bundle" && \
      "$(plutil -extract CFBundleIdentifier raw "$bundle/Contents/Info.plist" 2>/dev/null)" == "$bundle_identifier" ]] \
      || die "Refusing to replace an unrecognized bundle at $bundle. Move it out of the way and try again."
  fi
}

require_app_closed() {
  local pid invocation
  while read -r pid; do
    [[ -n "$pid" ]] || continue
    invocation="$(ps -ww -p "$pid" -o command= 2>/dev/null)" || continue
    if [[ "$launch" == 1 || "$invocation" == "$app/Contents/MacOS/Pouple" || \
      "$invocation" == "$app/Contents/MacOS/Pouple "* ]]; then
      die "Quit Pouple from its menu, then rerun this installer."
    fi
  done < <(pgrep -u "$UID" -x Pouple || true)
}

require_app_closed
check_existing_bundle "$app"
check_existing_bundle "$backup"
if [[ "$app" != /Applications/Pouple.app && -d /Applications/Pouple.app ]]; then
  printf 'A separate copy exists at /Applications/Pouple.app. Use %s for this build.\n' "$app"
fi
mkdir -m 700 -- "$stage" || die "Another install may be in progress. If it was interrupted, inspect $stage before retrying."

work=""
cleanup() {
  local status=$?
  if [[ -n "$work" ]]; then rm -rf -- "$work" || status=1; fi
  if [[ -d "$stage/previous.app" ]]; then
    if [[ ! -e "$app" && ! -L "$app" ]] && mv -- "$stage/previous.app" "$app"; then
      printf 'Restored the previous Pouple installation.\n' >&2
    else
      printf 'The previous Pouple installation is safe at %s.\n' "$stage/previous.app" >&2
      exit 1
    fi
  fi
  rm -rf -- "$stage"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# The web build replaces the checked-in fallback page; work on a copy of the source.
work="$(mktemp -d "${TMPDIR:-/tmp}/pouple-build.XXXXXX")"
rsync -a --exclude='/.git' --exclude='/.DS_Store' \
  --exclude='/app/.build' --exclude='/app/.swiftpm' \
  --exclude='/core/internal/ui/dist' --exclude='/core/pouple-core' \
  --exclude='/web/node_modules' --exclude='/web/dist' \
  "$root/app" "$root/brand" "$root/core" "$root/scripts" "$root/web" "$work/"

version=0.1.0
build="$(git -C "$root" rev-list --count HEAD 2>/dev/null || echo 1)"
tag="$(git -C "$root" describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' \
  --exclude '*[-+]*' 2>/dev/null || true)"
if [[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  version="${tag#v}"
fi

printf '==> Building the web interface\n'
(
  cd "$work/web"
  pnpm install --frozen-lockfile
  pnpm build:embed
)
printf '==> Building the Go core\n'
mkdir -- "$work/build"
(
  cd "$work/core"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$go_arch" go build -trimpath -ldflags '-s -w' \
    -o "$work/build/pouple-core" ./cmd/pouple-core
)
bash "$work/scripts/collect-licenses.sh"
PROUPLE_VERSION="$version" PROUPLE_BUILD="$build" PROUPLE_SIGN_IDENTITY='' PROUPLE_NOTARY_PROFILE='' \
  bash "$work/scripts/build-app.sh" --core "$work/build/pouple-core" --licenses "$work/build/third-party-licenses"

printf '==> Verifying the installation bundle\n'
ditto "$work/build/Pouple.app" "$stage/Pouple.app"
codesign --verify --deep --strict "$stage/Pouple.app"
require_app_closed
check_existing_bundle "$app"
check_existing_bundle "$backup"

if [[ -d "$app" ]]; then
  mv -- "$app" "$stage/previous.app"
fi
mv -- "$stage/Pouple.app" "$app"
if [[ -d "$stage/previous.app" ]]; then
  if [[ -d "$backup" ]]; then
    mv -- "$backup" "$stage/older.app"
  fi
  mv -- "$stage/previous.app" "$backup"
  printf 'Previous installation saved at %s\n' "$backup"
fi
printf 'Installed Pouple %s at %s\n' "$version" "$app"
write_install_record "$root" "$version" || true
printf 'Your connections and settings are kept.\n'

if [[ "$launch" == 1 ]]; then
  open "$app" || die "Pouple was installed, but could not be opened. Open $app from Finder."
fi
