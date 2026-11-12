#!/bin/sh
# The chat view of the four agents on Android, for crashes and hangs: fake Claude Code, Codex, Pi and OpenCode 2
# (companion/internal/testfake/fakeagent, replaying their recordings; a conversation of 40 turns from the start, a
# command that printed 500 lines), in an isolated herdr-core, against the Companion given. For each agent, as
# e2e/ios-chat-four.sh: open, the long output, a picture, scrolling the long conversation, the app in the background
# and back (AWAY seconds), the Companion stopped and started again. The app must stay one process from launch to the
# end: a crash fails with Android's crash log. Steps are found by the recordings' ids (fake_list, fake_look). Fake
# data only.
# Usage: ANDROID_SERIAL=emulator-5554 [AGENTS="claude-code codex pi opencode"] [AWAY="10 60"] \
#          e2e/android-chat-four.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
[ -z "$shot" ] || mkdir -p "$shot"
agents=${AGENTS:-claude-code codex pi opencode}
away=${AWAY:-10 60}
app=$(cd "$(dirname "$0")/.." && pwd)
companion="$app/../companion"
chat="$companion/internal/chat"
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
recordings() {
  case $1 in
    claude-code) d="$chat/claude/testdata/claude-code/2.1.322"; echo "$d/turns.jsonl $d/steps.jsonl $d/image.jsonl" ;;
    codex) d="$chat/codex/testdata/codex/0.167.1"; echo "$d/turns.jsonl $d/steps.jsonl $d/files.jsonl" ;;
    pi) d="$chat/pi/testdata/pi/1.4.0"; echo "$d/turns.jsonl $d/steps.jsonl $d/files.jsonl" ;;
    opencode) d="$chat/opencode/testdata/opencode/2.2.0"; echo "$d/turns.json $d/steps.json $d/files.json" ;;
    *) echo "no agent $1" >&2; exit 2 ;;
  esac
}
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/u" "$work/state" "$work/config" "$work/data" "$work/claude" "$work/codex" "$work/pi"
: > "$work/u/.zshrc"
step="building the fakes"
(cd "$companion" && CGO_ENABLED=0 go build -buildvcs=false -o "$work/fakeagent" ./internal/testfake/fakeagent)
# herdr-core gives every terminal the environment of what started it: the agents' folders are set here, for the
# fakes and for the Companion's readers alike.
clean() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/u" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmac4-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
    CLAUDE_CONFIG_DIR="$work/claude" CODEX_HOME="$work/codex" PI_CODING_AGENT_DIR="$work/pi" "$@"
}
lhc() { clean "$hc" "$@"; }
serve_pid=; step=start
cleanup() {
  status=$?
  trap - EXIT; trap '' INT TERM HUP
  set +e
  # A command that failed under set -e says nothing of itself: say where, and what the Companion wrote.
  [ "$status" != 0 ] && { echo "FAIL: exit $status during: $step"; tail -20 "$work/serve.log" 2>/dev/null | sed 's/^/  serve: /'; }
  [ -n "$serve_pid" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  pkill -f -- "--state $state" 2>/dev/null || true
  lhc server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM HUP
step="starting the fakes in herdr-core"
for a in $agents; do
  s=$(printf %s "$a" | tr -d -)
  mkdir -p "$work/u/src/demo-$s"
  # shellcheck disable=SC2046 # the recordings' paths, one word each
  lhc run --bg --keep --cwd "$work/u/src/demo-$s" --name "$s" --label herdr.space="spc$s" --label herdr.tab="tbc$s" -- \
    "$work/fakeagent" -agent "$a" -preload 40 -long 500 -hc "$hc" $(recordings "$a") >/dev/null
done
serve() {
  clean "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" >> "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0; until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; return 1; }; sleep 0.1; done
}
step="starting the Companion"
serve
# HM_CHAT4_DRY=1: no simulator. Only the core, the fakes and the Companion: each terminal must be known as its agent.
if [ "${HM_CHAT4_DRY:-}" = 1 ]; then
  sleep 2
  for a in $agents; do
    s=$(printf %s "$a" | tr -d -)
    got=$(lhc get "$s" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("status") or {}).get("app",""), (d.get("labels") or {}).get("herdr-anywhere.chat.session","")[-6:])')
    case $got in "$a "*) echo "ok: $s is $got" ;; *) echo "FAIL: $s is known as '$got', not $a"; exit 1 ;; esac
  done
  echo PASS; exit 0
