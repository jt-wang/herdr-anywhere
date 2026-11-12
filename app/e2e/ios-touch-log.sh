#!/bin/sh
# Whether a tap reached the app at all: while an iOS suite's XCUITest runs, the simulator's log of the app's UIKit event
# dispatch is kept (log stream, debug level); the test notes "tapping …" just before each tap and a line just after.
# For each such pair this says how many touch events (UIEvent type 0) UIKit sent the app's windows in between, and keeps
# those lines. A tap the page never saw that UIKit did send was lost in the app; one UIKit never sent never reached it.
# Usage: e2e/ios-touch-log.sh --start UDID FILE &     the log, until killed
#        e2e/ios-touch-log.sh NOTES LOG [FOLDER]      the taps in NOTES (the test's notes) against LOG; FOLDER keeps the lines
#        e2e/ios-touch-log.sh --self-test
# The app's process is Herdr (ios/build.sh builds Herdr.app).
# Also the lines the test copy's window writes, "hm-touch …" (e2e/ios-touch-window.swift): where each touch landed.
# And the system's own lines that say a touch was cancelled (backboardd and SpringBoard hand touches to the app).
predicate='(process == "Herdr" AND (subsystem == "com.apple.UIKit" OR eventMessage BEGINSWITH "hm-touch "))'\
' OR ((process == "backboardd" OR process == "SpringBoard") AND eventMessage CONTAINS[c] "cancel")'
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
    landed=$(awk -v a="$a" -v b="$b" '{ split($2, t, /[:.]/); m = ((t[1] * 60 + t[2]) * 60 + t[3]) * 1000 + t[4]
      if (m >= a && m <= b && / hm-touch /) print }' "$log")
    # The first touch event to the last (a window's line for each, "to windows:"): how long UIKit held the touch down.
    held=$(printf '%s\n' "$lines" | awk '/UIEvent type: 0;.*to windows:/ { split($2, t, /[:.]/)
      m = ((t[1] * 60 + t[2]) * 60 + t[3]) * 1000 + t[4]; if (f == "") f = m; l = m } END { if (f != "") print l - f }')
    # backboardd cancels a touch whose device (the simulator's) went away while it was still down.
    gone=$(awk -v a="$a" -v b="$b" '{ split($2, t, /[:.]/); m = ((t[1] * 60 + t[2]) * 60 + t[3]) * 1000 + t[4]
      if (m >= a && m <= b && /backboardd/ && /digitizer did disappear/ && /touching/) { print "yes"; exit } }' "$log")
    echo "  UIKit sent the app $count touch events from $from to $to ($what); the page's own record says whether it got them${held:+; UIKit held it $held ms}${gone:+; the simulator touch device went away while the touch was down, and backboardd cancelled it}"
    [ -z "$landed" ] || printf '%s\n' "$landed" | sed 's/.* hm-touch /    app window: /'
    # Kept: every line in the tap's time, in time order (UIKit's other news, the system's touch handling, the window's).
    [ -n "$keep" ] && { echo "== $from to $to: $what"; awk -v a="$a" -v b="$b" '{ split($2, t, /[:.]/)
      m = ((t[1] * 60 + t[2]) * 60 + t[3]) * 1000 + t[4]; if (m >= a && m <= b) print }' "$log" | sort -s -k2,2; } >> "$keep/uikit-taps.txt"
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
DAY 10:00:00.001 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 0; subtype: 0; to windows: 1
DAY 10:00:27.410 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 0; subtype: 0; to windows: 1
DAY 10:00:27.420 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 4; subtype: 0; to windows: 1
DAY 10:00:27.411 Df Herdr[100:200] hm-touch phase=began view=WKContentView in-picture=no gestures=[WKSyntheticTapGestureRecognizer possible]
DAY 10:00:27.462 Df Herdr[100:200] (Foundation) hm-touch phase=watch +51ms view-in-window=yes touch-gestures=[WKSyntheticTapGestureRecognizer@0x1 possible]
DAY 10:00:27.480 Db Herdr[100:200] [com.apple.UIKit:EventDispatch] Sending UIEvent type: 0; subtype: 0; to windows: 1
DAY 10:00:27.481 Df Herdr[100:200] hm-touch phase=ended view=WKContentView in-picture=no gestures=[WKSyntheticTapGestureRecognizer ended]
DAY 10:00:10.000 Df Herdr[100:200] [com.apple.UIKit:Scene] scene line outside any tap
DAY 10:00:27.470 Df Herdr[100:200] [com.apple.UIKit:Scene] scene line inside the tap
DAY 10:00:27.475 Df backboardd[50:60] [com.apple.backboard:touch] cancel line inside the tap
DAY 10:00:27.476 Df backboardd[50:60] [com.apple.BackBoard:TouchEvents] cancel -- digitizer did disappear:<BKDirectTouchState: 0x1; touchScreen; builtin; senderID: 0x2; contacts: contacts: []; destinations: []>
EOF
  # Each line starts with its day, as log stream writes it: today's, put in when the test runs, so this file holds no
  # date (the public files hold none). Only the time of day is read.
  sed "s/^DAY /$(date +%Y-%m-%d) /" "$t/log" > "$t/log.day" && mv "$t/log.day" "$t/log"
  mkdir "$t/keep"
  sh "$0" "$t/notes" "$t/log" "$t/keep" > "$t/said"
  grep -q "sent the app 0 touch events from 10:00:05.100 to 10:00:05.218" "$t/said" ||
    { echo "FAIL: ios-touch-log self-test: a tap never made is not 0 events: $(cat "$t/said")"; fail=1; }
  grep -q "sent the app 2 touch events from 10:00:27.200 to 10:00:27.900" "$t/said" ||
    { echo "FAIL: ios-touch-log self-test: a tap made is not its 2 touch events: $(cat "$t/said")"; fail=1; }
  # The test copy's window (e2e/ios-touch-window.swift) says where each touch landed: those lines go with the tap.
  grep -A2 "touch events from 10:00:27.200" "$t/said" | grep -q "app window: phase=began view=WKContentView in-picture=no" ||
    { echo "FAIL: ios-touch-log self-test: where the touch landed is not said with its tap: $(cat "$t/said")"; fail=1; }
  # And what the window saw every 50 ms between (phase=watch), in order.
  grep -A3 "touch events from 10:00:27.200" "$t/said" | sed -n 3p | grep -q "app window: phase=watch +51ms" ||
    { echo "FAIL: ios-touch-log self-test: the window's look in between is not said in order: $(cat "$t/said")"; fail=1; }
  # How long UIKit held the touch down: its first touch event to its last, 10:00:27.410 to 10:00:27.480.
  grep -q "touch events from 10:00:27.200 .*; UIKit held it 70 ms" "$t/said" ||
    { echo "FAIL: ios-touch-log self-test: the tap's hold by UIKit's clock is not said: $(cat "$t/said")"; fail=1; }
  # The simulator's touch device went away while a finger was still down on it: backboardd cancels that touch, and
  # says so ("digitizer did disappear" with a contact "touching"). One with no contact left is the device going away
  # after the lift, as after every tap, and says nothing.
  grep -q "touch events from 10:00:27.200 .*backboardd cancelled" "$t/said" &&
    { echo "FAIL: ios-touch-log self-test: a device gone after the lift is called a cancel: $(cat "$t/said")"; fail=1; }
  sed 's/contacts: contacts: \[\]/contacts: contacts: [pathIndex: 2; touchIdentifier: 1; touching; locked]/' "$t/log" > "$t/log2"
  sh "$0" "$t/notes" "$t/log2" > "$t/said2"
  grep -q "touch events from 10:00:27.200 .*; the simulator touch device went away while the touch was down, and backboardd cancelled it" "$t/said2" ||
    { echo "FAIL: ios-touch-log self-test: a device gone under the finger is not said: $(cat "$t/said2")"; fail=1; }
  grep -A1 "touch events from 10:00:05.100" "$t/said" | grep -q "app window:" &&
    { echo "FAIL: ios-touch-log self-test: a landing for a tap with no event"; fail=1; }
  [ "$(grep -c 'UIEvent type: 0;' "$t/keep/uikit-taps.txt")" = 2 ] ||
    { echo "FAIL: ios-touch-log self-test: the kept lines are not the tap's: $(cat "$t/keep/uikit-taps.txt")"; fail=1; }
  # Everything else the log has in a tap's time goes with it too (a scene, a window, the keyboard, the system's touch
  # handling), so a touch that ended cancelled can be set beside what happened then; nothing from outside a tap.
  grep -q "scene line inside the tap" "$t/keep/uikit-taps.txt" && grep -q "cancel line inside the tap" "$t/keep/uikit-taps.txt" &&
    ! grep -q "outside any tap" "$t/keep/uikit-taps.txt" ||
    { echo "FAIL: ios-touch-log self-test: the kept lines are not all of the tap's time: $(cat "$t/keep/uikit-taps.txt")"; fail=1; }
  : > "$t/empty"; sh "$0" "$t/notes" "$t/empty" > "$t/said"
  grep -q "nothing captured" "$t/said" || { echo "FAIL: ios-touch-log self-test: an empty log is not called out: $(cat "$t/said")"; fail=1; }
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: ios-touch-log self-test" || exit 1
else
  report "$@"
fi
