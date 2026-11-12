#!/bin/sh
# A picture in its sheet, zoomed on an iOS simulator with XCUITest's touches (e2e/ios-uitest Zoom): pinched it zooms,
# dragged it moves and stops at the picture's edge, a double tap zooms out and another in, holding it still copies it,
# Copy still copies the path, and Close still closes. Light, then dark.
# The app is e2e/picture-app.tsx (the made-up computer of fixtures/fake.tsx with the drawn sunset open), packaged
# under its own id by e2e/zoom-package.sh, so an installed Herdr Anywhere stays as it is. Fake data only; needs no
# computer of its own.
# Usage: SIM_UDID=<udid> e2e/ios-zoom.sh [APP] [folder for screenshots and the recording]
# SIM_UDID is required, so the run never lands on another simulator (an iPhone SE is 375 pt wide). Without a
# simulator .app it packages one (nsh). Needs xcodegen, Xcode and python3 with Pillow.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
bundle=""; case ${1:-} in *.app|*.app/) bundle=${1%/}; shift;; esac
out=${1:-}
id=io.jingtao.herdranywhere.zoomcheck
work=$(mktemp -d)
. "$app/e2e/zoom-checks.sh"
appearance=""
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null
  [ -z "$appearance" ] || { xcrun simctl ui "$SIM_UDID" appearance "$appearance"; echo "put back: appearance $(xcrun simctl ui "$SIM_UDID" appearance)"; }
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

[ -n "$bundle" ] || bundle=$("$app/e2e/zoom-package.sh" ios-simulator "$work/app")
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
appearance=$(xcrun simctl ui "$SIM_UDID" appearance)
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$bundle"
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
[ -z "$out" ] || mkdir -p "$out"
for scheme in light dark; do
  xcrun simctl ui "$SIM_UDID" appearance $scheme
  xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
  xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
  rec=""
  if [ $scheme = light ] && [ -n "$out" ]; then xcrun simctl io "$SIM_UDID" recordVideo --force "$out/zoom-ios.mp4" >/dev/null 2>&1 & rec=$!; fi
  rc=0
  TEST_RUNNER_OUT="$work/$scheme.txt" TEST_RUNNER_SHOTS="$work/$scheme" TEST_RUNNER_BUNDLE=$id \
    perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/Zoom -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/test-$scheme.log" 2>&1 || rc=$?
  [ -z "$rec" ] || { kill -INT $rec; wait $rec 2>/dev/null || true; }
  sed 's/^/  /' "$work/$scheme.txt" 2>/dev/null || true
  [ "$rc" = 0 ] || { say "FAIL: xcodebuild test exit $rc"; grep -E "error:|XCTAssert|failed" "$work/test-$scheme.log" | head -8; continue; }
  [ -z "$out" ] || for f in "$work/$scheme"-*.png; do cp "$f" "$out/"; done
  grep -q "sheet open after the drags: true" "$work/$scheme.txt" && say "ok: the sheet stays open under the drags" || { say "FAIL: the drags closed the sheet"; continue; }
  box=$(sed -n 's/^box //p' "$work/$scheme.txt")
  bg=$([ $scheme = dark ] && echo '#25272b' || echo '#ffffff') # the sheet's colour (nsh.json popover)
  zoom_checks "$work" $scheme "$box" "$bg"
  grep -q "held, asks to copy: true" "$work/$scheme.txt" && say "ok: holding the picture still asks to copy it" || say "FAIL: holding the picture said nothing"
  grep -q "Copy says copied: true" "$work/$scheme.txt" && say "ok: Copy still copies the path" || say "FAIL: Copy said nothing"
  grep -q "closed: true" "$work/$scheme.txt" && say "ok: Close closes the sheet" || say "FAIL: Close left the sheet open"
done
[ $failed = 0 ] && echo PASS || { echo FAILED; exit 1; }