fi
step="pairing and installing the app"
invite=$(clean "$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"$adb" logcat -b crash -c >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
# The bounds of the nodes whose resource-id starts with $1 and contains $2 (any), the last one; and its centre.
last_bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | grep -F "${2:-}" | tail -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
centre() { awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'; }
has() { xml | grep -qF "$1"; }
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/$1.png" || true; }
# What a failure shows of the screen leaves out the pairing code (in the link field when pairing failed).
unpaired() { sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g'; }
fail() {
  echo "FAIL: $1"; snap fail; [ -n "$shot" ] && { xml | unpaired > "$shot/fail.xml"; cp "$work/serve.log" "$shot/serve.log"; }
  xml | unpaired | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1
}
# The app is the process launched; another (or none) is a crash, with Android's crash log.
alive() {
  now=$("$adb" shell pidof "$id" | tr -d '\r')
  [ "$now" = "$pid" ] && return 0
  echo "FAIL: $1: the app is not the process launched (pid $pid at launch, '$now' now)"
  "$adb" logcat -b crash -d 2>/dev/null | tail -40 | sed 's/^/  crash: /'
  [ -n "$shot" ] && { "$adb" logcat -b crash -d > "$shot/crash.txt" 2>/dev/null; cp "$work/serve.log" "$shot/serve.log"; }
  exit 1
}
wait_for() { for _ in $(seq 1 "${3:-30}"); do has "$1" && return 0; sleep 1; done; fail "$2"; }
# A whole id ends with a quote: tap_last 'pair"' is Pair, where tap_last pair is also pair-privacy, a link to the web.
tap_last() { # resource-id prefix, and a part of it
  xy=""; for _ in $(seq 1 20); do xy=$(xml | last_bounds "$1" "${2:-}" | centre); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || fail "no $1${2:+…$2} on screen"
  "$adb" shell input tap $xy
}
type_text() { rest=$1; while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-10 | sed 's/ /%s/g')'" < /dev/null; rest=$(printf %s "$rest" | cut -c11-); sleep 0.3; done; }
# The composer's text: compose-input is the field's wrapper, the text its EditText child's (as android-type-link.sh
# reads the link field). Read from a dump on stdin.
composed() {
  python3 -c '
import html, re, sys
s = sys.stdin.read()
i = s.find("resource-id=\"compose-input\"")
m = re.search(r"<node [^>]*class=\"android\.widget\.EditText\"[^>]*>", s[i:]) if i >= 0 else None
t = re.search(r" text=\"([^\"]*)\"", m.group(0)) if m else None
print(html.unescape(t.group(1)) if t else "")'
}
# adb's input text drops characters on CI's emulator (a prompt went out as "HELLO, what is in this pictu"): the
# composer is typed in 10-character pieces and read back; a wrong one is cleared and typed again, three times at most.
compose() {
  for n in 1 2 3; do
    type_text "$1"; got=$(xml | composed); [ "$got" = "$1" ] && return 0
    echo "note: the composer holds '$got', not '$1' (try $n); cleared and typed again"
    "$adb" shell input keyevent KEYCODE_MOVE_END $(printf 'KEYCODE_DEL %.0s' $(seq 1 $((${#got} + 1))))
  done
  fail "could not type '$1' into the composer"
}
# With an if, not an &&: a function whose last command is a failed && returns 1, which ends the script under set -e.
# One Back, then up to 3 s for the keyboard to go; a second only when it is still up then. Two Backs a second apart
# closed the pane: on CI the chat view was gone after a prompt each time two had been sent (Pi, OpenCode), as the
# keyboard still read as shown a second after the first. backs counts them, for sent() to say when the view is gone.
backs=0
shown() { "$adb" shell dumpsys input_method | grep -q 'mInputShown=true'; }
keys_down() {
  for n in 1 2; do
    if shown; then "$adb" shell input keyevent KEYCODE_BACK; backs=$((backs + 1)); else return 0; fi
    for _ in 1 2 3 4 5 6; do sleep 0.5; if ! shown; then return 0; fi; done
    if [ "$n" = 1 ]; then echo "note: the keyboard still up 3 s after a Back; a second Back"; fi
  done
  return 0
}
# A prompt sent from the composer; the chat shows it as the newest turn, finished.
sent() {
  tap_last compose-input; compose "$1"; tap_last 'compose-send"'; b=$backs; keys_down
  has 'resource-id="chat-view"' || fail "$2: the chat view is gone right after the prompt was sent, with $((backs - b)) Back(s) sent for the keyboard"
  wait_for "text=\"$1\"" "$2: the chat did not show '$1'" "${3:-40}"
  for _ in $(seq 1 20); do has 'text="Working…"' || break; sleep 1; done
  echo "ok: $2: '$1' shown"
}
size=$("$adb" shell wm size | tr -d '\r' | sed -n 's/^Physical size: //p' | tail -1); w=${size%x*}; h=${size#*x}
# A node whose text starts with $1 lies on the screen, between the header and the composer.
on_screen() { xml | tr '>' '\n' | grep -F "text=\"$1" | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\2 \4/' | awk -v h="$h" '$1 > h * 0.08 && $2 < h * 0.9 {f=1} END {exit !f}'; }

step="launching the app"
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
pid=$("$adb" shell pidof "$id" | tr -d '\r')
[ -n "$pid" ] || fail "the app did not start"
# adb's input text drops characters on CI's emulator (15 of the code's 22 got in): the helper types in short pieces
# and reads the field back.
"$(dirname "$0")/android-type-link.sh" "$invite"
keys_down
tap_last 'pair"'
wait_for 'resource-id="space-' "the phone did not pair"

first=1
for a in $agents; do
  s=$(printf %s "$a" | tr -d -)
  step="$a"
  tap_last "space-spc$s"
  # After the first agent a pane opens in its chat (the view picked last), with no chat button.
  sleep 2; has 'resource-id="chat-view"' || tap_last show-chat
  wait_for 'resource-id="chat-view"' "$a: no chat view"
  # open
  if [ "$a" = opencode ]; then
    wait_for 'resource-id="chat-unknown"' "$a: does not say it can't tell the conversation yet" 20
    sent "HELLO again" "$a open"
  fi
  wait_for 'text="040 ' "$a: the long conversation did not show"
  snap "$a-open"; alive "$a open"; echo "ok: $a open: turn 040 shown"
  # long
  sent "LIST the files here" "$a long"
  tap_last fold-
  tap_last step- fake_list
  wait_for 'Show all 500 lines' "$a long: the command's 500 lines are not offered" 20
  tap_last run-all-
  sleep 2; snap "$a-long"; alive "$a long"; echo "ok: $a long: Show all 500 lines"
  # image
  # A new picture: one whose id wasn't there (the chat holds 30 turns, so the oldest one's may go as one comes).
  images() { xml | tr '>' '\n' | grep -oE 'resource-id="image-[^"]+"' | grep -v 'image-source-' | sort -u; }
  before=$(images)
  if [ "$a" = claude-code ]; then sent "HELLO, what is in this picture?" "$a image"; else
    sent "LOOK at shot.png" "$a image"; tap_last fold-; tap_last step- fake_look
  fi
  new=""; for _ in $(seq 1 20); do new=$(images | grep -vxF "$before" || true); [ -n "$new" ] && break; sleep 1; done
  [ -n "$new" ] || fail "$a image: no new picture showed"
  snap "$a-image"; alive "$a image"; echo "ok: $a image: new pictures $(echo $new)"
  # scroll: the chat holds the last 30 turns; the oldest is numbered 01x
  top=""; for _ in $(seq 1 40); do on_screen "01" && { top=1; break; }; "$adb" shell input swipe $((w / 2)) $((h * 3 / 10)) $((w / 2)) $((h * 7 / 10)) 150; done
  [ -n "$top" ] || fail "$a scroll: did not reach the oldest turns"
  snap "$a-top"; alive "$a scroll"
  last="LOOK at shot.png"; [ "$a" = claude-code ] && last="HELLO, what is in this picture?"
  bottom=""; for _ in $(seq 1 40); do on_screen "$last" && { bottom=1; break; }; "$adb" shell input swipe $((w / 2)) $((h * 7 / 10)) $((w / 2)) $((h * 3 / 10)) 150; done
  [ -n "$bottom" ] || fail "$a scroll: did not get back to the newest turn"
  echo "ok: $a scroll: the oldest turns and back"
  # away
  if [ -n "$first" ]; then secs_list=$away; else secs_list=${away%% *}; fi
  for secs in $secs_list; do
    "$adb" shell input keyevent KEYCODE_HOME; sleep "$secs"
    "$adb" shell am start -n "$id/.MainActivity" >/dev/null
    wait_for 'resource-id="chat-view"' "$a away $secs s: the chat is gone" 15
    alive "$a away $secs s"
    off=no; has 'resource-id="chat-offline"' && off=yes
    snap "$a-away$secs"; echo "ok: $a away $secs s: chat there (says it can't reach the computer: $off)"
    sent "EDIT after $secs s away" "$a away $secs s" 60
  done
  # cut
  pkill -f -- "--state $state" 2>/dev/null || true; echo "--- stopped $(date +%T)" >> "$work/serve.log"
  wait_for 'resource-id="chat-offline"' "$a cut: nothing says the computer can't be reached" 45
  has "text=\"EDIT after ${away%% *} s away\"" || fail "$a cut: the turns went"
  snap "$a-cut"; alive "$a cut"; echo "ok: $a cut: says it can't reach the computer, the turns kept"
  echo "--- started $(date +%T)" >> "$work/serve.log"; serve
  gone=""; for _ in $(seq 1 60); do has 'resource-id="chat-offline"' || { gone=1; break; }; sleep 1; done
  [ -n "$gone" ] || fail "$a cut: still says it can't reach the computer"
  sent "CHANGE after the restart" "$a cut" 60
  snap "$a-back"; alive "$a back"
  tap_last back; sleep 1
  first=
done
alive "the end"
echo PASS
