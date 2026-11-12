#!/bin/sh
# A sub-agent kept as a tab, on an iOS simulator (CI: an iPhone SE, 375 pt wide), against a real Companion and
# herdr-core (e2e/subkeep-core.sh: an agent with two sub-agents, fake data only). XCUITest's taps (e2e/ios-uitest
# SubKeep), light then dark, each with a sub-agent of its own: its row on home opens a peek, Keep makes it a tab, and
# back on home its row opens that tab, with no peek and no second Keep. A screenshot at each step, for the
# before-and-after pictures: run on the commit before the fix, the second tap shows the peek again and Keep makes a
# second tab. Passes when, in both themes, the second tap opened the tab, the shared layout has one tab for the
# sub-agent, and it still runs.
# The app is built from ios-e2e.tsx; the pairing link goes into its storage after the install (as e2e/ios-tailcat.sh).
# Usage: SIM_UDID=<udid> e2e/ios-subkeep.sh OUT-DIR
#   OUT-DIR gets <theme>-1-home.png, -2-peek.png, -3-kept.png, -4-second-tap.png, -5-switcher.png.
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
out=${1:?usage: e2e/ios-subkeep.sh OUT-DIR}
app=$(cd "$(dirname "$0")/.." && pwd)
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$out"
. "$app/e2e/subkeep-core.sh"  # sk, sk_start, sk_tabs, sk_state, sk_stop
. "$app/e2e/companion.sh"     # companion_start, companion_stop
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  companion_stop || status=1
  xcrun simctl ui "$SIM_UDID" appearance light >/dev/null 2>&1
  sk_stop
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
sk_start "$work" "$run"
companion_start "$work/serve.log" sk "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$sk_hc" --state "$work/companion" || exit 1
"$app/ios/build.sh" ios-e2e.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl ui "$SIM_UDID" appearance light
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
link=$(sk "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$work/companion" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
store="$(xcrun simctl get_app_container "$SIM_UDID" "$id" data)/Library/Application Support/nsh/$id/storage.json"
mkdir -p "$(dirname "$store")"
python3 -c 'import json,sys; json.dump({"e2e-pair": sys.argv[2]}, open(sys.argv[1], "w"))' "$store" "$link"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do i=$((i + 1)); [ $i -gt 90 ] && { echo "FAIL: the app never connected"; exit 1; }; sleep 1; done

cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
failed=0
keep() { # THEME KID
  theme=$1 kid=$2
  xcrun simctl ui "$SIM_UDID" appearance "$theme"
  rc=0
  TEST_RUNNER_KID="$kid" TEST_RUNNER_OUT="$work/$theme.txt" TEST_RUNNER_SHOTS="$out/$theme" \
    perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/SubKeep -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
    -resultBundlePath "$work/uitest/result-$theme.xcresult" > "$work/uitest/test-$theme.log" 2>&1 || rc=$?
  sed 's/^/  /' "$work/$theme.txt" 2>/dev/null || true
  if [ "$rc" != 0 ]; then
    echo "FAIL: $theme (xcodebuild test exit $rc)"
    "$app/e2e/ios-xcresult.sh" "$work/uitest/result-$theme.xcresult" "$out/$theme-failure" all
    mkdir -p "$out/$theme-failure"
    # The screen's elements where the test stopped (SubKeep.swift), unless they hold a pairing link.
    for f in "$work/$theme.txt"-*.txt; do
      [ -f "$f" ] && ! grep -qE 'pair=|herdr[0-9]:|token=' "$f" && cp "$f" "$out/$theme-failure/elements-${f##*/$theme.txt-}"
    done
    tail -30 "$work/uitest/test-$theme.log" | sed 's/^/  /'
    failed=1; return
  fi
  grep -q '^first tap on the row: nothing opened' "$work/$theme.txt" && echo "NOTE: $theme: the first tap on the row opened nothing; the second did"
  second=$(sed -n 's/^second tap: //p' "$work/$theme.txt")
  tabs=$(sk_tabs "$kid"); state=$(sk_state "$kid")
  echo "$theme: tapping its row again opened: $second; tabs for it in the shared layout: $tabs; it is $state"
  [ "$second" = tab ] && [ "$tabs" = 1 ] && [ "$state" = live ] || failed=1
}
keep light "$sk_kid1"
keep dark "$sk_kid2"
[ "$failed" = 0 ] && echo PASS || { echo "FAIL: a sub-agent kept as a tab was offered as a peek again, or has more than one tab"; exit 1; }
