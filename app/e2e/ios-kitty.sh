#!/bin/sh
# Oh My Pi's pictures on an iOS simulator, with Oh My Pi itself: the same steps as e2e/android-kitty.sh. omp reads a
# made-up chart for a stand-in model (e2e/fake-model, no key, no real model) and shows it with Kitty graphics while the
# phone has the pane open; the chart must be on the phone's screen with only the terminal's background beside it
# (e2e/kitty-chart.py), light and dark, and after the app is stopped and opened again (a new connection, herdr-core's
# screen). XCUITest opens the space (e2e/ios-uitest Kitty) and holds the page while this script takes the screenshots.
# Fake data only: a project folder named acme-app, a model named Test Model. omp (18.13.3) runs only where the suites
# run, a CI runner (app/e2e/ci/tools.sh omp fetches it), never on a Mac of ours.
# Usage: SIM_UDID=<udid> HM_TEST_OMP=<omp binary> e2e/ios-kitty.sh [folder for screenshots]
#   HM_KITTY_BEFORE=1: an app from before the pictures, for screenshots of how it was: the same steps, each check
#   printed as "before:" and none failing the run.
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, xcodegen and Xcode.
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard ios-kitty # a login entry of its own, never the person's
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
: "${HM_TEST_OMP:?set HM_TEST_OMP to Oh My Pi 18.13.3 (app/e2e/ci/tools.sh omp)}"
app=$(cd "$(dirname "$0")/.." && pwd)
sh "$app/e2e/ios-connect-wait.sh" --self-test || exit 1
shots=${1:-}
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7695; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
mport=8199; while nc -z 127.0.0.1 "$mport" 2>/dev/null; do mport=$((mport + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hkiXXXX)
: > "$work/started" # what e2e/ios-failure-logs.sh keeps is from after this
proj=$work/u/src/acme-app
mkdir -p "$work/u/.omp/agent" "$proj/docs" "$work/state" "$work/config" "$work/data" "$work/bin"
: > "$work/u/.zshrc"
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
(cd "$app/e2e/fake-model" && GO111MODULE=off go build -o "$work/bin/fake-model" main.go)
"$work/bin/fake-model" -png "$proj/docs/chart.png"
ln -s "$HM_TEST_OMP" "$work/bin/omp"
# No setup screen, no update check at start (its notice would change the screen), no plugin updates.
cat > "$work/u/.omp/agent/config.yml" <<EOF
startup:
  checkUpdate: false
  setupWizard: false
marketplace:
  autoUpdate: "off"
EOF
# The stand-in model; it takes pictures (input: [text, image]), or omp shows none.
cat > "$work/u/.omp/agent/models.yml" <<EOF
providers:
  local:
    baseUrl: http://127.0.0.1:$mport/v1
    api: openai-completions
    apiKey: test
    models:
      - id: test-model
        name: Test Model
        input: [text, image]
EOF
# herdr-core, the Companion and omp in the made-up home; xcodebuild and simctl keep the runner's own.
L() { HC_SESSION="hmioskitty-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh PATH="$work/bin:$PATH" XDG_STATE_HOME="$work/state" \
  XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" "$@"; }
serve_pid="" model_pid="" test_pid="" tail_pid=""
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  [ -z "$test_pid" ] || { kill "$test_pid"; wait "$test_pid"; } 2>/dev/null || true
  [ -z "$tail_pid" ] || kill "$tail_pid" 2>/dev/null
  xcrun simctl ui "$SIM_UDID" appearance light >/dev/null 2>&1
  [ -z "$serve_pid" ] || { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  [ -z "$model_pid" ] || kill "$model_pid" 2>/dev/null
  L "$hc" server stop >/dev/null 2>&1 || true
  if [ $status != 0 ]; then
    echo "--- the stand-in model's log"; tail -20 "$work/model.log" 2>/dev/null
    echo "--- the last UI test's log"; grep -E "error:|XCTAssert|failed|Kitty" "$work/test.log" 2>/dev/null | tail -10
    [ -z "$shots" ] || "$app/e2e/ios-failure-logs.sh" "$work/started" "$work/serve.log" "$shots/failure" || true
  fi
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}"
  autostart_guard_end || exit 1
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

"$work/bin/fake-model" -addr "127.0.0.1:$mport" -read docs/chart.png > "$work/model.log" 2>&1 &
model_pid=$!
L "$hc" run --bg --keep --cwd "$proj" --label herdr.space=spacme --label herdr.tab=tbomp -- omp --model local/test-model >/dev/null
term=t1
screen() { L "$hc" read "$term" 2>/dev/null || true; }
until_screen() { # text, seconds
  for _ in $(seq 1 "$2"); do screen | grep -qF "$1" && return 0; sleep 1; done
  echo "FAIL: \"$1\" not on omp's screen after $2 s:"; screen | grep -v '^[[:space:]]*$' | tail -15; exit 1
}
until_screen "Test Model" 60

L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0; until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; exit 1; }; sleep 0.1; done
# The link is built into the app, whose build on a busy runner can take longer than the code's 10 minutes by default
# (the Companion then answers 410 to the app's pairing, and it never connects): an hour for this run.
link=$(L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" pair --ttl 60m --url "http://127.0.0.1:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
# The test app opens at this run's pairing link (ios-e2e.tsx); its terminal page is the one shipped (VIA none).
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$work/pair.ts"
built=$("$app/e2e/ios-touch-app.sh" "$work" "$work/pair.ts" none) || exit 1

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl ui "$SIM_UDID" appearance light
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$built"
# The app connects to this run's Companion (e2e/ios-connect-wait.sh), up to 120 s: a CI runner near load 100 once took
# over 60. Opened again, it waits on a copy of the log from the launch on, as the first connect is in the log already.
# No screenshot on a miss: the app not yet paired can show the pairing link, which must not be uploaded.
launch() {
  log=$work/serve.log
  if [ -n "$tail_pid" ] || grep -q "device .* connected (binary," "$log"; then
    log=$work/serve-again.log; tail -n 0 -F "$work/serve.log" > "$log" 2>/dev/null & tail_pid=$!
    sleep 0.5
  fi
  xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
  HM_CONNECT_WAIT=${HM_CONNECT_WAIT:-120} "$app/e2e/ios-connect-wait.sh" "$SIM_UDID" "$id" "$log" "$work/started" ${shots:+"$shots/connect-failure"} || exit 1
  [ -z "$tail_pid" ] || { kill "$tail_pid"; wait "$tail_pid"; } 2>/dev/null || true
  tail_pid=""
}
launch
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)

