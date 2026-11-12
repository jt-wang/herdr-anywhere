#!/bin/sh
# The chat view's decision card on an iOS simulator (XCUITest's taps, e2e/ios-uitest Ask), against an isolated
# herdr-core: stand-ins for Claude Code 2.1.325 (e2e/fake-ask) ask two questions and ask to run a command, light and
# then dark (a pair of panes each, as a question answered stays answered). With the card (app/src/ui/ask-card.tsx in
# this tree) both are answered from the card; without it (the commit before) the same screens are shot only. The app
# is built from ios-e2e.tsx, which opens it at this run's pairing link (as e2e/ios-chat.sh). Fake data only.
# Usage: SIM_UDID=<udid> e2e/ios-ask.sh [folder for screenshots]
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, python3, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shots=${1:-}
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
card=no; [ -f "$app/src/ui/ask-card.tsx" ] && card=yes
port=7795; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hiXXXX); state=$(mktemp -d /tmp/hitcXXXX)
mkdir -p "$work/h/src" "$work/state" "$work/config" "$work/data" "$work/claude" "$work/bin"
(cd "$app/../companion" && CGO_ENABLED=0 go build -buildvcs=false -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
(cd "$app/e2e/fake-ask" && GO111MODULE=off go build -o "$work/bin/claude" main.go)
: > "$work/h/.zshrc"
placeholder=$(cat "$app/e2e/pair.ts")
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  LANG=en_US.UTF-8 HC_SESSION="hmiosask-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" CLAUDE_CONFIG_DIR="$work/claude" "$@"; }
. "$app/e2e/companion.sh"  # companion_start, companion_stop
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  companion_stop || status=1
  L "$hc" server stop >/dev/null 2>&1 || true
  xcrun simctl ui "$SIM_UDID" appearance light 2>/dev/null
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP
for look in light dark; do
  mkdir -p "$work/h/src/shop-$look" "$work/h/src/site-$look"
  L "$hc" run --bg --keep --cwd "$work/h/src/shop-$look" --name "asking-$look" --label herdr.space="spask$look" --label herdr.tab="tbask$look" \
    "$work/bin/claude" "$app/e2e/files/ask/question.json" >/dev/null
  L "$hc" run --bg --keep --cwd "$work/h/src/site-$look" --name "running-$look" --label herdr.space="spperm$look" --label herdr.tab="tbperm$look" \
    "$work/bin/claude" "$app/e2e/files/ask/permission.json" >/dev/null
done
companion_start "$work/serve.log" L "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" || exit 1
link=$(L "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
[ -n "$shots" ] && mkdir -p "$shots"
echo "card: $card"
for look in light dark; do
  xcrun simctl ui "$SIM_UDID" appearance "$look"
  xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
  xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
  # The app is up and paired once the companion logs the phone connected (the first time; then it reconnects).
  i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do i=$((i + 1)); [ $i -gt 60 ] && { echo "FAIL: the app never connected"; exit 1; }; sleep 1; done
  rc=0
  TEST_RUNNER_OUT="${shots:-$work}/ask-$look.txt" TEST_RUNNER_SHOTS="${shots:-$work}/ask-$look" TEST_RUNNER_CARD=$card \
    TEST_RUNNER_SPACEQ="spask$look" TEST_RUNNER_SPACEP="spperm$look" \
    perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/Ask -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test-$look.log" 2>&1 || rc=$?
  sed 's/^/  /' "${shots:-$work}/ask-$look.txt" 2>/dev/null || true
  [ "$rc" = 0 ] || { echo "FAIL: xcodebuild test ($look) exit $rc"; grep -E "error:|XCTAssert|failed" "$work/uitest/test-$look.log" | head -8; exit 1; }
  echo "ok: $look"
done
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
echo PASS
