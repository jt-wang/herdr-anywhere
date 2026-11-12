#!/bin/sh
# The experimental progress page on an Android device or emulator against a real computer side: an isolated
# herdr-core with three stand-in Claude panes (e2e/fake-claude), the companion on loopback (10.0.2.2 from an
# emulator), and a progress file shaped like the sample scene. Version one is view only: the phone
# only shows; the computer files and judges. The script pairs, turns on Settings › Experimental › Progress page and picks the agent
# that sorts (which runs `progress organizer --agent claude-code && progress on` on the computer), then:
#   turning on scans the open panes; two panes stop on their tasks; the filing pane's answers (this script stands in
#   for it with `track place`, in English as the agent would for an English phone) show up on their own: a finished
#   task and a task found in the scan, ticked under their goal, and Needs you (a pane asking) → its Open button opens that pane → a
#   task opens with its own tasks → the agent list says On: (only says it; it opens nothing).
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-progress.sh [apk] [out-dir]
#   out-dir gets a screenshot per step and flow.mp4 (adb screenrecord). SETUP_ONLY=1: the computer side alone, no
#   device: turn on from the command line, one pass, the stand-in answers, check and print the tree.
#   DARK=1: the device in dark mode for the run, put back as it was after.
set -eu
app=$(cd "$(dirname "$0")/.." && pwd)
apk=${1:-$app/android/build/Herdr.apk}
out=${2:-}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7701
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
run=$(mktemp -d /tmp/hpXXXX)
mkdir -p "$work/home/src/acme-shop" "$work/home/src/acme-app" "$work/home/.local/bin" "$work/state" "$work/config" "$work/data" "$work/bin"
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
(cd "$app/e2e/fake-claude" && GO111MODULE=off go build -o "$work/bin/claude" main.go)
: > "$work/home/.zshrc"
# The app runs $HOME/.local/bin/herdr-anywhere on the computer, where the README installs it.
cp "$work/herdr-anywhere" "$work/home/.local/bin/herdr-anywhere"
export HC_SESSION="hpandroid-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export HERDR_ANYWHERE_STATE="$work/companion" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
# The stand-in Claude is the `claude` the computer finds, for the agent list and for the filing pane it starts.
export PATH="$work/bin:$PATH"
cleanup() {
  [ -n "${rec_pid:-}" ] && kill "$rec_pid" 2>/dev/null
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  [ -n "${night:-}" ] && "$adb" shell cmd uimode night "$night" >/dev/null 2>&1
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
}
trap cleanup EXIT INT TERM

pane() { # name dir env…
  n=$1 d=$2; shift 2
  "$hc" run --bg --keep --cwd "$work/home/src/$d" --name "$n" --label herdr.space="sp$d" --label herdr.tab="tb$n" -- env "$@" "$work/bin/claude" >/dev/null
}
# payments and mobile stop a few seconds after their hand-off: payments done, mobile asking.
pane payments acme-shop FAKE_STATE=working FAKE_THEN=done FAKE_AFTER=6 FAKE_MSG='The comparison is ready for you to read'
pane checkout acme-shop FAKE_STATE=working
pane mobile acme-app FAKE_STATE=working FAKE_THEN=done FAKE_AFTER=8 FAKE_MSG='Round prices to whole numbers, or keep the cents?'
pane design acme-app FAKE_STATE=idle
sleep 1
# The design pane's Claude session, on no task yet: turning the page on goes through it once.
pid=$("$hc" get design --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["foreground"]["pid"])')
mkdir -p "$HOME/.claude/sessions" "$HOME/.claude/projects/-src-acme-app"
echo "{\"pid\":$pid,\"sessionId\":\"s-phone\"}" > "$HOME/.claude/sessions/$pid.json"
python3 - "$HOME/.claude/projects/-src-acme-app/s-phone.jsonl" <<'PY'
import json, sys
lines = [{"type": "user", "origin": {"kind": "human"}, "promptSource": "typed", "message": {"role": "user", "content": "Tighten the button spacing on Android"}},
         {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "Spacing is 12 now"}], "stop_reason": "end_turn"}}]
open(sys.argv[1], "w").write("".join(json.dumps(l, ensure_ascii=False) + "\n" for l in lines))
PY

# The progress file, as the sample scene. Each pane is put on its task the way the filing pane does it:
# what was typed there is queued (Q1, the queue being empty), then placed.
B="$work/herdr-anywhere"
t() { "$B" track "$@" --herdr - >/dev/null; }
t add P2 "Compare payment providers"
t file --pane hc:payments "Compare three payment providers and write it up"
t place Q1 --task P2
t add --std "Customers can pay in two steps" P1 "Ship the new checkout for the Acme shop"
t add --under P1 --stage doing T1 "Let customers save their card"
t file --pane hc:checkout "Start with the card form"
t place Q1 --task T1
t add --under T1 --stage done T2 "Add the card form"
t add --under T1 --stage parked T3 "Check the card number as it is typed"
t add --under T1 --stage parked T4 "Let customers remove a saved card"
t add --under P1 --stage parked --next "Waits for the card work" T5 "Test the checkout on an Android phone"
t add --under P1 --stage doing T6 "Show prices in the customer's currency"
t file --pane hc:mobile "Show each price in the customer's own currency"
t place Q1 --task T6
t file --pane hc:checkout "Also send a receipt email after payment"
t place Q1 --task new --under P1 --title "Send a receipt email after payment" --why "Looks like a new task"
file="$HERDR_ANYWHERE_STATE/track.json"
q() { python3 -c "import json,sys; q=json.load(open('$file')).get('queue') or []; sys.exit(0 if $1 else 1)"; }

