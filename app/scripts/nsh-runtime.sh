#!/bin/sh
# Matched CLI + full web/Android runtime for the vendored v0.3.0 patch stack.
# Usage: nsh-runtime.sh web|android <dedicated source> <out>
#        nsh-runtime.sh --verify-web|--verify-android <out>
#        nsh-runtime.sh --prepare-android-package <out>  # derive from an already verified AAR
# Source belongs to this build; never pass a developer checkout or ~/.nsh.
set -eu
here=$(cd "$(dirname "$0")/../ios/nsh-patches" && pwd -P)
key=$("$here/key.sh")
toolchain=$(printf '%s\n' "$key" | awk '{print $4}')
base=76fa8a7fed47ea51aea01fa85bb40960be15d984
web_stack=8388608
folder() { case "$1" in web) echo web ;; android) echo embed/android ;; *) exit 2 ;; esac; }
android_recipe() {
  cat "$here/../../android/nsh-patches"/0*.patch "$here/../../android/nsh-patches/patch-aar.sh" | shasum -a 256 | awk '{print $1}'
}
verify() {
  "$here/build-nsh-cli.sh" --verify "$2/cli"
  dir="$2/$(folder "$1")"
  [ "$(cat "$dir/nsh-build.txt")" = "$key" ] || { echo 'runtime patch key mismatch' >&2; return 1; }
  case "$1" in
    web)
      [ "$(cat "$dir/nsh-web-build.txt")" = "stack-size=$web_stack" ] || { echo 'web runtime stack recipe mismatch; rebuild with nsh-runtime.sh' >&2; return 1; }
      expected=$(printf '%s\n' nsh.wasm nsh-dev.wasm nsh.js nsh-web-build.txt nsh-build.txt) ;;
    android) expected=$(printf '%s\n' nsh.aar nsh-build.txt) ;;
  esac
  [ "$(awk '{print $2}' "$dir/SHA256SUMS")" = "$expected" ] || { echo 'incomplete runtime checksums' >&2; return 1; }
  (cd "$dir" && shasum -a 256 -c SHA256SUMS)
  if [ "$1" = android ] && [ "${3:-all}" != aar ]; then
    standalone="$2/android/release"
    [ "$(cat "$standalone/nsh-build.txt")" = "$key" ] || { echo 'standalone Android patch key mismatch' >&2; return 1; }
    [ "$(cat "$standalone/nsh-android-build.txt")" = "$(android_recipe)" ] || { echo 'standalone Android Java recipe mismatch' >&2; return 1; }
    expected=$(printf '%s\n' classes.dex lib/arm64-v8a/libnsh.so lib/armeabi-v7a/libnsh.so lib/x86_64/libnsh.so nsh-android-build.txt nsh-build.txt)
    [ "$(awk '{print $2}' "$standalone/SHA256SUMS")" = "$expected" ] || { echo 'incomplete standalone Android checksums' >&2; return 1; }
    (cd "$standalone" && shasum -a 256 -c SHA256SUMS)
  fi
}
prepare_android_package() {
  # Reuse only the exact checked CLI/AAR. Recompile its Java with the same pooled-builder fix as the real app.
  verify android "$1" aar
  package_root=$(cd "$1" && pwd -P)
  standalone="$package_root/android/release"
  mkdir -p "$standalone"
  # A failed compiler/dependency step must leave no completion manifest blocking a later retry.
  rm -f "$standalone/SHA256SUMS"
  dexwork=$(mktemp -d)
  trap 'rm -f "${index:-}"; rm -rf "$dexwork"' EXIT
  "$here/../../android/nsh-patches/patch-aar.sh" "$package_root/embed/android/nsh.aar" "$dexwork/nsh.aar"
  python3 -I - "$dexwork/nsh.aar" "$dexwork" "$standalone" <<'PY'
import sys, zipfile
from pathlib import Path
aar, work, out = map(Path, sys.argv[1:])
with zipfile.ZipFile(aar) as z:
    (work / 'classes.jar').write_bytes(z.read('classes.jar'))
    for abi in ('arm64-v8a', 'armeabi-v7a', 'x86_64'):
        target = out / 'lib' / abi / 'libnsh.so'
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(z.read(f'jni/{abi}/libnsh.so'))
PY
  "$ANDROID_HOME/build-tools/35.0.0/d8" --release --min-api 28 --lib "$ANDROID_HOME/platforms/android-35/android.jar" --output "$standalone" "$dexwork/classes.jar"
  printf '%s\n' "$key" > "$standalone/nsh-build.txt"
  android_recipe > "$standalone/nsh-android-build.txt"
  (cd "$standalone" && shasum -a 256 classes.dex lib/arm64-v8a/libnsh.so lib/armeabi-v7a/libnsh.so lib/x86_64/libnsh.so nsh-android-build.txt nsh-build.txt > SHA256SUMS.tmp && mv SHA256SUMS.tmp SHA256SUMS)
  rm -rf "$dexwork"
}
case "${1:-}" in
  --verify-web) verify web "${2:?runtime output}"; exit ;;
  --verify-android) verify android "${2:?runtime output}"; exit ;;
  --prepare-android-package) prepare_android_package "${2:?runtime output}"; verify android "$2"; exit ;;
