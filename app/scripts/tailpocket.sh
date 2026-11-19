#!/bin/sh
# Builds TailPocket's phone library (its connectmobile package, with gomobile) from the copy of its commit 81f6f8b (v0.3.0-pre.3) in
# third_party, into that copy's build/, where its Swift package and Gradle modules look for it:
#   apple   → build/apple/TailPocketMobile.xcframework (ios/build.sh)
#   android → build/android/m2, a local Maven repository holding tailpocket-mobile 0.3.0-pre.3 (android/build.sh)
# A target already built is kept; FORCE=1 builds it again. Usage: scripts/tailpocket.sh [apple] [android]
# Needs Go; apple needs Xcode, android the Android SDK and NDK.
set -eu
tp=$(cd "$(dirname "$0")/../../third_party/tailpocket-81f6f8b" && pwd -P)
cd "$tp"
mkdir -p build/bin build/apple build/android
go build -o build/bin/gomobile golang.org/x/mobile/cmd/gomobile
go build -o build/bin/gobind golang.org/x/mobile/cmd/gobind
PATH="$tp/build/bin:$PATH"
# gomobile records the module's directory in the library (-trimpath does not remove it), so it builds through a
# fixed path that names no user or machine, as TailPocket's scripts/build-mobile.sh does.
src=/tmp/herdr-tailpocket-src
ln -sfn "$tp" "$src"
trap 'rm -f "$src"' EXIT
cd "$src"
for target in ${*:-apple android}; do
  case "$target" in
  apple)
    out=build/apple/TailPocketMobile.xcframework
    [ -z "${FORCE:-}" ] && [ -d "$out" ] && continue
    rm -rf "$out"
    gomobile bind -target=ios,iossimulator -iosversion=15 -trimpath -ldflags="-s -w" -tags ts_omit_ssh -o "$out" ./connectmobile
    ;;
  android)
    m2=build/android/m2/io/jingtao/tailpocket/tailpocket-mobile/0.3.0-pre.3
    [ -z "${FORCE:-}" ] && [ -f "$m2/tailpocket-mobile-0.3.0-pre.3.aar" ] && continue
    export ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
    export JAVA_HOME="${JAVA_HOME:-/Applications/Android Studio.app/Contents/jbr/Contents/Home}" # gomobile runs javac
    PATH="$JAVA_HOME/bin:$PATH"
    gomobile bind -target=android -androidapi 26 -javapkg io.jingtao.tailpocket -trimpath -ldflags="-s -w" \
      -tags ts_omit_ssh -o build/android/tailpocket-mobile.aar ./connectmobile
    mkdir -p "$m2"
    cp build/android/tailpocket-mobile.aar "$m2/tailpocket-mobile-0.3.0-pre.3.aar"
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
      '<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>' \
      '<groupId>io.jingtao.tailpocket</groupId><artifactId>tailpocket-mobile</artifactId><version>0.3.0-pre.3</version>' \
      '<packaging>aar</packaging></project>' > "$m2/tailpocket-mobile-0.3.0-pre.3.pom"
    ;;
  *) echo "unknown target $target" >&2; exit 2 ;;
  esac
  echo "built TailPocket for $target"
done
