#!/bin/sh
# Packages e2e/picture-app.tsx (the made-up computer of fixtures/fake.tsx with the drawn sunset open) under its own id,
# for android-zoom.sh and ios-zoom.sh: as an APK (android) or a simulator .app (ios-simulator), into FOLDER. With
# CHAT=1 the sunset is in a chat instead (e2e/chat-picture-params.ts), to be opened from there.
# Usage: [CHAT=1] e2e/zoom-package.sh android|ios-simulator FOLDER. Needs nsh; for Android a JDK (JAVA_HOME, or Android
# Studio's); for iOS the app's build tools and patched NativeShell (ios/build.sh).
set -eu
target=$1 dir=$2
app=$(cd "$(dirname "$0")/.." && pwd)
if [ "$target" = android ]; then
  runtime=${NSH_RUNTIME:-${NSH_HOME:-}}
  [ -n "$runtime" ] || { echo 'Set NSH_RUNTIME to a matched runtime built by app/scripts/nsh-runtime.sh android.' >&2; exit 1; }
  sh "$app/scripts/nsh-runtime.sh" --verify-android "$runtime" >&2
  export NSH_RUNTIME="$runtime" PATH="$runtime/cli/bin:$PATH"
fi
[ -n "${JAVA_HOME:-}" ] || [ ! -d "/Applications/Android Studio.app" ] || export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
# An entry at the app's root, so its fonts and icons are packed (as site-src/shots.sh does).
params=picture-params; [ "${CHAT:-}" != 1 ] || params=chat-picture-params
printf "// Written by e2e/zoom-package.sh.\nimport './e2e/%s.ts'\nimport './e2e/picture-app.tsx'\n" "$params" > "$app/.zoom-check.tsx"
trap 'rm -f "$app/.zoom-check.tsx"' EXIT
if [ "$target" = ios-simulator ]; then
  # nsh package copies the CLI's prebuilt runner. This fixture must exercise the app's patched iOS engine too.
  NSH_IOS_APP_ID=io.jingtao.herdranywhere.zoomcheck "$app/ios/build.sh" .zoom-check.tsx simulator >/dev/null
  mkdir -p "$dir"
  rm -rf "$dir/Herdr zoom check.app"
  cp -R "$app/ios/build/Herdr.app" "$dir/Herdr zoom check.app"
  printf '%s\n' "$dir/Herdr zoom check.app"
else
  (cd "$app" && nsh package --target "$target" .zoom-check.tsx --id io.jingtao.herdranywhere.zoomcheck --name "Herdr zoom check" -o "$dir" >/dev/null)
  ls "$dir" | grep -E '\.(apk|app)$' | sed "s|^|$dir/|"
fi
