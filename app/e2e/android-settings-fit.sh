#!/bin/sh
# Every text on Settings shows whole, before and after the terminal's input box is used, at the phone's font size and
# at 130 %: android-text-fit.py reads each text's box in a screenshot for its last word. NativeShell measured a text
# once, and on Android 15 an EditText left the layouts after it breaking lines by glyph bounds: Settings opened again
# showed "Your computer asks GitHub once a", the line measured as one broken into two in a box one line high
# (app/android/nsh-patches/0001). So Settings is paged through first, then the input box is used, then Settings again.
# Needs Pillow and tesseract (e2e/ci/tools.sh shots).
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-settings-fit.sh APK COMPANION [folder for screenshots]
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard android-settings-fit # a login entry of its own, never the person's
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
here=$(cd "$(dirname "$0")" && pwd); app=$(cd "$here/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$here/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
python3 "$here/android-text-fit.py" --self-test
port=7771; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmsfXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
echo skip_global_compinit=1 > "$work/home/.zshenv" # Ubuntu's compinit can stop at a y/n question
export HC_SESSION="hmsf-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spfit --label herdr.tab=tbfit >/dev/null
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
scale=$("$adb" shell settings get system font_scale | tr -d '\r'); case $scale in null|'') scale=1.0 ;; esac
cleanup() { "$adb" shell settings put system font_scale "$scale" >/dev/null 2>&1 || true; kill "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"; autostart_guard_end || exit 1; }
trap 'adb_exit $? cleanup' EXIT
trap 'exit 130' INT; trap 'exit 143' TERM
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
centre() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
tap() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  "$adb" shell input tap $xy
}
back_home() { for _ in 1 2 3 4; do xml | grep -q 'resource-id="space-spfit"' && return 0; "$adb" shell input keyevent KEYCODE_BACK; sleep 2; done
  echo "FAIL: did not get back to the home screen"; exit 1; }
# Settings from the top to the end at the phone's font size and at 130 %, each page checked; $1 names the pass
# (before or after), in the screenshots' names too.
pages() {
  bad=0
  for s in "$scale" 1.3; do
    "$adb" shell settings put system font_scale "$s"; sleep 3
    for _ in 1 2 3 4 5 6; do "$adb" shell input swipe 540 700 540 2000 150; done; sleep 2
    last=""
    for page in 1 2 3 4 5 6 7 8 9 10; do
      "$adb" exec-out screencap -p > "$work/p.png"; xml > "$work/p.xml"
      now=$(cksum < "$work/p.xml"); [ "$now" = "$last" ] && break; last=$now # a swipe that moved nothing: the end
      [ -n "$shot" ] && cp "$work/p.png" "$shot/settings-fit-$1-$s-$page.png"
      python3 "$here/android-text-fit.py" "$work/p.png" "$work/p.xml" > "$work/fit" || bad=1
      sed "s/^/  $1 the input box, font $s, page $page: /" "$work/fit"
      "$adb" shell input swipe 540 1800 540 900 600; sleep 2
    done
  done
  "$adb" shell settings put system font_scale "$scale"; sleep 2
  [ $bad = 0 ] || { echo "FAIL: a text on Settings is cut off $1 the input box was used (above)"; exit 1; }
  echo "ok: every text on Settings shows whole $1 the input box was used, at font $scale and 1.3"
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$here/android-type-link.sh" "$invite"
tap pair
for _ in $(seq 1 60); do xml | grep -q 'resource-id="space-spfit"' && break; sleep 1; done
tap settings; sleep 2
pages before
back_home
tap space-spfit; sleep 3
xml | grep -q 'resource-id="compose-input"' || tap to-box
tap compose-input; sleep 3
"$adb" shell dumpsys input_method | grep -q 'mInputShown=true' || { echo "FAIL: the input box did not take the keyboard"; exit 1; }
back_home
tap settings; sleep 2
pages after