# The UI test opens the space and holds the page until $work/done-N exists; the screenshots are taken meanwhile.
open_pane() { # N
  rm -f "$work/kitty-$1.txt"
  TEST_RUNNER_OUT="$work/kitty-$1.txt" TEST_RUNNER_DONE="$work/done-$1" TEST_RUNNER_SPACE=spacme TEST_RUNNER_TERMINAL="$term" \
    perl -e 'alarm 900; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/Kitty -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
    -resultBundlePath "$work/uitest/result-$1.xcresult" > "$work/test.log" 2>&1 &
  test_pid=$!
  for _ in $(seq 1 600); do
    grep -q "terminal showed" "$work/kitty-$1.txt" 2>/dev/null && { sleep 4; return 0; }
    kill -0 "$test_pid" 2>/dev/null || break
    sleep 1
  done
  echo "FAIL: the space's terminal never showed"; sed 's/^/  /' "$work/kitty-$1.txt" 2>/dev/null || true
  [ -z "$shots" ] || "$app/e2e/ios-xcresult.sh" "$work/uitest/result-$1.xcresult" "$shots/failure" all || true
  exit 1
}
close_pane() { # N
  : > "$work/done-$1"
  rc=0; wait "$test_pid" || rc=$?; test_pid=""
  [ $rc = 0 ] && return 0
  # Before the pictures the run is there for its screenshots, taken by now: what the UI test found is only noted.
  if [ "${HM_KITTY_BEFORE:-}" = 1 ]; then echo "before: the UI test ended with $rc"; sed 's/^/  /' "$work/kitty-$1.txt" 2>/dev/null || true; return 0; fi
  echo "FAIL: the UI test ended with $rc"; sed 's/^/  /' "$work/kitty-$1.txt" 2>/dev/null || true; exit 1
}
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
# The chart on the phone's screen: a screenshot (kept in the folder for screenshots), read by kitty-chart.py. A screen
# still redrawing (after the switch to dark, on a busy machine) is taken again, every 2 s for up to 20 s; the last one
# is kept. Before the pictures, one screenshot.
chart() { # name, what it shows
  f=$work/$1.png t=0
  while :; do
    perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "$f" >/dev/null 2>&1
    if out=$(python3 "$app/e2e/kitty-chart.py" "$f"); then r=ok; else r=FAIL; fi
    [ $r = ok ] || [ "${HM_KITTY_BEFORE:-}" = 1 ] || [ $t -ge 20 ] && break
    sleep 2; t=$((t + 2))
  done
  [ -z "$shots" ] || { mkdir -p "$shots"; cp "$f" "$shots/$1.png"; }
  if [ "${HM_KITTY_BEFORE:-}" = 1 ]; then echo "before: $2: $( [ $r = ok ] && echo drawn || echo "not drawn")"
  else say "$r: $2$( [ $t = 0 ] || echo " (after $t s more)")"; [ $r = ok ] || echo "$out"; fi
}

open_pane 1
L "$hc" send "$term" 'Show me the chart in docs/chart.png' >/dev/null
until_screen "That is the chart." 90
sleep 3
chart light-live "the chart omp read is on the phone while the pane is open"
xcrun simctl ui "$SIM_UDID" appearance dark; sleep 3
chart dark-live "dark: the chart is still there"
close_pane 1
# The app stopped and opened again: a new connection, and herdr-core's screen with the picture in it.
xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
launch
open_pane 2
chart dark-reload "the app stopped and opened again: the chart is drawn again"
close_pane 2
[ $failed = 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
