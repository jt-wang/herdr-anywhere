#!/bin/sh
# The Android app on a device or emulator, paired and connected through TailPocket over Tailcat's real relays: an
# isolated herdr-core with a fake project, the companion with `serve --tailcat` on this computer's loopback only, and
# the invite `herdr-anywhere pair` prints typed into the pairing field. Passes when the companion logs the phone's binary
# connection, the home screen lists the project and no TCP connection reaches the companion's loopback port (an
# emulator could reach it as 10.0.2.2); reopened, the app connects again without pairing; then, revoked, the /hc
# request ends with a log line naming a Tailcat address, the app asks to pair again, and then the companion cuts the
# phone off Tailcat.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-tailcat.sh [apk] [screenshot.png]
# Needs the internet, go, hc and the Android SDK.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=${1:-$app/android/build/Herdr.apk}
shot=${2:-}
# What a failure prints and keeps of the companion's log: no token, and every address masked except the loopback's, so
# a request from this computer reads as local.
mask='s/token=[^ &]+/token=…/; s/ (127\.0\.0\.1|::1) (GET|POST)/ @loopback:\1@ \2/; s/[0-9a-f:.]{7,} (GET|POST)/<addr> \1/; s/@loopback:([^@]*)@/\1/'
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
export HC_SESSION="hmatc-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spatc --label herdr.tab=tbatc >/dev/null
cleanup() {
  rc=$?
  # A failure keeps the companion's whole log: beside the screenshot, or in a temporary file when none was asked for.
  if [ "$rc" -ne 0 ] && [ -s "$work/serve.log" ]; then
    kept=${shot:+${shot%.*}-companion.log}
    kept=${kept:-$(mktemp -t android-tailcat-companion)}
    sed -E "$mask" "$work/serve.log" > "$kept" && echo "companion log: $kept"
  fi
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
"$work/herdr-anywhere" serve --tailcat --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0
until grep -q "listening on Tailcat" "$work/serve.log" 2>/dev/null; do
  i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: serve --tailcat did not start"; tail -5 "$work/serve.log"; exit 1; }
  sleep 0.1
done
invite=$("$work/herdr-anywhere" pair --state "$state" 2>/dev/null | grep -o 'herdr1:[A-Za-z0-9_-]*' | head -1)
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
[ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot"
if [ -z "$ok" ]; then
  echo "FAIL: within 120 s, not connected over binary through Tailcat, or home does not list acme-api"
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep 'resource-id="pair-error"' | sed -E 's/.*text="([^"]*)".*/  phone says: \1/' || true
  grep -vE " GET /assets/" "$work/serve.log" | tail -8 | sed -E "$mask"
  exit 1
fi
# Through Tailcat, the companion's side of the connection lives in its Tailcat node: no TCP connection reaches its
# loopback port.
if lsof -nP -a -p "$serve_pid" -iTCP:"$port" -sTCP:ESTABLISHED >/dev/null 2>&1; then
  echo "FAIL: the app reached the companion on the loopback, not through Tailcat"; exit 1
fi
# What Settings says about this tunnel: its name, and the route it measures. HM_E2E_ROUTE=relay makes the companion
# send only through the relay (TS_DEBUG_ALWAYS_USE_DERP), so the other answer is seen too.
route_word='Direct to your computer'; [ "${HM_E2E_ROUTE:-direct}" = relay ] && route_word='Through a relay'
tap settings
"$adb" shell input swipe 500 1500 500 500 300
seen=""
for _ in $(seq 1 40); do
  if screen_has "$route_word" && screen_has 'End-to-end encrypted tunnel'; then seen=1; break; fi
  sleep 1
done
[ -n "$seen" ] || {
  echo "FAIL: Settings does not show the tunnel and \"$route_word\""
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -o ' text="[^"]\+"' | head -30
  exit 1
}
echo "Settings shows the tunnel and: $route_word"
# Opened again, the app connects without pairing.
"$adb" shell am force-stop "$id"
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
again=""
for _ in $(seq 1 60); do
  if [ "$(grep -c "device .* connected (binary," "$work/serve.log")" -ge 2 ] && screen_has acme-api; then again=1; break; fi
  sleep 1
done
[ -n "$again" ] || { echo "FAIL: opened again, the app did not connect within 60 s"; exit 1; }
# Revoking the phone closes its socket, which ends the /hc request; the companion logs it then, from the phone's
# Tailcat address.
device=$("$work/herdr-anywhere" devices --state "$state" | awk 'NR == 2 { print $1 }')
"$work/herdr-anywhere" revoke --state "$state" "$device" >/dev/null
for _ in $(seq 1 30); do grep -qE " GET /hc " "$work/serve.log" && break; sleep 1; done
grep -qE " GET /hc " "$work/serve.log" || { echo "FAIL: the companion logged no /hc request after the revoke"; exit 1; }
if grep -E " GET /hc " "$work/serve.log" | grep -qE "^[^ ]+ [^ ]+ (127\.0\.0\.1|::1) "; then
  echo "FAIL: the companion logged the /hc request from the loopback"; exit 1
fi
for _ in $(seq 1 30); do screen_has 'This computer no longer knows this phone' && break; sleep 1; done
screen_has 'This computer no longer knows this phone' || { echo "FAIL: after the revoke the app does not ask to pair again"; exit 1; }
# Then the companion cuts the phone off Tailcat too: only once its socket has closed, as cutting it off first would drop
# the close that says to pair again.
for _ in $(seq 1 15); do grep -q "tailcat: revoked phone" "$work/serve.log" && break; sleep 1; done
grep -q "tailcat: revoked phone" "$work/serve.log" || { echo "FAIL: the revoked phone is still on Tailcat 15 s after it was told"; exit 1; }
echo "PASS: the Android app paired from the invite, connected over binary through Tailcat (home listed acme-api ${took} s after launch), connected again when reopened; revoked, it asks to pair again and is cut off Tailcat"
