#!/bin/bash
# An iOS simulator on a GitHub macOS runner: Xcode 26.6 and a new iPhone 17 on iOS 26.5. Not 26.3: its simulator's
# JavaScriptCore crashes the terminal page about half the time (a simulator-only address cut; 26.5 is fixed).
# HM_XCODE and HM_IOS pick another Xcode and runtime (tests.yml's ios-main job: Xcode 27.0 and iOS 27.0, the SDK and
# the system the TestFlight build and its testers' phones have).
# Writes SIM_UDID to $GITHUB_ENV; the suites take it so they never land on another simulator.
# Usage: [HM_XCODE=/Applications/Xcode_27.0.app HM_IOS="iOS 27.0"] app/e2e/ci/ios-up.sh
set -euo pipefail
xcode=${HM_XCODE:-/Applications/Xcode_26.6.app}
ios=${HM_IOS:-iOS 26.5}
sudo xcode-select -s "$xcode"
# Not piped into head: xcodebuild aborts when its output closes early (SIGPIPE, exit 134 on the runner).
xcodebuild -version > "${RUNNER_TEMP:-/tmp}/xcode-version" && head -1 "${RUNNER_TEMP:-/tmp}/xcode-version"
runtime=$(xcrun simctl list runtimes -j | python3 -c '
import json, sys
r = [x for x in json.load(sys.stdin)["runtimes"] if x["name"] == sys.argv[1] and x["isAvailable"]]
print(r[0]["identifier"] if r else "")' "$ios")
[ -n "$runtime" ] || { echo "FAIL: no $ios simulator runtime"; xcrun simctl list runtimes; exit 1; }
type=$(xcrun simctl list devicetypes -j | python3 -c '
import json, sys
t = [x["identifier"] for x in json.load(sys.stdin)["devicetypes"] if x["name"] == "iPhone 17"]
print(t[0] if t else "")')
[ -n "$type" ] || { echo "FAIL: no iPhone 17 device type"; exit 1; }
udid=$(xcrun simctl create hm "$type" "$runtime")
xcrun simctl boot "$udid"
xcrun simctl bootstatus "$udid" -b >/dev/null
echo "SIM_UDID=$udid" >> "${GITHUB_ENV:-/dev/null}"
echo "booted: $udid, $runtime"
