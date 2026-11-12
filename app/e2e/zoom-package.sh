#!/bin/sh
# Packages e2e/picture-app.tsx (the made-up computer of fixtures/fake.tsx with the drawn sunset open) under its own id,
# for android-zoom.sh and ios-zoom.sh: as an APK (android) or a simulator .app (ios-simulator), into FOLDER.
# Usage: e2e/zoom-package.sh android|ios-simulator FOLDER. Needs nsh; for android a JDK (JAVA_HOME, or Android Studio's).
set -eu
target=$1 dir=$2
app=$(cd "$(dirname "$0")/.." && pwd)
[ -n "${JAVA_HOME:-}" ] || [ ! -d "/Applications/Android Studio.app" ] || export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
# An entry at the app's root, so its fonts and icons are packed (as site-src/shots.sh does).
printf "// Written by e2e/zoom-package.sh.\nimport './e2e/picture-app.tsx'\n" > "$app/.zoom-check.tsx"
trap 'rm -f "$app/.zoom-check.tsx"' EXIT
(cd "$app" && nsh package --target "$target" .zoom-check.tsx --id io.jingtao.herdranywhere.zoomcheck --name "Herdr zoom check" -o "$dir" >/dev/null)
ls "$dir" | grep -E '\.(apk|app)$' | sed "s|^|$dir/|"
