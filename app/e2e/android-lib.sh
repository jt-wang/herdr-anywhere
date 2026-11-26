#!/bin/sh
# Finding and tapping a view on an Android device by its resource-id, for e2e/android.sh, which sources this file with
# $adb set. Kept apart so a test can run them against a fake adb (companion/internal/testfake: TestAndroidTap).

# The screen's accessibility tree into /sdcard/hm.xml. On a busy emulator a command can fail once (on CI a run ended
# with exit 137, a process killed, seconds after pairing, and the screen dumped fine right after); under set -e a failed
# dump ended the run before tap's retries, with no FAIL line (TestAndroidTap: a dump killed once). A failed dump leaves the last file, and the caller's own check decides.
dump() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1 || true; }
# The centre of the view with this resource-id (an NSH testID), from the accessibility tree; an id ending in "-" is a
# prefix (space-<id>, terminal-<address>).
at() {
  case $1 in *-) q="resource-id=\"$1" ;; *) q="resource-id=\"$1\"" ;; esac
  dump
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "$q" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
# On a slow emulator Android can cover the app with "<some app> isn't responding" (on CI: "Pixel Launcher
# isn't responding" hid the pairing screen). Wait is the system's own answer to it; the app under test is not touched.
not_responding() {
  w=$(at android:id/aerr_wait); [ -n "$w" ] || return 1
  echo "note: a system \"isn't responding\" dialog covered the screen; pressed Wait"; "$adb" shell input tap $w; sleep 1
}
# not_responding returns 1 when there is no dialog; under set -e that ended the run at the first miss, so the retries
# below never ran (TestAndroidTap: the view shows on the second look).
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; not_responding || true; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
# The centre of the view with this resource-id from a dump made now, or nothing: the last dump's file is removed first,
# and a failed dump gives nothing rather than the screen before.
fresh_at() {
  "$adb" shell rm -f /sdcard/hm.xml >/dev/null 2>&1 || true
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1 || return 0
  "$adb" shell cat /sdcard/hm.xml 2>/dev/null | tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
# Taps the view once it is at the same place in two fresh dumps in a row, a second or more apart, at that place, without
# looking it up again. A dump that fails or lacks the view starts the count over. After ten dumps without that, it fails
# without a tap. Prints each dump's place and where it tapped.
tap_steady() {
  prev=""; seen=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    cur=$(fresh_at "$1")
    seen="$seen $(echo "${cur:-none}" | tr ' ' ',')"
    if [ -n "$cur" ] && [ "$cur" = "$prev" ]; then
      echo "note: $1 at$seen; tapped at $(echo "$cur" | tr ' ' ',')"
      "$adb" shell input tap $cur
      return 0
    fi
    prev=$cur
    sleep 1
  done
  echo "FAIL: $1 was not at one place in two dumps in a row (at$seen); not tapped"; exit 1
}
