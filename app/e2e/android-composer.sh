#!/bin/sh
# The input box under a terminal shows its hint whole at the phone's text sizes: 1.0, 1.3 and 2.0 (Android's largest).
# Opens Try it (sample data on the phone, no computer), a terminal, and reads the box out of a screenshot at each size:
# each line of the hint ("Tell Claude"; T, l, C and d rise about 0.7 of the text size) must stand at least 0.6 of the
# text size tall. At 2.0 a box fixed at 36 dp showed 17 px of them at 84 px text: their top half cut off, at the box's
# padding, not its border. Saves composer-<size>.png. The text size is put back to what it was.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-composer.sh <apk> <folder for screenshots>
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=$1
shots=$2; mkdir -p "$shots"
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
font0=$("$adb" shell settings get system font_scale | tr -d '\r'); case $font0 in null|'') font0=1.0;; esac
trap '"$adb" shell settings put system font_scale "$font0"' EXIT; trap 'exit 130' INT; trap 'exit 143' TERM

xml() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml; }
# The bounds (left top right bottom) of the view with this resource-id; an id ending in "-" is a prefix.
bounds() {
  case $1 in *-) q="resource-id=\"$1" ;; *) q="resource-id=\"$1\"" ;; esac
  xml | tr '>' '\n' | grep -F "$q" | head -1 | sed -nE 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/p'
}
# A view not found after a few tries may be below the screen (Try it is, at text size 1.3): the page is moved up.
tap() {
  b=""; for i in $(seq 1 15); do b=$(bounds "$1"); [ -n "$b" ] && break; [ $((i % 4)) = 0 ] && "$adb" shell input swipe 540 1600 540 700 300; sleep 1; done
  [ -n "$b" ] || { echo "FAIL: no view $1 on screen"; "$adb" exec-out screencap -p > "$shots/fail.png"; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}

dpi=$("$adb" shell wm density | tr -dc '0-9\n' | tail -1)
"$adb" install -r "$apk" >/dev/null 2>&1 || { "$adb" uninstall "$id" >/dev/null; "$adb" install "$apk" >/dev/null; }
failed=0
for size in 1.0 1.3 2.0; do
  "$adb" shell settings put system font_scale "$size"
  "$adb" shell pm clear "$id" >/dev/null
  "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
  tap try-it
  tap space-sp2
  tap terminal-
  sleep 2
  [ -n "$(bounds compose-input)" ] || tap to-box
  # The keyboard down, the box empty: the hint is what shows.
  "$adb" shell dumpsys window InputMethod | grep -q "isOnScreen=true" && { "$adb" shell input keyevent 111; sleep 1; }
  sleep 2
  box=$(bounds compose-input)
  [ -n "$box" ] || { echo "FAIL: $size: no input box on screen"; failed=1; continue; }
  "$adb" exec-out screencap -p > "$shots/composer-$size.png"
  # Rows of the box with letters in them: pixels far from the box's own colour, inside its border (3 px top and
  # bottom left out, and the rounded corners: 16 px at each end). The box's text is 14 px at text size 1.0.
  verdict=$(python3 - "$shots/composer-$size.png" $box "$(python3 -c "print(14 * $size * $dpi / 160)")" <<'PY'
import sys
from collections import Counter
from PIL import Image
img = Image.open(sys.argv[1]).convert('RGB')
l, t, r, b = map(int, sys.argv[2:6])
em = float(sys.argv[6])
inner = (l + 16, t + 3, r - 16, b - 3)
px = img.load()
bg = Counter(px[x, y] for x in range(inner[0], inner[2], 3) for y in range(inner[1], inner[3], 3)).most_common(1)[0][0]
far = lambda c: sum(abs(c[i] - bg[i]) for i in range(3)) > 120
ink = [y for y in range(inner[1], inner[3]) if sum(far(px[x, y]) for x in range(inner[0], inner[2])) >= 2]
if not ink:
    print(f'no hint in the box ({b - t} px tall)')
else:
    # Each line of the hint, a run of rows with letters, stands whole: a hint that wraps had its second line's top
    # 5 px showing at 2.0 in a box one line tall.
    lines = [[ink[0], ink[0]]]
    for y in ink[1:]:
        if y == lines[-1][1] + 1: lines[-1][1] = y
        else: lines.append([y, y])
    tall = [e - s + 1 for s, e in lines]
    whole = all(h >= 0.6 * em for h in tall)
    print(f'{"whole" if whole else "cut"}: box {b - t} px, lines {tall} px tall at {em:g} px text, {ink[0] - t} px below the box top')
PY
)
  case $verdict in whole*) echo "ok: $size: the hint shows whole ($verdict)";; *) echo "FAIL: $size: $verdict"; failed=1;; esac
done
[ $failed = 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