# What the filing pane would answer: the stopped panes by the agent's last words, the scanned session as a task.
answer() {
  for _ in $(seq 1 40); do q "len([x for x in q if x['kind']=='stop'])==2 and any(x['kind']=='session' for x in q)" && break; sleep 1; done
  q "len([x for x in q if x['kind']=='stop'])==2 and any(x['kind']=='session' for x in q)" || { echo "FAIL: the stops and the scan were not queued: $(python3 -c "import json; print(json.load(open('$file')).get('queue'))")"; exit 1; }
  python3 -c "import json; [print(x['id'], x['kind'], x.get('task','-')) for x in json.load(open('$file'))['queue']]" | while read -r id kind task; do
    case "$kind:$task" in
      stop:P2) "$B" track place "$id" --stage done --herdr - >/dev/null ;;
      stop:T6) "$B" track place "$id" --stage waiting --waiting "Round prices to whole numbers, or keep the cents?" --herdr - >/dev/null ;;
      session:-) "$B" track place "$id" --task new --under P1 --title "Tighten the button spacing on Android" --stage done --herdr - >/dev/null ;;
    esac
  done
  q "not q" || failed step1 "answers left items queued"
  python3 - "$file" <<'PY' || failed step2 "the answers did not reach the tasks"
import json, sys
t = {x["id"]: x for x in json.load(open(sys.argv[1]))["things"]}
assert t["P2"]["stage"] == "done" and t["T6"]["stage"] == "waiting" and t["T6"].get("waiting")
assert any(x["outcome"] == "Tighten the button spacing on Android" and x.get("stage") == "done" and x.get("parent") == "P1" for x in t.values())
PY
  echo "ok: the stops and the scanned session were judged and filed"
}

if [ "${SETUP_ONLY:-}" = 1 ]; then
  "$B" progress organizer --agent claude-code --hc "$hc" >/dev/null && "$B" progress on
  sleep 9 # both panes have stopped
  "$B" progress once --hc "$hc"
  answer
  "$B" track tree --herdr -
  echo PASS
  exit 0
fi
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
"$B" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
sleep 1
link=$("$B" pair --url "http://10.0.2.2:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)

at() {
  case $1 in *-) q="resource-id=\"$1" ;; *) q="resource-id=\"$1\"" ;; esac
  "$adb" shell uiautomator dump /sdcard/hp.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hp.xml | tr '>' '\n' | grep -F "$q" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
# The top edge of a view, in screen pixels.
top_of() {
  "$adb" shell uiautomator dump /sdcard/hp.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hp.xml | tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[[0-9]+,([0-9]+)\].*/\1/'
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || failed step3 "no view $1 on screen"
  "$adb" shell input tap $xy
}
screen_has() { "$adb" shell uiautomator dump /sdcard/hp.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hp.xml | grep -q "$1"; }
wait_screen() { for _ in $(seq 1 "${2:-15}"); do screen_has "$1" && return 0; sleep 1; done; return 1; }
shot() { [ -n "$out" ] && "$adb" exec-out screencap -p > "$out/$1.png" || true; }
# On a failure: the screen, its view tree and the companion's log, then the reason.
failed() { shot "fail-$1"; [ -n "$out" ] && { "$adb" exec-out uiautomator dump /dev/tty > "$out/fail-$1.xml" 2>/dev/null; cp "$work/serve.log" "$out/serve.log"; }; echo "FAIL: $2"; exit 1; }
pair_failed() { failed pair "$1"; }
in_file() { python3 -c "import json,sys; d=json.load(open('$file')); t={x['id']:x for x in d.get('things',[])}; sys.exit(0 if $1 else 1)"; }
wait_file() { for _ in $(seq 1 15); do in_file "$1" && return 0; sleep 1; done; echo "FAIL: $2"; exit 1; }
swipe_up() { "$adb" shell input swipe 540 1700 540 700 300; sleep 1; }
swipe_down() { "$adb" shell input swipe 540 700 540 1700 300; sleep 1; }

[ -n "$out" ] && mkdir -p "$out"
# The page and the stand-in's answers are in English: the device must be too.
loc=$("$adb" shell getprop persist.sys.locale | tr -d '\r'); [ -n "$loc" ] || loc=$("$adb" shell getprop ro.product.locale | tr -d '\r')
case "$loc" in en*) ;; *) echo "FAIL: the device's language is $loc; this script checks English words"; exit 1 ;; esac
if [ "${DARK:-}" = 1 ]; then
  night=$("$adb" shell cmd uimode night | tr -d '\r' | awk '{print $NF}')
  "$adb" shell cmd uimode night yes >/dev/null
