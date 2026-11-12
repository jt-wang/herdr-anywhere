#!/bin/sh
# The iOS app on a simulator against a real computer: an isolated herdr-core with a fake project and the companion on
# this computer's loopback, which the simulator shares. A script cannot tap the simulator, so the app is built
# (ios/build.sh, its own host project) from ios-e2e.tsx, which opens it at this run's pairing link. Passes when the
# app ships its sounds, the phone is connected over binary (the companion logs it) and its home screen lists the
# project (read from the accessibility tree with idb).
# The software keyboard on iOS, with real taps: e2e/ios.sh's setup, then the XCUITest in e2e/ios-uitest opens the
# space and taps the menu's Type in the terminal, puts the keyboard away with the key bar's button and taps it again.
# Passes when the keyboard comes up both times. Then the input box row's switch, and typing on the keyboard's keys
# straight into the terminal, whose program (e2e/typing-raw.py) logs the bytes: letters, space, Return with no stray
# backspace, a lowercase letter after "teh " (the keyboard switched Shift on there by itself), and no
# form bar (˄ ˅ Done) over the keyboard. Its build and result stay in the run's temporary folder.
# Usage: SIM_UDID=<udid> e2e/ios-keyboard.sh [screenshot.png]
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, idb, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shot=${1:-}
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/sam/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/sam/.zshrc"
placeholder=$(cat "$app/e2e/pair.ts")
cleanup() {
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  # The same session the core was started in below, or `server stop` finds no core and leaves its holder running.
  HC_SESSION="hmios-$$" HC_SOCKET="" HOME="$work/sam" XDG_RUNTIME_DIR="$run" "$hc" server stop >/dev/null 2>&1 || true
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
  xcrun simctl shutdown "$SIM_UDID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM
(
  export HC_SESSION="hmios-$$" HC_SOCKET="" HOME="$work/sam" SHELL=/bin/zsh
  export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
  export GIT_CONFIG_GLOBAL="$work/sam/.gitconfig"
  cp "$app/e2e/typing-raw.py" "$work/sam/src/acme-api/raw.py"
  "$hc" run --bg --keep --cwd "$work/sam/src/acme-api" --label herdr.space=spios --label herdr.tab=tbios python3 raw.py >/dev/null
  HERDR_ANYWHERE_STATE="$work/companion" exec "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1
) &
serve_pid=$!
i=0
until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do
  i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; exit 1; }
  sleep 0.1
done
link=$(HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "pair printed no link" >&2; exit 1; }
printf "export const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
# Herdr's alert sounds ship with the app, where the app's asset: URIs look (next to its bundle, as nsh build lays out).
container=$(xcrun simctl get_app_container "$SIM_UDID" "$id")
for s in request done; do
  [ -f "$container/nsh/assets/assets/sounds/$s.mp3" ] || { echo "FAIL: the app has no sounds/$s.mp3"; exit 1; }
done
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null

screen() { perl -e 'alarm 60; exec @ARGV' idb ui describe-all --udid "$SIM_UDID" 2>/dev/null; }
ok=""
for _ in $(seq 1 40); do
  if grep -q "device .* connected (binary," "$work/serve.log" && screen | grep -q '"AXLabel":"acme-api"'; then ok=1; break; fi
  sleep 1
done
[ -n "$shot" ] && perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "$shot" >/dev/null 2>&1
if [ -z "$ok" ]; then
  echo "FAIL: not connected over binary, or home does not list acme-api, within 40 s"
  screen | grep -o '"AXLabel":"[^"]*"' | head -20 | sed 's/^/  on screen: /'
  grep -vE " GET /assets/" "$work/serve.log" | tail -5 | sed -E 's/token=[^ &]+/token=…/'
  exit 1
fi
screen | grep -q '"AXIdentifier":"pair"\|"AXUniqueId":"pair"' && { echo "FAIL: the pairing screen is still up"; exit 1; }
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
rc=0
TEST_RUNNER_OUT="$work/typein.txt" TEST_RUNNER_RAW="$work/sam/src/acme-api/hm-raw" perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/TypeIn -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 || rc=$?
[ -n "$shot" ] && perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "${shot%.png}-keyboard.png" >/dev/null 2>&1
sed 's/^/  /' "$work/typein.txt" 2>/dev/null || true
[ -n "$shot" ] && { cp "$work/typein-terminal.png" "${shot%.png}-terminal.png" 2>/dev/null; cp "$work/uitest/test.log" "${shot%.png}-xctest.log" 2>/dev/null; } || true
if [ "$rc" != 0 ]; then echo "FAIL: the keyboard test (xcodebuild test exit $rc)"; grep -E "error:|XCTAssert|failed" "$work/uitest/test.log" | head -8; exit 1; fi
echo "PASS: Type in the terminal brings the keyboard up, also after the key bar's hide button; typing straight into the terminal sends what was typed"
