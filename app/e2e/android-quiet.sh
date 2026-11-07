#!/bin/sh
# The Android app through TailPocket's real tunnel (as e2e/android-tailcat.sh), through what ends a phone's connection:
# in the background, off the network, frozen. After each, in front again, it has to be connected by itself, with a
# new socket, and the status has to say so. Prints what happened for each; fails if it stays on a dead socket.
# HM_QUIET_ROUTE=direct goes straight to the companion instead of through the tunnel: the same test without TailPocket.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-quiet.sh [apk] [folder for screenshots]
# Needs the internet, go, hc and the Android SDK.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=${1:-$app/android/build/Herdr.apk}
shot=${2:-}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
# HERDR_ANYWHERE_BIN runs a release's companion instead of building one.
if [ -n "${HERDR_ANYWHERE_BIN:-}" ]; then cp "$HERDR_ANYWHERE_BIN" "$work/herdr-anywhere"; else (cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere); fi
run=$(mktemp -d /tmp/hmXXXX)
# The companion's state holds the Tailcat identity (private keys); its control socket path must stay under 104 bytes.
state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
# Ubuntu's /etc/zsh/zshrc runs compinit, which can stop at a y/n question and eat what the phone types first.
echo skip_global_compinit=1 > "$work/home/.zshenv"
export HC_SESSION="hmaqt-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spatc --label herdr.tab=tbatc >/dev/null
cleanup() {
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
}
trap cleanup EXIT INT TERM

# Install first: Tailcat's pairing window lasts 2 minutes.
"$adb" install -r "$apk" >/dev/null
"$adb" shell pm clear "$id" >/dev/null
# Android 13 and later post nothing until this is allowed, and the app asks on its first connection.
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

[ "${HM_E2E_ROUTE:-direct}" = relay ] && export TS_DEBUG_ALWAYS_USE_DERP=1
if [ "${HM_QUIET_ROUTE:-tailpocket}" = direct ]; then
  "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
  serve_pid=$!
  sleep 1
  invite=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
else
  "$work/herdr-anywhere" serve --tailcat --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0
  until grep -q "listening on Tailcat" "$work/serve.log" 2>/dev/null; do
    i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: serve --tailcat did not start"; tail -5 "$work/serve.log"; exit 1; }
    sleep 0.1
  done
  invite=$("$work/herdr-anywhere" pair --state "$state" 2>/dev/null | grep -o 'herdr1:[A-Za-z0-9_-]*' | head -1)
fi
[ -n "$invite" ] || { echo "FAIL: pair printed no invite"; exit 1; }

# The centre of the view with this resource-id (an NSH testID), from the accessibility tree.
at() {
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
screen_has() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | grep -q "$1"; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
started=$(date +%s)
tap pair-link
# One long `adb shell input text` keeps only its first ~200 characters (2026-10-31: 208 of 529 in this field, 204 in
# Chrome's URL bar); in 80-character pieces the field holds all 529.
rest=$invite
while [ -n "$rest" ]; do
  piece=$(printf %s "$rest" | cut -c1-80); rest=$(printf %s "$rest" | cut -c81-)
  "$adb" shell input text "'$piece'" < /dev/null
  sleep 0.5
done
"$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
typed=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -A3 -F 'resource-id="pair-link"' | grep -F 'class="android.widget.EditText"' |
  sed -E 's/.* text="([^"]*)".*/\1/' | head -1)
[ "$typed" = "$invite" ] || { echo "FAIL: the field holds ${#typed} of the invite's ${#invite} characters"; exit 1; }
tap pair

ok=""
for _ in $(seq 1 120); do
  if grep -q "device .* connected (binary," "$work/serve.log" && screen_has acme-api; then ok=1; break; fi
  sleep 1
done
took=$(($(date +%s) - started))
if [ -z "$ok" ]; then
  echo "FAIL: within 120 s, not connected over binary through Tailcat, or home does not list acme-api"
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep 'resource-id="pair-error"' | sed -E 's/.*text="([^"]*)".*/  phone says: \1/' || true
  grep -vE " GET /assets/" "$work/serve.log" | tail -8 | sed -E 's/token=[^ &]+/token=…/; s/[0-9a-f:.]{7,} (GET|POST)/<addr> \1/'
  exit 1
fi
# Through Tailcat, the companion's side of the connection lives in its Tailcat node: no TCP connection reaches its
# loopback port.
if [ "${HM_QUIET_ROUTE:-tailpocket}" != direct ] && lsof -nP -a -p "$serve_pid" -iTCP:"$port" -sTCP:ESTABLISHED >/dev/null 2>&1; then
  echo "FAIL: the app reached the companion on the loopback, not through Tailcat"; exit 1
fi

