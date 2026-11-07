#!/bin/sh
# Typing straight into the terminal with the phone's own keyboard, on the emulator with real taps on Gboard's keys.
# The terminal runs a program that logs every read as bytes (e2e/typing-raw.py); each check compares what it got.
# Gboard before the page read its edits itself (2026-11-07): Enter sent "\r" and a stray backspace in 4 of 5 taps, a
# picked correction ("teh" -> the) sent the field's old text again, and the first backspace after a pinyin word was
# dropped in 2 of 3 tries.
# Needs an emulator with a 1080x2400 screen at 420 dpi (Gboard's keys are tapped where they are on it) and Gboard
# with two languages, English (US) first and Chinese pinyin (拼音) second; the globe key switches between them.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-typing.sh APK [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; shot=${2:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
[ "$("$adb" shell wm size | tail -1 | awk '{print $NF}')" = 1080x2400 ] && [ "$("$adb" shell wm density | tail -1 | awk '{print $NF}')" = 420 ] \
  || { echo "FAIL: the screen is not 1080x2400 at 420 dpi, where this script knows Gboard's keys"; exit 1; }
port=7801; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtyXXXX)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
export HC_SESSION="hmty-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
raw="$work/home/src/acme-api/hm-raw"
cp "$app/e2e/typing-raw.py" "$work/home/src/acme-api/raw.py"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spty --label herdr.tab=tbty python3 raw.py >/dev/null
"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
cleanup() { kill "$serve_pid" 2>/dev/null; "$hc" server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"; }
trap cleanup EXIT INT TERM
sleep 1
invite=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
centre() { tr '>' '\n' | grep -F "$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
tap() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "resource-id=\"$1\""); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
keyboard_up() { "$adb" shell dumpsys window InputMethod | grep -q "isOnScreen=true"; }
# Gboard's keys on the 1080x2400 screen, read off a screenshot (2026-11-07).
at() { case $1 in q) echo 58 1715;; w) echo 165 1715;; e) echo 272 1715;; r) echo 379 1715;; t) echo 486 1715;; y) echo 593 1715;; u) echo 700 1715;; i) echo 807 1715;; o) echo 914 1715;; p) echo 1021 1715;;
  a) echo 111 1870;; s) echo 218 1870;; d) echo 325 1870;; f) echo 432 1870;; g) echo 539 1870;; h) echo 646 1870;; j) echo 753 1870;; k) echo 860 1870;; l) echo 967 1870;;
  z) echo 218 2025;; x) echo 325 2025;; c) echo 432 2025;; v) echo 539 2025;; b) echo 646 2025;; n) echo 753 2025;; m) echo 860 2025;;
  _) echo 593 2180;; '<') echo 994 2025;; '>') echo 994 2180;; '@') echo 325 2180;; '1') echo 538 1575;; '0') echo 80 1574;; esac; }
# Taps keys: letters, _ space, < backspace, > Enter, @ the globe, 1 the middle suggestion, 0 the first candidate.
keys() { s=$1; while [ -n "$s" ]; do c=$(printf %s "$s" | cut -c1); s=$(printf %s "$s" | cut -c2-); "$adb" shell input tap $(at "$c"); sleep 0.35; done; }
# What the program got since the last look, as a Python bytes literal; and the line it leaves, backspaces applied.
got() { sleep 1.5; python3 -c 'import ast,sys; print(repr(b"".join(ast.literal_eval(l) for l in open(sys.argv[1]) if l.strip())))' "$raw"; : > "$raw"; }
line() { python3 -c 'import ast,sys
out=[]
for c in ast.literal_eval(sys.argv[1]).decode():
    out.pop() if c == "\x7f" and out else (None if c == "\x7f" else out.append(c))
print(repr("".join(out)))' "$1"; }
fails=0
check() { if [ "$2" = "$3" ]; then echo "ok: $1"; else echo "FAIL: $1: got $2, wanted $3"; fails=$((fails + 1)); fi; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-80)'" < /dev/null; rest=$(printf %s "$rest" | cut -c81-); sleep 0.5; done
tap pair
tap space-spty
for _ in $(seq 1 30); do [ -e "$raw" ] && break; sleep 1; done
sleep 2
# The keyboard in the terminal: the input box row's switch when there is one, else the menu's Type in the terminal.
if xml | grep -q 'resource-id="to-terminal"'; then tap to-terminal; else
  tap space-more; sleep 1
  xy=""; for _ in 1 2 3 4 5; do xy=$(xml | centre 'text="Type in the terminal"'); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no Type in the terminal in the menu"; exit 1; }
  "$adb" shell input tap $xy
fi
up=""; for _ in 1 2 3 4 5; do keyboard_up && { up=1; break; }; sleep 1; done
[ -n "$up" ] || { echo "FAIL: no keyboard in the terminal"; exit 1; }
sleep 1; : > "$raw"

keys hi; b=$(got)
if [ "$b" != "b'hi'" ]; then keys '@'; sleep 1; keys '<<<<'; : > "$raw"; keys hi; b=$(got); fi
check "letters arrive as typed" "$b" "b'hi'"
keys '<<'; got >/dev/null
for i in 1 2 3 4 5; do keys 'ab>'; check "Enter $i: \\r and no stray backspace" "$(got)" "b'ab\\r'"; done
keys hello_teh; sleep 1; [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/typing-correction.png"; keys 1
check "a picked correction leaves 'hello the ' on the line" "$(line "$(got)")" "'hello the '"
keys '>'; got >/dev/null
keys '<'; check "a backspace with nothing typed" "$(got)" "b'\\x7f'"
# Pinyin: n i h a o, space picks the first candidate.
keys '@'; sleep 1.5; : > "$raw"
keys nihao; sleep 1; [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/typing-pinyin.png"
check "nothing goes while pinyin composes" "$(got)" "b''"
keys _; check "space sends the first candidate" "$(got)" "$(python3 -c 'print(repr("你好".encode()))')"
for i in 1 2 3; do keys '<'; check "backspace $i after a pinyin word" "$(got)" "b'\\x7f'"; done
keys shijie_; got >/dev/null
keys '<'; check "a backspace after a second word" "$(got)" "b'\\x7f'"
# The same in the input box (a native field), read by sending it: Gboard spent the first backspace after a picked word
# there too (2026-11-07: 你好 and one backspace sent 你好).
tap to-box; sleep 1; tap compose-input; sleep 1; got >/dev/null
keys nihao_; sleep 1; keys '<'; sleep 1; tap compose-send
check "input box: 你好 and one backspace sends 你" "$(got)" "$(python3 -c 'print(repr("你\r".encode()))')"
tap compose-input; sleep 1
keys shijie_; sleep 1; keys '<'; sleep 1; tap compose-send
check "input box: 世界 and one backspace sends 世" "$(got)" "$(python3 -c 'print(repr("世\r".encode()))')"
keys '@'; sleep 1
[ "$fails" = 0 ] || { echo "FAIL: $fails check(s)"; exit 1; }
echo "PASS: typing straight into the terminal sends what was typed"
