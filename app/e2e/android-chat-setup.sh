#!/bin/sh
# Pi's chat setup on Android, end to end: the installed Pi (in a throwaway HOME, against the fake Responses API) in an
# isolated herdr-core, and a Companion that finds Pi's folder there. The first Pi chat asks once; Not now leaves the
# page with Install; Install puts the file in Pi's folder; the running Pi is told to type /reload, and the phone types
# it; the chat then shows the conversation. Settings shows the row on, and Remove takes the file away. In Simplified
# Chinese, light. Fake data only.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-chat-setup.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
[ -z "$shot" ] || mkdir -p "$shot"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
pi=$(command -v pi || echo "$HOME/.local/bin/pi")
[ -x "$pi" ] || { echo "SKIP: no pi on this computer"; exit 0; }
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
fport=7810; while nc -z 127.0.0.1 "$fport" 2>/dev/null; do fport=$((fport + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
agent="$work/home/.pi/agent"
mkdir -p "$work/home/src/acme-web" "$agent" "$work/state" "$work/config" "$work/data"
printf 'a\n' > "$work/home/src/acme-web/notes.txt"; : > "$work/home/.zshrc"
cat > "$agent/models.json" <<CFG
{"providers": {"fake": {"baseUrl": "http://127.0.0.1:$fport/v1", "api": "openai-responses", "apiKey": "fake",
  "models": [{"id": "gpt-5.5", "name": "Fake GPT"}]}}}
CFG
(cd "$app/../companion" && go build -o "$work/fake-responses" ./internal/chat/fakeresponses/serve)
. "$app/e2e/companion.sh"  # companion_start, companion_stop
clean() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/home" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmps-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
    PI_CODING_AGENT_DIR="$agent" PI_SKIP_VERSION_CHECK=1 PI_OFFLINE=1 "$@"
}
lhc() { clean "$hc" "$@"; }
fake_pid=
cleanup() {
  # A second INT or TERM (a runner that stops the children and then the script) must not cut this short: the
  # herdr-core, the agents' own background servers and the Companion would stay running.
  trap - EXIT; trap '' INT TERM HUP
  set +e  # under set -e, the first thing not found (pkill with nothing to stop) would end the script here
  companion_stop || stop_failed=1
  [ -n "$fake_pid" ] && kill "$fake_pid" 2>/dev/null
  "$adb" shell cmd locale set-app-locales "$id" --locales "" </dev/null >/dev/null 2>&1 || true
  [ -n "${ime_found:-}" ] && { put enabled_input_methods "$ime_found"; put selected_input_method_subtype "$sub_found"; "$adb" shell ime set "$ime" </dev/null >/dev/null; "$adb" shell sync </dev/null; } 2>/dev/null || true
  lhc server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  # A Companion that did not end fails the run (companion_stop said which).
  [ -z "${stop_failed:-}" ] || exit 1
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM HUP
"$work/fake-responses" "127.0.0.1:$fport" > "$work/fake.log" 2>&1 & fake_pid=$!
lhc run --bg --keep --cwd "$work/home/src/acme-web" --name pi --label herdr.space=sppi1 --label herdr.tab=tbpi1 "$pi" --model fake/gpt-5.5 >/dev/null
sleep 7
# Nothing is typed into Pi unless its screen shows the fake model and no question.
screen=$(lhc read pi 2>/dev/null)
printf %s "$screen" | grep -qiE "update|log ?in|press enter|trust" && { echo "STOP: a prompt on Pi's screen"; exit 1; }
printf %s "$screen" | grep -q "gpt-5.5" || { echo "STOP: Pi is not on the fake model"; exit 1; }
companion_start "$work/serve.log" clean "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" || exit 1
invite=$(clean "$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
centre() { bounds "$1" | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'; }
has() { xml | grep -qF "$1"; }
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/$1.png" || true; }
wait_for() { for _ in $(seq 1 30); do has "$1" && return 0; sleep 1; done; echo "FAIL: $2"; snap fail; [ -n "$shot" ] && xml > "$shot/fail.xml"
  xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1; }
# A tap on the node whose resource-id is $1.
# A system dialog's button by its text (the row under it may say the same).
dialog_button() { xml | tr '>' '\n' | grep -F 'resource-id="android:id/button' | grep -F "text=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'; }
tap() {
  m="resource-id=\"$1\""
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$m"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; snap fail; xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -30; exit 1; }
  "$adb" shell input tap $xy
}
type_text() { "$adb" shell input text "'$(printf %s "$1" | sed 's/ /%s/g')'" < /dev/null; }
# Gboard goes back to a language it used before once the app is in zh-CN: on pinyin, typed Latin letters turn into
# Chinese ("HELLO there" came out "HE LLOYDthere" with the English layout chosen just before typing). For the run,
# Gboard has only its English layout (an enabled subtype whose locale is en*) turned on and selected; the run ends
# with the list and the selection as it found them, synced to disk before the emulator goes down.
ime=com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME
# A secure setting, the value quoted whole for the device's shell (a ';' there ends the command).
put() { "$adb" shell "settings put secure $1 '$2'" </dev/null; }
english() {
  for h in $("$adb" shell settings get secure enabled_input_methods | tr -d '\r' | tr ':' '\n' | grep -F "$ime;" | cut -d';' -f2- | tr ';' ' '); do
    "$adb" shell dumpsys input_method | tr -d '\r' | grep -E "mSubtypeLocale=en[_A-Za-z]* .*mSubtypeHashCode=$h\$" | head -1 | grep -q . && { echo "$h"; return; }
  done
}
en_sub=; ime_found=; sub_found=
latin() {
  [ -n "$en_sub" ] || en_sub=$(english)
  [ -n "$en_sub" ] || { echo "FAIL: Gboard has no English layout turned on"; exit 1; }
  if [ -z "$ime_found" ]; then
    sub_found=$("$adb" shell settings get secure selected_input_method_subtype </dev/null | tr -d '\r')
    ime_found=$("$adb" shell settings get secure enabled_input_methods </dev/null | tr -d '\r')
  fi
  only=$(printf %s "$ime_found" | tr ':' '\n' | awk -v ime="$ime" -v en="$en_sub" 'index($0, ime ";") == 1 || $0 == ime { $0 = ime ";" en } 1' | paste -sd: -)
  put enabled_input_methods "$only"; put selected_input_method_subtype "$en_sub"; "$adb" shell ime set "$ime" </dev/null >/dev/null
}
# latin with a box in focus, then type only once Gboard is up again on the English layout. latin restarts Gboard
# under the focused box (it must come after the tap: on focus Gboard goes back to pinyin), and letters typed while it
# restarts are lost: typed at once, the first typing in Pi's chat lost its first one or two letters in 5 of 6 tries
# ("ELLO there"); typed after this wait, none in 6.
latin_focused() {
  latin
  for _ in $(seq 1 40); do
    "$adb" shell dumpsys input_method </dev/null | grep -q 'mInputShown=true' &&
      [ "$("$adb" shell settings get secure selected_input_method_subtype </dev/null | tr -d '\r')" = "$en_sub" ] && break
    sleep 0.25
  done
  sleep 1
}
file="$agent/extensions/herdr-anywhere.ts"

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
latin
tap pair-link
type_text "$invite"
for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done
tap pair
wait_for 'resource-id="space-' "the phone did not pair"
# Paired in the device's language, then Simplified Chinese.
"$adb" shell cmd locale set-app-locales "$id" --locales zh-CN </dev/null >/dev/null; sleep 1
"$adb" shell am force-stop "$id"; "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
wait_for 'resource-id="space-' "the app did not come back in Chinese"

tap space-sppi1
wait_for 'resource-id="show-chat"' "Pi's pane has no chat button"
tap show-chat
wait_for 'resource-id="chat-setup-ask"' "the first Pi chat did not ask"
has 'text="~/.pi/agent/extensions/herdr-anywhere.ts"' || { echo "FAIL: the ask does not show the file as ~/.pi/…"; snap fail; xml | tr '>' '\n' | grep -oE 'text="[^"]*extensions[^"]*"'; exit 1; }
sleep 1; snap setup-ask
echo "ok: the first Pi chat asks once, naming the file as ~/.pi/…"
tap chat-setup-later
wait_for 'resource-id="chat-setup-install"' "after Not now the page has no Install"
[ -e "$file" ] && { echo "FAIL: Not now put the file there"; exit 1; }
sleep 1; snap setup-needed
echo "ok: after Not now the chat page says what is missing, with Install"
# Back to the terminal and to the chat again: no question by itself any more.
tap chat-terminal
wait_for 'resource-id="show-chat"' "the terminal did not come back"
tap show-chat
wait_for 'resource-id="chat-setup-install"' "the page did not come back"
sleep 2; has 'resource-id="chat-setup-ask"' && { echo "FAIL: it asked again"; exit 1; }
echo "ok: it does not ask by itself again"
tap chat-setup-install
wait_for 'resource-id="chat-setup-reload"' "after Install the running Pi was not told to /reload"
[ -f "$file" ] || { echo "FAIL: Install did not put the file there"; exit 1; }
sleep 1; snap setup-reload
echo "ok: Install puts the file there; the running Pi is told to type /reload"
tap chat-setup-reload
wait_for 'resource-id="chat-view"' "the chat view went away"
for _ in $(seq 1 20); do has 'resource-id="chat-setup-reload"' || break; sleep 1; done
has 'resource-id="chat-setup-reload"' && { echo "FAIL: /reload typed, the chat still waits for it"; exit 1; }
echo "ok: the phone typed /reload and Pi's conversation is found"
tap compose-input
latin_focused
type_text "HELLO there"
has 'text="HELLO there"' || { echo "FAIL: the keyboard changed what was typed"; snap fail; exit 1; }
tap compose-send
for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done
wait_for 'Hello from the fake model.' "the chat did not show Pi's reply"
sleep 1; snap setup-chat
echo "ok: the chat shows Pi's conversation"

tap back
tap settings
wait_for 'resource-id="setup-pi"' "Settings has no Pi chat row"
sleep 1; snap settings-on
tap setup-pi-remove
xy=""; for _ in $(seq 1 20); do xy=$(dialog_button 移除); [ -n "$xy" ] && break; sleep 1; done
[ -n "$xy" ] || { echo "FAIL: Remove did not ask first"; snap fail; xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -30; exit 1; }
[ -e "$file" ] || { echo "FAIL: the file went before the answer"; exit 1; }
sleep 1; snap settings-remove-ask
"$adb" shell input tap $xy
wait_for 'resource-id="setup-pi-install"' "after Remove the row has no Install"
[ -e "$file" ] && { echo "FAIL: Remove left the file"; exit 1; }
[ -e "$agent/extensions" ] && { echo "FAIL: Remove left the extensions folder"; exit 1; }
sleep 1; snap settings-off
echo "ok: Remove takes the file away and leaves nothing; the row offers Install"
echo "exit 0"
