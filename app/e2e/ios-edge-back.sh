#!/bin/sh
# Going back with a swipe from the screen's left edge, on the iOS simulator (XCUITest), on the app's Try it data: no
# computer, no companion. e2e/ios-uitest's EdgeBack test swipes on each page that has one to go back to (a space on a
# working agent, a space on an agent that asks, Settings from home, Settings from a space), at three heights, two speeds
# and two distances from the edge, N times each. Prints how many went back, per page, height, speed and distance, and
# how many changed the terminal's tab instead. Passes when every swipe went back; MIN_BACK=<percent> lowers the bar.
# Usage: SIM_UDID=<udid> [APP=<Herdr.app> [MARK=<string in its bundle>]] [RECORD=<file.mp4>] [N=6] [PAGES=space,space-asking,settings,space-settings] [HEIGHTS=header,middle,low] [SPEEDS=slow,fast]
#        [STARTS=2,12] e2e/ios-edge-back.sh [results.txt]
# SIM_UDID is required, so the run never lands on another simulator. Needs nsh, go, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
work=$(mktemp -d)
out=${1:-$work/edge-back.txt}
cleanup() {
  xcrun simctl shutdown "$SIM_UDID" 2>/dev/null || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}"
}
trap cleanup EXIT
trap 'exit 1' INT TERM # cleanup runs on EXIT; a bare TERM trap would carry on after it
# APP installs another build of the app; MARK is a string its bundle must hold, checked in the copy installed too.
build=${APP:-$app/ios/build/Herdr.app}
if [ -n "${MARK:-}" ] && ! grep -a -q "$MARK" "$build/nsh/bundle.nshc"; then echo "FAIL: $build does not hold $MARK"; exit 1; fi
[ -n "${SKIP_BUILD:-}${APP:-}" ] || "$app/ios/build.sh" App.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$build"
if [ -n "${MARK:-}" ] && ! grep -a -q "$MARK" "$(xcrun simctl get_app_container "$SIM_UDID" "$id")/nsh/bundle.nshc"; then echo "FAIL: the installed app does not hold $MARK"; exit 1; fi
# A first launch on a fresh boot is slow; the test's own launch comes after this one.
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null; sleep 8; xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true

cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
: > "$out"
rc=0
# RECORD=<file.mp4>: the simulator's screen while the test runs.
rec_pid=""
# <file>.start: when the recording started, in ms since 1970, for the test's "touch" lines (when each finger went down).
if [ -n "${RECORD:-}" ]; then
  xcrun simctl io "$SIM_UDID" recordVideo --codec h264 --force "$RECORD" > "$work/rec.log" 2>&1 & rec_pid=$!
  # recordVideo prints "Recording started" once it records.
  i=0; until grep -q "Recording started" "$work/rec.log" || [ $i -gt 50 ]; do i=$((i + 1)); sleep 0.1; done
  # Second line: how long that took in tenths of a second (51: the line never came, and the start is late by up to 5 s).
  { python3 -c 'import time; print(int(time.time() * 1000))'; echo "$i"; } > "${RECORD%.*}.start"; sleep 1
fi
TEST_RUNNER_OUT="$out" TEST_RUNNER_N="${N:-6}" TEST_RUNNER_PAGES="${PAGES:-space,space-asking,settings,space-settings}" \
  TEST_RUNNER_HEIGHTS="${HEIGHTS:-}" TEST_RUNNER_SPEEDS="${SPEEDS:-}" TEST_RUNNER_STARTS="${STARTS:-}" \
  perl -e 'alarm 5400; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -only-testing:UITests/EdgeBack -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
  -clonedSourcePackagesDirPath "$work/uitest/spm" -packageCachePath "$work/uitest/spm-cache" > "$work/test.log" 2>&1 || rc=$?
# recordVideo writes the file out when it gets SIGINT.
[ -n "$rec_pid" ] && { kill -INT "$rec_pid"; wait "$rec_pid" 2>/dev/null || true; }
if [ "$rc" != 0 ]; then
  echo "FAIL: the EdgeBack test (xcodebuild test exit $rc)"; grep -E "error:|XCTAssert|failed|Failed" "$work/test.log" | head -12
  cp "$work/test.log" "${out%.txt}.xcodebuild.log" 2>/dev/null; exit 1
fi
head -1 "$out"; grep "^under " "$out" || true
# page height distance speed: went back / swipes, tab changed
awk '/^page=/{ split($0, f, " "); for (i in f) { split(f[i], kv, "="); v[kv[1]] = kv[2] }
  if (v["back"] == "skip") { skip++; next }
  k = sprintf("%-15s %-6s x=%-2s %-4s", v["page"], v["y"], v["x"], v["v"]); n[k]++; b[k] += v["back"]; if (v["tab"] == "changed") c[k]++
  all++; ok += v["back"] }
  END { for (k in n) printf "%s  back %d/%d  tab changed %d\n", k, b[k], n[k], c[k]; printf "TOTAL back %d/%d, skipped %d\n", ok, all, skip }' "$out" | sort
min=${MIN_BACK:-100}
awk -v min="$min" '/^page=/ && !/back=skip/{ all++; if (/back=1/) ok++ } END { exit !(all > 0 && ok * 100 >= min * all) }' "$out" \
  && echo "PASS: at least $min% of the edge swipes went back" || { echo "FAIL: fewer than $min% of the edge swipes went back"; exit 1; }
