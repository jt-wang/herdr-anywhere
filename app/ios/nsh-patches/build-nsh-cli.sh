#!/bin/sh
# The test CLI from the same base, patches and toolchain as NativeShell.
# Usage: build-nsh-cli.sh <dedicated source> <out>, or --verify <out>.
# Source and Cargo output belong to this build; no reset/clean or global install.
set -eu
here=$(cd "$(dirname "$0")" && pwd -P)
base=76fa8a7fed47ea51aea01fa85bb40960be15d984
for n in 1 2 3 4 5 6 7 8 9; do
  (set -- "$here"/000$n-*.patch; [ "$#" -eq 1 ] && [ -f "$1" ]) || { echo "expected all nine nsh patches" >&2; exit 1; }
done
key=$("$here/key.sh")
verify() {
  [ "$(cat "$1/nsh-build.txt")" = "$key" ] || { echo "nsh CLI key does not match these patches" >&2; return 1; }
  [ -x "$1/bin/nsh" ] || { echo "nsh CLI executable is missing" >&2; return 1; }
  files=$(awk 'NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-f]/ { exit 1 } { print $2 }' "$1/SHA256SUMS") || return 1
  [ "$(printf '%s\n' "$files" | LC_ALL=C sort)" = "$(printf '%s\n' bin/nsh nsh-build.txt)" ] || {
    echo "nsh CLI checksum manifest must cover exactly the binary and build key" >&2; return 1
  }
  (cd "$1" && shasum -a 256 -c SHA256SUMS)
}
if [ "${1:-}" = --verify ]; then verify "${2:?out folder}"; exit; fi
src=${1:?dedicated nsh source}; out=${2:?out folder}
[ ! -e "$out/nsh-build.txt" ] || { echo "output already exists; use --verify to check a cache" >&2; exit 1; }
[ -e "$src/.git" ] || git clone -q --depth 1 --branch v0.3.0 https://github.com/longbridge/nsh "$src"
src=$(cd "$src" && pwd -P)
[ "$(git -C "$src" rev-parse HEAD)" = "$base" ] || { echo "nsh source is not the pinned v0.3.0 base" >&2; exit 1; }
[ -z "$(git -C "$src" status --porcelain)" ] || { echo "nsh source has changes; use a fresh dedicated source" >&2; exit 1; }
for p in "$here"/0*.patch; do git -C "$src" apply "$p"; done
export CARGO_HOME=${CARGO_HOME:-$src/.cargo-cache}
export CARGO_TARGET_DIR="$src/target"
sep=$(printf '\037')
export CARGO_ENCODED_RUSTFLAGS="--remap-path-prefix=$src=/nsh${sep}--remap-path-prefix=$CARGO_HOME=/cargo${sep}--remap-path-prefix=$CARGO_TARGET_DIR=/target"
export CC_SHELL_ESCAPED_FLAGS=1
export CFLAGS="\"-ffile-prefix-map=$src=/nsh\" \"-ffile-prefix-map=$CARGO_HOME=/cargo\" \"-ffile-prefix-map=$CARGO_TARGET_DIR=/target\""
(cd "$src" && rustup run 1.99.1 cargo build --release -p nsh-cli --locked)
binary="$CARGO_TARGET_DIR/release/nsh"
if strings "$binary" | grep -F -e "$src" -e "$CARGO_HOME" -e "$CARGO_TARGET_DIR" -e "$HOME" >/dev/null; then
  echo "nsh CLI still records build paths" >&2; exit 1
fi
mkdir -p "$out/bin"
cp "$binary" "$out/bin/nsh"
printf '%s\n' "$key" > "$out/nsh-build.txt"
(cd "$out" && shasum -a 256 bin/nsh nsh-build.txt > SHA256SUMS)
verify "$out"
