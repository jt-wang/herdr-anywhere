#!/bin/sh
# Puts TailPocket's phone library, as its public release v0.3.0 (jt-wang/tailpocket@e1aea0fafe2341985b8a5cbd98f39d9949b0e544)
# ships it, into the copy of its SDKs in third_party, where its Swift package and Gradle modules look for it:
#   apple   → build/apple/TailPocketMobile.xcframework (ios/build.sh)
#   android → build/android/m2, a local Maven repository holding tailpocket-mobile 0.3.0 (android/build.sh)
# Each file is checked against the SHA-256 below, from the release's SHA256SUMS, before it is unpacked.
# A target already in place is kept; FORCE=1 fetches it again. Usage: scripts/tailpocket.sh [apple] [android]
# Needs curl, unzip and shasum or sha256sum.
set -eu
release=https://github.com/jt-wang/tailpocket/releases/download/v0.3.0
tp=$(cd "$(dirname "$0")/../../third_party/tailpocket-e1aea0f" && pwd -P)
mkdir -p "$tp/build/download"
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }
fetch() { # file, sha256
  f="$tp/build/download/$1"
  [ -f "$f" ] && [ "$(sha256 "$f")" = "$2" ] && return 0
  curl -fsSL --retry 5 --retry-delay 5 -o "$f.part" "$release/$1"
  got=$(sha256 "$f.part")
  [ "$got" = "$2" ] || { rm -f "$f.part"; echo "tailpocket.sh: $1 has SHA-256 $got, want $2" >&2; exit 1; }
  mv "$f.part" "$f"
}
for target in ${*:-apple android}; do
  case "$target" in
  apple)
    out="$tp/build/apple/TailPocketMobile.xcframework"
    [ -z "${FORCE:-}" ] && [ -d "$out" ] && continue
    fetch TailPocketMobile.xcframework.zip 6e9140f0cbee5fbf83f392b2b8e08ce542398424bd410dbc507c0ac641bd604c
    rm -rf "$out"; mkdir -p "$tp/build/apple"
    unzip -q "$tp/build/download/TailPocketMobile.xcframework.zip" -d "$tp/build/apple"
    [ -d "$out" ] || { echo "tailpocket.sh: no TailPocketMobile.xcframework in the release's zip" >&2; exit 1; }
    ;;
  android)
    aar="$tp/build/android/m2/io/jingtao/tailpocket/tailpocket-mobile/0.3.0/tailpocket-mobile-0.3.0.aar"
    [ -z "${FORCE:-}" ] && [ -f "$aar" ] && continue
    fetch tailpocket-android-m2.zip 9348d35617330bb8b144b5ef151dfbbbf61814b07db946ac9d70bf2fb04a69b5
    rm -rf "$tp/build/android/m2"; mkdir -p "$tp/build/android"
    unzip -q "$tp/build/download/tailpocket-android-m2.zip" -d "$tp/build/android"
    [ -f "$aar" ] || { echo "tailpocket.sh: no tailpocket-mobile 0.3.0 in the release's zip" >&2; exit 1; }
    ;;
  *) echo "unknown target $target" >&2; exit 2 ;;
  esac
  echo "put TailPocket's library in place for $target"
done
