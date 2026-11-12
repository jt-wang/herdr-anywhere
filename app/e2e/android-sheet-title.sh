#!/bin/sh
# The new-tab sheet's title reads whole each time it opens, for a space on another machine. Its title was drawn cut to
# "Add an" (of "Add an agent") once the sheet had opened before. The title is read off the screen with tesseract:
# the view tree holds the whole text even when the screen shows part of it. The first opening is the reader's own
# check: it must read the whole title there, or the reader is what failed.
# The other machine is this computer under another home, reached with `hc machine add --via "env HOME=… sh -c"`.
# Needs hc, tesseract and python3 with Pillow (e2e/ci/tools.sh).
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-sheet-title.sh APK COMPANION SHOTS
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shots=$3
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); boxrun=$(mktemp -d /tmp/hbXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$shots" "$work/me/src/acme-api" "$work/box/src/acme-web" "$work/state" "$work/config" "$work/data" "$work/boxstate"
: > "$work/me/.zshrc"
export HC_SESSION="hmtitle-$$" HC_SOCKET="" HOME="$work/me" SHELL=/bin/sh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/me/.gitconfig"
box="env HOME=$work/box XDG_RUNTIME_DIR=$boxrun XDG_STATE_HOME=$work/boxstate HC_SESSION=hmtitlebox-$$"
serve_pid=""
cleanup() {
  [ -n "$serve_pid" ] && kill "$serve_pid" 2>/dev/null
  "$adb" shell cmd uimode night no >/dev/null 2>&1 || true
  "$hc" server stop >/dev/null 2>&1 || true
  [ -x "$work/box/.local/bin/hc" ] && $box "$work/box/.local/bin/hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$boxrun" "$state"
}
trap cleanup EXIT INT TERM

"$hc" machine add --via "$box sh -c" --name box --install >/dev/null
"$hc" run --on box --bg --keep --cwd "$work/box/src/acme-web" --label herdr.space=spweb --label herdr.tab=tbweb -- sh -i >/dev/null
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"$adb" shell cmd uimode night no >/dev/null

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
tap() {
  b=""; for _ in $(seq 1 30); do b=$(xml | bounds "$1\""); [ -n "$b" ] && break; sleep 1; done
  [ -n "$b" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-80)'" < /dev/null; rest=$(printf %s "$rest" | cut -c81-); sleep 0.5; done
tap pair
b=""; for _ in $(seq 1 30); do b=$(xml | bounds 'add-agent-box/spweb"'); [ -n "$b" ] && break; sleep 1; done
if [ -z "$b" ]; then
  echo "FAIL: home has no space on box"
  "$adb" exec-out screencap -p > "$shots/fail-home.png"
  echo "on screen (ids): $(xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | sed -E 's/resource-id="(.*)"/\1/' | head -40 | tr '\n' ' ')"
  echo "on screen (texts): $(xml | tr '>' '\n' | grep -oE ' text="[^"]+"' | sed -E 's/ text="(.*)"/\1/; s/#pair=[A-Za-z0-9_-]+/#pair=…/' | head -30 | tr '\n' '|')"
  echo "hc ls: $("$hc" ls --json 2>&1 | python3 -c 'import json,sys; print([(t["address"], (t.get("labels") or {}).get("herdr.space")) for t in json.load(sys.stdin)["terminals"]])' 2>&1)"
  echo "hc machine ls: $("$hc" machine ls 2>&1 | head -5 | tr '\n' ' ')"
  echo "serve log: $(tail -5 "$work/serve.log" | sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g' | tr '\n' ' ')"
  exit 1
fi

# One opening of the sheet: the title as tesseract reads it off the screen, from the left of the title's view to the
# sheet's right edge (a cut title's view is narrower than its text).
failed=0
open() { # $1 the shot's name
  tap add-agent-box/spweb; sleep 3
  t=""; for _ in $(seq 1 10); do t=$(xml | tr '>' '\n' | grep -F ' text="Add an agent"' | tail -1); [ -n "$t" ] && break; sleep 1; done
  [ -n "$t" ] || { echo "FAIL: $1: the sheet has no title Add an agent"; "$adb" exec-out screencap -p > "$shots/$1.png"; exit 1; }
  set -- "$1" $(printf '%s' "$t" | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/')
  "$adb" exec-out screencap -p > "$shots/$1.png"
  python3 - "$shots/$1.png" "$shots/$1-title.png" "$2" "$3" "$4" "$5" <<'PY'
import sys
from PIL import Image, ImageOps, ImageStat
shot, out, left, top, right, bottom = sys.argv[1], sys.argv[2], *map(int, sys.argv[3:])
im = Image.open(shot).convert('L')
crop = im.crop((max(0, left - 20), max(0, top - 10), im.width - max(0, left - 20), min(im.height, bottom + 10)))
if ImageStat.Stat(crop).mean[0] < 128:  # dark: tesseract reads dark letters on light
    crop = ImageOps.invert(crop)
crop.resize((crop.width * 2, crop.height * 2)).save(out)
PY
  read_=$(tesseract "$shots/$1-title.png" - --psm 7 2>/dev/null | tr -d '\n')
  seen=$(printf %s "$read_" | tr -dc 'A-Za-z' | tr 'A-Z' 'a-z')
  echo "$1: title view at $2,$3 to $4,$5 ($(( $4 - $2 )) px wide); read: $read_"
  "$adb" shell input keyevent 4; sleep 2
}
whole() { [ "$seen" = addanagent ]; }

open 1-first
whole || { echo "FAIL: the first opening's title reads \"$read_\", not Add an agent: the reader, not the sheet, is what fails here"; exit 1; }
echo "ok: the first opening's title reads whole (the reader works)"
open 2-second
whole && echo "ok: the second opening's title reads whole" || { echo "FAIL: the second opening's title reads \"$read_\": cut"; failed=1; }
"$adb" shell cmd uimode night yes >/dev/null; sleep 3
open 3-dark
whole && echo "ok: in dark, its title reads whole" || { echo "FAIL: in dark, its title reads \"$read_\": cut"; failed=1; }
"$adb" shell cmd uimode night no >/dev/null; sleep 3
open 4-light-again
whole && echo "ok: back in light, its title reads whole" || { echo "FAIL: back in light, its title reads \"$read_\": cut"; failed=1; }
exit $failed