esac
kind=${1:?web or android}; sub=$(folder "$kind")
src=${2:?dedicated source}; out=${3:?runtime output}
if [ -e "$out/$sub/nsh-build.txt" ]; then
  if [ "$kind" = android ] && [ ! -f "$out/android/release/SHA256SUMS" ]; then prepare_android_package "$out"; fi
  verify "$kind" "$out"; exit
fi
mkdir -p "$(dirname "$src")" "$out"
out=$(cd "$out" && pwd -P)
if [ ! -e "$out/cli/nsh-build.txt" ]; then
  if [ -n "${NSH_CLI_BUILD:-}" ]; then
    "$here/build-nsh-cli.sh" --verify "$NSH_CLI_BUILD"
    mkdir -p "$out/cli/bin"
    cp "$NSH_CLI_BUILD/bin/nsh" "$out/cli/bin/nsh"
    cp "$NSH_CLI_BUILD/nsh-build.txt" "$NSH_CLI_BUILD/SHA256SUMS" "$out/cli/"
  else
    rustup toolchain install "$toolchain" --profile minimal
    "$here/build-nsh-cli.sh" "$src" "$out/cli"
  fi
fi
"$here/build-nsh-cli.sh" --verify "$out/cli"
src=$(cd "$src" && pwd -P)
[ "$(git -C "$src" rev-parse HEAD)" = "$base" ] || { echo 'wrong runtime source base' >&2; exit 1; }
# Compare all source contents to the exact base + patch stack in a private index.
# Do not reset/clean the source or modify its real index. Build caches are not code.
index=$(mktemp); rm "$index"
trap 'rm -f "$index"' EXIT
export GIT_INDEX_FILE="$index"
git -C "$src" read-tree "$base"
for p in "$here"/0*.patch; do git -C "$src" apply --cached "$p"; done
git -C "$src" update-index -q --refresh
git -C "$src" diff-files --quiet --ignore-submodules
[ -z "$(git -C "$src" ls-files --others --exclude-standard -- ':(exclude,top).cargo-cache')" ] || { echo 'unexpected runtime source files' >&2; exit 1; }
unset GIT_INDEX_FILE
export CARGO_HOME=${CARGO_HOME:-$src/.cargo-cache} CARGO_TARGET_DIR="$src/target"
export CARGO_BUILD_JOBS=${CARGO_BUILD_JOBS:-4}
sep=$(printf '\037')
export CARGO_ENCODED_RUSTFLAGS="--remap-path-prefix=$src=/nsh${sep}--remap-path-prefix=$CARGO_HOME=/cargo${sep}--remap-path-prefix=$CARGO_TARGET_DIR=/target"
export CC_SHELL_ESCAPED_FLAGS=1
export CFLAGS="\"-ffile-prefix-map=$src=/nsh\" \"-ffile-prefix-map=$CARGO_HOME=/cargo\" \"-ffile-prefix-map=$CARGO_TARGET_DIR=/target\""
dest="$out/$sub"
mkdir -p "$dest"
case "$kind" in
  web)
    rustup target add --toolchain "$toolchain" wasm32-unknown-unknown
    export AR_wasm32_unknown_unknown=${AR_wasm32_unknown_unknown:-llvm-ar}
    # Encoded remap flags override .cargo/config.toml: retain its 8 MiB stack.
    # The wasm default 1 MiB overflows while constructing the real app tree.
    export CARGO_ENCODED_RUSTFLAGS="${CARGO_ENCODED_RUSTFLAGS}${sep}-C${sep}link-arg=-zstack-size=$web_stack${sep}--cfg${sep}getrandom_backend=\"custom\""
    # Same flags/profile as v0.3.0 runtime.rs. Keep both published and dev hosts matched.
    for mode in bytecode-only dev; do
      (cd "$src" && rustup run "$toolchain" cargo build --locked --profile web -p nsh-platform-web --features "$mode" --target wasm32-unknown-unknown)
      name=nsh.wasm; [ "$mode" != dev ] || name=nsh-dev.wasm
      cp "$src/target/wasm32-unknown-unknown/web/nsh_platform_web.wasm" "$dest/$name"
    done
    cp "$src/crates/nsh-platform-web/web/nsh.js" "$dest/nsh.js"
    printf 'stack-size=%s\n' "$web_stack" > "$dest/nsh-web-build.txt"
    files='nsh.wasm nsh-dev.wasm nsh.js nsh-web-build.txt'
    ;;
  android)
    export ANDROID_NDK_HOME=${ANDROID_NDK_HOME:-${ANDROID_HOME:?Android SDK}/ndk/27.2.12479018}
    [ -d "$ANDROID_NDK_HOME" ] || { echo 'install ndk;27.2.12479018 with sdkmanager' >&2; exit 1; }
    rustup target add --toolchain "$toolchain" aarch64-linux-android armv7-linux-androideabi x86_64-linux-android
    (cd "$src" && rustup run "$toolchain" cargo xtask android --release)
    python3 -I - "$src/target/android/release" "$dest/nsh.aar" <<'PY'
