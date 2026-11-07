#!/bin/sh
# e2e/key-row.test.tsx on a real phone at its narrowest: the key row must stay on one line.
#   e2e/key-row.sh android OUT   ANDROID_SERIAL's screen at 360 dp and 411 dp, and at 360 dp with the largest text size
#   e2e/key-row.sh ios UDID OUT  that simulator at the default text size and the largest one (left booted)
# Screenshots go to OUT as key-row-<where>.png. The Android screen's density and text size are put back after.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd); cd "$here"
fail=0
# The look in use, named as the gallery names it (src/ui/pane-bar.tsx KEY_ROW_LOOK).
look=$(sed -nE "s/^export const KEY_ROW_LOOK: KeyRowLook = \{ labels: '([a-z]+)', size: '([a-z]+)' \}.*/\1-\2/p" src/ui/pane-bar.tsx)
run() { # $1: name for the screenshot; the rest: nsh test's target arguments
  name=$1; shift
  mkdir -p e2e/shots; rm -f e2e/shots/key-row.png
  if nsh test "$@" e2e/key-row.test.tsx > "$out/key-row-$name.log" 2>&1; then echo "ok: $name"; else echo "FAIL: $name"; fail=1; fi
  grep -E '^[a-z]+-[a-z]+: ' "$out/key-row-$name.log" | sed 's/^/  /' || true
  if [ -f e2e/shots/key-row.png ]; then mv e2e/shots/key-row.png "$out/key-row-$name.png"; fi
}
case ${1:-} in
  android)
    : "${ANDROID_SERIAL:?set ANDROID_SERIAL}"; out=$2; mkdir -p "$out"
    adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
    # nsh test signs its APK with apksigner, which needs a JDK (the same one android/build.sh uses).
    export JAVA_HOME="${JAVA_HOME:-/Applications/Android Studio.app/Contents/jbr/Contents/Home}"
    px=$("$adb" shell wm size | tail -1 | awk '{print $NF}' | cut -dx -f1)
    trap '"$adb" shell wm density reset; "$adb" shell settings put system font_scale 1.0' EXIT INT TERM
    for dp in 360 411; do "$adb" shell wm density $((px * 160 / dp)); sleep 3; run "android-${dp}dp" --target android; done
    "$adb" shell wm density $((px * 160 / 360)); "$adb" shell settings put system font_scale 2.0; sleep 3
    run android-360dp-largest-text --target android ;;
  ios)
    # The gallery built as the app (ios/build.sh), on that simulator, read from its accessibility tree with idb: a
    # script cannot drive nsh test's iOS host without its window.
    udid=$2; out=$3; mkdir -p "$out"
    id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' nsh.json)
    ios/build.sh e2e/key-row-gallery.tsx simulator > "$out/key-row-ios-build.log" 2>&1 || { echo "FAIL: ios/build.sh"; tail -5 "$out/key-row-ios-build.log"; exit 1; }
    xcrun simctl boot "$udid" 2>/dev/null || true
    xcrun simctl bootstatus "$udid" -b >/dev/null
    xcrun simctl install "$udid" ios/build/Herdr.app
    trap 'xcrun simctl ui "$udid" content_size large' EXIT INT TERM
    name=$(xcrun simctl list devices | grep "$udid" | sed -E 's/^ *(.*) \(.*/\1/' | tr ' ()' '-__' | tr -s '_-')
    for size in large accessibility-extra-extra-extra-large; do
      xcrun simctl ui "$udid" content_size "$size"
      xcrun simctl terminate "$udid" "$id" 2>/dev/null || true
      xcrun simctl launch "$udid" "$id" >/dev/null; sleep 6
      perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$udid" screenshot "$out/key-row-$name-$size.png" >/dev/null 2>&1
      seen=$(perl -e 'alarm 60; exec @ARGV' idb ui describe-all --udid "$udid" 2>/dev/null | grep -oE '"AXLabel":"[a-z]+-[a-z]+: [^"]*"' | sed 's/"AXLabel":"//; s/"$//')
      echo "$seen" | sed "s/^/  $name $size: /"
      echo "$seen" | grep -q "^$look: fits$" && echo "ok: $name, $size ($look)" || { echo "FAIL: $name, $size: the key row ($look) wraps or cuts a label"; fail=1; }
    done ;;
  *) echo "usage: e2e/key-row.sh android OUT | ios UDID OUT" >&2; exit 2 ;;
esac
exit $fail
