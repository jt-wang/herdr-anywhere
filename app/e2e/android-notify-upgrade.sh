#!/bin/sh
# An update over 0.1.13 and its notification channels. Android keeps a channel as the person or the first version made
# it, so the new version moves to new channel ids: the old ones go, the new ones pop up with Herdr's sounds, and what
# the person chose on an old one stays. Two runs, each from a fresh install of OLD: (a) untouched, (b) "An agent
# finished" turned off and "An agent needs you" set to silent on Android's channel pages first.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-notify-upgrade.sh OLD.apk NEW.apk OUT
# NEW.apk must be signed with OLD.apk's key. Needs python3 and the Android SDK.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
old=$1; new=$2; out=$3
mkdir -p "$out"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
failed=0
fail() { echo "FAIL: $*"; failed=1; }

# The app's live channels as "id importance sound", one a line. Android keeps a deleted channel in its records with
# mDeleted=true (seen on Android 15, 2026-11-07), so those are left out.
channels() {
  "$adb" shell dumpsys notification --noredact | grep -E "NotificationChannel\{mId='[^']*'" | grep -v 'mDeleted=true' |
    grep -E "mId='(needs-you|finished|agent-needs-you|agent-finished)'" |
    sed -E "s/.*mId='([^']*)'.*mImportance=([0-9]+).*mSound=([^,]*),.*/\1 \2 \3/" | sort -u
}
start() { "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null; sleep 3; "$adb" shell input keyevent 3; sleep 1; }
fresh_old() {
  "$adb" uninstall "$id" >/dev/null 2>&1 || true
  "$adb" install "$old" >/dev/null
  "$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS
  start
}
update() { "$adb" install -r "$new" >/dev/null; start; }

# A channel's page in Android's settings, and the first switch on it (the channel on or off), or a choice by its words.
page() { "$adb" shell am force-stop com.android.settings; "$adb" shell am start -a android.settings.CHANNEL_NOTIFICATION_SETTINGS --es android.provider.extra.APP_PACKAGE "$id" --es android.provider.extra.CHANNEL_ID "$1" >/dev/null; sleep 2; }
tap_node() {
  "$adb" shell uiautomator dump /sdcard/u.xml >/dev/null 2>&1
  xy=$("$adb" shell cat /sdcard/u.xml | tr '>' '\n' | grep -E "$1" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}')
  [ -n "$xy" ] || { echo "no node $1"; return 1; }
  "$adb" shell input tap $xy; sleep 1
}

echo "(a) untouched"
fresh_old
channels | tee "$out/a-before.txt"
grep -q '^finished 2 ' "$out/a-before.txt" || fail "0.1.13 did not make its finished channel at 2"
update
channels | tee "$out/a-after.txt"
grep -qE "^agent-needs-you 4 android.resource://$id/raw/request" "$out/a-after.txt" || fail "(a) agent-needs-you is not 4 with Herdr's request sound"
grep -qE "^agent-finished 4 android.resource://$id/raw/done" "$out/a-after.txt" || fail "(a) agent-finished is not 4 with Herdr's done sound"
grep -qE '^(needs-you|finished) ' "$out/a-after.txt" && fail "(a) the old channels are still there"

echo "(b) the person turned finished off and needs-you silent"
fresh_old
page finished
tap_node 'class="android.widget.Switch"' || true
page needs-you
tap_node 'text="Silent"' || true
"$adb" exec-out screencap -p > "$out/b-silent.png"
channels | tee "$out/b-before.txt"
grep -q '^finished 0 ' "$out/b-before.txt" || fail "(b) could not turn the old finished channel off"
grep -q '^needs-you 2 ' "$out/b-before.txt" || fail "(b) could not make the old needs-you channel silent"
update
channels | tee "$out/b-after.txt"
grep -q '^agent-finished 0 ' "$out/b-after.txt" || fail "(b) finished, turned off before, is on after the update"
grep -q '^agent-needs-you 2 ' "$out/b-after.txt" || fail "(b) needs-you, silent before, is not silent after the update"
grep -qE '^(needs-you|finished) ' "$out/b-after.txt" && fail "(b) the old channels are still there"

[ "$failed" = 0 ] || { echo "FAIL: see above; collected in $out"; exit 1; }
echo "PASS: an update moves to the new channels, with Herdr's sounds, and keeps what the person chose ($out)"
