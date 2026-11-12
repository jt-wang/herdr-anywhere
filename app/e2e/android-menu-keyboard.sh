#!/bin/sh
# Screenshots only: a space's ⋯ menu opened while the input box has the keyboard up, on Android, against an isolated
# herdr-core and the companion on loopback (10.0.2.2 from the emulator). Takes the input box with the keyboard up, then
# the open menu, and writes whether the keyboard was up under the menu. Stops with FAIL when the keyboard did not come
# up first or the menu did not open, so no screenshot shows another state.
# Usage: ANDROID_SERIAL=emulator-5554 SHOT_TAG=before-android360-light e2e/android-menu-keyboard.sh APK folder
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; shots=$2; tag=${SHOT_TAG:-android}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
readonly id
port=7891; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmmkXXXX)
mkdir -p "$work/u/src/quill_notes" "$work/state" "$work/config" "$work/data" "$shots"
: > "$work/u/.zshrc"
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
export HC_SESSION="hmmenukbd-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh
. "$app/e2e/autostart-guard.sh"; autostart_guard android-menu-keyboard # a login entry of its own, never the person's
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/u/.gitconfig"
"$hc" run --bg --keep --cwd "$work/u/src/quill_notes" --label herdr.space=spquill --label herdr.tab=tbquill >/dev/null

"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
cleanup() { st=$?; kill "$serve_pid" 2>/dev/null; "$hc" server stop >/dev/null 2>&1 || true; autostart_guard_end || st=1; chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" "${state:?}"; exit $st; }
trap cleanup EXIT
trap 'exit 1' INT TERM
sleep 1
invite=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
wait_for() { b=""; for _ in $(seq 1 ${2:-30}); do b=$(xml | bounds "$1"); [ -n "$b" ] && return 0; sleep 1; done; return 1; }
tap() {
  wait_for "$1" || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}
keyboard() { "$adb" shell dumpsys input_method | grep -q 'mInputShown=true'; }
shot() { "$adb" exec-out screencap -p > "$shots/$tag-$1.png"; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$(dirname "$0")/android-type-link.sh" "$invite"
keyboard && { "$adb" shell input keyevent 4; sleep 1; }
tap pair
tap space-spquill
wait_for compose-input 40 || { echo "FAIL: the space did not open"; exit 1; }
sleep 2
tap compose-input
up=0; for _ in $(seq 1 10); do keyboard && { up=1; break; }; sleep 1; done
[ "$up" = 1 ] || { echo "FAIL: the keyboard did not come up for the input box"; exit 1; }
sleep 1; shot keyboard
tap space-more
wait_for menu 10 || { echo "FAIL: the menu did not open"; exit 1; }
sleep 1.5; shot menu
keyboard && k=up || k=down
echo "$tag: keyboard $k under the open menu" | tee -a "$shots/keyboard.txt"
