#!/bin/sh
# An alert's two lines on Android: folded, the notification shows the first (what the agent asks or did) alone;
# unfolded, both, the second being where it is (space › tab). Before the fix the folded line read
# "Bash: npm test api › 2", Android having joined the two lines with a space. Uses Try it (sample data on the phone,
# no computer): Codex asks about 12 s after it opens, and the app is in the background by then, so it is a
# notification. Reads the shade with uiautomator in three states: the app's group folded (one line per notification,
# as the shade first shows it), the group unfolded with the Codex notification folded, and that one unfolded.
# Screenshots and dumps of each go to the out folder.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-notify-folded.sh APK OUT
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1
out=$2
mkdir -p "$out"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
trap 'adb_exit $?' EXIT
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

dump() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml > "$out/$1.xml"; }
at() {
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
tap() {
  xy=""; for _ in $(seq 1 15); do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; "$adb" exec-out screencap -p > "$out/fail.png"; exit 1; }
  "$adb" shell input tap $xy
}
# The views of the notification titled $2 in a dump, one `id=text` a line (id without its package; a newline in a
# text as \n): the innermost row holding the title, as the shade nests an app's group of notifications in one row.
row() {
  python3 -I - "$out/$1.xml" "$2" <<'PY2'
import sys, xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
ROW = 'com.android.systemui:id/expandableNotificationRow'
def has(n): return any(v.get('text') == sys.argv[2] for v in n.iter())
def inner(n):
    for c in n.iter():
        if c is not n and c.get('resource-id') == ROW and has(c): return inner(c)
    return n
rows = [n for n in root.iter() if n.get('resource-id') == ROW and has(n)]
if rows:
    for v in inner(rows[0]).iter():
        if v.get('text'): print(v.get('resource-id', '').split(':id/')[-1] + '=' + v.get('text').replace('\n', '\\n'))
PY2
}
# The centre of the fold/unfold button of the group row whose app name is $2 (the app's group) or, with $3, of the
# notification titled $2.
button() {
  python3 -I - "$out/$1.xml" "$2" <<'PY2'
import sys, re, xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
ROW = 'com.android.systemui:id/expandableNotificationRow'
def own(r):  # r's views, without those of rows inside it
    yield r
    for c in r:
        if c.get('resource-id') != ROW: yield from own(c)
for r in root.iter():
    if r.get('resource-id') != ROW: continue
    views = list(own(r))
    if not any(v.get('text') == sys.argv[2] for v in views): continue
    for v in views:
        if v.get('resource-id') == 'android:id/expand_button':
            a, b, c, d = map(int, re.findall(r'\d+', v.get('bounds'))); print((a + c) // 2, (b + d) // 2); sys.exit()
PY2
}

"$adb" install -r "$apk" >/dev/null 2>&1 || { "$adb" uninstall "$id" >/dev/null; "$adb" install "$apk" >/dev/null; }
"$adb" shell pm clear "$id" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap try-it
sleep 2
"$adb" shell input keyevent 3
title=""
for _ in $(seq 1 30); do
  title=$("$adb" shell dumpsys notification --noredact | grep -A40 "pkg=$id " | grep -oE 'android.title=String \([^)]*\)' | grep -i codex | head -1 | sed -E 's/android.title=String \((.*)\)/\1/')
  [ -n "$title" ] && break
  sleep 1
done
[ -n "$title" ] || { echo "FAIL: no notification from Codex within 30 s"; exit 1; }
echo "posted: $title"
sleep 6 # past the heads-up
"$adb" shell cmd statusbar expand-notifications
sleep 2
failed=0
fail() { echo "FAIL: $*"; failed=1; }

# Folded as the shade first shows it: the app's notifications in one group, a line each (what the screenshot in
# the bug showed). Then the group unfolded, each notification folded. Then the Codex one unfolded.
dump group
"$adb" exec-out screencap -p > "$out/group.png"
row group "$title" | tee "$out/group.txt"
grep -qx 'notification_text=Bash: npm test' "$out/group.txt" && echo "ok: in the folded group, the first line alone" ||
  fail "in the folded group, the line reads '$(grep notification_text= "$out/group.txt" | cut -d= -f2-)', not 'Bash: npm test' alone"

xy=$(button group "Herdr Anywhere")
[ -n "$xy" ] || { fail "no group row for Herdr Anywhere"; xy="0 0"; }
"$adb" shell input tap $xy
sleep 2
dump folded
"$adb" exec-out screencap -p > "$out/folded.png"
row folded "$title" | tee "$out/folded.txt"
grep -qx 'text=Bash: npm test' "$out/folded.txt" && echo "ok: folded, the first line alone" ||
  fail "folded, the notification reads '$(grep '^text=' "$out/folded.txt" | cut -d= -f2-)', not 'Bash: npm test' alone"

xy=$(button folded "$title")
[ -n "$xy" ] || { fail "no unfold button on the Codex notification"; xy="0 0"; }
"$adb" shell input tap $xy
sleep 2
dump unfolded
"$adb" exec-out screencap -p > "$out/unfolded.png"
row unfolded "$title" | tee "$out/unfolded.txt"
grep -qxF 'big_text=Bash: npm test\napi › 2' "$out/unfolded.txt" && echo "ok: unfolded, both lines" ||
  fail "unfolded, the notification reads '$(grep '^big_text=' "$out/unfolded.txt" | cut -d= -f2-)', not 'Bash: npm test' then 'api › 2'"

"$adb" shell cmd statusbar collapse
"$adb" shell input keyevent 3
[ "$failed" = 0 ] || { echo "FAIL: see above; collected in $out"; exit 1; }
echo "PASS: folded, the first line alone; unfolded, both ($out)"
