#!/bin/sh
# The input box under a terminal shows its hint whole at iOS text sizes: the default (Large), the largest standard one
# and the largest accessibility one. The XCUITest in e2e/ios-uitest (Composer) opens Try it (sample data, no computer),
# a terminal and its input box; this script reads the box out of the screenshot, as e2e/android-composer.sh does: each
# line of the hint must stand at least 0.6 of the text size tall. The text size is the box's 14 pt times the body
# text's size at that setting over its size at Large (17 pt), as iOS sets them. Saves composer-<size>.png. The
# simulator's text size is put back to what it was and the simulator shut down.
# Usage: SIM_UDID=<udid> e2e/ios-composer.sh <folder for screenshots> [Herdr.app]
# Without Herdr.app the app is built from App.tsx (ios/build.sh). Needs nsh, go, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
shots=$1; mkdir -p "$shots"; shots=$(cd "$shots" && pwd)
bundle=${2:-}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
work=$(mktemp -d)
size0=""
cleanup() {
  [ -n "$size0" ] && xcrun simctl ui "$SIM_UDID" content_size "$size0" 2>/dev/null
  rm -rf "$work"
  xcrun simctl shutdown "$SIM_UDID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

if [ -z "$bundle" ]; then
  "$app/ios/build.sh" App.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
  bundle="$app/ios/build/Herdr.app"
fi
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
perl -e 'alarm 600; exec @ARGV' xcodebuild build-for-testing -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/build.log" 2>&1 || { echo "FAIL: the XCUITest did not build"; tail -5 "$work/build.log"; exit 1; }

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
size0=$(xcrun simctl ui "$SIM_UDID" content_size)
failed=0
# Body text at each setting, in points: 17 at Large.
for size in large:17 extra-extra-extra-large:23 accessibility-extra-extra-extra-large:53; do
  name=${size%:*}; body=${size#*:}
  xcrun simctl ui "$SIM_UDID" content_size "$name"
  xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
  xcrun simctl install "$SIM_UDID" "$bundle"
  xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
  rm -f "$work/frame.txt"
  rc=0
  TEST_RUNNER_OUT="$work/frame.txt" TEST_RUNNER_SHOT="$shots/composer-$name.png" perl -e 'alarm 300; exec @ARGV' xcodebuild test-without-building -quiet \
    -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/Composer -destination "id=$SIM_UDID" \
    -derivedDataPath "$work/uitest/derived" > "$work/test-$name.log" 2>&1 || rc=$?
  if [ "$rc" != 0 ] || [ ! -s "$work/frame.txt" ]; then
    echo "FAIL: $name: the XCUITest did not reach the input box (exit $rc)"; grep -E "error:|XCTAssert|failed" "$work/test-$name.log" | head -4 | sed 's/^/  /'
    cp "$work/test-$name.log" "$shots/test-$name.log"
    failed=1; continue
  fi
  verdict=$(python3 - "$shots/composer-$name.png" $(cat "$work/frame.txt") "$body" <<'PY'
import sys
from collections import Counter
from PIL import Image
img = Image.open(sys.argv[1]).convert('RGB')
x, y, w, h, scale, body = map(float, sys.argv[2:8])
l, t, r, b = round(x * scale), round(y * scale), round((x + w) * scale), round((y + h) * scale)
em = 14 * body / 17 * scale
# Inside the box's border: 3 px top and bottom left out, and the rounded corners, 16 pt at each end.
inner = (l + round(16 * scale), t + 3, r - round(16 * scale), b - 3)
px = img.load()
bg = Counter(px[i, j] for i in range(inner[0], inner[2], 3) for j in range(inner[1], inner[3], 3)).most_common(1)[0][0]
far = lambda c: sum(abs(c[k] - bg[k]) for k in range(3)) > 120
ink = [j for j in range(inner[1], inner[3]) if sum(far(px[i, j]) for i in range(inner[0], inner[2])) >= 2]
if not ink:
    print(f'no hint in the box ({b - t} px tall)')
else:
    lines = [[ink[0], ink[0]]]
    for j in ink[1:]:
        if j == lines[-1][1] + 1: lines[-1][1] = j
        else: lines.append([j, j])
    tall = [e - s + 1 for s, e in lines]
    whole = all(v >= 0.6 * em for v in tall)
    print(f'{"whole" if whole else "cut"}: box {b - t} px, lines {tall} px tall at {em:g} px text, {ink[0] - t} px below the box top')
PY
)
  case $verdict in whole*) echo "ok: $name: the hint shows whole ($verdict)";; *) echo "FAIL: $name: $verdict"; failed=1;; esac
done
xcrun simctl ui "$SIM_UDID" content_size "$size0"
back=$(xcrun simctl ui "$SIM_UDID" content_size)
[ "$back" = "$size0" ] || { echo "FAIL: the text size reads $back, not $size0, after putting it back"; failed=1; }
size0=""
[ $failed = 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
