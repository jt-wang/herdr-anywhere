#!/bin/sh
# A picture's path in a terminal, tapped on Android: the sheet shows the full path and the picture the computer read,
# Copy copies the path, holding the picture puts it on Android's clipboard (Copied.java, which only the app that
# pastes may read), and a file named like a picture that isn't one says so. Light, then dark. Fake data only.
# The terminal is here, or with HM_PICTURE_IMAGE in a container, for screenshots to publish (picture-terminal.sh).
# Usage: ANDROID_SERIAL=emulator-5554 [HM_PICTURE_IMAGE=alpine:latest] e2e/android-picture.sh APK COMPANION [folder for screenshots]
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard android-picture # a login entry of its own, never the person's
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shots=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
# Ubuntu's /etc/zsh/zshrc runs compinit, which can stop at a y/n question and eat what is typed first.
echo skip_global_compinit=1 > "$work/home/.zshenv"
# docker finds its daemon by this home's settings, before HOME is the fake one.
[ -z "${HM_PICTURE_IMAGE:-}" ] || export DOCKER_HOST="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}"
# The emulator console wants the token in the real home (adb emu reads ~/.emulator_console_auth_token).
home=$HOME
export HC_SESSION="hmpicture-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
L() { "$@"; }
. "$app/e2e/picture-terminal.sh"
serve_pid=""
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e # every step runs, even after one fails (a companion already gone)
  "$adb" shell cmd uimode night no >/dev/null 2>&1 || true
  "$adb" shell settings put system show_touches "${touches:-0}" >/dev/null 2>&1 || true
  [ -z "$serve_pid" ] || kill "$serve_pid" 2>/dev/null; "$hc" server stop >/dev/null 2>&1 || true
  picture_cleanup
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  autostart_guard_end || exit 1
  exit "$status"
}
trap 'adb_exit $? cleanup' EXIT; trap 'cleanup; exit 130' INT; trap 'cleanup; exit 143' TERM HUP
picture_terminal
if [ "${HM_PICTURE_VIA:-}" = tailcat ]; then
  # HM_PICTURE_ROUTE=relay: the companion sends only through Tailcat's relay (as android-tailcat.sh does); else
  # Tailcat goes direct when it can, which it can between an emulator and this computer.
  [ "${HM_PICTURE_ROUTE:-}" = relay ] && export TS_DEBUG_ALWAYS_USE_DERP=1
  "$bin" serve --tailcat --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0; until grep -q "listening on Tailcat" "$work/serve.log" 2>/dev/null; do i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: serve --tailcat did not start"; exit 1; }; sleep 0.1; done
  invite=$("$bin" pair --state "$state" 2>/dev/null | grep -o 'herdr1:[A-Za-z0-9_-]*' | head -1)
else
  "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
  serve_pid=$!
  sleep 1
  invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
fi
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
# Android 13+ asks for notifications once paired; that question would cover the screen.
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
# Sets B to the view's bounds (left top right bottom), or ends the run saying what is on screen. Not in $( ): an exit
# there would only end the subshell.
wait_for() {
  B=""; for _ in $(seq 1 30); do B=$(xml | bounds "$1\""); [ -n "$B" ] && return 0; sleep 1; done
  echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1
}
tap() { wait_for "$1"; set -- $B; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2)); }
shot() { [ -n "$shots" ] && { mkdir -p "$shots"; "$adb" exec-out screencap -p > "$shots/$1.png"; } || true; }
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
has() { xml | grep -qF "$1"; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$(dirname "$0")/android-type-link.sh" "$invite"
tap pair
tap "space-$space"
wait_for "terminal-$term"; set -- $B; left=$1; top=$2; right=$3; bottom=$4
sleep 3
cols=$("$hc" get "$term" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["cols"])')
rows=$("$hc" get "$term" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["rows"])')
# The page has no side padding and 12 px above the first row and below the last.
dpi=$("$adb" shell wm density | tr -dc '0-9\n' | tail -1)
pad=$((12 * dpi / 160))
cell() { echo $(( left + ($2 * 2 + 1) * (right - left) / (2 * cols) )) $(( top + pad + ($1 * 2 + 1) * (bottom - top - 2 * pad) / (2 * rows) )); }
echo "terminal $cols × $rows"

for scheme in light dark; do
  [ $scheme = dark ] && { "$adb" shell cmd uimode night yes >/dev/null; sleep 2; }
  shot "$scheme-terminal"
  "$adb" shell input tap $(cell 0 12)
  wait_for picture-image
  sleep 1; shot "$scheme-sheet"
  has "$full\"" &&
    say "ok: the tapped path opens the picture under its full path" || say "FAIL: no full path over the picture"
  if [ $scheme = light ]; then
    tap picture-copy-path; sleep 0.5
    has 'text="Copied"' && say "ok: Copy copies the path" || say "FAIL: Copy said nothing"
    wait_for picture; set -- $B; x=$((($1 + $3) / 2)); y=$((($2 + $4) / 2))
    "$adb" shell input swipe $x $y $x $y 900; sleep 1.2
    shot "$scheme-held"
    has 'Picture copied' && say "ok: holding the picture copies it" || say "FAIL: holding the picture said nothing ($(xml | grep -oE 'text="[^"]*(copy|Copy)[^"]*"' | head -3))"
    # Only the app that pastes may read it: not adb's shell. A root shell (after `adb root`, which android-quiet.sh
    # uses) reads any app's files, so then it reads as the shell's own user, 2000.
    as=""; [ "$("$adb" shell id -u | tr -d '\r')" = 0 ] && as="su 2000"
    out=$("$adb" shell $as content read --uri "content://$id.copied/picture.png" 2>&1 | head -2 || true)
    case $out in *Security*|*Permission*|*denied*|*"not exported"*) say "ok: the copied picture isn't open to other apps";; *) say "FAIL: adb's shell read the copied picture: $out";; esac
    # One file holds it (a release build can't be looked into: run-as needs a debuggable one).
    if [ "$("$adb" shell run-as "$id" echo yes 2>/dev/null | tr -d '\r')" = yes ]; then
      files=$("$adb" shell run-as "$id" ls cache/copied | tr -d '\r' | tr '\n' ' ')
      [ "$files" = "picture.png " ] && say "ok: one file holds it" || say "FAIL: the cache holds '$files'"
    else
      echo "note: a release build: the app's cache can't be looked into"
    fi
  fi
  tap picture-close; sleep 1
  # Not pictures there: a text file named .png, a path only mentioned. Nothing opens.
  for at in "3 9 notes.png" "6 12 art/sunset-wide.png"; do
    set -- $at
    "$adb" shell input tap $(cell $1 $2); sleep 3
    xml | grep -q 'resource-id="picture-sheet' && { say "FAIL: $3 opened the sheet"; tap picture-close; sleep 1; } || say "ok: $3 opens nothing"
  done
  shot "$scheme-not-pictures"
