#!/bin/sh
# A picture in its sheet, zoomed on Android with real touches through the emulator's console (two fingers for the
# pinch): pinched it zooms, dragged it moves and stops at the picture's edge, a double tap zooms out and another in,
# holding it still copies it, Copy still copies the path, and Close still closes. Light, then dark, at 360 dp.
# The app is e2e/picture-app.tsx (the made-up computer of fixtures/fake.tsx with the drawn sunset open), packaged
# under its own id by e2e/zoom-package.sh, so an installed Herdr Anywhere stays as it is. Fake data only; needs no
# computer of its own.
# CHAT=1: the sunset is in a chat (e2e/chat-picture-params.ts) and is opened from there with a tap, on the whole
# screen. Then also: Close and back go back to the chat, a swipe down closes it, and a drag down while zoomed in
# moves the picture instead. The APK given must be packaged with CHAT=1 too.
# Usage: [CHAT=1] ANDROID_SERIAL=emulator-5554 e2e/android-zoom.sh [APK] [folder for screenshots and the recording]
# Without an APK it packages one (nsh and a JDK). Needs the SDK, python3 with Pillow, and an emulator (adb emu).
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the emulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=""; case ${1:-} in *.apk) apk=$1; shift;; esac
out=${1:-}
chat=${CHAT:-}
export ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
adb="$ANDROID_HOME/platform-tools/adb"; command -v adb >/dev/null 2>&1 && adb=$(command -v adb)
id=io.jingtao.herdranywhere.zoomcheck
work=$(mktemp -d)
. "$app/e2e/zoom-checks.sh"
density=$("$adb" shell wm density | tr -d '\r' | grep -o 'Override density: [0-9]*' | grep -o '[0-9]*$' || true)
night=$("$adb" shell cmd uimode night | tr -d '\r' | sed 's/Night mode: //')
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  "$adb" uninstall "$id" >/dev/null 2>&1
  if [ -n "$density" ]; then "$adb" shell wm density "$density" >/dev/null; else "$adb" shell wm density reset >/dev/null; fi
  "$adb" shell cmd uimode night "$night" >/dev/null
  echo "put back: $("$adb" shell wm density | tr -d '\r' | tr '\n' ' '), night $("$adb" shell cmd uimode night | tr -d '\r' | sed 's/Night mode: //')"
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

[ -n "$apk" ] || apk=$("$app/e2e/zoom-package.sh" android "$work/apk")
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null

# 360 dp wide: the narrowest phone the app is checked at.
set -- $("$adb" shell wm size | tr -d '\r' | tail -1 | sed -E 's/.*: ([0-9]+)x([0-9]+)/\1 \2/'); sw=$1 sh=$2
"$adb" shell wm density $((sw * 160 / 360))

xml() { "$adb" shell rm -f /sdcard/hz.xml; "$adb" shell uiautomator dump /sdcard/hz.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hz.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
wait_for() {
  B=""; for _ in $(seq 1 40); do B=$(xml | bounds "$1"); [ -n "$B" ] && return 0; sleep 1; done
  echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -30; exit 1
}
has() { xml | grep -qF "$1"; }
shot() { "$adb" exec-out screencap -p > "$work/$1.png"; [ -z "$out" ] || { mkdir -p "$out"; cp "$work/$1.png" "$out/$1.png"; }; }
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
# Touches as a finger's, through the emulator's console: its axes run 0..32767 over the screen's pixels.
ex() { echo $(($1 * 32767 / sw)); }
ey() { echo $(($1 * 32767 / sh)); }
emu() { HOME=${EMU_HOME:-$HOME} "$adb" emu event send "$@" | grep -v '^OK' >&2 || true; }
down() { emu EV_ABS:ABS_MT_SLOT:$1 EV_ABS:ABS_MT_TRACKING_ID:$(($1 + 1)) EV_ABS:ABS_MT_POSITION_X:$(ex $2) EV_ABS:ABS_MT_POSITION_Y:$(ey $3) EV_ABS:ABS_MT_PRESSURE:50 EV_KEY:BTN_TOUCH:1 EV_SYN:0:0; }
move() { emu EV_ABS:ABS_MT_SLOT:$1 EV_ABS:ABS_MT_POSITION_X:$(ex $2) EV_ABS:ABS_MT_POSITION_Y:$(ey $3) EV_SYN:0:0; }
up() { emu EV_ABS:ABS_MT_SLOT:$1 EV_ABS:ABS_MT_TRACKING_ID:-1 EV_SYN:0:0; }
lift() { emu EV_KEY:BTN_TOUCH:0 EV_SYN:0:0; }
tap_id() { wait_for "$1"; set -- $B; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2)); }
tap2() { # x y: two taps 120 ms apart, each 60 ms down
  for _ in 1 2; do down 0 $1 $2; sleep 0.06; up 0; lift; sleep 0.12; done
}
pinch() { # x y from to: two fingers on a level line through (x, y), from `from` px apart to `to`, in 12 steps
  px=$1 py=$2 p0=$(($3 / 2)) p1=$(($4 / 2)) # names of its own: sh has no locals, and the box is l t r b
  down 0 $((px - p0)) $py; down 1 $((px + p0)) $py
  for k in $(seq 1 12); do pd=$((p0 + (p1 - p0) * k / 12)); move 0 $((px - pd)) $py; move 1 $((px + pd)) $py; sleep 0.02; done
  sleep 0.2; up 1; up 0; lift
}

