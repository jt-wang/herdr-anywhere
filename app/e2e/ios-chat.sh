#!/bin/sh
# The chat view on an iOS simulator (XCUITest's taps, e2e/ios-uitest Chat): a fake Claude Code (e2e/fake-claude.py,
# replaying the 2.1.322 fixtures) answers two prompts typed in the composer; the reply's video shows its length and plays
# in the app with the keyboard put away, and its PDF opens in a preview. Against an isolated herdr-core; the app is built
# from ios-e2e.tsx, which opens it at this run's pairing link (as e2e/ios-picture.sh). Fake data only.
# Usage: SIM_UDID=<udid> e2e/ios-chat.sh [folder for screenshots]
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, python3, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shots=${1:-}
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
fixtures="$app/../companion/internal/chat/claude/testdata/claude-code/2.1.322"
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
(cd "$app/../companion" && CGO_ENABLED=0 go build -buildvcs=false -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
# The files the RENDER reply names: a made-up 3 s test pattern and a one-page PDF (e2e/files).
mkdir -p "$work/h/src/acme-api/renders" "$work/h/src/acme-api/docs" "$work/state" "$work/config" "$work/data" "$work/claude"
cp "$app/e2e/files/clip.mp4" "$work/h/src/acme-api/renders/demo.mp4"; cp "$app/e2e/files/guide.pdf" "$work/h/src/acme-api/docs/"
: > "$work/h/.zshrc"
placeholder=$(cat "$app/e2e/pair.ts")
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  LANG=en_US.UTF-8 HC_SESSION="hmioschat-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" CLAUDE_CONFIG_DIR="$work/claude" "$@"; }
. "$app/e2e/companion.sh"  # companion_start, companion_stop
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e # every step runs, even after one fails (a companion already gone)
  companion_stop || status=1
  L "$hc" server stop >/dev/null 2>&1 || true
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP
L "$hc" run --bg --keep --cwd "$work/h/src/acme-api" --name claude --label herdr.space=spchat --label herdr.tab=tbchat \
  python3 "$app/e2e/fake-claude.py" "$fixtures/image.jsonl" "$fixtures/files.jsonl" >/dev/null
companion_start "$work/serve.log" L "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" || exit 1
link=$(L "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl ui "$SIM_UDID" appearance light
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
# The app is up and paired once the companion logs the phone connected.
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do i=$((i + 1)); [ $i -gt 60 ] && { echo "FAIL: the app never connected"; exit 1; }; sleep 1; done
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
[ -n "$shots" ] && mkdir -p "$shots"
rc=0
TEST_RUNNER_OUT="${shots:-$work}/chat.txt" TEST_RUNNER_SHOTS="${shots:-$work}/chat" TEST_RUNNER_SPACE=spchat \
  perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -only-testing:UITests/Chat -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 || rc=$?
sed 's/^/  /' "${shots:-$work}/chat.txt" 2>/dev/null || true
[ "$rc" = 0 ] || { echo "FAIL: xcodebuild test exit $rc"; grep -E "error:|XCTAssert|failed" "$work/uitest/test.log" | head -8; exit 1; }
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
echo PASS
