#!/bin/sh
# The iOS app on a simulator, paired and connected through TailPocket over Tailcat's real relays: an isolated
# herdr-core with a fake project, the companion with `serve --tailcat`, and the invite `herdr-anywhere pair` prints fed
# to the app (a script cannot tap the simulator, so ios-e2e.tsx opens the app at it, as e2e/ios.sh does with a link).
# Passes when the companion logs the phone's binary connection, the home screen lists the project and no TCP
# connection reaches the companion's loopback port (where it listens too); reopened, the app connects again without
# pairing; then, revoked, the /hc request ends with a log line naming a Tailcat address, the app asks to pair again, and
# then the companion cuts the phone off Tailcat.
# Usage: SIM_UDID=<udid> e2e/ios-tailcat.sh [screenshot.png]
# Needs the internet, go, nsh, hc, idb, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shot=${1:-}
# What a failure prints and keeps of the companion's log: no token, and every address masked except the loopback's, so
# a request from this computer reads as local.
mask='s/token=[^ &]+/token=…/; s/ (127\.0\.0\.1|::1) (GET|POST)/ @loopback:\1@ \2/; s/[0-9a-f:.]{7,} (GET|POST)/<addr> \1/; s/@loopback:([^@]*)@/\1/'
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
# The companion's state holds the Tailcat identity (private keys); its control socket path must stay under 104 bytes.
state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
placeholder=$(cat "$app/e2e/pair.ts")
opens=$(cat "$app/e2e/open-settings.ts")
cleanup() {
  rc=$?
  # A failure keeps the companion's whole log: beside the screenshot, or in a temporary file when none was asked for.
  if [ "$rc" -ne 0 ] && [ -s "$work/serve.log" ]; then
    kept=${shot:+${shot%.*}-companion.log}
    kept=${kept:-$(mktemp -t ios-tailcat-companion)}
    sed -E "$mask" "$work/serve.log" > "$kept" && echo "companion log: $kept"
  fi
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  HC_SESSION="hmtc-$$" HC_SOCKET="" HOME="$work/home" XDG_RUNTIME_DIR="$run" "$hc" server stop >/dev/null 2>&1 || true
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  printf "%s\n" "$opens" > "$app/e2e/open-settings.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
}
trap cleanup EXIT INT TERM

# Tailcat's pairing window lasts 2 minutes, so the app is built once before it opens; the second build, with the
# invite, is incremental.
"$app/ios/build.sh" ios-e2e.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null

(
  export HC_SESSION="hmtc-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
  export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
  export GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
  "$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=sptc --label herdr.tab=tbtc >/dev/null
  [ "${HM_E2E_ROUTE:-direct}" = relay ] && export TS_DEBUG_ALWAYS_USE_DERP=1
  exec "$work/herdr-anywhere" serve --tailcat --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1
) &
serve_pid=$!
i=0
until grep -q "listening on Tailcat" "$work/serve.log" 2>/dev/null; do
  i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: serve --tailcat did not start"; tail -5 "$work/serve.log"; exit 1; }
  sleep 0.1