import sys, zipfile
from pathlib import Path
built, target = map(Path, sys.argv[1:])
manifest = b'''<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.longbridge.nsh">
  <uses-sdk android:minSdkVersion="28" />
  <uses-permission android:name="android.permission.INTERNET" />
</manifest>
'''
def entry(z, name, data):
    info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
    info.compress_type = zipfile.ZIP_DEFLATED
    z.writestr(info, data)
with zipfile.ZipFile(str(target) + '.tmp', 'w') as z:
    entry(z, 'AndroidManifest.xml', manifest)
    entry(z, 'R.txt', b'')
    entry(z, 'classes.jar', (built / 'classes.jar').read_bytes())
    for abi in ('arm64-v8a', 'armeabi-v7a', 'x86_64'):
        entry(z, f'jni/{abi}/libnsh.so', (built / 'lib' / abi / 'libnsh.so').read_bytes())
Path(str(target) + '.tmp').replace(target)
PY
    files=nsh.aar
    ;;
esac
# Publish metadata only after every file exists. Never relabel old binaries.
printf '%s\n' "$key" > "$dest/nsh-build.txt"
(cd "$dest" && shasum -a 256 $files nsh-build.txt > SHA256SUMS)
[ "$kind" != android ] || prepare_android_package "$out"
verify "$kind" "$out"
