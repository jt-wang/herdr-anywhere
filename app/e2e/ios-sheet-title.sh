#!/bin/sh
# e2e/android-sheet-title.sh on an iPhone simulator: the new-tab sheet's title reads whole each time it opens, for a
# space on another machine: four openings for the space on box (light, again, dark, light), one for the space on dock,
# and two after a terminal was started from the sheet (light, dark). The taps are XCUITest's (e2e/ios-uitest SheetTitle),
# which saves the screen and the title's place at each opening; the title is read off the picture with tesseract, as on
# Android (the view tree holds the whole text even when the screen shows part of it). The first opening is the reader's
# own check. The app is built from ios-e2e.tsx, which opens it at this run's pairing link (as e2e/ios-chat.sh).
# Each other machine is this computer under another home and PATH (`hc machine add --via "env HOME=… sh -c"`); its one
# agent is e2e/fake-claude, built here as claude. Needs go, hc, nsh, tesseract, python3 with Pillow, xcodegen and Xcode.
# Usage: SIM_UDID=<udid> e2e/ios-sheet-title.sh COMPANION SHOTS
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
bin=$1; shots=$2
app=$(cd "$(dirname "$0")/.." && pwd)
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(cd "$(mktemp -d)" && pwd -P); run=$(mktemp -d /tmp/hmXXXX); boxrun=$(mktemp -d /tmp/hbXXXX); dockrun=$(mktemp -d /tmp/hdXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
# Every machine's folder is under this computer's home (its real path, as the machines report folders), so the sheet shows
# it as ~/src/<name>, a made-up name, never a path of this computer.
mkdir -p "$shots" "$work/me/src/acme-api" "$work/me/src/acme-web" "$work/state" "$work/config" "$work/data" "$work/boxstate" "$work/me/src/acme-docs" "$work/dockstate" "$work/t"
: > "$work/me/.zshrc"
placeholder=$(cat "$app/e2e/pair.ts")
# This computer's side under its own home; xcrun and xcodebuild keep the real one (the simulators live under it).
L() { env HC_SESSION="hmtitle-$$" HC_SOCKET="" HOME="$work/me" SHELL=/bin/sh XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/me/.gitconfig" "$@"; }
box="env HOME=$work/box PATH=$work/box/bin:$work/box/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin XDG_RUNTIME_DIR=$boxrun XDG_STATE_HOME=$work/boxstate HC_SESSION=hmtitlebox-$$"
dock="env HOME=$work/dock PATH=$work/dock/bin:$work/dock/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin XDG_RUNTIME_DIR=$dockrun XDG_STATE_HOME=$work/dockstate HC_SESSION=hmtitledock-$$"
serve_pid=""; test_pid=""
: > "$work/started" # what e2e/ios-failure-logs.sh keeps is from after this
"$app/e2e/ios-failure-logs.sh" --sample-load "$work/load.txt" >/dev/null 2>&1 & sampler=$! # the machine's load, kept with a failure
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  # The sampler first: a sample written while rm -rf runs makes load.txt again, and the folder is not deleted.
  { kill "$sampler"; wait "$sampler"; } 2>/dev/null
  [ -n "$test_pid" ] && kill "$test_pid" 2>/dev/null
  [ -n "$serve_pid" ] && kill "$serve_pid" 2>/dev/null
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  xcrun simctl ui "$SIM_UDID" appearance light 2>/dev/null
  L "$hc" server stop >/dev/null 2>&1
  [ -x "$work/box/.local/bin/hc" ] && $box "$work/box/.local/bin/hc" server stop >/dev/null 2>&1
  [ -x "$work/dock/.local/bin/hc" ] && $dock "$work/dock/.local/bin/hc" server stop >/dev/null 2>&1
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" "${boxrun:?}" "${dockrun:?}" "${state:?}"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

for m in box dock; do mkdir -p "$work/$m/bin"; (cd "$app/e2e/fake-claude" && GO111MODULE=off go build -o "$work/$m/bin/claude" main.go); done
L "$hc" machine add --via "$box sh -c" --name box --install >/dev/null
L "$hc" run --on box --bg --keep --cwd "$work/me/src/acme-web" --label herdr.space=spweb --label herdr.tab=tbweb -- sh -i >/dev/null
L "$hc" machine add --via "$dock sh -c" --name dock --install >/dev/null
L "$hc" run --on dock --bg --keep --cwd "$work/me/src/acme-docs" --label herdr.space=spdocs --label herdr.tab=tbdocs -- sh -i >/dev/null
L "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0; until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { echo "FAIL: the companion did not start"; exit 1; }; sleep 0.1; done
link=$(L "$bin" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl ui "$SIM_UDID" appearance light; xcrun simctl ui "$SIM_UDID" content_size large
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do i=$((i + 1)); [ $i -gt 60 ] && { echo "FAIL: the app never connected"; exit 1; }; sleep 1; done
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
echo light > "$work/t/now"
TEST_RUNNER_DIR="$work/t" xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -only-testing:UITests/SheetTitle -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" -resultBundlePath "$work/uitest/result.xcresult" > "$work/uitest.log" 2>&1 &
test_pid=$!
# The themes the test asks for.
while kill -0 "$test_pid" 2>/dev/null; do
  w=$(cat "$work/t/want" 2>/dev/null || true)
  if [ -n "$w" ] && [ "$w" != "$(cat "$work/t/now")" ]; then xcrun simctl ui "$SIM_UDID" appearance "$w"; sleep 1; printf %s "$w" > "$work/t/now"; fi
  sleep 1
done
rc=0; wait "$test_pid" || rc=$?; test_pid=""
cp "$work"/t/*.png "$shots/" 2>/dev/null || true
if [ "$rc" != 0 ]; then
  # xcodebuild -quiet leaves the failures' messages out of its log: they are in the result bundle, with XCTest's
  # screenshots at each; the Companion's log, the simulator's crash reports and the machine's load go with them.
  echo "FAIL: the test (exit $rc)"
  "$app/e2e/ios-xcresult.sh" "$work/uitest/result.xcresult" "$shots/failure" all
  "$app/e2e/ios-failure-logs.sh" "$work/started" "$work/serve.log" "$shots/failure" || true
  exit 1
fi

# Each opening: the title as tesseract reads it, from the left of the title's view to the sheet's right edge (a cut
# title's view is narrower than its text).
failed=0; seen=""; read_=""
check() { # $1 the opening's name
  set -- $(grep "^$1 " "$work/t/frames.txt")
  [ $# = 5 ] || { echo "FAIL: $1: the test left no title place"; exit 1; }
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
}
whole() { [ "$seen" = addanagent ]; }
check 1-first
whole || { echo "FAIL: the first opening's title reads \"$read_\", not Add an agent: the reader, not the sheet, is what fails here"; exit 1; }
echo "ok: the first opening's title reads whole (the reader works)"
for o in 2-second 3-dark 4-light-again 5-other-machine 6-after-start 7-after-start-dark; do
  check $o
  whole && echo "ok: $o: its title reads whole" || { echo "FAIL: $o: its title reads \"$read_\": cut"; failed=1; }
done
exit $failed
