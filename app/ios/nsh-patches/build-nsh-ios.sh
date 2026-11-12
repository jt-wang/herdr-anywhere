#!/bin/sh
# NativeShell.xcframework for the iOS app: nsh v0.3.0 with the upstream patches beside this script (0001 #530, 0002
# #1014; each file says where it comes from), built with Xcode on a Mac by nsh's own `cargo xtask ios-embed`.
# ios/build.sh takes the framework from $NSH_IOS_EMBED and checks its nsh-build.txt against these patches.
# NSH_PATCHES="0001" applies only those (by number); all when unset.
#
# Usage: app/ios/nsh-patches/build-nsh-ios.sh <nsh source> <out>
#   <nsh source>  a clone of https://github.com/longbridge/nsh (cloned there when missing); it is reset to v0.3.0
#   <out>         gets NativeShell.xcframework and nsh-build.txt
# CARGO_TARGET_DIR is where Rust builds (default: a new temporary folder, removed after). RUSTUP_TOOLCHAIN names the
# rustup toolchain (default: the version key.sh names; e.g. stable on a Mac where stable is that version); its rustc
# must be that version.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
src=${1:?nsh source}; out=${2:?out folder}
base=76fa8a7fed47ea51aea01fa85bb40960be15d984 # v0.3.0
# These three paths go into compiler flags (below) that cargo and cc split at spaces: a path with a space breaks the
# build half way (rustc: "must contain '='"), so it is refused before anything starts.
for v in "src=$src" "CARGO_HOME=${CARGO_HOME:-$HOME/.cargo}" "CARGO_TARGET_DIR=${CARGO_TARGET_DIR:-}"; do
  case ${v#*=} in *" "*)
    echo "${v%%=*} has a space (${v#*=}): the flags that keep this computer's paths out of the framework are split at spaces; use a folder without spaces" >&2
    exit 1 ;; esac
done
key=$("$here/key.sh"); rust=$(printf %s "$key" | awk '{print $4}')
# A toolchain that isn't installed is an error here, never installed by rustup on the way.
export RUSTUP_TOOLCHAIN="${RUSTUP_TOOLCHAIN:-$rust}" RUSTUP_AUTO_INSTALL=0
got=$(rustc --version | awk '{print $2}')
[ "$got" = "$rust" ] || { echo "rustc of toolchain $RUSTUP_TOOLCHAIN is $got, not $rust" >&2; exit 1; }
[ -d "$src/.git" ] || git clone -q https://github.com/longbridge/nsh "$src"
git -C "$src" cat-file -e "$base^{commit}" 2>/dev/null || git -C "$src" fetch -q origin tag v0.3.0
[ "$(git -C "$src" rev-parse 'v0.3.0^{commit}')" = "$base" ] || { echo "v0.3.0 is not $base" >&2; exit 1; }
git -C "$src" checkout -q --detach "$base"; git -C "$src" reset -q --hard; git -C "$src" clean -qfdx
for p in $("$here/key.sh" --files); do git -C "$src" apply "$p"; done
tmp=
if [ -z "${CARGO_TARGET_DIR:-}" ]; then tmp=$(mktemp -d); export CARGO_TARGET_DIR="$tmp"; fi
trap '[ -z "$tmp" ] || rm -rf "$tmp"' EXIT
# nsh's xtask links the Rust library from <source>/target whatever CARGO_TARGET_DIR says: that path leads to it.
rm -rf "$src/target"; ln -s "$CARGO_TARGET_DIR" "$src/target"
# No path of this computer goes into the framework (and from it into the app): the sources, cargo's registry and the
# build folder are renamed in what the compilers record. The framework is checked for any that are left.
cargo_home=${CARGO_HOME:-$HOME/.cargo}
remap="--remap-path-prefix=$src=/nsh --remap-path-prefix=$cargo_home=/cargo --remap-path-prefix=$CARGO_TARGET_DIR=/target"
export CARGO_TARGET_AARCH64_APPLE_IOS_RUSTFLAGS="$remap" CARGO_TARGET_AARCH64_APPLE_IOS_SIM_RUSTFLAGS="$remap"
export CFLAGS="-ffile-prefix-map=$src=/nsh -ffile-prefix-map=$cargo_home=/cargo -ffile-prefix-map=$CARGO_TARGET_DIR=/target"
(cd "$src" && cargo xtask ios-embed --no-catalyst --no-dev)
# The linker records the library's path (under <source>/target) in each binary's debug map: debug symbols go.
find "$src/dist/nsh/embed/ios/NativeShell.xcframework" -type f -name NativeShell -exec xcrun strip -S {} \;
left=$(find "$src/dist/nsh/embed/ios/NativeShell.xcframework" -type f -exec strings {} + | grep -E "$src|$cargo_home|$CARGO_TARGET_DIR|$HOME" | head -3)
[ -z "$left" ] || { echo "the framework still names paths of this computer:" >&2; printf '%s\n' "$left" | cut -c1-120 >&2; exit 1; }
rm -rf "$out/NativeShell.xcframework"; mkdir -p "$out"
cp -R "$src/dist/nsh/embed/ios/NativeShell.xcframework" "$out/"
printf '%s\n' "$key" > "$out/nsh-build.txt"
echo "built $out/NativeShell.xcframework ($key)"
