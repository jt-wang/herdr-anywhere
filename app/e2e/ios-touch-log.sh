#!/bin/sh
# Whether a tap reached the app at all: while an iOS suite's XCUITest runs, the simulator's log of the app's UIKit event
# dispatch is kept (log stream, debug level); the test notes "tapping …" just before each tap and a line just after.
# For each such pair this says how many touch events (UIEvent type 0) UIKit sent the app's windows in between, and keeps
# those lines. A tap the page never saw that UIKit did send was lost in the app; one UIKit never sent never reached it.
# Usage: e2e/ios-touch-log.sh --start UDID FILE &     the log, until killed
#        e2e/ios-touch-log.sh NOTES LOG [FOLDER]      the taps in NOTES (the test's notes) against LOG; FOLDER keeps the lines
#        e2e/ios-touch-log.sh --self-test
# The app's process is Herdr (ios/build.sh builds Herdr.app).
predicate='process == "Herdr" AND subsystem == "com.apple.UIKit"'
# HH:MM:SS.mmm to milliseconds since midnight.
ms() { echo "$1" | awk -F'[:.]' '{ printf "%d\n", (($1 * 60 + $2) * 60 + $3) * 1000 + $4 }'; }
report() {
  notes=$1 log=$2 keep=${3:-}
  n=$(wc -l < "$log" 2>/dev/null | tr -d ' ') || n=0
  echo "  the app's UIKit log: ${n:-0} lines$([ "${n:-0}" = 0 ] && echo ", nothing captured: it cannot tell a tap that reached the app")"
  [ -n "$keep" ] && : > "$keep/uikit-taps.txt"
  # Each "tapping" note and the note after it.
  awk '{ print $1 "\t" $0 }' "$notes" | awk -F'\t' 'p != "" { print p "\t" $1 "\t" w; p = "" } index($2, " tapping ") { p = $1; w = $2 }' |
  while IFS="$(printf '\t')" read -r from to what; do
    a=$(ms "$from"); b=$(($(ms "$to") + 500)); what=$(echo "$what" | cut -d' ' -f6-)
    # Compact lines: date, time, type, process, then the message.
    lines=$(awk -v a="$a" -v b="$b" '{ split($2, t, /[:.]/); m = ((t[1] * 60 + t[2]) * 60 + t[3]) * 1000 + t[4]
      if (m >= a && m <= b && /UIEvent type: 0;/) print }' "$log")
    count=$(printf '%s' "$lines" | grep -c . || true)
    held=""
    if [ "$count" -gt 0 ]; then
      first=$(printf '%s\n' "$lines" | head -1 | awk '{print $2}'); last=$(printf '%s\n' "$lines" | tail -1 | awk '{print $2}')
      held="; the first at $first, the last $(($(ms "$last") - $(ms "$first"))) ms after (500 ms or more held is a long press)"
    fi
    echo "  UIKit sent the app $count touch events from $from to $to ($what)$held; the page's own record says whether it got them"
    [ -n "$keep" ] && { echo "== $from to $to: $what"; printf '%s\n' "$lines"; } >> "$keep/uikit-taps.txt"
  done
}
if [ "${1:-}" = --start ]; then
  exec xcrun simctl spawn "$2" log stream --level debug --style compact --predicate "$predicate" > "$3" 2>&1
elif [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  # The notes as Picture.swift writes them, and UIKit's lines as log stream --style compact writes them.
  cat > "$t/notes" <<'EOF'
10:00:04.040 +4.1 s load 75.43 terminal 52 × 32
10:00:05.100 +5.2 s load 72.27 tapping the path (NO_TAP_FIRST: no tap is made)
10:00:05.218 +5.3 s load 72.27 did not tap the path, 47.1 s after the appearance switch
10:00:25.563 +25.7 s load 70.37 tap the path, picture shown: false
10:00:27.200 +27.3 s load 70.37 tapping the path again
10:00:27.900 +28.0 s load 70.37 tapped the path again
EOF
  cat > "$t/log" <<'EOF'
2000-01-01 10:00:00.001 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 0; subtype: 0; to windows: 1
2000-01-01 10:00:27.410 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 0; subtype: 0; to windows: 1
2000-01-01 10:00:27.420 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 4; subtype: 0; to windows: 1
2000-01-01 10:00:27.480 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 0; subtype: 0; to windows: 1
EOF
  mkdir "$t/keep"
  sh "$0" "$t/notes" "$t/log" "$t/keep" > "$t/said"
  grep -q "sent the app 0 touch events from 10:00:05.100 to 10:00:05.218" "$t/said" ||
    { echo "FAIL: ios-touch-log self-test: a tap never made is not 0 events: $(cat "$t/said")"; fail=1; }
  grep -q "sent the app 2 touch events from 10:00:27.200 to 10:00:27.900" "$t/said" ||
    { echo "FAIL: ios-touch-log self-test: a tap made is not its 2 touch events: $(cat "$t/said")"; fail=1; }
  # From the first touch event to the last: how long the finger was down by UIKit's times (a hold of 500 ms or more is
  # a long press), none for a tap with no event.
  grep -q "touch events from 10:00:27.200 to 10:00:27.900 (.*); the first at 10:00:27.410, the last 70 ms after" "$t/said" ||
    { echo "FAIL: ios-touch-log self-test: how long the finger was down is not said: $(cat "$t/said")"; fail=1; }
  grep "from 10:00:05.100" "$t/said" | grep -q "the first at" && { echo "FAIL: ios-touch-log self-test: a time for a tap with no event"; fail=1; }
  [ "$(grep -c 'type: 0;' "$t/keep/uikit-taps.txt")" = 2 ] ||
    { echo "FAIL: ios-touch-log self-test: the kept lines are not the tap's: $(cat "$t/keep/uikit-taps.txt")"; fail=1; }
  : > "$t/empty"; sh "$0" "$t/notes" "$t/empty" > "$t/said"
  grep -q "nothing captured" "$t/said" || { echo "FAIL: ios-touch-log self-test: an empty log is not called out: $(cat "$t/said")"; fail=1; }
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: ios-touch-log self-test" || exit 1
else
  report "$@"
fi
