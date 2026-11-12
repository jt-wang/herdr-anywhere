#!/bin/sh
# The new-tab sheet's title reads whole each time it opens, for a space on another machine. Its title was drawn cut to
# "Add an" (of "Add an agent") once the sheet had opened before, on a second machine's space after the first's. The
# sheet of the space on box opens four times (light, again, dark, light), then the one on dock, then once more after a
# terminal was started from it (from then on every opening drew it cut), in light and in dark. The title is read off
# the screen with tesseract: the view tree holds the whole text even when the screen shows part of it. The first
# opening is the reader's own check: it must read the whole title there, or the reader is what failed.
# Each other machine is this computer under another home and PATH, reached with `hc machine add --via "env HOME=… sh -c"`;
# its one agent is e2e/fake-claude, built here as claude. Needs go, hc, tesseract and python3 with Pillow.
# e2e/ios-sheet-title.sh is the same check on an iPhone simulator.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-sheet-title.sh APK COMPANION SHOTS
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shots=$3
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(cd "$(mktemp -d)" && pwd -P); run=$(mktemp -d /tmp/hmXXXX); boxrun=$(mktemp -d /tmp/hbXXXX); dockrun=$(mktemp -d /tmp/hdXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
# Every machine's folder is under this computer's home (its real path, as the machines report folders), so the sheet shows
# it as ~/src/<name>, a made-up name, never a path of this computer.
mkdir -p "$shots" "$work/me/src/acme-api" "$work/me/src/acme-web" "$work/state" "$work/config" "$work/data" "$work/boxstate" "$work/me/src/acme-docs" "$work/dockstate"
: > "$work/me/.zshrc"
export HC_SESSION="hmtitle-$$" HC_SOCKET="" HOME="$work/me" SHELL=/bin/sh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/me/.gitconfig"
box="env HOME=$work/box PATH=$work/box/bin:$work/box/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin XDG_RUNTIME_DIR=$boxrun XDG_STATE_HOME=$work/boxstate HC_SESSION=hmtitlebox-$$"
dock="env HOME=$work/dock PATH=$work/dock/bin:$work/dock/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin XDG_RUNTIME_DIR=$dockrun XDG_STATE_HOME=$work/dockstate HC_SESSION=hmtitledock-$$"
serve_pid=""
cleanup() {
  [ -n "$serve_pid" ] && kill "$serve_pid" 2>/dev/null
  "$adb" shell cmd uimode night no >/dev/null 2>&1 || true
  "$hc" server stop >/dev/null 2>&1 || true
  [ -x "$work/box/.local/bin/hc" ] && $box "$work/box/.local/bin/hc" server stop >/dev/null 2>&1 || true
  [ -x "$work/dock/.local/bin/hc" ] && $dock "$work/dock/.local/bin/hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" "${boxrun:?}" "${dockrun:?}" "${state:?}"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

# Each machine has one agent, the repository's stand-in Claude Code, and only system folders besides: the menu lists it
# and nothing this computer has installed.
for m in box dock; do mkdir -p "$work/$m/bin"; (cd "$app/e2e/fake-claude" && GO111MODULE=off go build -o "$work/$m/bin/claude" main.go); done
"$hc" machine add --via "$box sh -c" --name box --install >/dev/null
"$hc" run --on box --bg --keep --cwd "$work/me/src/acme-web" --label herdr.space=spweb --label herdr.tab=tbweb -- sh -i >/dev/null
"$hc" machine add --via "$dock sh -c" --name dock --install >/dev/null
"$hc" run --on dock --bg --keep --cwd "$work/me/src/acme-docs" --label herdr.space=spdocs --label herdr.tab=tbdocs -- sh -i >/dev/null
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"$adb" shell cmd uimode night no >/dev/null

# What a failure prints hides the invite's code: a mistyped invite can leave one the #pair= pattern misses (#pai=…, or
# pair=… with no #), so everything after a # and after a pair= goes.
mask() { sed -E 's/#.*/#…/; s/pair=[^ |"]*/pair=…/g'; }
xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
tap() {
  b=""; for _ in $(seq 1 30); do b=$(xml | bounds "$1\""); [ -n "$b" ] && break; sleep 1; done
  [ -n "$b" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
# A slow emulator drops typed characters (the companion then refuses the code): android-type-link.sh types the invite
# in 10-character pieces and reads the field back until it holds all of it.
"$(dirname "$0")/android-type-link.sh" "$invite"
# At 360 dp the keyboard covers Pair, and the tap lands on the keyboard: put it away first, with Back only while it is
# up (on this screen Back would leave the app).
ime() { "$adb" shell dumpsys input_method | grep -oE 'mInputShown=(true|false)' | head -1 | cut -d= -f2; }
case $(ime) in
true) "$adb" shell input keyevent 4; sleep 1; [ "$(ime)" = false ] || { echo "FAIL: the keyboard stays up over Pair"; exit 1; } ;;
false) ;;
*) echo "FAIL: dumpsys input_method does not say whether the keyboard is up"; exit 1 ;;
esac
tap pair
b=""; for _ in $(seq 1 30); do x=$(xml); b=$(printf '%s' "$x" | bounds 'add-agent-box/spweb"'); [ -n "$b" ] && [ -n "$(printf '%s' "$x" | bounds 'add-agent-dock/spdocs"')" ] && break; b=""; sleep 1; done
if [ -z "$b" ]; then
  # The pairing screen shows the invite: no picture of it.
  if xml | grep -qF 'resource-id="pair-link"'; then echo "FAIL: still on the pairing screen: $(xml | tr '>' '\n' | grep -F 'resource-id="pair-error"' | grep -oE ' text="[^"]*"' | head -1 | mask)"
  else echo "FAIL: home has no space on box or on dock"; "$adb" exec-out screencap -p > "$shots/fail-home.png"; fi
  echo "on screen (ids): $(xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | sed -E 's/resource-id="(.*)"/\1/' | head -40 | tr '\n' ' ')"
  echo "on screen (texts): $(xml | tr '>' '\n' | grep -oE ' text="[^"]+"' | sed -E 's/ text="(.*)"/\1/' | mask | head -30 | tr '\n' '|')"
  echo "hc ls: $("$hc" ls --json 2>&1 | python3 -c 'import json,sys; print([(t["address"], (t.get("labels") or {}).get("herdr.space")) for t in json.load(sys.stdin)["terminals"]])' 2>&1)"
  echo "hc machine ls: $("$hc" machine ls 2>&1 | head -5 | tr '\n' ' ')"
  echo "serve log: $(tail -5 "$work/serve.log" | mask | tr '\n' ' ')"
  exit 1
fi

# One opening of the sheet: the title as tesseract reads it off the screen, from the left of the title's view to the
# sheet's right edge (a cut title's view is narrower than its text).
failed=0
open() { # $1 the shot's name, $2 the space (default the one on box)
  tap "add-agent-${2:-box/spweb}"; sleep 3
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
# Then the sheet of a space on another machine (where both cuts were seen: the second machine's, after the first's).
open 5-other-machine dock/spdocs
whole && echo "ok: on another machine's space, its title reads whole" || { echo "FAIL: on another machine's space, its title reads \"$read_\": cut"; failed=1; }
# A tab started from the sheet (Terminal), home again, then the sheet once more.
tap "add-agent-box/spweb"; sleep 3
tap new-terminal
for _ in $(seq 1 30); do xml | grep -q 'resource-id="terminal-box/' && break; sleep 1; done
for _ in 1 2 3 4; do xml | grep -q 'resource-id="terminal-' || break; "$adb" shell input keyevent 4; sleep 1.5; done
open 6-after-start
whole && echo "ok: after a tab was started from it, its title reads whole" || { echo "FAIL: after a tab was started from it, its title reads \"$read_\": cut"; failed=1; }
"$adb" shell cmd uimode night yes >/dev/null; sleep 3
open 7-after-start-dark
whole && echo "ok: after that, in dark, its title reads whole" || { echo "FAIL: after that, in dark, its title reads \"$read_\": cut"; failed=1; }
"$adb" shell cmd uimode night no >/dev/null
exit $failed
