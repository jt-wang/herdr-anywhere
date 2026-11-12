#!/bin/sh
# The chat view's decision card on Android, against an isolated herdr-core: two stand-ins for Claude Code 2.1.325
# (e2e/fake-ask) show the screens Claude Code drew when it asked two questions and when it asked to run a command
# (e2e/files/ask), take the keys it takes there, and write its session record.
# With the card (app/src/ui/ask-card.tsx in this tree): the first question's card, a digit answers it; the second
# question's boxes ticked with taps; Next goes to the review, Submit answers; the card goes and the reply shows. Then
# the permission's card, Yes runs it. Without it (the commit before the card): the same screens, shot only, for the
# before-and-after pictures. The terminal view, with its row of choices, is shot too. Fake data only.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-ask.sh APK COMPANION [folder for screenshots]
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
card=no; [ -f "$app/src/ui/ask-card.tsx" ] && card=yes
port=7790; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/haXXXX); state=$(mktemp -d /tmp/hatcXXXX)
mkdir -p "$work/u/src/demo-shop" "$work/u/src/demo-site" "$work/state" "$work/config" "$work/data" "$work/claude" "$work/bin"
(cd "$app/e2e/fake-ask" && GO111MODULE=off go build -o "$work/bin/claude" main.go)
: > "$work/u/.zshrc"
echo skip_global_compinit=1 > "$work/u/.zshenv"
. "$app/e2e/companion.sh"  # companion_start, companion_stop
clean() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/u" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmask-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
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
lhc run --bg --keep --cwd "$work/u/src/demo-shop" --name asking --label herdr.space=spask --label herdr.tab=tbask \
  "$work/bin/claude" "$app/e2e/files/ask/question.json" >/dev/null
lhc run --bg --keep --cwd "$work/u/src/demo-site" --name running --label herdr.space=spperm --label herdr.tab=tbperm \
  "$work/bin/claude" "$app/e2e/files/ask/permission.json" >/dev/null
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
# The pane's chat: its button, unless the chat is already up.
chat() { wait_for 'resource-id="show-chat"' "the agent's pane has no chat button"; tap show-chat; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
# adb's input text drops characters on CI's emulator: the helper types in short pieces and reads the field back.
"$app/e2e/android-type-link.sh" "$invite"
for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done
tap pair
wait_for 'resource-id="space-spask"' "the phone did not pair (the invite typed, Pair tapped)"
echo "ok: paired (card: $card)"

# The questions. The terminal first: its row of choices.
tap space-spask
wait_for 'resource-id="terminal-' "the asking pane's terminal did not show"
wait_for 'resource-id="choice-1"' "the terminal view has no row of choices for the question"
sleep 2; snap 1-terminal-question
chat
if [ "$card" = no ]; then
  wait_for 'Which plan should the fake shop use?' "the chat does not show the question"
  sleep 2; snap 2-chat-question
  tap show-terminal; wait_for 'resource-id="terminal-' "the terminal did not come back"
  tap back; sleep 1
  tap space-spperm
  wait_for 'resource-id="choice-1"' "the terminal view has no row of choices for the permission"
  sleep 2; snap 6-terminal-permission
  chat
  sleep 3; snap 7-chat-permission
  echo "ok: the screens before the card, shot"
  echo PASS
  exit 0
fi
wait_for 'resource-id="ask-card"' "no card for the question"
wait_for 'Question 1 of 2' "the card does not say which question"
for x in ask-row-1 ask-row-2 ask-row-3 ask-own ask-skip; do has "resource-id=\"$x\"" || fail "the card has no $x"; done
has 'Answer it in the terminal' && fail "the card says to answer in the terminal"
sleep 1; snap 2-card-question
tap ask-row-2
wait_for 'Question 2 of 2' "the card did not go on to the second question after 2"
wait_for 'resource-id="ask-box-1-off"' "the second question's boxes do not show"
sleep 1; snap 3-card-multi
tap ask-row-1
wait_for 'resource-id="ask-box-1-on"' "a tap on Home did not tick it"
tap ask-row-3
wait_for 'resource-id="ask-box-3-on"' "a tap on Help did not tick it"
tap ask-next
wait_for 'Ready to submit your answers?' "Next did not go to the review"
sleep 1; snap 4-card-review
tap ask-row-1
gone 'resource-id="ask-card"' "the card stayed after Submit answers"
wait_for 'Noted your answers.' "the reply after the answers did not show"
sleep 1; snap 5-answered
echo "ok: both questions answered from the card: a digit, two ticks, Next, Submit answers"

tap show-terminal; wait_for 'resource-id="terminal-' "the terminal did not come back"
tap back; sleep 1
tap space-spperm
wait_for 'resource-id="choice-1"' "the terminal view has no row of choices for the permission"
sleep 2; snap 6-terminal-permission
chat
wait_for 'resource-id="ask-card"' "no card for the permission"
wait_for 'wants to run a command' "the permission's card does not say what it asks"
sleep 1; snap 7-card-permission
tap ask-row-1
gone 'resource-id="ask-card"' "the card stayed after Yes"
echo "ok: the permission answered from the card"
echo PASS
