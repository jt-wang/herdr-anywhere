#!/bin/sh
# Pictures of each menu and of a toast on an iOS simulator, light and dark, to compare a change's before and after: the
# app built from ios-e2e-menus.tsx (the fake core's busy scene, made-up names, no Companion), driven by XCUITest
# (e2e/ios-uitest Menus: real touches from a fresh launch, a picture only once the menu's own words are on screen),
# status bar at 9:41. Also what a swipe up, a tap and a drag in from the left edge do to the toast (light only).
# With HM_MENUS_SE=1 the same again on an iPhone SE (375 pt, no Dynamic Island) made on the same runtime, deleted after.
# Writes FOLDER/<phone>/<scheme>-<picture>.png and <scheme>.txt (what the test saw). Fails when a picture's words never
# showed, so no wrong picture is kept.
# Usage: SIM_UDID=<udid> [HM_MENUS_SE=1] e2e/ios-menus.sh FOLDER
# SIM_UDID is required, so the run never lands on another simulator. Needs nsh, go, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
out=${1:?usage: e2e/ios-menus.sh FOLDER}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
work=$(mktemp -d)
mkdir -p "$out"
cur=""; se=""
cleanup() {
  if [ -n "$cur" ]; then
    xcrun simctl uninstall "$cur" "$id" 2>/dev/null || true
    xcrun simctl status_bar "$cur" clear 2>/dev/null || true
    xcrun simctl ui "$cur" appearance light 2>/dev/null || true
  fi
  [ -n "$se" ] && { xcrun simctl shutdown "$se" 2>/dev/null; xcrun simctl delete "$se" 2>/dev/null; } || true
  rm -rf "${work:?}"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM

"$app/ios/build.sh" ios-e2e-menus.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
cp -R "$app/ios/build/Herdr.app" "$work/Herdr.app"
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
X="-project $work/uitest/KeyboardCheck.xcodeproj -scheme UITests -derivedDataPath $work/uitest/derived"
# shellcheck disable=SC2086
perl -e 'alarm 900; exec @ARGV' xcodebuild build-for-testing -quiet $X -destination 'generic/platform=iOS Simulator' > "$work/runner.log" 2>&1 ||
  { tail -20 "$work/runner.log"; echo "FAIL: building the UI test runner"; exit 1; }

failed=0
phone() { # udid name
  cur=$1; d="$out/$2"; mkdir -p "$d"
  xcrun simctl boot "$cur" 2>/dev/null || true
  xcrun simctl bootstatus "$cur" -b >/dev/null
  # Right after boot the system shell may not be up yet, and a test launched then hangs: wait until an app launches.
  i=0; until xcrun simctl launch "$cur" com.apple.Preferences >/dev/null 2>&1 || [ $i -gt 30 ]; do sleep 2; i=$((i + 1)); done
  xcrun simctl terminate "$cur" com.apple.Preferences 2>/dev/null || true; sleep 3
  xcrun simctl status_bar "$cur" override --time 9:41 --dataNetwork wifi --wifiMode active --wifiBars 3 --cellularMode active --cellularBars 4 --batteryState charged --batteryLevel 100
  xcrun simctl uninstall "$cur" "$id" 2>/dev/null || true
  xcrun simctl install "$cur" "$work/Herdr.app"
  xcrun simctl launch "$cur" "$id" >/dev/null; sleep 8 # a cold first launch is slow
  for scheme in light dark; do
    xcrun simctl ui "$cur" appearance "$scheme"
    rc=0
    # shellcheck disable=SC2086
    TEST_RUNNER_OUT="$d/$scheme.txt" TEST_RUNNER_SHOTS="$d/$scheme" TEST_RUNNER_ROW="$work/row.json" TEST_RUNNER_BUNDLE="$id" TEST_RUNNER_SCHEME="$scheme" \
      perl -e 'alarm 900; exec @ARGV' xcodebuild test-without-building -quiet $X -only-testing:UITests/Menus -destination "id=$cur" \
      -resultBundlePath "$work/result-$2-$scheme.xcresult" > "$work/test-$2-$scheme.log" 2>&1 || rc=$?
    sed "s/^/  $2 $scheme: /" "$d/$scheme.txt" 2>/dev/null || true
    if [ "$rc" != 0 ] || [ ! -s "$d/$scheme.txt" ] || grep -q '^FAIL' "$d/$scheme.txt"; then
      echo "FAIL: $2 $scheme (xcodebuild test exit $rc)"; failed=1
      "$app/e2e/ios-xcresult.sh" "$work/result-$2-$scheme.xcresult" "$d/$scheme-failure" all || true
    else echo "ok: $2 $scheme, $(ls "$d" | grep -c "^$scheme-.*\.png$") pictures"; fi
  done
  xcrun simctl uninstall "$cur" "$id"; xcrun simctl status_bar "$cur" clear; xcrun simctl ui "$cur" appearance light
}

runtime=$(xcrun simctl list devices -j | python3 -c '
import json, sys
for rt, ds in json.load(sys.stdin)["devices"].items():
    for d in ds:
        if d["udid"] == sys.argv[1]: print(rt); print(d.get("deviceTypeIdentifier", "").rsplit(".", 1)[-1])' "$SIM_UDID")
name=$(echo "$runtime" | sed -n 2p | tr -cd 'A-Za-z0-9-'); runtime=$(echo "$runtime" | sed -n 1p)
[ -n "$runtime" ] || { echo "FAIL: no simulator $SIM_UDID"; exit 1; }
phone "$SIM_UDID" "${name:-phone}"
cur=""
if [ "${HM_MENUS_SE:-}" = 1 ]; then
  xcrun simctl shutdown "$SIM_UDID" 2>/dev/null || true
  se=$(xcrun simctl create hm-se "iPhone SE (3rd generation)" "$runtime") || { echo "FAIL: no iPhone SE on $runtime"; exit 1; }
  phone "$se" iPhone-SE
  cur=""; xcrun simctl shutdown "$se" 2>/dev/null || true; xcrun simctl delete "$se"; se=""
fi
[ "$failed" = 0 ] && echo PASS || exit 1
