#!/bin/sh
# The chat view on Android for Codex and OpenCode (1 or 2), end to end: the installed Codex and an OpenCode binary, each in
# a throwaway HOME against the fake Responses API, in an isolated herdr-core, and this branch's Companion.
# OpenCode, before it has worked, says it can't tell the conversation yet; a prompt from the phone makes it work, and
# the chat then shows the turn by itself. Codex's turn shows once it has worked. For each, a tap opens the turn's
# steps and the command's output. App in the device's language. Fake data only.
# OpenCode 2 says which conversation it shows while it works; OpenCode 1 says nothing, and its terminal title tells.
# One OpenCode per run: both keep their sessions in the same opencode.db. Run it once with each.
# Usage: ANDROID_SERIAL=emulator-5554 OPENCODE=/path/to/opencode [E2E_CACHE=dir] \
#          e2e/android-chat-agents.sh APK COMPANION [folder for screenshots]
# E2E_CACHE: where OpenCode may keep its cache (default: the run's temp folder); emptied on the way out.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
: "${OPENCODE:?set OPENCODE to an OpenCode 1 or 2 binary}"
apk=$1; bin=$2; shot=${3:-}
[ -z "$shot" ] || mkdir -p "$shot"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
codex=$(command -v codex || true)
[ -n "$codex" ] || { echo "SKIP: no codex on this computer"; exit 0; }
# Even --version writes OpenCode's log under its data folder: it runs in a throwaway home, never this user's.
vhome=$(mktemp -d)
said=$(env -i PATH=/usr/bin:/bin HOME="$vhome" XDG_DATA_HOME="$vhome/data" XDG_STATE_HOME="$vhome/state" XDG_CONFIG_HOME="$vhome/config" \
  XDG_CACHE_HOME="$vhome/cache" "$OPENCODE" --version 2>/dev/null || true)
