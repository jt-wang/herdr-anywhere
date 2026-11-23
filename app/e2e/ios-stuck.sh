#!/bin/sh
# What the iOS app shows when it still can't reach the computer, on a simulator through TailPocket over Tailcat's real
# relays: paired with a companion of its own (an isolated herdr-core with a fake project, `serve --tailcat`, the invite
# handed over in the app's storage as e2e/ios-tailcat.sh does), then the companion stops. Through the tunnel that is no
# answer, so after STUCK_MS (store.ts) the banner says it still can't reach the computer, with Try again and Other
# options (kit.tsx ConnBanner); Other options opens the sheet of causes (StuckSheet). XCUITest (e2e/ios-uitest Stuck)
# checks both and shoots them, in the light theme and the dark. Then the same on the local network alone (`serve
# --routes lan`), where the stopped computer refuses the connection: Companion must come first (in OUT/lan).
# Usage: SIM_UDID=<udid> [HM_STUCK_ROUTES="tailcat lan"] e2e/ios-stuck.sh <output dir>
# Needs the internet, go, nsh, hc, xcodegen and Xcode.
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard ios-stuck # a login entry of its own, never the person's
. "$(dirname "$0")/tailcat-invite.sh" # the invite pair shows, and the y its emoji check asks for
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
out=${1:?output dir}
mkdir -p "$out"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
# The companion's state holds the Tailcat identity (private keys); its control socket path must stay under 104 bytes.
state=$(mktemp -d /tmp/hmstXXXX)
mkdir -p "$work/u/src/acme-api" "$work/state" "$work/config" "$work/data" "$work/t"
: > "$work/u/.zshrc"
cleanup() {
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  xcrun simctl ui "$SIM_UDID" appearance light 2>/dev/null || true
  HC_SESSION="hmst-$$" HC_SOCKET="" HOME="$work/u" XDG_RUNTIME_DIR="$run" "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  autostart_guard_end || exit 1
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM

# Tailcat's pairing window lasts 2 minutes: the app and the XCUITest runner are built before it opens.
"$app/ios/build.sh" ios-e2e.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
perl -e 'alarm 1200; exec @ARGV' xcodebuild build-for-testing -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/build.log" 2>&1 \
  || { echo "FAIL: the XCUITest runner did not build"; tail -20 "$work/uitest/build.log"; exit 1; }
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
store="$(xcrun simctl get_app_container "$SIM_UDID" "$id" data)/Library/Application Support/nsh/$id/storage.json"

hcenv() { HC_SESSION="hmst-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/u/.gitconfig" "$@"; }
hcenv "$hc" run --bg --keep --cwd "$work/u/src/acme-api" --label herdr.space=spst --label herdr.tab=tbst >/dev/null

# start <tailcat|lan>: a companion on a fresh state, the app paired with it from a fresh install's storage, connected.
start() {
  rm -rf "$state"; state=$(mktemp -d /tmp/hmstXXXX); : > "$work/serve.log"
  case $1 in
    tailcat) routes="--tailcat $(tailcat_no_lan "$work/herdr-anywhere")"; ready="listening on Tailcat" ;;
    lan) routes="--routes lan"; ready="listening for phones on the local network" ;;
  esac
  (
    export HC_SESSION="hmst-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh
    export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
    export GIT_CONFIG_GLOBAL="$work/u/.gitconfig"
    # shellcheck disable=SC2086 # $routes is the flags, split
    exec "$work/herdr-anywhere" serve $routes --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1
  ) &
  serve_pid=$!
  i=0
  until grep -q "$ready" "$work/serve.log" 2>/dev/null; do
    i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: serve $routes did not start"; tail -5 "$work/serve.log"; exit 1; }
    sleep 0.1
  done
  invite=$(tailcat_invite "$work/herdr-anywhere" "$state" "$work/pair.out")
  [ -n "$invite" ] || { echo "FAIL: pair printed no invite"; exit 1; }
  xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
  mkdir -p "$(dirname "$store")"
  python3 -c 'import json,sys; json.dump({"e2e-pair": sys.argv[2]}, open(sys.argv[1], "w"))' "$store" "$invite"
  xcrun simctl ui "$SIM_UDID" appearance light
  xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
  i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do
    i=$((i + 1)); [ $i -gt 120 ] && { echo "FAIL: $1: not connected within 120 s"; exit 1; }; sleep 1
  done
  sleep 3
}

# The companion stops. Through Tailcat's relays that is no answer: the likeliest cause is the network (the hint's
# default). On the local network the computer refuses the connection (TailPocket's lastFailure "refused"), which
# ranks Companion first: Stuck checks the line under the banner and that the sheet lists Companion above the network.
for route in ${HM_STUCK_ROUTES:-tailcat lan}; do
  dir="$out"; expect=""
  [ "$route" = lan ] && { dir="$out/lan"; expect=companion; mkdir -p "$dir"; }
  start "$route"
  xcrun simctl io "$SIM_UDID" screenshot "$dir/0-connected.png" >/dev/null 2>&1 || true
  echo "$route: the companion stops"
  kill "$serve_pid"; wait "$serve_pid" 2>/dev/null || true; serve_pid=""
  for look in light dark; do
    xcrun simctl ui "$SIM_UDID" appearance "$look"
    rc=0
    TEST_RUNNER_DIR="$dir" TEST_RUNNER_THEME="$look" TEST_RUNNER_EXPECT="$expect" perl -e 'alarm 600; exec @ARGV' xcodebuild test-without-building -quiet \
      -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/Stuck -destination "id=$SIM_UDID" \
      -derivedDataPath "$work/uitest/derived" -resultBundlePath "$work/uitest/result-$route-$look.xcresult" > "$work/uitest/test-$route-$look.log" 2>&1 || rc=$?
    if [ "$rc" != 0 ]; then
      echo "FAIL: $route, $look: xcodebuild test exit $rc"
      grep -E "error:|XCTAssert|failed" "$work/uitest/test-$route-$look.log" | head -10 | sed 's/^/  /'
      xcrun simctl io "$SIM_UDID" screenshot "$dir/$look-failure.png" >/dev/null 2>&1 || true
      "$app/e2e/ios-xcresult.sh" "$work/uitest/result-$route-$look.xcresult" "$dir/$look-failure" all || true
      exit 1
    fi
    echo "ok: $route, $look"
  done
done
echo "PASS: stopped, the banner says it still can't reach the computer with Try again and Other options; the sheet lists each cause with what to do (light and dark); on the local network the computer refuses, and Companion comes first: $out"
