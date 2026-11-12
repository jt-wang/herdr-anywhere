#!/bin/sh
# Oh My Pi's pictures on Android, with Oh My Pi itself: omp reads a made-up chart for a stand-in model (e2e/fake-model,
# no key, no real model) and shows it in its tool output with Kitty graphics. The phone has the pane open while omp
# draws; the chart must be on the phone's screen (e2e/kitty-chart.py), light and dark, after the pane is left and
# opened again (herdr-core sends one screen of placeholders, no picture), and after the app is stopped and opened
# again (a new connection, herdr-core's history). Fake data only: a project folder named acme-app, a model named Test
# Model. omp (18.13.3) runs only where the suites run, a CI runner (app/e2e/ci/tools.sh omp fetches it), never on a
# Mac.
# Usage: ANDROID_SERIAL=emulator-5554 HM_TEST_OMP=<omp binary> e2e/android-kitty.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
: "${HM_TEST_OMP:?set HM_TEST_OMP to Oh My Pi 18.13.3 (app/e2e/ci/tools.sh omp)}"
apk=$1; bin=$2; shots=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7797; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
mport=8099; while nc -z 127.0.0.1 "$mport" 2>/dev/null; do mport=$((mport + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hkXXXX); state=$(mktemp -d /tmp/hkcXXXX)
proj=$work/u/src/acme-app
mkdir -p "$work/u/.omp/agent" "$proj/docs" "$work/state" "$work/config" "$work/data" "$work/bin"
: > "$work/u/.zshrc"
echo skip_global_compinit=1 > "$work/u/.zshenv"
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
export HC_SESSION="hmkitty-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh PATH="$work/bin:$PATH"
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
serve_pid="" model_pid=""
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  "$adb" shell cmd uimode night no >/dev/null 2>&1 || true
  [ -z "$serve_pid" ] || kill "$serve_pid" 2>/dev/null
  [ -z "$model_pid" ] || kill "$model_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  [ $status = 0 ] || { echo "--- the stand-in model's log"; tail -20 "$work/model.log"; }
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  exit "$status"
}
trap cleanup EXIT; trap 'cleanup; exit 130' INT; trap 'cleanup; exit 143' TERM HUP

"$work/bin/fake-model" -addr "127.0.0.1:$mport" -read docs/chart.png > "$work/model.log" 2>&1 &
model_pid=$!
"$hc" run --bg --keep --cwd "$proj" --label herdr.space=spacme --label herdr.tab=tbomp -- omp --model local/test-model >/dev/null
term=t1
screen() { "$hc" read "$term" 2>/dev/null || true; }
until_screen() { # text, seconds
  for _ in $(seq 1 "$2"); do screen | grep -qF "$1" && return 0; sleep 1; done
  echo "FAIL: \"$1\" not on omp's screen after $2 s:"; screen | grep -v '^[[:space:]]*$' | tail -15; exit 1
}
until_screen "Test Model" 60

"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
# Android 13+ asks for notifications once paired; that question would cover the screen.
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
# Sets B to the view's bounds, or ends the run saying what is on screen. Not in $( ): an exit there would only end the
# subshell.
wait_for() {
  B=""; for _ in $(seq 1 30); do B=$(xml | bounds "$1\""); [ -n "$B" ] && return 0; sleep 1; done
  echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1
}
tap() { wait_for "$1"; set -- $B; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2)); }
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
# The chart on the phone's screen now: a screenshot (kept in the folder for screenshots), read by kitty-chart.py.
chart() { # name, what it shows
  f=$work/$1.png
  "$adb" exec-out screencap -p > "$f"
  [ -z "$shots" ] || { mkdir -p "$shots"; cp "$f" "$shots/$1.png"; }
  if out=$(python3 "$app/e2e/kitty-chart.py" "$f"); then say "ok: $2"; else say "FAIL: $2"; echo "$out"; fi
}
open_pane() { tap "space-spacme"; wait_for "terminal-$term"; sleep 4; }
# The app's and its WebView renderer's memory (PSS), from Android's summary: printed to compare with the page's
# measurements in a desktop browser (model/kitty.ts); not checked.
memory() { echo "memory, $1:"; "$adb" shell dumpsys meminfo 2>/dev/null | sed -n '/Total PSS by process/,/^$/p' | grep -E "$id|sandboxed_process" | sed 's/^ */  /' || true; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$(dirname "$0")/android-type-link.sh" "$invite"
tap pair
open_pane
memory "the pane open, no picture yet"
"$hc" send "$term" 'Show me the chart in docs/chart.png' >/dev/null
until_screen "That is the chart." 90
sleep 3
chart light-live "the chart omp read is on the phone while the pane is open"
memory "the chart drawn"
"$adb" shell cmd uimode night yes >/dev/null; sleep 3
chart dark-live "dark: the chart is still there"
# Left and opened again: herdr-core sends one screen of placeholders; the app still has the picture.
for _ in 1 2 3; do xml | grep -q 'resource-id="space-spacme"' && break; "$adb" shell input keyevent 4; sleep 1; done
open_pane
chart dark-back "the pane left and opened again: the chart is drawn again"
# The app stopped and opened again: a new connection, and herdr-core's history with the picture in it.
"$adb" shell am force-stop "$id"
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
open_pane
chart dark-reload "the app stopped and opened again: the chart is drawn again"
[ $failed = 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
