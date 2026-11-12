#!/bin/sh
# A picture's path in a terminal, tapped on an iOS simulator (XCUITest's taps, e2e/ios-uitest Picture): the sheet shows the picture under its full
# path, holding it puts the picture on the pasteboard, and a file named like a picture that isn't one, or a path only mentioned, opens nothing.
# The app is built from ios-e2e.tsx, which opens it at this run's pairing link (as e2e/ios.sh). Fake data only.
# The terminal is here, or with HM_PICTURE_IMAGE in a container, for screenshots to publish (picture-terminal.sh).
# Usage: SIM_UDID=<udid> [HM_PICTURE_IMAGE=alpine:latest] e2e/ios-picture.sh [folder for screenshots]
# HM_MISS_SPACE=1 makes the light run's first tap on the space miss (e2e/ios-uitest/tests/OpenSpace.swift).
# HM_PICTURE_MISS_FIRST=1 is its self-test: the dark run's first tap lands beside the path, so the suite must fail with
# the test's record of that tap (the terminal's frame, the time since the switch to dark, a second tap).
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shots=${1:-}
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7691; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/home" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
[ -z "${HM_PICTURE_IMAGE:-}" ] || export DOCKER_HOST="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}"
L() { HC_SESSION="hmiospic-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" "$@"; }
. "$app/e2e/picture-terminal.sh"
serve_pid=
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e # every step runs, even after one fails (a companion already gone)
  [ -n "$serve_pid" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  L "$hc" server stop >/dev/null 2>&1 || true
  picture_cleanup
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP
picture_terminal
L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0; until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; exit 1; }; sleep 0.1; done
link=$(L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
# The app is built from a copy of app/ whose terminal page also tells the test when the page got each touch and what it
# made of it (e2e/touch-times.ts), in a label the test reads, as the terminal runs a shell; app/ itself is not changed.
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$work/pair.ts"
built=$("$app/e2e/ios-touch-app.sh" "$work" "$work/pair.ts" label) || exit 1

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$built"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null

# The app is up and paired once the companion logs the phone connected.
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do i=$((i + 1)); [ $i -gt 60 ] && { echo "FAIL: the app never connected"; exit 1; }; sleep 1; done
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
size="$work/size"; before=$(L "$hc" get "$term" --json | python3 -c 'import json,sys; t=json.load(sys.stdin); print(t["cols"], t["rows"])')
# Each run: the XCUITest taps; meanwhile the terminal's size, once the phone has set it, goes to $size for it.
# The time of the appearance switch just before, for the test to say how long after it the tap came.
appearance() { xcrun simctl ui "$SIM_UDID" appearance "$1"; switched=$(python3 -c 'import time; print(time.time())'); }
picture() {
  rm -f "$size"; [ -n "${2:-}" ] && echo "$2" > "$size"
  miss=""; [ "$1" = dark ] && miss=${HM_PICTURE_MISS_FIRST:-}
  space_miss=""; [ "$1" = light ] && space_miss=${HM_MISS_SPACE:-}
  TEST_RUNNER_OUT="$work/picture-$1.txt" TEST_RUNNER_SHOTS="${shots:-$work}/$1" TEST_RUNNER_SIZE="$size" TEST_RUNNER_SPACE="$space" \
    TEST_RUNNER_TERMINAL="$term" TEST_RUNNER_FULL="$full" TEST_RUNNER_SWITCHED="$switched" TEST_RUNNER_MISS_FIRST="$miss" TEST_RUNNER_MISS_SPACE="$space_miss" \
    perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/Picture -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
    -resultBundlePath "$work/uitest/result-$1.xcresult" > "$work/uitest/test-$1.log" 2>&1 &
  t=$!
  [ -s "$size" ] || for _ in $(seq 1 120); do
    now=$(L "$hc" get "$term" --json | python3 -c 'import json,sys; t=json.load(sys.stdin); print(t["cols"], t["rows"])')
    [ "$now" != "$before" ] && { sleep 2; L "$hc" get "$term" --json | python3 -c 'import json,sys; t=json.load(sys.stdin); print(t["cols"], t["rows"])' > "$size"; break; }
    kill -0 $t 2>/dev/null || break; sleep 1
  done
  rc=0; wait $t || rc=$?
  sed 's/^/  /' "$work/picture-$1.txt" 2>/dev/null || true
  [ "$rc" = 0 ] && { echo "ok: $1"; return; }
  echo "FAIL: $1 (xcodebuild test exit $rc)"
  echo "the app's lines (each space it opened, in the Companion's log; e2e/ios-touch-app.sh):"
  "$app/e2e/app-lines.sh" "/hm-e2e/openSpace/" "$work/serve.log"
  # The screen's elements around the tap on the path, as the test noted them.
  [ -n "$shots" ] && { mkdir -p "$shots/$1-failure"; for f in "$work/picture-$1.txt"-*.txt; do [ -f "$f" ] && cp "$f" "$shots/$1-failure/${f##*/picture-$1.txt-}"; done; } || true
  "$app/e2e/ios-xcresult.sh" "$work/uitest/result-$1.xcresult" ${shots:+"$shots/$1-failure"} all
  exit 1
}
[ -n "$shots" ] && mkdir -p "$shots"
appearance light
picture light
appearance dark
picture dark "$(cat "$size")"
xcrun simctl ui "$SIM_UDID" appearance light
echo PASS
