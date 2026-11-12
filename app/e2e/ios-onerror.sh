#!/bin/sh
# An error the app's JS doesn't catch, on an iOS simulator: e2e/ios.sh's setup with the app built from
# ios-e2e-error.tsx, whose timer throws 10 s after the app starts. Passes when the Companion's log shows the app about
# to throw (its /hm-e2e/throwing request), the app's process is still the one launched 5 s after (checked from the
# launch on, since on a slow computer the throw can come before home lists the space), and a tap on the space
# (XCUITest) opens its terminal. When the app quits, the crash report the run left is named; given a screenshot, a failure also
# keeps the Companion's log and the simulator's crash reports beside it (e2e/ios-failure-logs.sh).
# Usage: SIM_UDID=<udid> e2e/ios-onerror.sh [screenshot.png]
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, idb, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shot=${1:-}
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/sam/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/sam/.zshrc"
: > "$work/started" # what e2e/ios-failure-logs.sh keeps, and the crash reports named, are from after this
placeholder=$(cat "$app/e2e/pair.ts")
cleanup() {
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  # The same session the core was started in below, or `server stop` finds no core and leaves its holder running.
  HC_SESSION="hmioserr-$$" HC_SOCKET="" HOME="$work/sam" XDG_RUNTIME_DIR="$run" "$hc" server stop >/dev/null 2>&1 || true
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
  # As ios-keyboard.sh and ios-tui-scroll.sh do after their XCUITest: left booted, the next suite's idb read the
  # accessibility tree empty while its screenshot showed the screen.
  xcrun simctl shutdown "$SIM_UDID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM
(
  export HC_SESSION="hmioserr-$$" HC_SOCKET="" HOME="$work/sam" SHELL=/bin/zsh
  export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
  export GIT_CONFIG_GLOBAL="$work/sam/.gitconfig"
  "$hc" run --bg --keep --cwd "$work/sam/src/acme-api" --label herdr.space=sperror --label herdr.tab=tberror >/dev/null
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
"$app/ios/build.sh" ios-e2e-error.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null

screen() { perl -e 'alarm 60; exec @ARGV' idb ui describe-all --udid "$SIM_UDID" 2>/dev/null; }
# The app's process, as launchd lists it in the simulator ("-" or nothing once it has quit).
pid() { xcrun simctl spawn "$SIM_UDID" launchctl list 2>/dev/null | awk -v id="UIKitApplication:$id" 'index($3, id) == 1 {print $1}'; }
# Given a screenshot, a failure keeps the Companion's log and the simulator's crash reports beside it.
keep() { [ -n "$shot" ] && [ -x "$app/e2e/ios-failure-logs.sh" ] && "$app/e2e/ios-failure-logs.sh" "$work/started" "$work/serve.log" "${shot%.png}-failure" || true; }
# The app's process is not the one launched: it quit (on a slow computer, before home even lists the space).
quit() {
  echo "FAIL: the app quit after a JS error nothing caught (pid $before at launch, '${1}' now)"
  grep ' /hm-e2e/throwing ' "$work/serve.log" | head -1 | sed 's/^/  app: /'
  # Where this computer keeps the simulator's crash reports.
  find "$HOME/Library/Logs/DiagnosticReports" -name 'Herdr-*.ips' -newer "$work/started" 2>/dev/null | sed 's/^/  crash report: /'
  keep; exit 1
}
before=""
for _ in $(seq 1 10); do before=$(pid); case $before in ''|-|0) sleep 1 ;; *) break ;; esac; done
case $before in ''|-|0) echo "FAIL: launchd lists no process for the app after its launch"; keep; exit 1 ;; esac
ok=""
for _ in $(seq 1 40); do
  now=$(pid); [ "$now" = "$before" ] || quit "$now"
  if grep -q "device .* connected (binary," "$work/serve.log" && screen | grep -q '"AXLabel":"acme-api"'; then ok=1; break; fi
  sleep 1
done
if [ -z "$ok" ]; then
  echo "FAIL: not connected over binary, or home does not list acme-api, within 40 s"
  screen | grep -o '"AXLabel":"[^"]*"' | head -20 | sed 's/^/  on screen: /'
  keep; exit 1
fi
for _ in $(seq 1 30); do grep -q ' /hm-e2e/throwing ' "$work/serve.log" && break; sleep 1; done
grep -q ' /hm-e2e/throwing ' "$work/serve.log" || { echo "FAIL: the app's timer never ran (no /hm-e2e/throwing in the Companion's log)"; keep; exit 1; }
sleep 5
after=$(pid)
[ -n "$shot" ] && perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "$shot" >/dev/null 2>&1
[ "$after" = "$before" ] || quit "$after"
grep ' /hm-e2e/throwing ' "$work/serve.log" | head -1 | sed 's/^/  app: /'
echo "ok: the app is still running 5 s after the error (pid $after)"
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
rc=0
TEST_RUNNER_SPACE=sperror perl -e 'alarm 300; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/OnError -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 || rc=$?
if [ "$rc" != 0 ]; then echo "FAIL: the tap after the error (xcodebuild test exit $rc)"; grep -E "error:|XCTAssert|failed" "$work/uitest/test.log" | head -8; keep; exit 1; fi
echo "ok: after the error a tap on the space opens its terminal"
echo PASS
