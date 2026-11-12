#!/bin/sh
# Scrolling a full-screen program that follows the mouse, with real touches on Android. e2e/tui-panels.py stands in
# for OpenCode: a prompt box that ignores the wheel, and two panels that move 3 rows per wheel report; a second copy
# moves 1 row per report, as Pi does.
# Up to 0.1.14 a drag sent a report per row of finger travel (OpenCode moved 3 times as far as the finger) at the
# finger's current cell (a drag or flick that ended over the prompt box stopped there).
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-tui-scroll.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shots=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
export HC_SESSION="hmscroll-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"

events="$work/wheels"; : > "$events"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spscroll --label herdr.tab=tbscroll \
  python3 -I "$app/e2e/tui-panels.py" "$events" >/dev/null
# The same stand-in moving 1 row per report, as Pi 1.4.0 does.
events1="$work/wheels1"; : > "$events1"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spone --label herdr.tab=tbone \
  python3 -I "$app/e2e/tui-panels.py" "$events1" 1 >/dev/null
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
cleanup() { kill "$serve_pid" 2>/dev/null; "$hc" server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"; }
trap cleanup EXIT INT TERM
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
tap() {
  b=""; for _ in $(seq 1 30); do b=$(xml | bounds "$1\""); [ -n "$b" ] && break; sleep 1; done
  [ -n "$b" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}
shot() { [ -n "$shots" ] && { mkdir -p "$shots"; "$adb" exec-out screencap -p > "$shots/$1.png"; } || true; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-80)'" < /dev/null; rest=$(printf %s "$rest" | cut -c81-); sleep 0.5; done
tap pair
tap space-spscroll

box=""; for _ in $(seq 1 30); do box=$(xml | bounds terminal-); [ -n "$box" ] && break; sleep 1; done
[ -n "$box" ] || { echo "FAIL: no terminal on screen"; exit 1; }
set -- $box; left=$1; top=$2; right=$3; bottom=$4
sleep 3
read cols rows half < "$events.layout"
[ "$cols" -lt 80 ] || { echo "FAIL: the terminal is $cols columns, not the phone's"; exit 1; }
# Rows on the phone: the page has no side padding and 12 px above the first row and below the last.
dpi=$("$adb" shell wm density | tr -dc '0-9\n' | tail -1)
pad=$((12 * dpi / 160))
ch=$(( (bottom - top - 2 * pad) * 1000 / rows ))     # a row's height, in thousandths of a screen pixel
y_of() { echo $(( top + pad + ($1 * 1000 - 500) * ch / 1000000 )); }   # the middle of row $1 (from 1)
x_left=$(( left + (right - left) / 4 )); x_right=$(( left + (right - left) * 3 / 4 ))
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
# A finger: down at row $2, 8 moves to row $3, held 400 ms, up. Prints what the program got.
drag() {
  : > "$events"
  x=$1; a=$(y_of "$2"); z=$(y_of "$3")
  "$adb" shell input motionevent DOWN "$x" "$a"
  i=1; while [ $i -le 8 ]; do "$adb" shell input motionevent MOVE "$x" $((a + (z - a) * i / 8)); i=$((i + 1)); done
  sleep 0.4; "$adb" shell input motionevent UP "$x" "$z"; sleep 1.5
}
count() { grep -c "$1" "$events" || true; }
# Reports only: other input (keys) is not a report.
reports() { grep -v "^keys" "$events" || true; }

# 1. A drag of 12 rows in the left panel: 4 reports (3 to 5), all on the row where the finger went down.
drag "$x_left" 3 15
n=$(reports | grep -c . || true); rowsat=$(reports | awk '{print $3}' | sort -u | tr '\n' ' ')
if [ "$n" -ge 3 ] && [ "$n" -le 5 ]; then say "ok: a 12-row drag sends $n reports, one per 3 rows"; else say "FAIL: a 12-row drag sent $n reports (one per 3 rows is 4): $(tr '\n' ';' < "$events")"; fi
if [ "$rowsat" = "3 " ]; then say "ok: every report is on row 3, where the finger went down"; else say "FAIL: the reports were on rows $rowsat; the finger went down on row 3"; fi
[ "$(count ignored)$(count right)" = 00 ] && say "ok: all of them scroll the left panel" || say "FAIL: some went elsewhere: $(tr '\n' ';' < "$events")"

# 2. Two panels: a drag in the right panel scrolls the right panel only.
drag "$x_right" 3 12
if [ "$(count right)" -ge 2 ] && [ "$(count left)$(count ignored)" = 00 ]; then say "ok: a drag in the right panel scrolls only the right panel ($(count right) reports)"
else say "FAIL: a drag in the right panel reached: $(awk '{print $4}' "$events" | sort | uniq -c | tr '\n' ';')"; fi

# 3. A flick from row 4 that ends over the prompt box: it keeps going, and nothing lands on the prompt box.
: > "$events"
"$adb" shell input swipe "$x_left" "$(y_of 4)" "$x_left" "$(y_of "$rows")" 120
sleep 3
shot flick
if [ "$(count ignored)" = 0 ] && [ "$(count left)" -ge 6 ]; then say "ok: a flick that ends over the prompt box keeps scrolling ($(count left) reports, all on row $(awk '{print $3}' "$events" | sort -u | tr '\n' ' '))"
else say "FAIL: a flick ending over the prompt box: $(awk '{print $4}' "$events" | sort | uniq -c | tr '\n' ';')"; fi
# Back down to the start for what follows.
drag "$x_left" 15 3; drag "$x_right" 12 3

# 4. The keyboard up: the page lifts the terminal so its last rows stay above the keyboard, and the prompt box is a
# bigger share of what shows. A drag that starts in the upper half of what shows still scrolls.
tap compose-input
sleep 2
"$adb" shell dumpsys window InputMethod | grep -q "isOnScreen=true" || say "FAIL: the keyboard did not come up"
# The WebView ends at the keyboard's top (API 35 emulator: 402 of 751 CSS px tall with it up), and the page lifts the terminal's
# last rows into it.
box=$(xml | bounds terminal-); set -- $box; vtop=$2; vbottom=$4
echo "info: with the keyboard up about $(( (vbottom - vtop - 2 * pad) * 1000 / ch )) of $rows rows show; the bottom 9 are the prompt box"
shot keyboard
: > "$events"
a=$(( vtop + (vbottom - vtop) / 4 )); z=$(( a + (vbottom - vtop) / 3 ))
"$adb" shell input motionevent DOWN "$x_left" "$a"
i=1; while [ $i -le 8 ]; do "$adb" shell input motionevent MOVE "$x_left" $((a + (z - a) * i / 8)); i=$((i + 1)); done
sleep 0.4; "$adb" shell input motionevent UP "$x_left" "$z"; sleep 1.5
if [ "$(count left)" -ge 1 ] && [ "$(count ignored)" = 0 ]; then say "ok: with the keyboard up a drag from the upper half scrolls ($(count left) reports on row $(awk '{print $3}' "$events" | sort -u | tr '\n' ' '))"
else say "FAIL: with the keyboard up a drag from the upper half reached: $(tr '\n' ';' < "$events")"; fi

# 5. A program that moves 1 row per report (Pi 1.4.0) gets one per row: 12 rows of finger, 12 reports. Up to 0.1.18
# the page sent one per 3 rows to every program, and Pi moved a third as far as the finger (issue #1).
"$adb" shell input keyevent 4; sleep 1; "$adb" shell input keyevent 4; sleep 2
tap space-spone
sleep 3
events=$events1
drag "$x_left" 3 15
n=$(reports | grep -c . || true)
if [ "$n" -ge 11 ] && [ "$n" -le 13 ]; then say "ok: a 12-row drag in a program of 1 row a report sends $n reports"
else say "FAIL: a 12-row drag in a program of 1 row a report sent $n reports (12 moves it as far as the finger): $(tr '\n' ';' < "$events")"; fi
drag "$x_left" 3 15
n=$(reports | grep -c . || true)
[ "$n" -ge 11 ] && [ "$n" -le 13 ] && say "ok: and again ($n)" || say "FAIL: the second 12-row drag sent $n reports"
exit $failed
