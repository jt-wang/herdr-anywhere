#!/bin/sh
# The switcher's new-tab buttons with real taps on Android: a new terminal is a WebView, which a test without one can't
# show. Each tap has to do what it says, again and again, with the new terminals on screen behind the sheet.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-switcher.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7761; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data" "$work/bin"
: > "$work/home/.zshrc"
# Ubuntu's /etc/zsh/zshrc runs compinit, which can stop at a y/n question and eat what the phone types first.
echo skip_global_compinit=1 > "$work/home/.zshenv"
# An agent to start that needs no account: the app offers what is on the PATH.
printf '#!/bin/sh\nexec /bin/zsh\n' > "$work/bin/claude"; chmod +x "$work/bin/claude"
# A program that takes over the screen and the mouse, as Pi and vim do, and writes down what it receives.
cat > "$work/bin/codex" <<EOF
#!/usr/bin/env python3
import os, sys, tty
tty.setraw(0)
os.write(1, b"\\033[?1049h\\033[?1000h\\033[?1006h\\033[Hfull screen program")
with open("$work/received", "ab", 0) as f:
    while True:
        f.write(os.read(0, 4096))
EOF
chmod +x "$work/bin/codex"
# HM_BUSY=1: an agent that redraws its whole screen 30 times a second, as a busy TUI does, behind the sheet.
if [ -n "${HM_BUSY:-}" ]; then
  printf '#!/bin/sh\nwhile :; do printf "\\033[H"; seq 1 60; sleep 0.03; done\n' > "$work/bin/claude"
fi
export PATH="$work/bin:$PATH" HC_SESSION="hmsw-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spsw --label herdr.tab=tbsw >/dev/null
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
cleanup() { kill "$serve_pid" 2>/dev/null; "$hc" server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"; }
trap cleanup EXIT INT TERM
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

# A dump waits for the screen to settle and can give up, which leaves the last file behind: start without one.
xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
centre() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
tap() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  "$adb" shell input tap $xy
}
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/switcher-$1.png" || true; }
terminals() { "$hc" ls --json | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["terminals"]))'; }
gone() { # the sheet closed
  for _ in $(seq 1 10); do
    dump=$(xml)
    case "$dump" in *'<hierarchy'*) printf %s "$dump" | grep -qF 'resource-id="switch-new-shell"' || return 0 ;; esac
    sleep 1
  done
  return 1
}
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do piece=$(printf %s "$rest" | cut -c1-80); rest=$(printf %s "$rest" | cut -c81-); "$adb" shell input text "'$piece'" < /dev/null; sleep 0.5; done
tap pair
tap space-spsw
sleep 3
failed=0
# A swipe over a program that takes over the screen reaches that program as the wheel.
tap switcher
sleep 1.5
tap switch-new-codex
gone || true
sleep 4
: > "$work/received"
"$adb" shell input swipe 500 700 500 1200 300
sleep 2
if grep -q '<64;' "$work/received"; then echo "swipe in a full-screen program: wheel received"; else echo "swipe in a full-screen program: nothing received"; failed=1; fi
for key in claude shell claude shell claude; do
  before=$(terminals)
  tap switcher
  sleep 1.5
  [ "$key" = claude ] && snap "sheet-$before"
  t0=$(python3 -c 'import time; print(time.time())')
  tap "switch-new-$key"
  closed=yes; gone || closed=no
  echo "  sheet gone or given up after $(python3 -c "import time; print(round(time.time()-$t0,1))") s"
  sleep 3
  after=$(terminals)
  echo "tap $key: terminals $before -> $after, sheet closed: $closed"
  snap "after-$after"
  [ "$after" -gt "$before" ] && [ "$closed" = yes ] || failed=1
  [ "$closed" = yes ] || { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }
done
# With terminals behind it, the rows above the new-tab buttons still answer.
tap switcher
sleep 1.5
first=$(xml | tr '>' '\n' | grep -oE 'resource-id="switch-tab-[^"]+"' | head -1 | sed -E 's/.*"(.*)"/\1/')
tap "$first"
closed=yes; gone || closed=no
echo "tap a tab row: sheet closed: $closed"
[ "$closed" = yes ] || failed=1
[ "$failed" = 0 ] && echo PASS || { echo FAIL; exit 1; }
