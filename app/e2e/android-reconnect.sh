#!/bin/sh
# What the Android app says when it can't reach the computer through TailPocket, over Tailcat's real relays.
# Pairs the app with a companion of its own (an isolated herdr-core with a fake project, `serve --tailcat` on the
# loopback only), then:
#   1. stops the companion (the computer restarted and nothing started it): the app must say to run
#      `herdr-anywhere start` and offer Pair again; the companion started again on the same state, the app connects by
#      itself with its old pairing;
#   2. with the app closed, takes the phone off the computer (revoke): opened, the app gets the same nothing back as in 1
#      (tailcat_lost_probe_test.go measured it), so it must show the same; Pair again opens pairing with Back, and a new
#      invite pairs.
# Screenshots of each step in the light and the dark theme go to the output directory, with the banner's words.
# HM_E2E_EXPECT=old only takes the screenshots (the app before the fix, which says neither).
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-reconnect.sh <apk> <output dir>
# Needs the internet, go, hc and the Android SDK.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=${1:?apk}
out=${2:?output dir}
mkdir -p "$out"
expect_new=1; [ "${HM_E2E_EXPECT:-new}" = old ] && expect_new=""
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
if [ -n "${HERDR_ANYWHERE_BIN:-}" ]; then cp "$HERDR_ANYWHERE_BIN" "$work/herdr-anywhere"; else (cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere); fi
run=$(mktemp -d /tmp/hmXXXX)
state=$(mktemp -d /tmp/hmrcXXXX)
mkdir -p "$work/hm/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/hm/.zshrc"
echo skip_global_compinit=1 > "$work/hm/.zshenv"
export HC_SESSION="hmarc-$$" HC_SOCKET="" HOME="$work/hm" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export GIT_CONFIG_GLOBAL="$work/hm/.gitconfig"
"$hc" run --bg --keep --cwd "$work/hm/src/acme-api" --label herdr.space=sparc --label herdr.tab=tbarc >/dev/null
night=$("$adb" shell cmd uimode night | sed -E 's/.*: //')
cleanup() {
  rc=$?
  [ "$rc" -ne 0 ] && [ -s "$work/serve.log" ] && sed -E 's/token=[^ &]+/token=…/' "$work/serve.log" > "$out/companion.log"
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  "$adb" shell cmd uimode night "$night" >/dev/null 2>&1 || true
  "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
}
trap cleanup EXIT INT TERM

"$adb" install -r "$apk" >/dev/null
"$adb" shell pm clear "$id" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

serve() {
  "$work/herdr-anywhere" serve --tailcat --listen "127.0.0.1:$port" --hc "$hc" --state "$state" >> "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0
  until [ "$(grep -c "listening on Tailcat" "$work/serve.log" 2>/dev/null)" -ge "$1" ]; do
    i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: serve --tailcat did not start"; tail -5 "$work/serve.log"; exit 1; }
    sleep 0.1
  done
}
invite() { "$work/herdr-anywhere" pair --state "$state" 2>/dev/null | grep -o 'herdr1:[A-Za-z0-9_-]*' | head -1; }
dump() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml; }
at() {
  dump | tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
screen_has() { dump | grep -qF "$1"; }
wait_for() { # text, seconds
  for _ in $(seq 1 "$2"); do screen_has "$1" && return 0; sleep 1; done
  return 1
}
type_invite() {
  tap pair-link
  rest=$1
  while [ -n "$rest" ]; do
    piece=$(printf %s "$rest" | cut -c1-80); rest=$(printf %s "$rest" | cut -c81-)
    "$adb" shell input text "'$piece'" < /dev/null
    sleep 0.5
  done
  tap pair
}
# shot <name>: the screen in the light and the dark theme, and every text on it.
shot() {
  for mode in no yes; do
    "$adb" shell cmd uimode night "$mode" >/dev/null
    sleep 2
    theme=light; [ "$mode" = yes ] && theme=dark
    "$adb" exec-out screencap -p > "$out/$1-$theme.png"
  done
  dump | tr '>' '\n' | grep -o ' text="[^"]\+"' | sed -E 's/ text="(.*)"/\1/' > "$out/$1.txt"
  echo "  $1: $(tr '\n' '|' < "$out/$1.txt" | cut -c1-300)"
}
must() { [ -z "$expect_new" ] && return 0; screen_has "$1" || { echo "FAIL: $2 (no \"$1\" on screen)"; exit 1; }; }

serve 1
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
type_invite "$(invite)"
wait_for acme-api 120 || { echo "FAIL: not connected within 120 s"; exit 1; }
shot 0-connected

echo "1. the companion stops"
kill "$serve_pid"; wait "$serve_pid" 2>/dev/null || true; serve_pid=""
stopped=$(date +%s)
for _ in $(seq 1 60); do dump | grep -q 'resource-id="conn-banner"' && break; sleep 1; done
# The first retries back off to 15 s: wait one round more for what the banner settles on.
sleep 20
echo "  banner after $(($(date +%s) - stopped)) s"
shot 1-companion-stopped
must 'herdr-anywhere start' "with the companion stopped the app doesn't say what to run"
must 'resource-id="pair-again"' "with the companion stopped the app offers no Pair again"
serve 2
wait_for acme-api 90 || { echo "FAIL: the companion back on the same state, the app did not connect by itself within 90 s"; exit 1; }
dump | grep -q 'resource-id="conn-banner"' && sleep 5
shot 1b-companion-back
echo "  back: the old pairing connects by itself"

echo "2. the phone is taken off the computer while the app is closed"
"$adb" shell am force-stop "$id"
device=$("$work/herdr-anywhere" devices --state "$state" | awk 'NR == 2 { print $1 }')
"$work/herdr-anywhere" revoke --state "$state" "$device" >/dev/null
for _ in $(seq 1 15); do grep -q "tailcat: revoked phone" "$work/serve.log" && break; sleep 1; done
grep -q "tailcat: revoked phone" "$work/serve.log" || { echo "FAIL: the phone is still on Tailcat"; exit 1; }
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
for _ in $(seq 1 60); do dump | grep -qE 'resource-id="(conn-banner|pair-link)"' && break; sleep 1; done
sleep 20
shot 2-phone-removed
must 'herdr-anywhere start' "with the phone removed the app doesn't say what to run"
must 'resource-id="pair-again"' "with the phone removed the app offers no Pair again"
[ -z "$expect_new" ] && { echo "DONE (screenshots only): $out"; exit 0; }
tap pair-again
wait_for 'resource-id="pair-back"' 10 || { echo "FAIL: Pair again opened no pairing screen with Back"; exit 1; }
shot 2b-pair-again
type_invite "$(invite)"
wait_for acme-api 120 || { echo "FAIL: paired again, the app did not connect within 120 s"; exit 1; }
shot 2c-paired-again
echo "PASS: stopped companion and removed phone each say to run herdr-anywhere start and offer Pair again; the old pairing comes back by itself; pairing again connects"