# grep -c says 0 and fails when nothing matched, which under set -e ends the script without a word.
connects() { grep -c "device .* connected (binary," "$work/serve.log" || true; }
ends() { grep -c "device .* disconnected" "$work/serve.log" || true; }
status() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -oE 'text="(Connected|Reconnecting[^"]*|Lost[^"]*)"' | head -1; }
pid() { "$adb" shell pidof "$id" | tr -d '\r'; }
term=$("$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')
tap "space-spatc"
sleep 3
failed=0
# A big file through the same line (HM_UPLOAD_MB): the system's picker, then the app's own upload.
if [ -n "${HM_UPLOAD_MB:-}" ]; then
  big="hm-upload-${HM_UPLOAD_MB}mb.bin"
  dd if=/dev/urandom of="$work/$big" bs=1048576 count="$HM_UPLOAD_MB" 2>/dev/null
  want=$(shasum -a 256 "$work/$big" | cut -d' ' -f1)
  "$adb" push "$work/$big" "/sdcard/Download/$big" >/dev/null
  "$adb" shell am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d "file:///sdcard/Download/$big" >/dev/null
  tap_text() {
    xy=""; for _ in $(seq 1 "${2:-15}"); do "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
      xy=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "text=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}')
      [ -n "$xy" ] && break; sleep 1; done
    [ -n "$xy" ] || { echo "FAIL: nothing says $1 on screen"; "$adb" exec-out screencap -p > "${shot:-/tmp}/upload-fail.png"; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -o ' text="[^"]\+"' | head -20; exit 1; }
    "$adb" shell input tap $xy
  }
  tap attach
  sleep 1
  tap_text "File"
  sleep 3
  tap_text "$big" 8 || true
  t0=$(date +%s)
  got=""; for _ in $(seq 1 600); do
    for f in "$work"/home/.cache/herdr-anywhere/uploads/*/"$big"; do
      [ -f "$f" ] && screen_has "$big" && [ "$(stat -f %z "$f")" = "$(stat -f %z "$work/$big")" ] && { got=$f; break 2; }
    done
    sleep 1
  done
  [ -n "$got" ] || { echo "FAIL: ${HM_UPLOAD_MB} MB did not land on the computer"; ls -la "$work"/home/.cache/herdr-anywhere/uploads/* 2>/dev/null | tail -3; exit 1; }
  [ "$(shasum -a 256 "$got" | cut -d' ' -f1)" = "$want" ] || { echo "FAIL: the file that landed is not the file sent"; exit 1; }
  echo "ok: ${HM_UPLOAD_MB} MB went through the app in $(($(date +%s) - t0)) s, same bytes, and its path is in the composer"
  [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/upload.png"
  "$adb" shell rm -f "/sdcard/Download/$big"; rm -f "$work/$big"
  "$adb" shell input keyevent 4; sleep 1
fi
# One disruption: what it is, how to start and how to end it. Long enough for the companion's ping (every 30 s, 15 s to answer).
case_() {
  name=$1 start=$2 stop=$3 hold=$4
  c0=$(connects); e0=$(ends)
  eval "$start"
  sleep "$hold"
  eval "${during:-}"
  eval "$stop"
  e1=$(ends)
  fine=""; for _ in $(seq 1 30); do [ "$(connects)" -gt "$c0" ] && { fine=1; break; }; sleep 1; done
  t=$SECONDS
  # No new socket is fine only if the companion never ended the old one.
  [ "$e1" = "$e0" ] && [ -z "$fine" ] && fine=kept
  "$hc" send --force "$term" "echo marker-$name" >/dev/null 2>&1 || true
  sleep 3
  [ -n "$dir" ] && "$adb" exec-out screencap -p > "$dir/$name.png"
  echo "$name: companion ended the socket during it: $((e1 - e0)); new socket within 30 s: ${fine:-NO}"
  [ -n "$fine" ] || failed=1
}
dir=$shot
[ -z "$dir" ] || mkdir -p "$dir"
printf '%s\n' "printf '\033]7501;state=working\033\\\\'; sleep 2; printf '\033]7501;state=blocked\033\\\\'" > "$work/home/src/acme-api/hm-away.sh"
# 50 s away: is the process running, is the foreground service on, and does an agent that asks now reach the shade?
away() {
  p=$(pid)
  echo "  away 50 s: process state: $("$adb" shell ps -o S,WCHAN -p "${p:-1}" | tail -1 | tr -s ' ')"
  echo "  away 50 s: foreground service running: $("$adb" shell dumpsys activity services "$id" | grep -c 'isForeground=true' || true)"
  "$hc" send --force "$term" 'sh hm-away.sh' >/dev/null 2>&1 || true
  got=no; for _ in 1 2 3 4 5 6 7 8 9 10; do "$adb" shell dumpsys notification --noredact | grep -q "NotificationRecord.*pkg=$id .*channel=agent-needs-you" && { got=yes; break; }; sleep 1; done
  echo "  away 50 s: an agent asking reached the notification shade: $got"
}
during=away
case_ background '"$adb" shell input keyevent 3' '"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null' 50
during=
case_ offline '"$adb" shell svc wifi disable; "$adb" shell svc data disable' '"$adb" shell svc wifi enable; "$adb" shell svc data enable' 50
"$adb" root >/dev/null 2>&1; sleep 2
"$adb" wait-for-device
case_ frozen '"$adb" shell kill -STOP $(pid)' '"$adb" shell kill -CONT $(pid)' 50
[ $failed = 0 ] && echo "PASS" || { echo "FAIL: stayed on a dead socket"; grep -E "connected|disconnected" "$work/serve.log" | cut -c1-90; exit 1; }
