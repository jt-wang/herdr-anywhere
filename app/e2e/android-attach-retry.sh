#!/bin/sh
# Real Android app, Companion and herdr-core. Only terminal.attach replies are held: the connection and its
# heartbeats stay live, so this cannot pass merely by exercising socket reconnect. The same pane must recover.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-attach-retry.sh APK COMPANION [screenshots directory]
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard android-attach
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the test device}"
apk=$1; bin=$2; shot=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$app/e2e/adb-note.sh"; adb_note "$adb"
. "$app/e2e/android-lib.sh"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=io.jingtao.herdranywhere
work=$(mktemp -d); run=$(mktemp -d /tmp/hmatXXXX)
port=7781; while nc -z 127.0.0.1 "$port" 2>/dev/null || nc -z 127.0.0.1 "$((port + 1))" 2>/dev/null; do port=$((port + 2)); done
proxy=$((port + 1))
mkdir -p "$work/u/src/grove" "$work/state" "$work/config" "$work/data"
: > "$work/u/.zshrc"; echo skip_global_compinit=1 > "$work/u/.zshenv"
(cd "$app/../companion" && go build -o "$work/proxy" ./internal/testfake/attachproxy)
export HOME="$work/u" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export HC_SESSION="hmat-$$" HC_SOCKET= SHELL=/bin/zsh GIT_CONFIG_GLOBAL="$work/u/.gitconfig"
"$hc" run --bg --keep --cwd "$work/u/src/grove" --label herdr.space=spattach --label herdr.tab=tbattach >/dev/null
term=$("$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')
cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    dump; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -45 | sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g' || true
    curl -fsS "http://127.0.0.1:$proxy/stats" || true
    tail -15 "$work/proxy.log" "$work/serve.log" 2>/dev/null || true
  fi
  [ -n "${proxy_pid:-}" ] && kill "$proxy_pid" 2>/dev/null || true
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null || true
  "$hc" server stop >/dev/null 2>&1 || true
  rm -rf "$work" "$run"; autostart_guard_end || exit 1
}
trap 'adb_exit $? cleanup' EXIT; trap 'exit 130' INT; trap 'exit 143' TERM
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$work/companion" > "$work/serve.log" 2>&1 & serve_pid=$!
"$work/proxy" -listen "127.0.0.1:$proxy" -upstream "http://127.0.0.1:$port" > "$work/proxy.log" 2>&1 & proxy_pid=$!
for _ in $(seq 1 30); do curl -fsS "http://127.0.0.1:$proxy/stats" >/dev/null 2>&1 && break; sleep 1; done
# 10.0.2.2 is Android's fixed emulator-to-host address, not a local machine's address.
invite=$("$bin" pair --url "http://10.0.2.2:$proxy" --state "$work/companion" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo 'FAIL: no test invite'; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$app/e2e/android-type-link.sh" "$invite"; tap pair
tap space-spattach
# Use the terminal rather than the first-open chat suggestion, if present.
xy=$(at show-terminal); [ -z "$xy" ] || "$adb" shell input tap $xy
"$hc" send "$term" 'printf "\033[H\033[2JATTACHVISIBLE\n"'
sleep 2
capture() {
  "$adb" exec-out screencap -p > "$work/screen.png"
  tesseract "$work/screen.png" stdout 2>/dev/null | grep -q "$2" || { echo "FAIL: $1 does not show $2"; exit 1; }
  [ -z "$shot" ] || { mkdir -p "$shot"; cp "$work/screen.png" "$shot/$1.png"; }
}
control() { curl -fsS "http://127.0.0.1:$proxy/fault?mode=$1" >/dev/null; }
live() {
  for _ in $(seq 1 40); do
    dump; screen=$("$adb" shell cat /sdcard/hm.xml)
    if printf '%s' "$screen" | grep -q "resource-id=\"terminal-$term\"" && ! printf '%s' "$screen" | grep -q 'resource-id="terminal-state"'; then return; fi
    sleep 1
  done
  echo 'FAIL: terminal did not attach without leaving the pane'; exit 1
}
live; capture live ATTACHVISIBLE
control hold; control drop
sleep 11
for _ in $(seq 1 25); do
  dump; screen=$("$adb" shell cat /sdcard/hm.xml)
  printf '%s' "$screen" | grep -q 'resource-id="terminal-retry"' && break
  sleep 1
done
printf '%s' "$screen" | grep -q 'resource-id="terminal-retry"' || { echo 'FAIL: timed-out attachment has no in-pane retry'; exit 1; }
printf '%s' "$screen" | grep -q 'Reconnecting' || { echo 'FAIL: timed-out attachment does not say Reconnecting'; exit 1; }
capture timeout ATTACHVISIBLE
# No tap, navigation or app restart: releasing only the attach replies must restore this terminal.
control pass; live
"$hc" send "$term" 'printf "RECOVEREDOUTPUT\n"'; sleep 2
capture recovered RECOVEREDOUTPUT
tap compose-input; "$adb" shell input text "'touch attach-recovered'"; tap compose-send
for _ in $(seq 1 15); do [ -e "$work/u/src/grove/attach-recovered" ] && break; sleep 1; done
[ -e "$work/u/src/grove/attach-recovered" ] || { echo 'FAIL: recovered pane cannot send to the terminal'; exit 1; }
echo 'ok: attach timeout, live heartbeat connection and automatic in-pane recovery'
# A subsequent socket drop must recover too, still without closing this pane.
control drop; sleep 2; live
tap compose-input; "$adb" shell input text "'touch attach-after-drop'"; tap compose-send
for _ in $(seq 1 15); do [ -e "$work/u/src/grove/attach-after-drop" ] && break; sleep 1; done
[ -e "$work/u/src/grove/attach-after-drop" ] || { echo 'FAIL: socket reconnect did not restore terminal input'; exit 1; }
"$hc" send "$term" 'printf "RECONNECTEDOUTPUT\n"'; sleep 2
capture reconnected RECONNECTEDOUTPUT
curl -fsS "http://127.0.0.1:$proxy/stats"
echo 'PASS: terminal attach timeout and socket drop recover without reopening the pane'
