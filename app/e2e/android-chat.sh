#!/bin/sh
# The chat view on Android, against an isolated herdr-core: a fake Claude Code (e2e/fake-claude.py, replaying turns
# Claude Code 2.1.322 wrote against a fake API) and a shell. The shell's pane has no chat button; the agent's has one.
# A prompt typed in the composer goes to the terminal as before; the chat then shows the turn, a tap opens its steps
# and a step's details, and one tap goes back to the terminal; then the same in dark. A reply's video shows a frame and
# plays in the app; its PDF opens in another app. The header is shot in all seven
# languages, light and dark. Fake data only.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-chat.sh APK COMPANION [folder for screenshots]
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard android-chat # a login entry of its own, never the person's
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
[ -z "$shot" ] || mkdir -p "$shot"
app=$(cd "$(dirname "$0")/.." && pwd)
fixtures="$app/../companion/internal/chat/claude/testdata/claude-code/2.1.322"
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/home/notes" "$work/state" "$work/config" "$work/data" "$work/claude"
printf 'a\n' > "$work/home/src/acme-api/notes.txt"; printf 'b\n' > "$work/home/src/acme-api/todo.txt"
# The files the RENDER reply names: a made-up 3 s test pattern and a one-page PDF (e2e/files).
mkdir -p "$work/home/src/acme-api/renders" "$work/home/src/acme-api/docs"
cp "$app/e2e/files/clip.mp4" "$work/home/src/acme-api/renders/demo.mp4"; cp "$app/e2e/files/guide.pdf" "$work/home/src/acme-api/docs/"
: > "$work/home/.zshrc"
# herdr-core passes the environment of whatever first started it to every terminal: started from inside a Claude Code
# session, every Claude in it inherits CLAUDE_CODE_CHILD_SESSION=1 and saves no transcript (seen with Claude Code 2.1.319).
. "$app/e2e/companion.sh"  # companion_start, companion_stop
clean() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/home" HERDR_ANYWHERE_AUTOSTART_NAME="$HERDR_ANYWHERE_AUTOSTART_NAME" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmac-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
    CLAUDE_CONFIG_DIR="$work/claude" "$@"
}
lhc() { clean "$hc" "$@"; }
# Every way out (done, a failed step, Ctrl-C, kill) stops the companion and the core and removes the temp files.
# Dark mode as found ("Night mode: no" → no), put back on the way out.
# Screen density and text size as found, put back on the way out (the narrow check changes both).
density=$("$adb" shell wm density | tr -d '\r' | sed -n 's/^Override density: //p'); font=$("$adb" shell settings get system font_scale | tr -d '\r')
night=$("$adb" shell cmd uimode night 2>/dev/null | tr -d '\r' | sed -n 's/^Night mode: //p'); case $night in yes|no|auto) ;; *) night=no;; esac
cleanup() {
  # A second INT or TERM (a runner that stops the children and then the script) must not cut this short: the
  # herdr-core, the agents' own background servers and the Companion would stay running.
  trap - EXIT; trap '' INT TERM HUP
  set +e  # under set -e, the first thing not found (pkill with nothing to stop) would end the script here
  companion_stop || stop_failed=1
  if [ -n "$density" ]; then "$adb" shell wm density "$density"; else "$adb" shell wm density reset; fi >/dev/null 2>&1 || true
  case $font in ''|null) "$adb" shell settings delete system font_scale ;; *) "$adb" shell settings put system font_scale "$font" ;; esac >/dev/null 2>&1 || true
  "$adb" shell cmd uimode night "$night" >/dev/null 2>&1 || true; "$adb" shell cmd locale set-app-locales "$id" --locales "" </dev/null >/dev/null 2>&1 || true
  lhc server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  autostart_guard_end || exit 1
  # A Companion that did not end fails the run (companion_stop said which).
  [ -z "${stop_failed:-}" ] || exit 1
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM HUP
lhc run --bg --keep --cwd "$work/home/src/acme-api" --name claude --label herdr.space=spac1 --label herdr.tab=tbac1 \
  python3 "$app/e2e/fake-claude.py" "$fixtures/turns.jsonl" "$fixtures/steps.jsonl" "$fixtures/files.jsonl" "$fixtures/gallery.jsonl" >/dev/null
