#!/bin/sh
# Scrolling a full-screen program that follows the mouse, with real drags on the iOS simulator (XCUITest), as
# e2e/android-tui-scroll.sh does on Android: e2e/tui-panels.py stands in for OpenCode (a prompt box that ignores the
# wheel, two panels that move 3 rows per wheel report). e2e/ios.sh's setup, then e2e/ios-uitest's Scroll test drags
# in each panel and flicks down over the prompt box. Passes when a drag sends a report per 3 rows of finger travel,
# all at the cell where the finger went down; a drag in the right panel reaches only that panel; and the flick keeps
# scrolling with nothing landing on the prompt box.
# Usage: SIM_UDID=<udid> e2e/ios-tui-scroll.sh [screenshot.png]
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
  "$hc" run --bg --keep --cwd "$work/sam/src/acme-api" --label herdr.space=spscroll --label herdr.tab=tbscroll python3 -I "$app/e2e/tui-panels.py" "$work/wheels" >/dev/null
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
: > "$work/wheels"
rc=0
TEST_RUNNER_EVENTS="$work/wheels" TEST_RUNNER_SPACE=spscroll perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/Scroll -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 || rc=$?
[ -n "$shot" ] && perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "${shot%.png}-scroll.png" >/dev/null 2>&1
if [ "$rc" != 0 ]; then echo "FAIL: the scroll test (xcodebuild test exit $rc)"; grep -E "error:|XCTAssert|failed" "$work/uitest/test.log" | head -8; exit 1; fi
read cols rows half < "$work/wheels.layout"
# The reports between two marks; a mark carries the terminal's height in points (12 of padding above and below).
part() { awk -v m="$1" '/^mark /{on = ($2 == m); next} on' "$work/wheels"; }
height=$(awk '/^mark drag/{print $3; exit}' "$work/wheels")
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
finger=$(python3 -c "import sys; h=float(sys.argv[1]); print(round(0.35*h/((h-24)/int(sys.argv[2])), 1))" "$height" "$rows")
n=$(part drag | grep -c . || true); want=$(python3 -c "import sys; print(int(float(sys.argv[1])//3))" "$finger")
if [ "$n" -ge $((want - 1)) ] && [ "$n" -le $((want + 1)) ]; then say "ok: a drag of $finger rows sends $n reports, one per 3 rows"; else say "FAIL: a drag of $finger rows sent $n reports (one per 3 rows is $want): $(part drag | tr '\n' ';')"; fi
r=$(part drag | awk '{print $3}' | sort -u | wc -l | tr -d ' ')
[ "$r" = 1 ] && say "ok: all on one row, where the finger went down" || say "FAIL: the drag's reports were on $r rows: $(part drag | awk '{print $3}' | sort -u | tr '\n' ' ')"
[ "$(part drag | grep -vc ' left$' || true)" = 0 ] && say "ok: all of them scroll the left panel" || say "FAIL: some went elsewhere: $(part drag | tr '\n' ';')"
if [ "$(part right | grep -c ' right$' || true)" -ge 2 ] && [ "$(part right | grep -vc ' right$' || true)" = 0 ]; then say "ok: a drag in the right panel scrolls only the right panel"
else say "FAIL: a drag in the right panel reached: $(part right | awk '{print $4}' | sort | uniq -c | tr '\n' ';')"; fi
if [ "$(part flick | grep -c ' ignored$' || true)" = 0 ] && [ "$(part flick | grep -c ' left$' || true)" -ge 6 ]; then say "ok: a flick that ends over the prompt box keeps scrolling ($(part flick | grep -c . ) reports)"
else say "FAIL: a flick ending over the prompt box: $(part flick | awk '{print $4}' | sort | uniq -c | tr '\n' ';')"; fi
exit $failed
