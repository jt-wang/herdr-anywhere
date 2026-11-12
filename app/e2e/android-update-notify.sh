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
if [ "${1:-}" = --self-test ]; then
  said=$(printf '%s\n' ' text="http://10.0.2.2:7721/#pair=Zx81-qq_Lm0pR4"' '127.0.0.1 GET /api/ws?token=Ab12_cd 101 2s' | mask)
  case $said in *Zx81*|*Ab12*) echo "FAIL: android-update-notify self-test: a secret is left: $said"; exit 1 ;; esac
  case $said in *'#pair=…'*'token=…'*) echo "PASS: android-update-notify self-test" ;; *) echo "FAIL: android-update-notify self-test: $said"; exit 1 ;; esac
  exit 0
fi
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
before=${BEFORE:-$(grep -q updateNotifyTitle "$app/src/strings/en.ts" || echo 1)}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
. "$(dirname "$0")/companion.sh"  # companion_start, companion_stop
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
posted() { records | grep -c . || true; }
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
tap_where "text=\"Herdr Anywhere $new is available\""
wait_for 'resource-id="update-banner"' 60
wait_for "Version $new is available" 10
snap home
echo "ok: its tap opened home with the notice"
gone=""; for _ in $(seq 1 15); do [ "$(posted)" = 0 ] && { gone=1; break; }; sleep 1; done
[ -n "$gone" ] || { echo "FAIL: the notification is still listed after its tap:"; records | cut -c1-240; exit 1; }
echo "ok: the tap took the notification away"

echo "--- the same version is told once"
"$adb" shell input keyevent KEYCODE_HOME
sleep 3
restart "$new"
sleep 30
[ "$(posted)" = 0 ] || { echo "FAIL: $new was posted again:"; records | cut -c1-240; exit 1; }
echo "ok: nothing more for $new"
echo PASS
