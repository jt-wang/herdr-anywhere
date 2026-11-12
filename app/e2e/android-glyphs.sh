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

# What tesseract reads in the terminal's part of a screenshot: below the header, above the composer (1080 x 2400
# screen), in lower case with no spaces. Checked on saved shots: the line read back in 4 whole ones (CI under
# swangle, the DOM renderer, a Mac's emulator) and in none of 4 cut ones. Lower case: tesseract reads the l of
# "login" as L in one of them.
reads() {
  python3 - "$1" "${1%.png}-terminal.png" <<'PY'
import sys
from PIL import Image
Image.open(sys.argv[1]).crop((0, 260, 1080, 1260)).save(sys.argv[2])
PY
  tesseract "${1%.png}-terminal.png" - --psm 6 2>/dev/null | tr -d ' \n' | tr 'A-Z' 'a-z'
}

# Screenshots into the given file until one reads back the terminal's first line. Try it raises a card over that
# line 12 s in, and the card stays 5 s; a card can only stop the line reading back, never make it read. So a failed
# read is the answer only after every shot for 7 s on the clock (more than 6 s) has failed. Each failed shot writes
# down the cards the view tree shows after it.
shot_reads() {
  t0=$(now); n=0 notes=""
  while :; do
    n=$((n + 1))
    "$adb" exec-out screencap -p > "$1"
    seen=$(reads "$1")
    case $seen in
      *updatethereadmeforthenewlogin*) echo "ok: the terminal's letters read back whole, shot $n${notes:+; before it: $notes}"; return 0 ;;
    esac
    notes="$notes${notes:+; }shot $n: card: $(cards)"
    el=$(($(now) - t0))
    [ "$el" -lt 7 ] || { echo "FAIL: the terminal's letters don't read back in $n shots over $el s ($notes); tesseract read: $seen"; exit 1; }
    sleep "${HM_GLYPHS_PAUSE:-1}"
  done
}

if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  # A stand-in adb and tesseract. Each screenshot takes the next word of $t/seq: whole (the line reads back), card
  # (Try it's card over it), cut (strokes missing); each view tree the next of $t/tree; the clock the next of $t/times.
  pop() { w=$(head -1 "$t/$1"); tail -n +2 "$t/$1" > "$t/$1.n"; mv "$t/$1.n" "$t/$1"; echo "${w:-$2}"; }
  fake_adb() {
    case "$*" in
      *screencap*) echo "screen:$(pop seq cut)" ;;
      *uiautomator*) case $(pop tree none) in
        card) echo '<hierarchy><node package="io.example.app" resource-id="toast-3" /></hierarchy>' ;;
        *) echo '<hierarchy><node package="io.example.app" resource-id="compose-input" /></hierarchy>' ;;
      esac ;;
    esac
  }
  reads() { case $(cat "$1") in screen:whole) echo "updatethereadmeforthenewlogin" ;; screen:card) echo "codexneedsyoubash:npmtest" ;; *) echo "upd4tetheread" ;; esac; }
  now() { pop times 999; }
  adb=fake_adb id=io.example.app HM_GLYPHS_PAUSE=0
  # Name, the screenshots in order, the view trees, the clock's numbers (one at the start, one after each failed
  # read), then pass or fail and the text the output must hold.
  case_() {
    printf '%s\n' $2 > "$t/seq"; printf '%s\n' $3 > "$t/tree"; printf '%s\n' $4 > "$t/times"
    out=$( (shot_reads "$t/shot.png") 2>&1 ) && got=pass || got=fail
    [ "$got" = "$5" ] && case $out in *"$6"*) true ;; *) false ;; esac ||
      { echo "FAIL: self-test $1: $got, wanted $5 with \"$6\" ($out)"; fail=1; }
  }
  case_ "reads at once" "whole" "" "0" pass "shot 1"
  case_ "under the card, then reads" "card card whole" "card card" "0 1 2" pass "shot 3; before it: shot 1: card: toast-3"
  case_ "the card for 6 s, then reads" "card card card whole" "card card card" "0 2 4 6" pass "shot 4"
  case_ "cut for 8 s" "cut cut cut cut cut" "" "0 2 4 6 8" fail "in 4 shots over 8 s"
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
shot_reads "$shot"
echo "saved $shot"
