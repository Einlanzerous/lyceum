#!/usr/bin/env bash
#
# Store-build hygiene guard (LYCM-104).
#
# The onboarding pivot in LYCM-101 rests on one promise: an install from the
# Play Store knows about nobody's server until someone scans an invite at it.
# The invite QR carries the origin (LYCM-102/103), so the app needs no
# compiled-in default — and must not have one. A public build that shipped with
# the maintainer's address inside would point every stranger who installed it at
# a private household's library, and would skip the connect prompt the whole
# scan flow starts from.
#
# Nothing in the Dart source can enforce that. It is a property of the *build
# command* — one `--dart-define` away from being untrue — and the mistake leaves
# no trace in a diff of the app. So it is checked here, from three sides:
#
#   config    the release build passes no --dart-define at all
#   hostnames the artifact names none of the maintainer's private hosts
#   origins   the Dart snapshot holds no absolute URL that isn't expected
#
# The last one is the general case: it catches a baked address whatever it
# points at, including one nobody thought to add to the denylist. A release
# snapshot turns out to hold a handful of URLs, all of them Flutter's own
# diagnostics plus the placeholder in the manual-address field, so an unexpected
# origin is a real signal rather than noise.
#
# Usage:
#   tool/check_store_build.sh                      # config checks only
#   tool/check_store_build.sh app-release.aab ...  # config + scan each artifact
#   tool/check_store_build.sh --self-test          # prove the scanner can fail
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
app=$(dirname -- "$here")     # mobile/lyceum
repo=$(cd -- "$app/../.." && pwd)

# Hostnames that identify the maintainer's own deployment. `lyceum-direct` is
# the SERV-60 direct edge a phone talks to; `zerogravity.industries` is the
# domain it sits under. Neither may appear anywhere in a public artifact.
FORBIDDEN_HOSTS=(
  "zerogravity.industries"
  "lyceum-direct"
)

# Every absolute URL a clean release snapshot is expected to contain. All but
# the first come from Flutter's own error messages; the first is the greyed-out
# example in the manual server-address field — an RFC1918 address that is a
# hint, not a default, and points at nothing outside the reader's own LAN.
#
# Adding to this list should be a deliberate, reviewed act: a new absolute URL
# inside a self-hosted client is exactly the kind of change worth noticing.
ALLOWED_ORIGINS=(
  "http://192.168.1.10:8080"      # server_settings.dart hintText
  "https://api.flutter.dev"
  "https://docs.flutter.dev"
  "https://github.com"
  "https://pub.dev"
)

RELEASE_WORKFLOW="$repo/.github/workflows/mobile-release.yml"

# Files that ship, or that decide what ships. Test fixtures are left out on
# purpose: they use reserved .example domains and reach no artifact.
SHIPPED_PATHS=(
  "$app/lib"
  "$app/assets"
  "$app/android"
  "$app/pubspec.yaml"
  "$RELEASE_WORKFLOW"
)

fail=0
note() { printf '%s\n' "$*" >&2; }
bad() { fail=1; note "FAIL: $*"; }

check_config() {
  if [ ! -f "$RELEASE_WORKFLOW" ]; then
    bad "release workflow not found at $RELEASE_WORKFLOW"
    return 0
  fi

  # Comment lines are stripped first: that file explains the rule it is being
  # held to, and prose about a flag is not the flag.
  local defines
  if defines=$(grep -vE '^[[:space:]]*#' "$RELEASE_WORKFLOW" | grep -n -- '--dart-define'); then
    note "$defines"
    bad "mobile-release.yml passes --dart-define; a store build bakes in no server (LYCM-104)"
  else
    note "ok: release build passes no --dart-define"
  fi

  local host hits clean=1
  for host in "${FORBIDDEN_HOSTS[@]}"; do
    # -I skips binaries (icons, fonts) — those are the artifact scan's job.
    # Markdown is skipped because the docs *do* name the private host: the
    # sideloaded family flavour in README.md is built from it, and docs ship
    # to nobody.
    if hits=$(grep -rInF --exclude='*.md' \
      --exclude-dir=build --exclude-dir=.gradle --exclude-dir=.dart_tool \
      -- "$host" "${SHIPPED_PATHS[@]}" 2>/dev/null); then
      note "$hits"
      bad "'$host' appears in files that ship (LYCM-104)"
      clean=0
    fi
  done
  [ "$clean" -eq 1 ] && note "ok: no private hostname in shipped sources or build config"
  return 0
}

# Unpack an APK/AAB and read every entry. A --dart-define value is const-folded
# into the Dart AOT snapshot (lib/<abi>/libapp.so, or base/lib/... in an AAB),
# compressed inside the zip, so the archive has to be expanded rather than
# grepped where it lies.
scan_artifact() {
  local artifact=$1
  if [ ! -f "$artifact" ]; then
    bad "artifact not found: $artifact"
    return 0
  fi

  local dir name
  name=$(basename -- "$artifact")
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' RETURN

  if ! unzip -q -o "$artifact" -d "$dir" 2>/dev/null; then
    bad "could not unpack $artifact"
    return 0
  fi

  local host hits clean=1
  for host in "${FORBIDDEN_HOSTS[@]}"; do
    # -a: the snapshot and the dex are binary; the string sits in them plainly.
    if hits=$(grep -rlaF -- "$host" "$dir" 2>/dev/null); then
      note "$(printf '%s\n' "$hits" | sed "s#^$dir/#  #")"
      bad "'$host' is baked into $name (LYCM-104)"
      clean=0
    fi
  done
  [ "$clean" -eq 1 ] &&
    note "ok: $name — no private hostname in $(find "$dir" -type f | wc -l) entries"

  scan_snapshot_origins "$dir" "$name"
  return 0
}

