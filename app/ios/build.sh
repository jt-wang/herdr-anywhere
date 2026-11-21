#!/bin/sh
# Builds the iOS app from its own host project (project.yml): the TSX app compiled into nsh/, Herdr's sounds beside it,
# TailPocket's Swift package linked, with its library (scripts/tailpocket.sh) and NSH's NativeShell.xcframework.
# Usage: ios/build.sh [entry.tsx] [simulator|device|prepare]; prepare stops once the project is generated.
# NSH_IOS_APP_ID gives a test fixture its own bundle id; otherwise the project's signing configuration supplies it.
# A device build signs with your team, from ios/Signing.local.xcconfig (ios/README.md). DEVICE=<id> registers that phone.
# Writes ios/build/Herdr.app. Needs nsh, go, xcodegen and Xcode.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
ios="$root/ios"
entry=${1:-App.tsx}
sdk=${2:-simulator}
# NativeShell is nsh v0.3.0 with ios/nsh-patches' upstream fixes, built by ios/nsh-patches/build-nsh-ios.sh into
# $NSH_IOS_EMBED (default: this Mac's cache). Its nsh-build.txt must name those patches, so no app is built on a
# NativeShell without them.
embed=${NSH_IOS_EMBED:-$HOME/Library/Caches/herdr-anywhere/nsh-ios}
want=$("$ios/nsh-patches/key.sh")
[ "$(cat "$embed/nsh-build.txt" 2>/dev/null)" = "$want" ] || {
  echo "ios/build.sh: no NativeShell built as '$want' in $embed; run app/ios/nsh-patches/build-nsh-ios.sh <nsh source> $embed" >&2; exit 1; }
"$root/scripts/tailpocket.sh" apple >/dev/null
# TailPocket's NSH package takes NativeShell from beside its Package.swift (left out of the repository).
nshpkg="$root/../third_party/tailpocket-32c50f3/sdk/apple/NSH"
rm -rf "$nshpkg/NativeShell.xcframework"
cp -R "$embed/NativeShell.xcframework" "$nshpkg/"
rm -rf "$ios/nsh"
(cd "$root" && nsh build "$entry" --release --out "$ios/nsh" >/dev/null)
if [ -n "${HM_TEST_BUNDLE_EVIDENCE:-}" ] && [ "$entry" = ios-e2e.tsx ] && [ "$sdk" = simulator ]; then
  python3 "$root/e2e/ios-subscriber-source.py" capture "$ios/nsh" "$HM_TEST_BUNDLE_EVIDENCE"
fi
rm -f "$ios/nsh/bundle.js" "$ios/nsh/bundle.js.map" # the app runs the bytecode
# A TSX bundle can't import an .mp3, so nsh build doesn't copy the sounds; they go where it puts imported files.
mkdir -p "$ios/nsh/assets/assets/sounds"
cp "$root"/assets/sounds/*.mp3 "$ios/nsh/assets/assets/sounds/"
if [ -n "${HM_TEST_MAIN_THREAD_MARKER:-}" ] && [ "$entry" = ios-e2e.tsx ] && [ "$sdk" = simulator ]; then
  diagnostic="$HM_TEST_MAIN_THREAD_MARKER/project"
  python3 "$root/e2e/ios-background-expiry.py" prepare "$ios" "$diagnostic"
  xcodegen generate --quiet --spec "$diagnostic/project.yml" --project "$ios" --project-root "$ios"
else
  (cd "$ios" && xcodegen generate --quiet)
fi
[ "$sdk" = prepare ] && exit 0
case $sdk in
  simulator) dest='generic/platform=iOS Simulator'; products=Release-iphonesimulator ;;
  device)
    if ! grep -qE '^ *DEVELOPMENT_TEAM *= *[A-Z0-9]+' "$ios/Signing.local.xcconfig" 2>/dev/null; then
      echo "a device build needs your Apple team: set DEVELOPMENT_TEAM and BUNDLE_ID_PREFIX in ios/Signing.local.xcconfig (ios/README.md)" >&2
      exit 2
    fi
    dest=${DEVICE:+id=$DEVICE}; dest=${dest:-generic/platform=iOS}; products=Release-iphoneos
    signing='-allowProvisioningUpdates -allowProvisioningDeviceRegistration' ;;
  *) echo "unknown target $sdk" >&2; exit 2 ;;
esac
# arm64 only, for the packages too: NSH ships no x86_64 simulator slice (project.yml's EXCLUDED_ARCHS covers the app).
set --
[ -z "${NSH_IOS_APP_ID:-}" ] || set -- "PRODUCT_BUNDLE_IDENTIFIER=$NSH_IOS_APP_ID"
xcodebuild -quiet -project "$ios/Herdr.xcodeproj" -scheme Herdr -configuration Release -destination "$dest" ARCHS=arm64 \
  -derivedDataPath "$ios/build/derived" -clonedSourcePackagesDirPath "$ios/build/spm" -packageCachePath "$ios/build/spm-cache" ${signing:-} "$@" build
rm -rf "$ios/build/Herdr.app"
cp -R "$ios/build/derived/Build/Products/$products/Herdr.app" "$ios/build/Herdr.app"
echo "→ ios/build/Herdr.app"
