#!/bin/sh
# A picture in its sheet, zoomed on an iOS simulator with XCUITest's touches (e2e/ios-uitest Zoom): pinched it zooms,
# dragged it moves and stops at the picture's edge, a double tap zooms out and another in, holding it still copies it,
# Copy still copies the path, and Close still closes. Light, then dark.
# The app is e2e/picture-app.tsx (the made-up computer of fixtures/fake.tsx with the drawn sunset open), packaged
# under its own id by e2e/zoom-package.sh, so an installed Herdr Anywhere stays as it is. Fake data only; needs no
# computer of its own.
# CHAT=1: the sunset is in a chat (e2e/chat-picture-params.ts) and is opened from there with a tap, on the whole
# screen. Then also: Close goes back to the chat, a swipe down closes it, and a drag down while zoomed in moves the
# picture instead. The .app given must be packaged with CHAT=1 too.
# Usage: [CHAT=1] SIM_UDID=<udid> e2e/ios-zoom.sh [APP] [folder for screenshots and the recording]
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
  if [ $scheme = light ] && [ -n "$out" ]; then xcrun simctl io "$SIM_UDID" recordVideo --force "$out/zoom-ios.mp4" > "$out/record.log" 2>&1 & rec=$!; fi
  rc=0
  TEST_RUNNER_OUT="$work/$scheme.txt" TEST_RUNNER_SHOTS="$work/$scheme" TEST_RUNNER_BUNDLE=$id TEST_RUNNER_CHAT=${CHAT:-} \
    perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:UITests/Zoom -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/test-$scheme.log" 2>&1 || rc=$?
  # A recording that ended by itself is said, and the checks still run: under set -e the failed kill ended the whole
  # suite before xcodebuild's result was read. recordVideo can end at once, e.g. "you don't have permission" to save
  # into the folder; its own words are in record.log.
  if [ -n "$rec" ]; then
    if kill -0 $rec 2>/dev/null; then kill -INT $rec 2>/dev/null || true; wait $rec 2>/dev/null || true
    else rrc=0; wait $rec 2>/dev/null || rrc=$?
      echo "note: the screen recording stopped part way (exit $rrc); $out/record.log: $(tail -3 "$out/record.log" | tr '\n' ' ')"; fi
  fi
  sed 's/^/  /' "$work/$scheme.txt" 2>/dev/null || true
  # The shots taken, a failed run's too: they show where it stopped (and a build from before a change, as it was).
  [ -z "$out" ] || for f in "$work/$scheme"-*.png; do [ ! -f "$f" ] || cp "$f" "$out/"; done
  [ "$rc" = 0 ] || { say "FAIL: xcodebuild test exit $rc"; grep -E "error:|XCTAssert|failed" "$work/test-$scheme.log" | head -8; continue; }
  grep -q "sheet open after the drags: true" "$work/$scheme.txt" && say "ok: the sheet stays open under the drags" || { say "FAIL: the drags closed the sheet"; continue; }
  box=$(sed -n 's/^box //p' "$work/$scheme.txt")
  bg=$([ $scheme = dark ] && echo '#25272b' || echo '#ffffff') # the sheet's colour (nsh.json popover)
  zoom_checks "$work" $scheme "$box" "$bg"
  grep -q "held, asks to copy: true" "$work/$scheme.txt" && say "ok: holding the picture still asks to copy it" || say "FAIL: holding the picture said nothing"
  grep -q "Copy says copied: true" "$work/$scheme.txt" && say "ok: Copy still copies the path" || say "FAIL: Copy said nothing"
  grep -q "closed: true" "$work/$scheme.txt" && say "ok: Close closes the sheet" || say "FAIL: Close left the sheet open"
  if [ "${CHAT:-}" = 1 ]; then
    grep -q "back in the chat: true" "$work/$scheme.txt" && say "ok: Close goes back to the chat" || say "FAIL: Close did not go back to the chat"
    grep -q "whole screen: true" "$work/$scheme.txt" && say "ok: the picture opens on the whole screen" || say "FAIL: the sheet is not on the whole screen"
    grep -q "a swipe down closes it: true" "$work/$scheme.txt" && say "ok: a swipe down closes the picture" || say "FAIL: a swipe down left the picture open"
    grep -q "zoomed in, open after a drag down: true" "$work/$scheme.txt" && say "ok: zoomed in, a drag down moves the picture and leaves it open" || say "FAIL: zoomed in, a drag down closed the picture"
  fi
done
[ $failed = 0 ] && echo PASS || { echo FAILED; exit 1; }