done

# Timing (HM_PICTURE_TIMES=N taps): from the finger's lift to the picture, from a screen recording. show_touches draws
# a dot while the finger is down; the lift is the frame it starts to fade; then the frame the sheet starts to come up
# and the one the picture settles (picture-timing.py). Through Tailcat, Settings then says which route the taps took.
if [ -n "${HM_PICTURE_TIMES:-}" ]; then
  # Dark: show_touches' white dot can't be seen on the light theme's white.
  "$adb" shell cmd uimode night yes >/dev/null; sleep 2
  touches=$("$adb" shell settings get system show_touches | tr -d '\r'); case $touches in 0|1) ;; *) touches=0;; esac
  "$adb" shell settings put system show_touches 1
  set -- $(cell 0 12); tx=$1 ty=$2
  # The touch goes in through the emulator's console (adb emu event send), as a finger's would: show_touches draws a
  # dot only for touches from the touch screen, not for `input`'s injected ones, and sendevent needs root. The
  # console's axes run 0..32767 over the screen's pixels.
  set -- $("$adb" shell wm size | tr -d '\r' | tail -1 | sed -E 's/.*: ([0-9]+)x([0-9]+)/\1 \2/'); sw=$1 sh=$2
  press() { # x y: down, 80 ms, up
    HOME=$home "$adb" emu event send EV_ABS:ABS_MT_SLOT:0 EV_ABS:ABS_MT_TRACKING_ID:1 EV_ABS:ABS_MT_POSITION_X:$(($1 * 32767 / sw)) \
      EV_ABS:ABS_MT_POSITION_Y:$(($2 * 32767 / sh)) EV_ABS:ABS_MT_PRESSURE:50 EV_KEY:BTN_TOUCH:1 EV_SYN:0:0 | grep -v '^OK' >&2 || true
    sleep 0.08
    HOME=$home "$adb" emu event send EV_ABS:ABS_MT_TRACKING_ID:-1 EV_KEY:BTN_TOUCH:0 EV_SYN:0:0 | grep -v '^OK' >&2 || true
  }
  "$adb" shell input tap $tx $ty; wait_for picture-image; sleep 1
  set -- $B; img="$1 $2 $3 $4"
  tap picture-close; sleep 1.5
  times=""
  for k in $(seq 1 "$HM_PICTURE_TIMES"); do
    "$adb" shell rm -f /sdcard/hmt.mp4
    "$adb" shell screenrecord --time-limit 7 /sdcard/hmt.mp4 & rec=$!
    sleep 2
    press $tx $ty
    wait $rec
    "$adb" pull /sdcard/hmt.mp4 "$work/t$k.mp4" >/dev/null 2>&1
    [ -z "${HM_PICTURE_VIDEOS:-}" ] || { mkdir -p "$HM_PICTURE_VIDEOS"; cp "$work/t$k.mp4" "$HM_PICTURE_VIDEOS/${HM_PICTURE_VIA:-direct}-$k.mp4"; }
    ms=$(python3 "$app/e2e/picture-timing.py" "$work/t$k.mp4" $tx $ty $img)
    times="$times $ms"
    tap picture-close; sleep 1.5
  done
  via=${HM_PICTURE_VIA:-direct}
  if [ "$via" = tailcat ]; then
    # The route these taps took, as Settings says it.
    for _ in 1 2 3; do has 'resource-id="settings"' && break; "$adb" shell input keyevent 4; sleep 1; done
    tap settings; route=""
    for i in $(seq 1 40); do
      has 'Direct to your computer' && { route=direct; break; }
      has 'Through a relay' && { route="$(xml | grep -o 'Through a relay[^"]*' | head -1)"; break; }
      [ $((i % 4)) = 1 ] && "$adb" shell input swipe 500 1500 500 900 300
      sleep 1
    done
    [ -n "$route" ] || xml | tr '>' '\n' | grep -o ' text="[^"]\+"' | grep -iE 'route|relay|direct|tunnel|Tailcat' >&2
    via="tailcat, route ${route:-unknown}"
  fi
  echo "tap → picture starts/settled, ms ($via):$times"
  "$adb" shell settings put system show_touches "$touches"; touches=""
fi
[ $failed = 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
