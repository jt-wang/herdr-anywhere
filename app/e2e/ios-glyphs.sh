#!/bin/sh
# Passes when the iOS terminal draws every character in e2e/agent-glyphs.ts, none of them as "missing", and
# its emoji in colour wherever the phone has emoji at all.
# Runs on any simulator: iOS 26.3 draws a missing character as a box, iOS 17.0 and 26.5 as nothing (ios-glyphs.tsx).
# Usage: SIM_UDID=<udid> e2e/ios-glyphs.sh [screenshot.png]   Needs nsh, go, idb, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
shot=${1:-}
app=$(cd "$(dirname "$0")/.." && pwd)
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
"$app/ios/build.sh" ios-glyphs.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
line=""
for _ in $(seq 1 40); do
  line=$(perl -e 'alarm 60; exec @ARGV' idb ui describe-all --udid "$SIM_UDID" 2>/dev/null | grep -o 'GLYPHS instrument=[^"]*' | head -1 || true)
  [ -n "$line" ] && break
  sleep 1
done
[ -n "$shot" ] && perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "$shot" >/dev/null 2>&1
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
[ -n "$line" ] || { echo "FAIL: the probe gave no answer within 40 s"; exit 1; }
echo "$line"
case "$line" in *"instrument=true"*) ;; *) echo "FAIL: the probe cannot tell a drawn character from a missing one"; exit 1 ;; esac
case "$line" in *"need=none "*) ;; *) echo "FAIL: characters the phone could not draw"; exit 1 ;; esac
# Emoji are the phone's: a phone that draws none with a plain system font is not the page's fault.
case "$line" in
  *"emojiPage=none "*) echo "PASS: every character is drawn, and emoji in colour" ;;
  *"emojiSystem=none "*) echo "FAIL: the phone draws emoji, the terminal page does not"; exit 1 ;;
  *) echo "PASS: every character is drawn. NOTE: this simulator draws no emoji, even with a plain system font" ;;
esac