rm -rf "$vhome"
case $said in [12].*|*" v"[12].*) ;; *) echo "STOP: $OPENCODE is not OpenCode 1 or 2 (it says: $said)"; exit 1;; esac
echo "OpenCode $said"
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
fport=7810; while nc -z 127.0.0.1 "$fport" 2>/dev/null; do fport=$((fport + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
cache=${E2E_CACHE:-$work/cache}; mkdir -p "$cache"; cache=$(mktemp -d "$cache/run.XXXX")
mkdir -p "$work/home/src/acme-api" "$work/home/src/acme-web" "$work/codex" "$work/state" "$work/config/opencode" "$work/data"
for d in acme-api acme-web; do printf 'a\n' > "$work/home/src/$d/notes.txt"; printf 'b\n' > "$work/home/src/$d/todo.txt"; done
: > "$work/home/.zshrc"
# Codex: no update check (its prompt picks Update now on Enter), the fake model, the folder trusted, no questions.
cat > "$work/codex/config.toml" <<CFG
check_for_update_on_startup = false
model = "gpt-5.5"
model_provider = "fake"
sandbox_mode = "workspace-write"
approval_policy = "never"
[model_providers.fake]
name = "Fake"
base_url = "http://127.0.0.1:$fport/v1"
wire_api = "responses"
env_key = "FAKE_API_KEY"
[projects."$work/home/src/acme-api"]
trust_level = "trusted"
[tui]
screen_reader_detection_done = true
# Seen: Codex 0.167.1 moves gpt-5.5 to gpt-6-sol unless this notice was answered; the requests go to the fake
# provider either way, and the screen check below looks for the fake model by name.
[notice.model_migrations]
"gpt-5.5" = "gpt-6-sol"
CFG
# OpenCode: no update, no sharing, only the fake provider.
cat > "$work/config/opencode/opencode.json" <<CFG
{"autoupdate": false, "share": "disabled", "enabled_providers": ["fake"], "disabled_providers": ["opencode"],
 "provider": {"fake": {"npm": "@ai-sdk/openai", "name": "Fake", "options": {"baseURL": "http://127.0.0.1:$fport/v1", "apiKey": "fake"},
   "models": {"gpt-5.5": {"name": "GPT-5.5", "tool_call": true}}}},
 "model": "fake/gpt-5.5"}
CFG
(cd "$app/../companion" && go build -o "$work/fake-responses" ./internal/chat/fakeresponses/serve)
. "$app/e2e/companion.sh"  # companion_start, companion_stop
clean() {
  env -i PATH="$(dirname "$codex"):/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin" HOME="$work/home" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmca-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
    XDG_CACHE_HOME="$cache" CODEX_HOME="$work/codex" FAKE_API_KEY=fake \
    OPENCODE_DISABLE_AUTOUPDATE=1 OPENCODE_DISABLE_MODELS_FETCH=1 OPENCODE_DISABLE_SHARE=1 OPENCODE_DISABLE_LSP_DOWNLOAD=1 OPENCODE_DISABLE_CLAUDE_CODE=1 \
    "$@"
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
  [ -n "${ime_found:-}" ] && { put enabled_input_methods "$ime_found"; put selected_input_method_subtype "$sub_found"; "$adb" shell ime set "$ime" </dev/null >/dev/null; "$adb" shell sync </dev/null; } 2>/dev/null || true
  lhc server stop >/dev/null 2>&1 || true
  # OpenCode 2 starts a background `serve --service` of its own; it ends by itself soon after, or here. One that was
  # starting while its terminal ended shows up a moment later, so look three times.
  for _ in 1 2 3; do sleep 1; pkill -f -- "$OPENCODE serve" 2>/dev/null; done
  # Codex's TUI leaves an app-server under CODEX_HOME running after its terminal ends.
  pkill -f -- "/$(basename "$work")/codex/" 2>/dev/null || true
  chmod -R u+w "$work" "$cache" 2>/dev/null; rm -rf "$work" "$run" "$state" "$cache"
  # A Companion that did not end fails the run (companion_stop said which).
  [ -z "${stop_failed:-}" ] || exit 1
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM HUP
"$work/fake-responses" "127.0.0.1:$fport" > "$work/fake.log" 2>&1 & fake_pid=$!
# hc runs the command line as a shell would: a path with spaces goes through a link named as the program.
mkdir -p "$work/bin"; ln -s "$OPENCODE" "$work/bin/opencode"
lhc run --bg --keep --cwd "$work/home/src/acme-api" --name cx --label herdr.space=spcx1 --label herdr.tab=tbcx1 "$codex" >/dev/null
lhc run --bg --keep --cwd "$work/home/src/acme-web" --name oc --label herdr.space=spoc1 --label herdr.tab=tboc1 "$work/bin/opencode" >/dev/null
sleep 10
# Nothing is typed into either unless its screen shows the fake model and no question.
screen=$(lhc read cx 2>/dev/null)
if printf %s "$screen" | grep -q "Use existing model"; then lhc keys cx esc >/dev/null; sleep 3; screen=$(lhc read cx 2>/dev/null); fi
printf %s "$screen" | grep -qiE "update now|update available|trust|sign in|log ?in|press enter|use existing model" && { echo "STOP: a prompt on Codex's screen"; printf '%s\n' "$screen" | tail -8; exit 1; }
printf %s "$screen" | grep -qi "gpt-5.5" || { echo "STOP: Codex is not on the fake model"; printf '%s\n' "$screen" | tail -8; exit 1; }
screen=$(lhc read oc 2>/dev/null)
printf %s "$screen" | grep -qiE "update|upgrade|sign in|log ?in|press enter|select|choose" && { echo "STOP: a prompt on OpenCode's screen"; printf '%s\n' "$screen" | tail -8; exit 1; }
printf %s "$screen" | grep -q "GPT-5.5" || { echo "STOP: OpenCode is not on the fake model"; printf '%s\n' "$screen" | tail -8; exit 1; }
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
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/$1.png" || true; }
wait_for() { for _ in $(seq 1 40); do has "$1" && return 0; sleep 1; done; echo "FAIL: $2"; snap fail; [ -n "$shot" ] && xml > "$shot/fail.xml"
  xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40; exit 1; }
tap() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; snap fail; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  "$adb" shell input tap $xy
}
# A tap on the node whose text is exactly $1 (a step's title: its row takes the tap).
tap_text() {
  xy=$(xml | tr '>' '\n' | grep -F "text=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}')
  [ -n "$xy" ] || { echo "FAIL: no \"$1\" on screen"; snap fail; exit 1; }
  "$adb" shell input tap $xy
}
type_text() { "$adb" shell input text "'$(printf %s "$1" | sed 's/ /%s/g')'" < /dev/null; }
hide_keys() { for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done; return 0; }
# Gboard can go back to a language it used before (pinyin turns typed Latin letters into Chinese). For the run it has
# only its English layout (an enabled subtype whose locale is en*) turned on and selected; the list and the selection
# go back as found on the way out, synced to disk before the emulator goes down.
ime=com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME
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
# The prompt typed in the composer, checked as typed before it is sent.
say() {
  tap compose-input; type_text "$1"
  has "text=\"$1\"" || { echo "FAIL: the keyboard changed what was typed"; snap fail; exit 1; }
  tap compose-send; hide_keys
}
# The LIST turn's steps open with a tap, and its command's output with another.
steps() { # $1: name for the screenshot
  tap fold-
  wait_for 'text="ls"' "$1: the turn's steps did not open"
  tap_text ls
  wait_for 'resource-id="run-' "$1: the command's details did not show"
  has 'todo.txt' || { echo "FAIL: $1: the command's output is not shown"; snap fail; exit 1; }
  snap "$1-step"
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
latin
tap pair-link
type_text "$invite"
hide_keys
tap pair
wait_for 'resource-id="space-' "the phone did not pair"

# OpenCode, before it has worked: it can't be told yet which conversation it shows.
tap space-spoc1
wait_for 'resource-id="show-chat"' "OpenCode's pane has no chat button"
has 'Tell OpenCode' || { echo "FAIL: the composer does not tell OpenCode by name"; snap fail; exit 1; }
tap show-chat
wait_for 'which conversation this is yet' "OpenCode before its first turn: not the can't-tell-yet text"
has 'OpenCode does some work' || { echo "FAIL: the text does not name OpenCode"; snap fail; exit 1; }
sleep 1; snap opencode-waiting
echo "ok: OpenCode before it has worked says it can't tell the conversation yet"
say "LIST the files here"
wait_for 'There are two files here.' "OpenCode's turn did not show by itself after it worked"
sleep 1; snap opencode-chat
echo "ok: once OpenCode worked, its turn shows without leaving the page"
steps opencode
echo "ok: OpenCode's steps and the command's output open with a tap"
tap show-terminal
wait_for 'resource-id="terminal-' "OpenCode's terminal did not come back"
tap back

# Codex: a prompt from the phone, then its turn.
tap space-spcx1
wait_for 'resource-id="terminal-' "Codex's terminal did not show"
say "LIST the files here"
wait_for 'resource-id="show-chat"' "Codex's pane has no chat button"
tap show-chat
wait_for 'There are two files here.' "Codex's turn did not show"
sleep 1; snap codex-chat
echo "ok: Codex's turn shows"
steps codex
echo "ok: Codex's steps and the command's output open with a tap"
tap show-terminal
wait_for 'resource-id="terminal-' "Codex's terminal did not come back"
echo "exit 0"