done
invite=$("$work/herdr-anywhere" pair --state "$state" 2>/dev/null | grep -o 'herdr1:[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: pair printed no invite"; exit 1; }
printf "export const PAIR: string = '%s'\n" "$invite" > "$app/e2e/pair.ts"
printf "export const OPEN_SETTINGS_ON_LAUNCH: number = 3\n" > "$app/e2e/open-settings.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh with the invite"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
printf "%s\n" "$opens" > "$app/e2e/open-settings.ts"

xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
started=$(date +%s)
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null

screen() { perl -e 'alarm 60; exec @ARGV' idb ui describe-all --udid "$SIM_UDID" 2>/dev/null; }
ok=""
for _ in $(seq 1 120); do
  if grep -q "device .* connected (binary," "$work/serve.log" && screen | grep -q '"AXLabel":"acme-api"'; then ok=1; break; fi
  sleep 1
done
took=$(( $(date +%s) - started ))
[ -n "$shot" ] && perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "$shot" >/dev/null 2>&1
if [ -z "$ok" ]; then
  echo "FAIL: within 120 s, not connected over binary through Tailcat, or home does not list acme-api"
  screen | grep -o '"AXLabel":"[^"]*"' | head -20 | sed 's/^/  on screen: /'
  grep -vE " GET /assets/" "$work/serve.log" | tail -8 | sed -E "$mask"
  exit 1
fi
screen | grep -q '"AXIdentifier":"pair"\|"AXUniqueId":"pair"' && { echo "FAIL: the pairing screen is still up"; exit 1; }
# Through Tailcat, the companion's side of the connection lives in its Tailcat node: no TCP connection reaches its
# loopback port.
if lsof -nP -a -p "$serve_pid" -iTCP:"$port" -sTCP:ESTABLISHED >/dev/null 2>&1; then
  echo "FAIL: the app reached the companion on the loopback, not through Tailcat"; exit 1
fi
# Opened again, the app connects without pairing: from its saved link and TailPocket's, kept in the Keychain.
xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
again=""
for _ in $(seq 1 60); do
  if [ "$(grep -c "device .* connected (binary," "$work/serve.log")" -ge 2 ] && screen | grep -q '"AXLabel":"acme-api"'; then again=1; break; fi
  sleep 1
done
[ -n "$again" ] || { echo "FAIL: opened again, the app did not connect within 60 s"; exit 1; }
# What Settings says about this tunnel: its name, and the route it measures. This third launch opens there
# (ios-e2e.tsx). HM_E2E_ROUTE=relay makes the companion send only through the relay (TS_DEBUG_ALWAYS_USE_DERP), so the
# other answer is seen too.
route_word='Direct to your computer'; [ "${HM_E2E_ROUTE:-direct}" = relay ] && route_word='Through a relay'
xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
seen=""
for _ in $(seq 1 60); do
  now=$(screen)
  if printf %s "$now" | grep -q "$route_word" && printf %s "$now" | grep -q 'End-to-end encrypted tunnel'; then seen=1; break; fi
  sleep 1
done
[ -n "$seen" ] || { echo "FAIL: Settings does not show the tunnel and \"$route_word\""; screen | grep -o '"AXLabel":"[^"]*"' | head -30; exit 1; }
echo "Settings shows the tunnel and: $route_word"
# Revoking the phone closes its socket, which ends the /hc request; the companion logs it then:
# "<date> <time> <remote address> GET /hc 101 …", the remote address being the phone's Tailcat address.
device=$("$work/herdr-anywhere" devices --state "$state" | awk 'NR == 2 { print $1 }')
"$work/herdr-anywhere" revoke --state "$state" "$device" >/dev/null
for _ in $(seq 1 30); do grep -qE " GET /hc " "$work/serve.log" && break; sleep 1; done
grep -qE " GET /hc " "$work/serve.log" || { echo "FAIL: the companion logged no /hc request after the revoke"; exit 1; }
if grep -E " GET /hc " "$work/serve.log" | grep -qE "^[^ ]+ [^ ]+ (127\.0\.0\.1|::1) "; then
  echo "FAIL: the companion logged the /hc request from the loopback"; exit 1
fi
for _ in $(seq 1 30); do screen | grep -q 'This computer no longer knows this phone' && break; sleep 1; done
screen | grep -q 'This computer no longer knows this phone' || { echo "FAIL: after the revoke the app does not ask to pair again"; screen | grep -o '"AXLabel":"[^"]*"' | head -30; grep -vE " GET /assets/" "$work/serve.log" | tail -6 | sed -E "$mask"; exit 1; }
# Then the companion cuts the phone off Tailcat too: only once its socket has closed, as cutting it off first would drop
# the close that says to pair again.
for _ in $(seq 1 15); do grep -q "tailcat: revoked phone" "$work/serve.log" && break; sleep 1; done
grep -q "tailcat: revoked phone" "$work/serve.log" || { echo "FAIL: the revoked phone is still on Tailcat 15 s after it was told"; exit 1; }
echo "PASS: the iOS app paired from the invite, connected over binary through Tailcat (home listed acme-api ${took} s after launch), connected again when reopened; revoked, it asks to pair again and is cut off Tailcat"