scan_snapshot_origins() {
  local dir=$1 name=$2 snapshots
  snapshots=$(find "$dir" -type f -name 'libapp.so')
  if [ -z "$snapshots" ]; then
    # A release APK/AAB always carries one per ABI. None means this is a debug
    # build (a JIT kernel blob) or an artifact this scan does not understand —
    # either way the origin check ran over nothing, which must not read as a pass.
    bad "$name contains no libapp.so — nothing to scan; is this a release build?"
    return 0
  fi

  # Match scheme + host + port only. Stopping at the path keeps the comparison
  # to the part that says *which server*, and makes it robust to a neighbouring
  # string running on past the URL in the snapshot's byte soup.
  local found unexpected
  found=$(printf '%s\n' "$snapshots" | xargs grep -ohaE 'https?://[A-Za-z0-9._:-]+' | sort -u)

  unexpected=$(printf '%s\n' "$found" |
    grep -vxF -f <(printf '%s\n' "${ALLOWED_ORIGINS[@]}") || true)

  if [ -n "$unexpected" ]; then
    note "$(printf '%s\n' "$unexpected" | sed 's/^/  /')"
    bad "$name — unexpected absolute URL(s) in the Dart snapshot (LYCM-104)"
  else
    note "ok: $name — snapshot holds only the $(printf '%s\n' "$found" | wc -l) expected URLs"
  fi
  return 0
}

# Build a zip without assuming `zip` is installed (it is not, on a stock Flutter
# dev box). python3 ships with both the runner image and the toolchain.
pack() {
  local src=$1 out=$2
  if command -v zip >/dev/null 2>&1; then
    (cd "$src" && zip -qr "$out" .)
  elif command -v python3 >/dev/null 2>&1; then
    # make_archive appends its own .zip, so the result is moved into place;
    # leaving it where python put it would hand the scan a missing file, and a
    # missing file fails for the wrong reason.
    python3 -c 'import shutil,sys; shutil.make_archive(sys.argv[1], "zip", sys.argv[2])' \
      "$out" "$src"
    mv -- "$out.zip" "$out"
  else
    bad "self-test needs zip or python3 to build its fixture"
    return 1
  fi
}

# A scanner that never fails is worse than no scanner, because it reads as
# proof. This plants a baked URL inside a zip shaped like an APK and asserts the
# scan rejects it — so a broken pattern or a wrong path surfaces here instead of
# as a quiet pass over a real store build. The third case is a host on no
# denylist: it can only be caught by the origin check, which keeps that check
# from rotting into decoration.
self_test() {
  local dir rc payload
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' RETURN

  for payload in "${FORBIDDEN_HOSTS[@]}" "books.example.invalid"; do
    rm -rf "${dir:?}/stage" "$dir/fake.apk"
    mkdir -p "$dir/stage/lib/arm64-v8a"
    # Binary, compressed and nested — the shape of a real libapp.so.
    printf '\x7fELF\x02\x01\x01\x00 https://%s/sign-in?token= \x00\x00' "$payload" \
      > "$dir/stage/lib/arm64-v8a/libapp.so"
    pack "$dir/stage" "$dir/fake.apk"

    rc=0
    ( fail=0; scan_artifact "$dir/fake.apk"; exit "$fail" ) 2>/dev/null || rc=$?
    if [ "$rc" -eq 0 ]; then
      bad "self-test: the scan passed an artifact with https://$payload baked in"
    else
      note "ok: self-test — a baked '$payload' is caught"
    fi
  done

  # And the other way round: a fixture holding only expected URLs must pass, or
  # the checks above prove nothing but that everything fails.
  rm -rf "${dir:?}/stage" "$dir/fake.apk"
  mkdir -p "$dir/stage/lib/arm64-v8a"
  printf '\x7fELF\x02\x01\x01\x00 %s \x00\x00' "${ALLOWED_ORIGINS[0]}" \
    > "$dir/stage/lib/arm64-v8a/libapp.so"
  pack "$dir/stage" "$dir/fake.apk"
  rc=0
  ( fail=0; scan_artifact "$dir/fake.apk"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$rc" -ne 0 ]; then
    bad "self-test: the scan rejected a clean artifact"
  else
    note "ok: self-test — a clean artifact passes"
  fi
  return 0
}

if [ "${1:-}" = "--self-test" ]; then
  self_test
else
  check_config
  for artifact in "$@"; do
    scan_artifact "$artifact"
  done
fi

if [ "$fail" -ne 0 ]; then
  note ""
  note "Store-build hygiene failed. A public build carries no server address:"
  note "the invite QR supplies one (LYCM-102/103). See mobile/lyceum/README.md."
  exit 1
fi
note "store-build hygiene: clean"
