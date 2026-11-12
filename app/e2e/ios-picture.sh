#!/bin/sh
# A picture's path in a terminal, tapped on an iOS simulator (XCUITest's taps, e2e/ios-uitest Picture): the sheet shows the picture under its full
# path, holding it puts the picture on the pasteboard, and a file named like a picture that isn't one, or a path only mentioned, opens nothing.
# The app is built from ios-e2e.tsx, which opens it at this run's pairing link (as e2e/ios.sh). Fake data only.
# The terminal is here, or with HM_PICTURE_IMAGE in a container, for screenshots to publish (picture-terminal.sh).
# Usage: SIM_UDID=<udid> [HM_PICTURE_IMAGE=alpine:latest] e2e/ios-picture.sh [folder for screenshots]
# HM_MISS_SPACE=1 makes the light run's first tap on the space miss (e2e/ios-uitest/tests/OpenSpace.swift).
# HM_PICTURE_MISS_FIRST=1 is its self-test: the dark run's first tap lands beside the path, so the suite must fail with
# the test's record of that tap (the terminal's frame, the time since the switch to dark, a second tap).
# HM_PICTURE_NO_TAP_FIRST=1: the dark run makes no first tap (only its notes), so the suite must fail and the app's UIKit
# log (e2e/ios-touch-log.sh) must show no touch then and touches for the second tap.
# HM_PICTURE_LOOP=N: after the light run, N more runs, dark and light by turns, each counted: how often the first tap
# on the path after an appearance switch opens nothing (Devices' ios_picture_selftest=loop, with no Tailcat suite).
# In a loop a first tap on the space that opened nothing is counted and its touch log kept, and the run goes on.
# HM_TOUCH_WINDOW=off builds the test app without its touch-noting window (e2e/ios-touch-app.sh), to compare.
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shots=${1:-}
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7691; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
: > "$work/started" # what e2e/ios-failure-logs.sh keeps is from after this
"$app/e2e/ios-failure-logs.sh" --sample-load "$work/load.txt" >/dev/null 2>&1 & sampler=$! # the machine's load, kept with a failure
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/home" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
[ -z "${HM_PICTURE_IMAGE:-}" ] || export DOCKER_HOST="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}"
L() { HC_SESSION="hmiospic-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" "$@"; }
. "$app/e2e/picture-terminal.sh"
. "$app/e2e/companion.sh"  # companion_start, companion_stop
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e # every step runs, even after one fails (a companion already gone)
  # The sampler first: a sample written while rm -rf runs makes load.txt again, and the folder is not deleted.
  { kill "$sampler"; wait "$sampler"; } 2>/dev/null || true
  [ -z "${touchlog:-}" ] || { kill "$touchlog"; wait "$touchlog"; } 2>/dev/null || true
  companion_stop || status=1
  L "$hc" server stop >/dev/null 2>&1 || true
  picture_cleanup
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" || echo "NOTE: left after the cleanup: $(ls -A "$work" 2>/dev/null | head -5 | tr '\n' ' ')"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP
picture_terminal
companion_start "$work/serve.log" L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$work/companion" || exit 1
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
size_now() { L "$hc" get "$term" --json | python3 -c 'import json,sys; t=json.load(sys.stdin); print(t["cols"], t["rows"])'; }
. "$app/e2e/ios-size-wait.sh"
sh "$app/e2e/ios-size-wait.sh" --self-test || exit 1
sh "$app/e2e/ios-touch-log.sh" --self-test || exit 1
size="$work/size"; before=$(size_now)
# Each run: the XCUITest taps; meanwhile the terminal's size, once the phone has set it, goes to $size for it.
# The time of the appearance switch just before, for the test to say how long after it the tap came.
appearance() { xcrun simctl ui "$SIM_UDID" appearance "$1"; switched=$(python3 -c 'import time; print(time.time())'); }
picture() {
  n=$1${round:+-$round} # the files' name: the appearance, and the round in a loop (HM_PICTURE_LOOP)
  rm -f "${size:?}"; [ -n "${2:-}" ] && echo "$2" > "$size"
  miss=""; [ "$1" = dark ] && miss=${HM_PICTURE_MISS_FIRST:-}
  no_tap=""; [ "$1" = dark ] && no_tap=${HM_PICTURE_NO_TAP_FIRST:-}
  # The app's UIKit event dispatch while the test runs: whether a tap reached the app at all.
  "$app/e2e/ios-touch-log.sh" --start "$SIM_UDID" "$work/uikit-$n.log" & touchlog=$!
  space_miss=""; [ "$1" = light ] && space_miss=${HM_MISS_SPACE:-}
  TEST_RUNNER_OUT="$work/picture-$n.txt" TEST_RUNNER_SHOTS="${shots:-$work}/$n" TEST_RUNNER_SIZE="$size" TEST_RUNNER_SPACE="$space" \
    TEST_RUNNER_TERMINAL="$term" TEST_RUNNER_FULL="$full" TEST_RUNNER_SWITCHED="$switched" TEST_RUNNER_MISS_FIRST="$miss" TEST_RUNNER_NO_TAP_FIRST="$no_tap" TEST_RUNNER_MISS_SPACE="$space_miss" \
    TEST_RUNNER_SPACE_MISS_NOTED="${HM_PICTURE_LOOP:+1}" \
    perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/Picture -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
    -resultBundlePath "$work/uitest/result-$n.xcresult" > "$work/uitest/test-$n.log" 2>&1 &
  t=$!
  [ -s "$size" ] || size_wait "$before" "$size" "$t"
  rc=0; wait $t || rc=$?
  { kill "$touchlog"; wait "$touchlog"; } 2>/dev/null || true; touchlog=
  sed 's/^/  /' "$work/picture-$n.txt" 2>/dev/null || true
  # A loop's first tap on the space that opened nothing: counted by the loop, with its touch log, and the run goes on.
  if grep -q "the first tap on the space opened nothing" "$work/picture-$n.txt" 2>/dev/null; then
    [ -n "$shots" ] && mkdir -p "$shots/$n-space"
    "$app/e2e/ios-touch-log.sh" "$work/picture-$n.txt" "$work/uikit-$n.log" ${shots:+"$shots/$n-space"}
  fi
  [ "$rc" = 0 ] && { echo "ok: $n"; return; }
  echo "FAIL: $n (xcodebuild test exit $rc)"
  echo "the app's lines (each space it opened, in the Companion's log; e2e/ios-touch-app.sh):"
  "$app/e2e/app-lines.sh" "/hm-e2e/openSpace/" "$work/serve.log"
  # The screen's elements around the tap on the path, as the test noted them.
  [ -n "$shots" ] && { mkdir -p "$shots/$n-failure"; for f in "$work/picture-$n.txt"-*.txt; do [ -f "$f" ] && cp "$f" "$shots/$n-failure/${f##*/picture-$n.txt-}"; done; } || true
  "$app/e2e/ios-touch-log.sh" "$work/picture-$n.txt" "$work/uikit-$n.log" ${shots:+"$shots/$n-failure"}
  "$app/e2e/ios-xcresult.sh" "$work/uitest/result-$n.xcresult" ${shots:+"$shots/$n-failure"} all
  [ -n "$shots" ] && "$app/e2e/ios-failure-logs.sh" "$work/started" "$work/serve.log" "$shots/$n-failure" || true
  exit 1
}
[ -n "$shots" ] && mkdir -p "$shots"
appearance light
picture light
if [ -n "${HM_PICTURE_LOOP:-}" ]; then
  # Rounds of what the dark run does, dark and light by turns, each its own XCUITest and each a sample: the appearance
  # switched, the app brought back by the test, and its first tap on the path (the space is open from the first run).
  # Each round goes on whatever the last did; the suite fails if any round did, after all of them.
  cr=$(cat "$size"); i=0 missed=0 failed=0
  grep -q "the first tap on the space opened nothing" "$work/picture-light.txt" && echo "the light run's first tap on the space opened nothing"
  while [ "$i" -lt "$HM_PICTURE_LOOP" ]; do
    i=$((i + 1)); [ $((i % 2)) = 1 ] && look=dark || look=light
    appearance "$look"
    if (round=$i picture "$look" "$cr") > "$work/round-$i.txt" 2>&1; then r=ok; else r=failed; failed=$((failed + 1)); fi
    cat "$work/round-$i.txt"
    grep -q "the first tap on the path opened no picture" "$work/round-$i.txt" && { r="first tap missed"; missed=$((missed + 1)); }
    echo "round $i of $HM_PICTURE_LOOP ($look): $r"
  done
  echo "rounds: $HM_PICTURE_LOOP; the first tap on the path opened no picture in $missed; failed in all: $failed"
  xcrun simctl ui "$SIM_UDID" appearance light
  [ "$failed" = 0 ] || exit 1
  echo PASS
  exit 0
fi
appearance dark
picture dark "$(cat "$size")"
xcrun simctl ui "$SIM_UDID" appearance light
echo PASS
