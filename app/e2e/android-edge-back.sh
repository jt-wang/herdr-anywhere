#!/bin/sh
# Going back with Android's back gesture from the screen's left edge, over a terminal, on the app's Try it data (no
# computer). Opens space sp1, swipes from the left edge at two heights over the terminal (45% and 80% of the screen),
# slow and fast, N times each, and counts how many went back to home. Passes when every one did. The device must use
# gesture navigation (Settings > System > Gestures), or an edge swipe is no back gesture at all.
# Usage: ANDROID_SERIAL=emulator-5554 [N=4] e2e/android-edge-back.sh <apk> [results.txt]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=$1; out=${2:-/dev/null}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
. "$app/e2e/android-lib.sh"
shown() { dump; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -qF "resource-id=\"$1"; }

mode=$("$adb" shell settings get secure navigation_mode | tr -d '\r')
[ "$mode" = 2 ] || { echo "FAIL: the device uses navigation mode $mode, not gestures (2): an edge swipe is no back gesture there"; exit 1; }
"$adb" install -r "$apk" >/dev/null 2>&1 || { "$adb" uninstall "$id" >/dev/null; "$adb" install "$apk" >/dev/null; }
"$adb" shell pm clear "$id" >/dev/null
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap try-it
size=$("$adb" shell wm size | tail -1 | sed 's/.*: //' | tr -d '\r'); w=${size%x*}; h=${size#*x}
: > "$out"
ok=0; all=0
for height in 45 80; do
  for speed in slow fast; do
    case $speed in slow) ms=600; to=$((w * 6 / 10));; fast) ms=120; to=$((w * 4 / 10));; esac
    i=0
    while [ $i -lt "${N:-4}" ]; do
      i=$((i + 1))
      shown pane-header || { tap space-sp1; sleep 3; } # no tap on the terminal: its keyboard would take the first back
      shown pane-header || { echo "FAIL: space sp1 did not open"; exit 1; }
      y=$((h * height / 100))
      "$adb" shell input swipe 1 "$y" "$to" "$y" "$ms"
      sleep 2
      if ! shown pane-header && shown space-sp1; then back=1; ok=$((ok + 1)); else back=0; fi
      all=$((all + 1))
      echo "y=$height% v=$speed i=$i back=$back" | tee -a "$out"
    done
  done
done
echo "back $ok/$all"
[ "$ok" = "$all" ] && echo "PASS: every edge swipe over the terminal went back" || { echo "FAIL: $((all - ok)) of $all edge swipes did not go back"; exit 1; }
