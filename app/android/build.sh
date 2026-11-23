#!/bin/sh
# Builds android/build/Herdr.apk: the app's bundle (nsh build) in its own MainActivity on the release nsh.aar with our
# fixes (nsh-patches), with the network security config that lets the phone talk plain http/ws to the companion, and
# TailPocket's modules from third_party with its phone library (scripts/tailpocket.sh). Needs NSH, Go, a JDK, the SDK
# and the NDK.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
export ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
export JAVA_HOME="${JAVA_HOME:-/Applications/Android Studio.app/Contents/jbr/Contents/Home}"
app="$root/android/app"
# Shimmer adds fields to the native batch protocol: Java AND Rust must match the CLI.
# The installed v0.3.0 AAR is not compatible. Keep the rebuilt runtime separate from it.
runtime=${NSH_HOME:-${XDG_CACHE_HOME:-$HOME/.cache}/herdr-nsh/$("$root/ios/nsh-patches/key.sh" | tr ' ' '-')/runtime}
if [ -n "${NSH_HOME:-}" ]; then
  sh "$root/scripts/nsh-runtime.sh" --verify-android "$runtime" || {
    echo 'NSH_HOME needs a matched full runtime; see ios/nsh-patches/README.md' >&2; exit 1;
  }
else
  sh "$root/scripts/nsh-runtime.sh" android "$(dirname "$runtime")/source" "$runtime"
fi
export NSH_HOME="$runtime"
export PATH="$runtime/cli/bin:$PATH"
"$root/scripts/tailpocket.sh" android >/dev/null
# The release nsh.aar with our patches to its Java runtime (nsh-patches/patch-aar.sh).
mkdir -p "$app/libs"
"$root/android/nsh-patches/patch-aar.sh" "${NSH_HOME:-$HOME/.nsh}/embed/android/nsh.aar" "$app/libs/nsh.aar"
export NSH_AAR="$app/libs/nsh.aar" # TailPocket's tailpocket-nsh module compiles against it; it looks in ~/.nsh otherwise
rm -rf "$app/src/main/assets/nsh"
nsh build "$root" --release --out "$app/src/main/assets/nsh"
rm -f "$app/src/main/assets/nsh/bundle.js" "$app/src/main/assets/nsh/bundle.js.map" # the device runs the bytecode
# Herdr's alert sounds (src/ui/root.tsx plays asset:/assets/sounds/*.mp3): a TSX bundle can't import an .mp3, so
# nsh build doesn't copy them; they go where it puts imported files.
mkdir -p "$app/src/main/assets/nsh/assets/assets/sounds"
cp "$root"/assets/sounds/*.mp3 "$app/src/main/assets/nsh/assets/assets/sounds/"
# The same two as the notification channels' sounds (MainActivity: android.resource://…/raw/request and raw/done).
mkdir -p "$app/src/main/res/raw"
cp "$root/assets/sounds/request.mp3" "$root/assets/sounds/done.mp3" "$app/src/main/res/raw/"
(cd "$root/android" && ./gradlew --quiet assembleRelease)
mkdir -p "$root/android/build"; cp "$app/build/outputs/apk/release/app-release.apk" "$root/android/build/Herdr.apk"
echo "→ android/build/Herdr.apk ($(du -h "$root/android/build/Herdr.apk" | cut -f1 | tr -d ' '))"
