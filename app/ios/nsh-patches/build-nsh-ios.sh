#!/bin/sh
# NativeShell.xcframework for the iOS app: nsh v0.3.0 with the upstream patches beside this script (0001 #530, 0002
# #1014, 0003 #1214, 0004 #1991, 0005 #835; each file says where it comes from), built with Xcode on a Mac by nsh's own `cargo xtask ios-embed`.
# ios/build.sh takes the framework from $NSH_IOS_EMBED and checks its nsh-build.txt against these patches.
#
# Usage: app/ios/nsh-patches/build-nsh-ios.sh <nsh source> <out>
#   <nsh source>  a clone of https://github.com/longbridge/nsh (cloned there when missing); it is reset to v0.3.0
#   <out>         gets NativeShell.xcframework and nsh-build.txt
# Rust builds in <nsh source>/target; CI keeps the source and build output in RUNNER_TEMP.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
src=${1:?nsh source}; out=${2:?out folder}
base=76fa8a7fed47ea51aea01fa85bb40960be15d984 # v0.3.0
key=$("$here/key.sh"); toolchain=$(printf %s "$key" | awk '{print $4}')
[ -d "$src/.git" ] || git clone -q https://github.com/longbridge/nsh "$src"
src=$(cd "$src" && pwd)
git -C "$src" cat-file -e "$base^{commit}" 2>/dev/null || git -C "$src" fetch -q origin tag v0.3.0
[ "$(git -C "$src" rev-parse 'v0.3.0^{commit}')" = "$base" ] || { echo "v0.3.0 is not $base" >&2; exit 1; }
git -C "$src" checkout -q --detach "$base"; git -C "$src" reset -q --hard; git -C "$src" clean -qfdx
for p in "$here"/0*.patch; do git -C "$src" apply "$p"; done
# v0.3.0's ios-embed links from the source tree's target directory. A separate Cargo target directory compiled
# successfully but failed at the linker in CI: libnsh_platform_ios.a was not found there.
export CARGO_TARGET_DIR="$src/target"
# No path of this computer goes into the framework (and from it into the app): the sources, cargo's registry and the
# build folder are renamed in what the compilers record. The framework is checked for any that are left.
cargo_home=${CARGO_HOME:-$HOME/.cargo}
remap="--remap-path-prefix=$src=/nsh --remap-path-prefix=$cargo_home=/cargo --remap-path-prefix=$CARGO_TARGET_DIR=/target"
export CARGO_TARGET_AARCH64_APPLE_IOS_RUSTFLAGS="$remap" CARGO_TARGET_AARCH64_APPLE_IOS_SIM_RUSTFLAGS="$remap"
export CFLAGS="-ffile-prefix-map=$src=/nsh -ffile-prefix-map=$cargo_home=/cargo -ffile-prefix-map=$CARGO_TARGET_DIR=/target"
(cd "$src" && rustup run "$toolchain" cargo xtask ios-embed --no-catalyst --no-dev)
left=$(find "$src/dist/nsh/embed/ios/NativeShell.xcframework" -type f -exec strings {} + | grep -E "$src|$cargo_home|$CARGO_TARGET_DIR|$HOME" | head -3)
[ -z "$left" ] || { echo "the framework still names paths of this computer:" >&2; printf '%s\n' "$left" | cut -c1-120 >&2; exit 1; }
rm -rf "$out/NativeShell.xcframework"; mkdir -p "$out"
cp -R "$src/dist/nsh/embed/ios/NativeShell.xcframework" "$out/"
printf '%s\n' "$key" > "$out/nsh-build.txt"
echo "built $out/NativeShell.xcframework ($key)"
