#!/bin/sh
# What NativeShell is built from: nsh v0.3.0, the toolchain and the patches' sha256. build-nsh-ios.sh writes it
# beside the framework, ios/build.sh checks it, and CI caches the framework under it.
# NSH_PATCHES picks patches by number (e.g. "0001"); all of them when unset, as ios/build.sh wants.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
patches() {
  if [ -z "${NSH_PATCHES:-}" ]; then ls "$here"/0*.patch; return; fi
  for n in $NSH_PATCHES; do f=$(ls "$here/$n"-*.patch 2>/dev/null) || { echo "no patch $n in $here" >&2; return 1; }; echo "$f"; done
}
files=$(patches)
[ "${1:-}" = --files ] && { printf '%s\n' "$files"; exit 0; }
# The patches by number only when some were picked: the key of all of them stays what CI caches under.
printf 'nsh v0.3.0 rust 1.99.1 patches %s%s\n' "${NSH_PATCHES:+$(printf %s "$NSH_PATCHES" | tr ' ' +) }" \
  "$(printf '%s\n' "$files" | xargs cat | shasum -a 256 | cut -c1-16)"
