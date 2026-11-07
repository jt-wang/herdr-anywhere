#!/bin/sh
# The phone keeps a terminal's width through a dropped connection while the app is in the background. herdr-core sizes
# a terminal by the viewer used last: when the phone's line drops, the computer's viewer is left, and if the phone
# doesn't take the width back on reconnect, what an agent writes meanwhile stays at the computer's width in the
# phone's history. A 32-column viewer stands in for a narrow split in Herdr.app; the terminal writes down its size.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-width.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7761; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
# Ubuntu's /etc/zsh/zshrc runs compinit, which can stop at a y/n question and eat what the phone types first.
echo skip_global_compinit=1 > "$work/home/.zshenv"
export HC_SESSION="hmwd-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
sizes="$work/sizes"; : > "$sizes"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spwd --label herdr.tab=tbwd \
  sh -c "last=; while :; do z=\$(stty size); [ \"\$z\" != \"\$last\" ] && echo \"\$z\" >> $sizes && last=\$z; sleep 0.2; done" >/dev/null
term=$("$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')
# The computer's viewer: hc attach in a 32-column pty, left alone.
python3 - "$hc" "$term" > /dev/null 2>&1 <<'PY' &
import os, pty, sys, fcntl, termios, struct, select
pid, fd = pty.fork()
if pid == 0:
    os.execvp(sys.argv[1], [sys.argv[1], 'attach', sys.argv[2]])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 32, 0, 0))
os.kill(pid, 28)
while True:
    r, _, _ = select.select([fd], [], [], 1)
    if r:
        try: os.read(fd, 65536)
        except OSError: break
PY
mac_pid=$!
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
cleanup() { "$adb" shell cmd connectivity airplane-mode disable >/dev/null 2>&1 || true; kill "$mac_pid" "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"; }
trap cleanup EXIT INT TERM
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
centre() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
tap() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  "$adb" shell input tap $xy
}
cols() { tail -1 "$sizes" | awk '{print $2}'; }
connections() { grep -c ' connected (' "$work/serve.log" || true; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-80)'" < /dev/null; rest=$(printf %s "$rest" | cut -c81-); sleep 0.5; done
tap pair
tap space-spwd
for _ in $(seq 1 20); do [ "$(cols)" != 32 ] && [ -n "$(cols)" ] && break; sleep 1; done
phone=$(cols)
[ -n "$phone" ] && [ "$phone" != 32 ] || { echo "FAIL: the phone did not size the terminal ($(tr '\n' ' ' < "$sizes"))"; exit 1; }
echo "ok: the phone sized the terminal to $phone columns, with a 32-column viewer on the computer"

# In the background, the line drops and comes back: the app reconnects by itself (the companion sees a new connection).
"$adb" shell input keyevent KEYCODE_HOME
sleep 5
before=$(connections)
"$adb" shell cmd connectivity airplane-mode enable
sleep 20
"$adb" shell cmd connectivity airplane-mode disable
for _ in $(seq 1 60); do [ "$(connections)" -gt "$before" ] && break; sleep 1; done
[ "$(connections)" -gt "$before" ] || { echo "FAIL: the app did not reconnect in the background"; tail -5 "$work/serve.log"; exit 1; }
# Still in the background, a while later: the width is the phone's.
sleep 15
[ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/width-background.png" || true
[ "$(cols)" = "$phone" ] || { echo "FAIL: after reconnecting in the background the terminal is $(cols) columns, not the phone's $phone ($(tr '\n' ' ' < "$sizes"))"; exit 1; }
echo "ok: the terminal keeps the phone's $phone columns after the line dropped and came back in the background"