fi
# The shared AVD may hold a newer build from another branch: allow the downgrade (version 13 over 15 was seen).
"$adb" install -r -d "$apk" >/dev/null
"$adb" shell pm clear "$id" >/dev/null
# A cleared app asks for notifications on its first screen (Android 13 and later), over everything this script
# reads: the test setup answers yes ahead of time.
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS 2>/dev/null || true
activity=$("$adb" shell cmd package resolve-activity --brief "$id" | tail -1 | tr -d '\r')
"$adb" shell am start -W -n "$activity" >/dev/null
# On a failure here: what the phone showed, its screen as text, and the companion's log.
wait_screen pair-link 20 || pair_failed "no pairing screen within 20 s"
tap pair-link
"$adb" shell input text "'$link'"
tap pair
# Paired: the home page lists the panes (by program and message: herdr-core panes started with --name have no tab name).
wait_screen "The comparison is ready for you to read" 30 || pair_failed "not paired within 30 s"
echo "ok: paired"

# Off by default: the first screen is the agent list. Turning the page on asks which agent sorts; picking one turns
# the computer's filing on with it.
screen_has "In progress" && { echo "FAIL: the progress page is on before it was turned on"; exit 1; }
grep -q '"on":true' "$HERDR_ANYWHERE_STATE/progress.json" 2>/dev/null && { echo "FAIL: filing is on before the page was turned on"; exit 1; }
tap settings
tap progress-first
# The switch's words follow the phone's language; the agents' names don't.
wait_screen 'resource-id="agent-claude-code"' 20 || failed step4 "no agent to pick"
sleep 1
shot 0-pick-agent
tap agent-claude-code
for _ in $(seq 1 15); do grep -q '"on":true' "$HERDR_ANYWHERE_STATE/progress.json" 2>/dev/null && break; sleep 1; done
grep -q '"on":true' "$HERDR_ANYWHERE_STATE/progress.json" || failed step5 "picking did not turn filing on"
grep -q '"agent": "claude-code"' "$HERDR_ANYWHERE_STATE/organizer.json" || failed step6 "picking did not set the agent"
grep -q '"lang": "en"' "$HERDR_ANYWHERE_STATE/organizer.json" || failed step7 "picking did not tell the computer the phone's language"
echo "ok: turning the page on picked Claude Code and turned filing on"
"$adb" shell am force-stop "$id"
if [ -n "$out" ]; then
  "$adb" shell rm -f /sdcard/flow.mp4
  "$adb" shell screenrecord --bit-rate 6000000 --time-limit 170 /sdcard/flow.mp4 &
  rec_pid=$!
  sleep 1
fi
"$adb" shell am start -W -n "$activity" >/dev/null
wait_screen "In progress" 20 || failed step8 "the first screen is not the progress page"
sleep 2
shot 1-home
switch_top=$(top_of view-switch)
answer
wait_screen "Round prices to whole numbers, or keep the cents?" 20 || failed step9 "Needs you does not show what the pane asks"
# The task found in the scan finished today: it stays under its goal, ticked (further down the page).
wait_screen "Tighten the button spacing on Android" 5 || { swipe_up; wait_screen "Tighten the button spacing on Android" 10; } || failed step10 "the page does not show the task found in the scan"
swipe_down; swipe_down
for x in "to check" dock-input; do screen_has "$x" && { echo "FAIL: the page still shows $x"; exit 1; }; done
sleep 2
shot 2-judged
echo "ok: the page shows the asking pane under Needs you and the finished tasks under their goal, with nothing to tick"

tap needs-task-T6-open
mobile=$("$hc" get mobile --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["address"])')
wait_screen "terminal-$mobile" 15 || failed step12 "Open does not open the asking pane"
sleep 2
shot 3-asking-pane
echo "ok: Open opens the pane that asks"
"$adb" shell input keyevent 4
sleep 1

tap task-T1
wait_screen "Tasks under it" 10 || failed step13 "the task does not show its own tasks"
sleep 2
shot 4-task
echo "ok: a task opens with its own tasks"
"$adb" shell input keyevent 4
sleep 1

# The switch stays where it is: below the status bar on both pages, so a tap on it is never lost to the status bar.
[ "$(top_of view-switch)" = "$switch_top" ] || failed switch-progress "the switch moved on the progress page: $(top_of view-switch), first $switch_top"
tap view-agents
wait_screen 'resource-id="doing-' 10
[ "$(top_of view-switch)" = "$switch_top" ] || failed switch-agents "the switch is at $switch_top on the progress page, $(top_of view-switch) on the agent list"
checkout=$("$hc" get checkout --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["address"])')
wait_screen "On: Send a receipt email after payment" 15 || failed agents "the agent list does not say what checkout is on ($checkout)"
sleep 2
shot 5-agents
echo "ok: the agent list says what checkout is on"

if [ -n "$out" ]; then
  kill -INT "$rec_pid" 2>/dev/null || true; wait "$rec_pid" 2>/dev/null || true; rec_pid=""
  sleep 2
  "$adb" pull /sdcard/flow.mp4 "$out/flow.mp4" >/dev/null
fi
echo PASS
