#!/bin/sh
# One terminal's text on an Android device: installs the APK, opens Try it (sample data on the phone, no computer),
# opens a terminal and saves a screenshot. Passes when the terminal's first line reads back from the screenshot
# (tesseract): CI's emulator under -gpu swiftshader_indirect drew it with strokes missing, and under -gpu swangle
# whole. Nothing is typed: adb's input text drops characters on CI's emulator. Also prints the WebView version
# and the GLES renderer the device draws with.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-glyphs.sh <apk> <screenshot.png>
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=$1
shot=$2
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
trap 'adb_exit $?' EXIT
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
readonly id

# The centre of the view with this resource-id; an id ending in "-" is a prefix (as in android.sh).
at() {
  case $1 in *-) q="resource-id=\"$1" ;; *) q="resource-id=\"$1\"" ;; esac
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "$q" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; "$adb" exec-out screencap -p > "${shot%.png}-fail.png"; exit 1; }
  "$adb" shell input tap $xy
}

echo "webview: $("$adb" shell dumpsys package com.google.android.webview | grep -m1 versionName | tr -d ' \r')"
echo "gles: $("$adb" shell dumpsys SurfaceFlinger | grep -m1 '^GLES:' | tr -d '\r')"
echo "abi: $("$adb" shell getprop ro.product.cpu.abi | tr -d '\r')"

"$adb" install -r "$apk" >/dev/null 2>&1 || { "$adb" uninstall "$id" >/dev/null; "$adb" install "$apk" >/dev/null; }
"$adb" shell pm clear "$id" >/dev/null
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap try-it
tap space-sp2
tap terminal-
sleep 4
# Trial only, not for merging: wait until Try it's card is up, so the screenshot lands while it is there.
i=0
until "$adb" shell "rm -f /sdcard/hm-up.xml; uiautomator dump /sdcard/hm-up.xml >/dev/null 2>&1; cat /sdcard/hm-up.xml" | grep -q 'resource-id="toast-'; do
  i=$((i + 1)); [ "$i" -le 40 ] || { echo "FAIL: trial: no card in the view tree in 40 looks"; exit 1; }; sleep 0.5
done
echo "trial: a card is up after $i more looks"
"$adb" exec-out screencap -p > "$shot"
echo "saved $shot"
# The terminal's part of the screen: below the header, above the composer (1080 x 2400 screen). Checked on saved
# shots: the line read back in 4 whole ones (CI under swangle, the DOM renderer, a Mac's emulator) and in none of
# 4 cut ones. Compared in lower case: tesseract reads the l of "login" as L in one of them.
python3 - "$shot" "${shot%.png}-terminal.png" <<'PY'
import sys
from PIL import Image
Image.open(sys.argv[1]).crop((0, 260, 1080, 1260)).save(sys.argv[2])
PY
seen=$(tesseract "${shot%.png}-terminal.png" - --psm 6 2>/dev/null | tr -d ' \n' | tr 'A-Z' 'a-z')
case $seen in
  *updatethereadmeforthenewlogin*) echo "ok: the terminal's letters read back whole" ;;
  *) echo "FAIL: the terminal's letters don't read back; tesseract read: $seen"; exit 1 ;;
esac
