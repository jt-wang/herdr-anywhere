#!/bin/sh
# Choices that follow the agent on Android, against an isolated herdr-core: a stand-in for Claude Code 2.1.325
# (e2e/fake-ask) shows the screens Claude Code drew when it asked three questions in one call, then two in another
# (e2e/files/stuck), and takes the keys it takes there; it stays blocked through each call's questions, as Claude
# Code does. A choice tapped on the home page, then one on the terminal view: with the fix (store.ts follows the
# screen, `private async follow` in this tree) each row shows the next question; without it (the commit before) the
# same taps are shot, for the before-and-after pictures. Fake data only.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-stuck.sh APK COMPANION [folder for screenshots]
#   DARK=1: the device in dark mode for the run, put back as it was after.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
[ -z "$shot" ] || mkdir -p "$shot"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$app/e2e/adb-note.sh"; adb_note "$adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
fixed=no; grep -q "private async follow" "$app/src/store.ts" && fixed=yes
port=7800; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/haXXXX); state=$(mktemp -d /tmp/hatcXXXX)
mkdir -p "$work/u/src/lamp-shop" "$work/state" "$work/config" "$work/data" "$work/claude" "$work/bin"
(cd "$app/e2e/fake-ask" && GO111MODULE=off go build -o "$work/bin/claude" main.go)
: > "$work/u/.zshrc"
echo skip_global_compinit=1 > "$work/u/.zshenv"
. "$app/e2e/companion.sh"  # companion_start, companion_stop
clean() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/u" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmstuck-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
    CLAUDE_CONFIG_DIR="$work/claude" "$@"
}
lhc() { clean "$hc" "$@"; }
night=$("$adb" shell cmd uimode night 2>/dev/null | tr -d '\r' | sed -n 's/^Night mode: //p'); case $night in yes|no|auto) ;; *) night=no;; esac
cleanup() {
  trap - EXIT; trap '' INT TERM HUP
  set +e
  companion_stop || stop_failed=1
  "$adb" shell cmd uimode night "$night" >/dev/null 2>&1 || true
  lhc server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  [ -z "${stop_failed:-}" ] || exit 1
}
trap 'adb_exit $? cleanup' EXIT
trap 'exit 130' INT; trap 'exit 143' TERM HUP
lhc run --bg --keep --cwd "$work/u/src/lamp-shop" --name lamps --label herdr.space=spstuck --label herdr.tab=tbstuck \
  "$work/bin/claude" "$app/e2e/files/stuck/stuck.json" >/dev/null
companion_start "$work/serve.log" clean "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" || exit 1
invite=$(clean "$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
[ "${DARK:-}" = 1 ] && "$adb" shell cmd uimode night yes >/dev/null
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/ha.xml; "$adb" shell uiautomator dump /sdcard/ha.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/ha.xml 2>/dev/null || true; }
centre() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'; }
has() { xml | grep -qF "$1"; }
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/$1.png" || true; }
# What a failure shows of the screen leaves out the pairing code (in the link field when pairing failed).
unpaired() { sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g'; }
fail() {
  echo "FAIL: $1"; snap fail; [ -n "$shot" ] && xml | unpaired > "$shot/fail.xml"
  xml | unpaired | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1
}
wait_for() { for _ in $(seq 1 30); do has "$1" && return 0; sleep 1; done; fail "$2"; }
gone() { for _ in $(seq 1 30); do has "$1" || return 0; sleep 1; done; fail "$2"; }
tap() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || fail "no $1 on screen"
  "$adb" shell input tap $xy
}
# A button whose resource-id matches a pattern (the home page's rows are named by the terminal's address).
tap_re() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | tr '>' '\n' | grep -E "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || fail "no $1 on screen"
  "$adb" shell input tap $xy
}
# What the row shows after a tap: the next question's first option with the fix; before it, said and shot as it is.
after_tap() {
  if [ "$fixed" = yes ]; then wait_for "text=\"$1\"" "$3: the row did not go on to the next question ($1)"
  else sleep 3; has "text=\"$2\"" && echo "before: $3 still shows $2" || echo "before: $3 does not show $2"; fi
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
# adb's input text drops characters on CI's emulator: the helper types in short pieces and reads the field back.
"$app/e2e/android-type-link.sh" "$invite"
for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done
tap pair
wait_for 'resource-id="space-spstuck"' "the phone did not pair (the invite typed, Pair tapped)"
echo "ok: paired (fixed: $fixed)"

# The home page: the first of three questions, its row; 1 answers it, Claude Code still blocked.
wait_for 'text="1. Top shelf"' "the home page has no row of choices for the first question"
sleep 1; snap 1-home-question-1
tap_re 'needs-[^"]*-choice-1'
after_tap '1. Amber' '1. Top shelf' 'the home page'
sleep 1; snap 2-home-after-tap
echo "ok: the home page's row after a tap"

# The terminal view, on the second question now: its row; 2 answers it.
tap space-spstuck
wait_for 'resource-id="terminal-' "the pane's terminal did not show"
wait_for 'resource-id="choice-1"' "the terminal view has no row of choices"
if [ "$fixed" = yes ]; then wait_for 'text="1. Amber"' "the terminal view's row is not the second question"; fi
sleep 2; snap 3-terminal-question-2
tap choice-2
after_tap '1. Ten' '1. Amber' 'the terminal view'
sleep 1; snap 4-terminal-after-tap
echo "ok: the terminal view's row after a tap"
echo PASS
