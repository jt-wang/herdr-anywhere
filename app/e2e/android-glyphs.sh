#!/bin/sh
# One terminal's text on an Android device: installs the APK, opens Try it (sample data on the phone, no computer),
# opens a terminal and saves a screenshot. Passes when the terminal's first line reads back from the screenshot
# (tesseract): CI's emulator under -gpu swiftshader_indirect drew it with strokes missing, and under -gpu swangle
# whole. Nothing is typed: adb's input text drops characters on CI's emulator. Also prints the WebView version
# and the GLES renderer the device draws with.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-glyphs.sh <apk> <screenshot.png>
#        e2e/android-glyphs.sh --self-test
set -eu

now() { date +%s; }

# The app's cards on screen now (Try it raises "Codex needs you" 12 s in, demo.ts CODEX_ASKS_MS; each card stays
# 5 s, store.ts): their resource-ids, "none", or "unknown" when the view tree came back without the app in it.
cards() {
  x=$("$adb" shell "rm -f /sdcard/hm-cards.xml; uiautomator dump /sdcard/hm-cards.xml >/dev/null 2>&1; cat /sdcard/hm-cards.xml" 2>/dev/null | tr '>' '\n') || true
  printf '%s\n' "$x" | grep -qF "package=\"$id\"" || { echo unknown; return 0; }
  c=$(printf '%s\n' "$x" | grep -o 'resource-id="toast-[0-9]*"' | sed 's/resource-id="//; s/"$//' | sort -u | tr '\n' ' ')
  echo "${c:-none}"
}

# A screenshot with no card on it, in the given file. A card sits over the terminal's first lines, the line this
# suite reads back. The shot counts only when the view tree shows no card just before it and just after it, both
# looks within 5 s: a card stays 5 s, so none can have come and gone in between. Otherwise it waits and tries again.
shot_clear() {
  tries=0 seen=""
  while :; do
    tries=$((tries + 1))
    [ "$tries" -le 25 ] || { echo "FAIL: no screenshot without a card in 25 tries: $seen"; exit 1; }
    t0=$(now); before=$(cards); "$adb" exec-out screencap -p > "$1"; after=$(cards); t1=$(now)
    if [ "$before" = none ] && [ "$after" = none ] && [ $((t1 - t0)) -le 4 ]; then
      echo "the screenshot: no card in the view tree before or after it, $((t1 - t0)) s apart${seen:+; it waited: $seen}"
      return 0
    fi
    seen="$seen${seen:+; }before: $before, after: $after, $((t1 - t0)) s"
    sleep "${HM_GLYPHS_PAUSE:-1}"
  done
}

if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  # A stand-in adb. Each look at the phone (a view tree, a screenshot) takes the next word of $t/seq: clear (the app,
  # no card), card (the app and a card), broken (the dump failed). The clock takes the next number of $t/times.
  pop() { w=$(head -1 "$t/$1"); tail -n +2 "$t/$1" > "$t/$1.n"; mv "$t/$1.n" "$t/$1"; echo "${w:-$2}"; }
  fake_adb() {
    case "$*" in
      *screencap*) echo "screen:$(pop seq clear)" ;;
      *uiautomator*) case $(pop seq clear) in
        clear) echo '<hierarchy><node package="io.example.app" resource-id="compose-input" /></hierarchy>' ;;
        card) echo '<hierarchy><node package="io.example.app" resource-id="toast-3" /><node package="io.example.app" resource-id="compose-input" /></hierarchy>' ;;
        broken) echo 'ERROR: null root node returned by UiTestAutomationBridge.' ;;
      esac ;;
    esac
  }
  now() { pop times "$(date +%s)"; }
  adb=fake_adb id=io.example.app HM_GLYPHS_PAUSE=0
  # Name, the looks in order (before, screenshot, after, for each try), the clock's numbers, then what must happen:
  # pass and the screenshot it keeps, or fail.
  case_() {
    printf '%s\n' $2 > "$t/seq"; printf '%s\n' $3 > "$t/times"
    out=$( (shot_clear "$t/shot.png") 2>&1 ) && got=pass || got=fail
    kept=$(cat "$t/shot.png" 2>/dev/null || true); rm -f "$t/shot.png"
    case $4 in
      fail) [ "$got" = fail ] || { echo "FAIL: self-test $1: passed, wanted a failure ($out)"; fail=1; } ;;
      *) [ "$got $kept" = "pass $4" ] || { echo "FAIL: self-test $1: $got, kept \"$kept\", wanted $4 ($out)"; fail=1; } ;;
    esac
  }
  case_ "no card" "clear clear clear" "" screen:clear
  case_ "a card, then gone" "card card card clear clear clear" "" screen:clear
  case_ "a card comes after the first look" "clear card card clear clear clear" "" screen:clear
  case_ "a card goes before the last look" "card card clear clear clear clear" "" screen:clear
  case_ "a failed view tree is no answer" "broken card clear clear clear clear" "" screen:clear
  # A card that came and went between two looks 6 s apart: on the screenshot, in neither view tree.
  case_ "looks 6 s apart" "clear card clear clear clear clear" "100 106 110 112" screen:clear
  case_ "a card that stays" "$(yes card | head -90 | tr '\n' ' ')" "" fail
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: android-glyphs self-test" || { echo "FAIL: android-glyphs self-test"; exit 1; }
  exit 0
fi
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
shot_clear "$shot"
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