for scheme in light dark; do
  "$adb" shell cmd uimode night $([ $scheme = dark ] && echo yes || echo no) >/dev/null
  "$adb" shell am force-stop "$id"
  "$adb" shell am start -n "$id/com.longbridge.nsh.AppActivity" >/dev/null
  # The box: the zooming scroll view, which stays put while the picture in it zooms (a build from before zoom has
  # only the picture, which is as big as the box until then).
  # In a chat: the pane opens in its chat view (shot as it is); a tap on the image there opens it.
  [ "$chat" != 1 ] || { tap_id space-sp1; wait_for image-u1.1; sleep 1; shot $scheme-chat; tap_id image-u1.1; }
  wait_for picture-image; box=$(xml | bounds picture-zoom); [ -n "$box" ] || box=$B; set -- $box; l=$1 t=$2 r=$3 b=$4; cx=$(((l + r) / 2)); cy=$(((t + b) / 2)); w=$((r - l))
  bg=$([ $scheme = dark ] && echo '#25272b' || echo '#ffffff') # the sheet's colour (nsh.json popover)
  sleep 1; shot $scheme-fitted
  rec=""
  if [ $scheme = light ]; then "$adb" shell rm -f /sdcard/hz.mp4; "$adb" shell screenrecord --time-limit 30 /sdcard/hz.mp4 & rec=$!; sleep 1; fi

  pinch $cx $cy $((w / 5)) $((w * 4 / 5)); sleep 1; shot $scheme-pinched
  B2=$(xml | bounds picture-zoom)
  [ -z "$B2" ] || [ "$B2" = "$box" ] && say "ok: zoomed in, it stays in its box" || say "FAIL: the box moved: $B2, was $box"
  # To the bottom right corner, then on past it. The drags start a fifth of the box in: near the screen's side, a
  # drag is Android's back gesture.
  fx=$((r - w / 5)) fy=$((b - (b - t) / 5)) tx=$((l + w / 5)) ty=$((t + (b - t) / 5))
  for _ in 1 2 3 4 5 6; do "$adb" shell input swipe $fx $fy $tx $ty 400; sleep 0.5; done
  sleep 1; shot $scheme-corner
  "$adb" shell input swipe $fx $fy $tx $ty 400; sleep 1.5; shot $scheme-corner-again
  if ! has 'resource-id="picture-image"'; then say "FAIL: the drags closed the sheet"; continue; fi
  say "ok: the sheet stays open under the drags"
  tap2 $cx $cy; sleep 1.2; shot $scheme-out
  tap2 $((l + w / 4)) $((t + (b - t) / 4)); sleep 1.2; shot $scheme-doubletap
  tap2 $cx $cy; sleep 1.2; shot $scheme-out2
  zoom_checks "$work" $scheme "$box" "$bg"
  if [ -n "$rec" ]; then
    "$adb" shell pkill -INT screenrecord >/dev/null 2>&1 || true; wait $rec 2>/dev/null || true; sleep 1
    [ -z "$out" ] || "$adb" pull /sdcard/hz.mp4 "$out/zoom.mp4" >/dev/null 2>&1 || true
  fi

  if [ $scheme = light ]; then
    "$adb" shell input swipe $cx $cy $cx $cy 900; sleep 1.2
    # This package has no Herdr host to copy with (MainActivity's page.copyImage): the hold says it couldn't. That it
    # says anything shows the hold still reaches the picture; android-picture.sh checks the copy on the real build.
    held=$(xml)
    case $held in *'Picture copied'*|*"Couldn't copy the picture"*) say "ok: holding the picture still asks to copy it";; *) say "FAIL: holding the picture said nothing";; esac
    sleep 2.5
    wait_for picture-copy-path; set -- $B; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2)); sleep 0.6
    has 'text="Copied"' && say "ok: Copy still copies the path" || say "FAIL: Copy said nothing"
  fi
  wait_for picture-close; set -- $B; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2)); sleep 1.5
  has 'resource-id="picture-sheet"' && say "FAIL: Close left the sheet open" || say "ok: Close closes the sheet"
  if [ "$chat" = 1 ]; then
    has 'resource-id="image-u1.1"' && say "ok: Close goes back to the chat" || say "FAIL: Close did not go back to the chat"
    # On the whole screen: the sheet from the top (under the status bar) to the bottom.
    tap_id image-u1.1; wait_for picture-sheet; set -- $B
    [ "$2" -lt $((sh / 10)) ] && [ "$4" -gt $((sh * 9 / 10)) ] && say "ok: the picture opens on the whole screen" || say "FAIL: the sheet is not on the whole screen: $B"
    "$adb" shell input keyevent 4; sleep 1.5
    xml > "$work/back.xml"
    ! grep -qF 'resource-id="picture-sheet"' "$work/back.xml" && grep -qF 'resource-id="image-u1.1"' "$work/back.xml" \
      && say "ok: back closes the picture, back in the chat" || say "FAIL: back did not go back to the chat"
    tap_id image-u1.1; wait_for picture-image; sleep 1
    "$adb" shell input swipe $cx $cy $cx $((sh * 9 / 10)) 250; sleep 1.5
    has 'resource-id="picture-sheet"' && say "FAIL: a swipe down left the picture open" || say "ok: a swipe down closes the picture"
    tap_id image-u1.1; wait_for picture-image; sleep 1
    pinch $cx $cy $((w / 5)) $((w * 4 / 5)); sleep 1
    "$adb" shell input swipe $cx $((t + (b - t) / 5)) $cx $((b - (b - t) / 5)) 400; sleep 1.5
    has 'resource-id="picture-image"' && say "ok: zoomed in, a drag down moves the picture and leaves it open" || say "FAIL: zoomed in, a drag down closed the picture"
    tap_id picture-close; sleep 1.5
  fi
done
[ $failed = 0 ] && echo PASS || { echo FAILED; exit 1; }
