#!/bin/sh
# A new version while the app is away, on Android: the phone in the background (the keep-alive service holds its
# connection), the computer's daily check finds a release, and the person learns of it from one notification, whose
# tap opens home with the notice; the same version is not told twice. The Companion's update.json is written as its
# daily check writes it, and the Companion restarted, so the phone reads it as it connects again (each hour otherwise).
# Its releases are at a closed port: nothing goes to GitHub. Fake data only.
# Before the app had this (its words have no updateNotifyTitle, or BEFORE=1), it posts nothing: then the suite passes
# when no notification comes, and keeps the same screenshots, as the picture of before.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-update-notify.sh APK COMPANION [folder for screenshots]
#        e2e/android-update-notify.sh --self-test    what it prints of a screen or a log keeps no pairing secret
set -eu
mask() { sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g; s/token=[A-Za-z0-9_-]+/token=…/g'; }
# From a screen dump: the expand button nearest above the node with $1 (a group's header sits above its rows), as
# "x y content-desc"; nothing when that node is not on screen.
expand_above() {
  tr '>' '\n' | awk -v want="$1" '
    { if (!match($0, /bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"/)) next
      b = substr($0, RSTART + 8, RLENGTH - 9); gsub(/[^0-9]+/, " ", b); split(b, a, " ") }
    index($0, "resource-id=\"android:id/expand_button\"") { n++; top[n] = a[2]; cx[n] = int((a[1] + a[3]) / 2); cy[n] = int((a[2] + a[4]) / 2)
      d[n] = match($0, /content-desc="[^"]*"/) ? substr($0, RSTART + 14, RLENGTH - 15) : "" }
    index($0, want) && title == "" { title = a[2] }
    END { if (title == "") exit; best = -1
      for (i = 1; i <= n; i++) if (top[i] < title && (best < 0 || top[i] > top[best])) best = i
      if (best > 0) print cx[best], cy[best], d[best] }'
}
# The expand button on the same row as the node with $1 (a notification's own), as "x y content-desc"; nothing when
# that node is not on screen or has no button beside it.
expand_beside() {
  tr '>' '\n' | awk -v want="$1" '
    { if (!match($0, /bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"/)) next
      b = substr($0, RSTART + 8, RLENGTH - 9); gsub(/[^0-9]+/, " ", b); split(b, a, " ") }
    index($0, "resource-id=\"android:id/expand_button\"") { n++; top[n] = a[2]; bot[n] = a[4]; cx[n] = int((a[1] + a[3]) / 2); cy[n] = int((a[2] + a[4]) / 2)
      d[n] = match($0, /content-desc="[^"]*"/) ? substr($0, RSTART + 14, RLENGTH - 15) : "" }
    index($0, want) && t == "" { t = a[2]; u = a[4] }
    END { if (t == "") exit
      for (i = 1; i <= n; i++) if (top[i] < u && bot[i] > t) { print cx[i], cy[i], d[i]; exit } }'
}
if [ "${1:-}" = --self-test ]; then
  said=$(printf '%s\n' ' text="http://10.0.2.2:7721/#pair=Zx81-qq_Lm0pR4"' '127.0.0.1 GET /api/ws?token=Ab12_cd 101 2s' | mask)
  case $said in *Zx81*|*Ab12*) echo "FAIL: android-update-notify self-test: a secret is left: $said"; exit 1 ;; esac
  case $said in *'#pair=…'*'token=…'*) ;; *) echo "FAIL: android-update-notify self-test: $said"; exit 1 ;; esac
  # The shade with Herdr's two notifications in one group: its expand button is the one above the title, not the
  # next group's below it.
  shade='<node text="Herdr Anywhere" resource-id="android:id/app_name_text" bounds="[180,620][410,668]" /><node resource-id="android:id/expand_button" content-desc="Expand" bounds="[894,612][996,676]" /><node text="Herdr Anywhere 99.0.0 is available" resource-id="android:id/title" bounds="[180,700][748,746]" /><node text="Android System" bounds="[180,1090][405,1138]" /><node resource-id="android:id/expand_button" content-desc="Expand" bounds="[894,1082][996,1146]" />'
  xy=$(printf '%s' "$shade" | expand_above 'text="Herdr Anywhere 99.0.0 is available"')
  [ "$xy" = "945 644 Expand" ] || { echo "FAIL: android-update-notify self-test: the group's expand button read as \"$xy\", not 945 644 Expand"; exit 1; }
  xy=$(printf '%s' "$shade" | expand_above 'text="Herdr Anywhere 98.0.0 is available"')
  [ -z "$xy" ] || { echo "FAIL: android-update-notify self-test: an expand button \"$xy\" for a title not on screen"; exit 1; }
  # The group open: the notification's own expand button is the one on its title's row, not the group's above it.
  open='<node resource-id="android:id/expand_button" content-desc="Collapse" bounds="[933,612][996,676]" /><node text="Herdr Anywhere 99.0.0 is available" resource-id="android:id/title" bounds="[180,775][748,820]" /><node text="Tap to update the computer from your phone" resource-id="android:id/text" bounds="[180,830][880,876]" /><node resource-id="android:id/expand_button" content-desc="Expand" bounds="[933,796][996,858]" /><node text="Connected to your computer" bounds="[180,990][645,1035]" /><node resource-id="android:id/expand_button" content-desc="Expand" bounds="[933,980][996,1042]" />'
  got=$(printf '%s' "$open" | expand_beside 'text="Herdr Anywhere 99.0.0 is available"')
  [ "$got" = "964 827 Expand" ] || { echo "FAIL: android-update-notify self-test: the notification's own button read as \"$got\", not 964 827 Expand"; exit 1; }
  echo "PASS: android-update-notify self-test"
  exit 0
fi
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
before=${BEFORE:-$(grep -q updateNotifyTitle "$app/src/strings/en.ts" || echo 1)}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
. "$(dirname "$0")/companion.sh"  # companion_start, companion_stop
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard android-update-notify # a login entry of its own, never the person's
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
new=99.0.0

free_port() { p=$1; while nc -z 127.0.0.1 "$p" 2>/dev/null; do p=$((p + 1)); done; echo "$p"; }
port=$(free_port 7731)
closed=$(free_port 7751)
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/h/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/h/.zshrc"
echo skip_global_compinit=1 > "$work/h/.zshenv"
export HC_SESSION="hmupdn-$$" HC_SOCKET="" HOME="$work/h" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export GIT_CONFIG_GLOBAL="$work/h/.gitconfig"
"$hc" run --bg --keep --cwd "$work/h/src/acme-api" --label herdr.space=spupdn --label herdr.tab=tbupdn >/dev/null
cleanup() {
  companion_stop "$state" || true
  "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  autostart_guard_end || exit 1
}
trap 'adb_exit $? cleanup' EXIT
trap 'exit 130' INT; trap 'exit 143' TERM

serve() { companion_start "$work/serve.log" "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" --releases "http://127.0.0.1:$closed" || exit 1; }
# What the daily check writes when it finds a release, the person having said yes to it.
found() { printf '{"checks":"on","latest":"%s"}' "$1" > "$state/update.json"; }

xml() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml; }
centre() { tr '>' '\n' | grep -F "$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
words() { xml | tr '>' '\n' | grep -oE ' text="[^"]+"' | head -20 | mask; }
tap_where() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: nothing on screen with $1"; words; exit 1; }
  "$adb" shell input tap $xy
}
wait_for() { for _ in $(seq 1 "$2"); do xml | grep -qF "$1" && return 0; sleep 1; done; echo "FAIL: \"$1\" did not show within $2 s"; words; exit 1; }
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/$1.png" || true; }
records() { "$adb" shell dumpsys notification --noredact | grep -E "NotificationRecord.*pkg=$id .*tag=new-version" || true; }
# Shown now: the keys of the active notifications only. dumpsys has more sections than the active list; its record's
# object id is still read above, to tell a second post from the same one.
posted() { "$adb" shell cmd notification list | grep -c "|$id|1|new-version|" || true; }
# The phone in the background reads the computer again once it is back: the Companion restarted with what it found.
restart() {
  companion_stop "$state" || exit 1
  found "$1"
  serve
}

found "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$app/nsh.json")"
serve
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: pair printed no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$(dirname "$0")/android-type-link.sh" "$invite"
tap_where 'resource-id="pair"'
wait_for 'resource-id="space-spupdn"' 60
sleep 3
xml | grep -qF 'resource-id="update-banner"' && { echo "FAIL: a notice on home before any new version"; exit 1; }
echo "ok: paired, nothing new yet"

echo "--- the computer finds $new while the app is away"
"$adb" shell input keyevent KEYCODE_HOME
sleep 3
restart "$new"
n=0; for _ in $(seq 1 90); do n=$(posted); [ "$n" -gt 0 ] && break; sleep 1; done
"$adb" shell cmd statusbar expand-notifications
sleep 2
snap notification
if [ -n "$before" ]; then
  [ "$n" = 0 ] || { echo "FAIL: BEFORE: this app posted a notification of a new version"; exit 1; }
  "$adb" shell cmd statusbar collapse
  echo "ok: before: nothing in the notifications for 90 s; the person learns of $new only by opening the app"
  "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
  wait_for "Version $new is available" 60
  snap home
  echo PASS
  exit 0
fi
[ "$n" -gt 0 ] || { echo "FAIL: no notification of $new within 90 s"; tail -5 "$work/serve.log" | mask; exit 1; }
wait_for "Herdr Anywhere $new is available" 10
echo "ok: one notification: Herdr Anywhere $new is available"
# The record's object id: the same one after the tap is one not taken away, another one a second post.
first=$(records | grep -oE 'NotificationRecord\(0x[0-9a-f]+' | head -1)
# With "Connected to your computer" beside it, Android may put Herdr's notifications in one group (its summary is
# ranker_group). A tap on a closed group opens the app as its icon does, not this notification: open the group first.
title="text=\"Herdr Anywhere $new is available\""; grouped=""
if "$adb" shell cmd notification list | grep -qF "|$id|2147483647|ranker_group|"; then
  grouped=" (Herdr's notifications were in one group)"
  xy=$(xml | expand_above "$title" | cut -d' ' -f1,2)
  [ -n "$xy" ] || { echo "FAIL: Herdr's notifications are in one group, and no expand button above the new version's"; words; exit 1; }
  "$adb" shell input tap $xy
  sleep 2
  echo "ok: Herdr's notifications were in one group; opened it to tap the new version's own"
fi
# The shot shows all its words: the notification's own expand button (on its title's row in a group, above the title
# alone) is tapped until it says Collapse; no shot otherwise.
own() { b=$(xml | expand_beside "$title"); [ -n "$b" ] || [ -n "$grouped" ] || b=$(xml | expand_above "$title"); echo "$b"; }
b=$(own)
case $b in
  *" Collapse"*) ;;
  "") echo "FAIL: no expand button of the new version's notification"; words; exit 1 ;;
  *) "$adb" shell input tap $(echo "$b" | cut -d' ' -f1,2)
     for _ in $(seq 1 5); do sleep 1; b=$(own); case $b in *" Collapse"*) break ;; esac; done
     case $b in *" Collapse"*) ;; *) echo "FAIL: the new version's notification did not open; its button reads \"$b\""; exit 1 ;; esac ;;
esac
snap notification-open
echo "ok: the new version's notification is open, all its words shown"
tap_where "$title"
wait_for 'resource-id="update-banner"' 60
wait_for "Version $new is available" 10
snap home
echo "ok: its tap opened home with the notice"
gone=""; for _ in $(seq 1 15); do [ "$(posted)" = 0 ] && { gone=1; break; }; sleep 1; done
if [ -z "$gone" ]; then
  now=$(records | grep -oE 'NotificationRecord\(0x[0-9a-f]+' | head -1)
  [ "$now" = "$first" ] && echo "FAIL: the notification is still listed after its tap$grouped (the same record, $first)" ||
    echo "FAIL: after its tap the notification was posted again$grouped ($first, then $now)"
  "$adb" shell cmd notification list | grep -F "|$id|"
  "$adb" shell dumpsys notification --noredact | grep -A12 "tag=new-version" | head -30
  exit 1
fi
echo "ok: the tap took the notification away"

echo "--- the same version is told once"
"$adb" shell input keyevent KEYCODE_HOME
sleep 3
restart "$new"
sleep 30
[ "$(posted)" = 0 ] || { echo "FAIL: $new was posted again:"; records | cut -c1-240; exit 1; }
echo "ok: nothing more for $new"
echo PASS
