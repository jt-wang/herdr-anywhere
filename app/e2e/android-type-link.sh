#!/bin/sh
# Types a pairing link into the app's link field on the pairing screen, and checks the field holds all of it.
# adb's input text drops characters on a slow emulator: on CI, 80-character pieces left 195, 204 and 301 of a
# 509-character Tailcat invite in the field. So the link goes in 10-character pieces, the field is read back, and each
# round deletes from the first wrong character and types the rest again (as android.sh does). A link longer than 200
# characters (a Tailcat invite, about 510) is pasted instead: CI's emulator types about 4 characters a second, so typing
# one took over 2 minutes, and the invite lasts 2. It goes on the phone's clipboard (android-clip.sh), is pasted into the
# field, and the clipboard is emptied again. Leaves the keyboard up; the caller taps pair.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-type-link.sh LINK
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
link=$1
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
trap 'adb_exit $?' EXIT
xml() { "$adb" shell rm -f /sdcard/hm-link.xml; "$adb" shell uiautomator dump /sdcard/hm-link.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm-link.xml 2>/dev/null || true; }
# The field's text: pair-link is the field's wrapper; the text is its EditText child's.
field() {
  xml | python3 -c '
import html, re, sys
s = sys.stdin.read()
i = s.find("resource-id=\"pair-link\"")
m = re.search(r"<node [^>]*class=\"android\.widget\.EditText\"[^>]*>", s[i:]) if i >= 0 else None
t = re.search(r" text=\"([^\"]*)\"", m.group(0)) if m else None
print(html.unescape(t.group(1)) if t else "")'
}
send() {
  rest=$1
  while [ -n "$rest" ]; do
    piece=$(printf %s "$rest" | cut -c1-10); rest=$(printf %s "$rest" | cut -c11-)
    "$adb" shell input text "'$piece'" </dev/null
  done
}
xy=""
for _ in $(seq 1 30); do
  xy=$(xml | tr '>' '\n' | grep -F 'resource-id="pair-link"' | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}')
  [ -n "$xy" ] && break; sleep 1
done
[ -n "$xy" ] || { echo "FAIL: no link field on screen"; exit 1; }
"$adb" shell input tap $xy
sleep 1
if [ ${#link} -gt 200 ]; then
  clip="$(dirname "$0")/android-clip.sh"
  tmp=$(mktemp -d); trap 'adb_exit $? rm -rf "$tmp"' EXIT
  (umask 077; printf %s "$link" > "$tmp/link"); : > "$tmp/empty"
  "$clip" set "$tmp/link"
  [ "$("$clip" get)" = "${#link}" ] || { echo "FAIL: the clipboard does not hold the link's ${#link} characters"; exit 1; }
  for _ in 1 2 3; do
    "$adb" shell input keyevent KEYCODE_PASTE
    got=$(field); [ "$got" = "$link" ] && break
    echo "after Paste the link field holds ${#got} of the link's ${#link} characters; again"
    "$adb" shell input keyevent KEYCODE_MOVE_END $(printf 'KEYCODE_DEL %.0s' $(seq 1 $((${#got} + 1))))
  done
  "$clip" set "$tmp/empty"
  [ "$got" = "$link" ] || { echo "FAIL: after Paste the link field holds ${#got} of the link's ${#link} characters"; exit 1; }
  exit 0
fi
send "$link"
# Each round keeps a longer right start; it gives up after three rounds that keep none more.
best=-1; stuck=0
while [ $stuck -lt 3 ]; do
  got=$(field); [ "$got" = "$link" ] && exit 0
  same=$(python3 -c 'import os,sys; print(len(os.path.commonprefix(sys.argv[1:3])))' "$got" "$link")
  if [ "$same" -gt "$best" ]; then best=$same; stuck=0; else stuck=$((stuck + 1)); fi
  echo "the link field holds ${#got} of the link's ${#link} characters, the first $same right; typing from there"
  # The field keeps focus and its cursor stays after the last character typed.
  [ "${#got}" -gt "$same" ] && "$adb" shell input keyevent KEYCODE_MOVE_END $(printf 'KEYCODE_DEL %.0s' $(seq 1 $((${#got} - same))))
  send "$(printf %s "$link" | cut -c$((same + 1))-)"
done
got=$(field)
[ "$got" = "$link" ] || { echo "FAIL: the link field holds ${#got} of the link's ${#link} characters"; exit 1; }