lhc run --bg --keep --cwd "$work/home/notes" --label herdr.space=spac2 --label herdr.tab=tbac2 >/dev/null
companion_start "$work/serve.log" clean "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" || exit 1
invite=$(clean "$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
# The bounds of the first node whose resource-id is $1, or starts with $1 when it ends in '-' (fold-).
bounds() { case $1 in *-) m="resource-id=\"$1" ;; *) m="resource-id=\"$1\"" ;; esac; tr '>' '\n' | grep -F "$m" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
centre() { bounds "$1" | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'; }
has() { xml | grep -qF "$1"; }
wait_for() { for _ in $(seq 1 30); do has "$1" && return 0; sleep 1; done; echo "FAIL: $2"; snap fail; [ -n "$shot" ] && xml > "$shot/fail.xml"
  xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1; }
# A system permission dialog over the app is said and turned down: none of these steps needs one.
dialog() {
  d=$(xml); printf %s "$d" | grep -q 'permissioncontroller:id/grant_dialog' || return 1
  echo "note: a permission dialog: $(printf %s "$d" | tr '>' '\n' | grep -F 'permission_message' | sed -E 's/.* text="([^"]*)".*/\1/')"
  "$adb" shell input tap $(printf %s "$d" | centre com.android.permissioncontroller:id/permission_deny_button); sleep 1
}
tap() {
  xy=""; for _ in $(seq 1 30); do dialog || true; xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; snap fail; [ -n "$shot" ] && xml > "$shot/fail.xml"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  "$adb" shell input tap $xy
}
type_text() { rest=$1; while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-80 | sed 's/ /%s/g')'" < /dev/null; rest=$(printf %s "$rest" | cut -c81-); sleep 0.5; done; }
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/$1.png" || true; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
type_text "$invite"
# The keyboard covers Pair (Gboard sometimes opens its clipboard panel there): closed first, so the tap reaches the button.
for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done
tap pair
wait_for 'resource-id="space-' "the phone did not pair (the invite typed, Pair tapped)"

# A shell: no chat button.
tap space-spac2
wait_for 'resource-id="terminal-' "the shell's terminal did not show"
sleep 3
has 'resource-id="show-chat"' && { echo "FAIL: a shell's pane has a chat button"; exit 1; }
echo "ok: a shell's pane has no chat button"
tap back

# The agent: a prompt through the composer, then the chat.
tap space-spac1
tap compose-input
type_text "LIST the files here"
tap compose-send
wait_for 'resource-id="show-chat"' "the agent's pane has no chat button"
tap show-chat
wait_for 'There are two files' "the chat did not show the reply"
snap chat
echo "ok: the prompt typed in the composer is a turn of the chat, with its reply"
tap fold-
wait_for 'resource-id="step-toolu_fake_list"' "the turn's steps did not open"
tap step-toolu_fake_list
# A command shows itself and what it printed, not its input as JSON.
wait_for 'resource-id="run-toolu_fake_list"' "the command's details did not show"
has 'todo.txt' || { echo "FAIL: the command's output is not shown"; exit 1; }
snap chat-step
echo "ok: a tap opens the turn's steps, a tap on a step its details"
tap show-terminal
wait_for 'resource-id="terminal-' "the terminal did not come back"
has 'There are two files' && { echo "FAIL: the chat is still up"; exit 1; }
echo "ok: one tap goes back to the terminal"

restart() { "$adb" shell am force-stop "$id"; "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null; }
# Dark: the same chat, its steps open.
"$adb" shell cmd uimode night yes >/dev/null
restart
tap space-spac1
tap show-chat
wait_for 'There are two files' "the chat did not show in dark"
tap fold-
wait_for 'resource-id="step-toolu_fake_list"' "the turn's steps did not open in dark"
snap chat-dark
tap show-terminal
wait_for 'resource-id="terminal-' "the terminal did not come back in dark"
echo "ok: the chat in dark"

# A change and a failed command, in light: four more prompts (EDIT, HELLO, CHANGE, BREAK in the fixtures' order).
"$adb" shell cmd uimode night no >/dev/null
restart
tap space-spac1
for p in "EDIT a file for me" "HELLO again" "CHANGE the notes" "BREAK the build"; do tap compose-input; type_text "$p"; tap compose-send; sleep 2; done
has 'resource-id="show-chat"' && tap show-chat
wait_for 'The check failed with code 3.' "the failed command's turn did not show"
# The last two folds: the CHANGE turn's and the BREAK turn's.
nth_fold() { xml | tr '>' '\n' | grep -F 'resource-id="fold-' | tail -n "$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
"$adb" shell input tap $(nth_fold 1); wait_for 'resource-id="step-toolu_fake_break"' "the failed command's step did not open"
has 'text="Failed"' || { echo "FAIL: the failed command's line does not say it failed"; exit 1; }
tap step-toolu_fake_break
wait_for 'Failed · exit code 3' "the failed command's exit code did not show"
has 'one test failed' || { echo "FAIL: what the failed command printed is not shown"; exit 1; }
snap chat-run
echo "ok: a failed command says so on its line, and its details show its exit code and what it printed"
"$adb" shell input tap $(nth_fold 2); wait_for 'resource-id="step-toolu_fake_edit"' "the edit's step did not open"
has 'text="+2 −0"' || { echo "FAIL: the edit's line does not say how many lines it added"; exit 1; }
tap step-toolu_fake_edit
wait_for 'resource-id="change-toolu_fake_edit"' "the edit's change did not show"
snap chat-change
echo "ok: an edit says on its line how many lines it added, and its details show them"
tap show-terminal
wait_for 'resource-id="terminal-' "the terminal did not come back after the steps"

# A reply that names a video and a PDF on the computer: cards under it. The phone makes the video's frame from the
# Companion's link for it and plays it in the app; the PDF goes, fetched whole, to another app on the phone (a viewer,
# or the share sheet when none opens PDFs).
tap compose-input; type_text "RENDER the demo video"; tap compose-send
tap show-chat
wait_for 'resource-id="file-docs/guide.pdf"' "the reply's files got no cards"
wait_for 'resource-id="video-frame-renders/demo.mp4"' "the phone made no frame of the video"
has 'text="0:03"' || { echo "FAIL: the video's card does not say how long it is"; exit 1; }
snap chat-files
echo "ok: a reply's video shows as a frame of it with its length, its PDF as a card"
tap file-renders/demo.mp4
wait_for 'resource-id="video-player"' "the video did not open in the player"
sleep 4
has "The video can't be played." && { echo "FAIL: the player could not play the video from the Companion"; exit 1; }
snap chat-video
tap video-close
wait_for 'resource-id="file-docs/guide.pdf"' "the player did not close"
echo "ok: a tap plays the video in the app, from the Companion"
"$adb" logcat -c
tap file-docs/guide.pdf
front() { "$adb" shell dumpsys activity activities | tr -d '\r' | grep -m1 -E 'topResumedActivity|mResumedActivity' | sed -E 's/.* ([^ ]+)\/([^ }]+).*/\1\/\2/'; }
i=0; until [ "$(front | cut -d/ -f1)" != "$id" ] || [ $i -gt 15 ]; do sleep 1; i=$((i+1)); done
opened_in=$(front)
[ "${opened_in%%/*}" != "$id" ] || { echo "FAIL: the PDF did not go to another app"; exit 1; }
got=$("$adb" logcat -d -s herdr:I | tr -d '\r' | grep -o 'handed over: guide.pdf, [0-9]* bytes' | tail -1 | grep -o '[0-9]* bytes' | cut -d' ' -f1)
[ "$got" = "$(wc -c < "$app/e2e/files/guide.pdf" | tr -d ' ')" ] || { echo "FAIL: the PDF the phone handed over is $got bytes"; exit 1; }
sleep 4  # the viewer draws the page a moment after it opens
snap chat-pdf-opened
echo "note: the PDF went to $opened_in"
echo "ok: a tap on the PDF hands it, whole, to another app on the phone"
"$adb" shell input keyevent KEYCODE_BACK; sleep 1
[ "$(front | cut -d/ -f1)" = "$id" ] || "$adb" shell input keyevent KEYCODE_BACK
tap show-terminal
wait_for 'resource-id="terminal-' "the terminal did not come back after the files"

# The header in seven languages, light and dark: the chat button fits beside the agent's name.
for mode in no yes; do
"$adb" shell cmd uimode night "$mode" >/dev/null
for l in en-US zh-CN zh-TW es-ES ko-KR ja-JP ru-RU; do
  "$adb" shell cmd locale set-app-locales "$id" --locales "$l" </dev/null >/dev/null
  restart
  tap space-spac1
  wait_for 'resource-id="show-chat"' "no chat button in $l"
  dump=$(xml)
  sw=$(printf %s "$dump" | bounds switcher); bt=$(printf %s "$dump" | bounds show-chat)
  # The switcher (name and place) ends before the button starts.
  echo "$sw $bt" | awk -v l="$l" '{ if ($3 > $5) { print "FAIL: in " l " the name runs into the chat button (" $0 ")"; exit 1 } }'
  snap "header-$l-$( [ "$mode" = yes ] && echo dark || echo light)"
done
done
echo "ok: the chat button fits in the header in all seven languages, light and dark"

# A narrow phone at Android's largest text: the agent's name is as wide beside the chat button at 360 dp as at 411 dp,
# so the button cuts none of it. HM_CHAT_NARROW_DP sets the narrow width (a smaller one should fail: the check can see a cut).
"$adb" shell cmd uimode night no >/dev/null; "$adb" shell cmd locale set-app-locales "$id" --locales "" </dev/null >/dev/null
px=$("$adb" shell wm size | tr -d '\r' | tail -1 | awk '{print $NF}' | cut -dx -f1)
title_dp() { "$adb" shell wm density $((px * 160 / $1)) >/dev/null; sleep 2; restart; tap space-spac1
  wait_for 'resource-id="show-chat"' "no chat button at $1 dp"; snap "header-$1dp-$("$adb" shell settings get system font_scale | tr -d '\r')"
  xml | bounds pane-name | awk -v dp="$1" -v px="$px" '{ printf "%.1f", ($3 - $1) * dp / px }'; }
"$adb" shell settings put system font_scale 2.0
wide=$(title_dp 411); narrow=$(title_dp "${HM_CHAT_NARROW_DP:-360}")
echo "note: the name at the largest text: ${wide} dp at 411 dp, ${narrow} dp at ${HM_CHAT_NARROW_DP:-360} dp"
awk -v w="$wide" -v n="$narrow" 'BEGIN { exit !(n + 1 < w) }' && { echo "FAIL: at ${HM_CHAT_NARROW_DP:-360} dp the chat button cuts the agent's name"; exit 1; }
echo "ok: at 360 dp and the largest text the chat button cuts none of the agent's name"

# With "+1 waiting" beside it (another pane asks a question), at 360 dp and the default text, in each language: the label
# gives way to "+1 ›" and the name is whole again; at 411 dp the label stays whole. At the largest text the widths are noted.
# The whole name's width is measured at the same density as the one it is held against: rounding to pixels differs by
# density, and a name 1 dp short is already cut ("Claude Co…").
"$adb" shell settings put system font_scale 1.0
whole411=$(title_dp 411); whole=$(title_dp 360)
lhc run --bg --keep --cwd "$work/home/notes" --label herdr.space=spac3 --label herdr.tab=tbac3 \
  sh -c "printf '\033]7501;state=blocked:kind=question:msg=$(printf 'Which port?' | base64)\033\\\\'; sleep 100000" >/dev/null
waiting_at() { # dp, font scale, language: "<name dp> <label>"
  "$adb" shell wm density $((px * 160 / $1)) >/dev/null; "$adb" shell settings put system font_scale "$2"
  "$adb" shell cmd locale set-app-locales "$id" --locales "$3" </dev/null >/dev/null; sleep 1; restart; tap space-spac1
  wait_for 'resource-id="next-waiting"' "no +1 waiting in $3 at $1 dp"; snap "header-$1dp-$2-waiting-$3"
  dump=$(xml); printf '%s %s' "$(printf %s "$dump" | bounds pane-name | awk -v dp="$1" -v px="$px" '{ printf "%.1f", ($3 - $1) * dp / px }')" \
    "$(printf %s "$dump" | tr '>' '\n' | grep -F 'resource-id="next-waiting"' -A3 | grep -o 'text="[^"]*"' | grep -v 'text=""' | head -1)"; }
set -- $(waiting_at 411 1.0 en-US); w=$1; shift
echo "note: 411 dp, +1 waiting: name $w dp of ${whole411} dp, $*"
case "$*" in *waiting*) ;; *) echo "FAIL: at 411 dp \"+1 waiting\" was cut short ($*)"; exit 1 ;; esac
for l in en-US zh-CN zh-TW es-ES ko-KR ja-JP ru-RU; do
  set -- $(waiting_at 360 1.0 "$l"); w=$1; shift
  echo "note: 360 dp, $l, +1 waiting: name $w dp of ${whole} dp, $*"
  awk -v w="$w" -v f="$whole" 'BEGIN { exit !(w + 0.2 < f) }' && { echo "FAIL: at 360 dp in $l, +1 waiting cuts the agent's name ($w of $whole dp)"; exit 1; }
done
echo "ok: at 360 dp with +1 waiting, the label gives way and the agent's name is whole, in all seven languages"
"$adb" shell settings put system font_scale 2.0; whole2=$(title_dp 360)
for l in en-US es-ES; do set -- $(waiting_at 360 2.0 "$l"); w=$1; shift; echo "note: 360 dp, largest text, $l, +1 waiting: name $w dp of ${whole2} dp, $*"; done
"$adb" shell cmd locale set-app-locales "$id" --locales "" </dev/null >/dev/null
