#!/bin/sh
# A tap on a button in a full-screen program that follows the mouse reaches it as a click. OpenCode's dialogs (its
# update notice among them) run their buttons only on a mouse release; up to 0.1.12 a tap reached nothing and the
# dialog stayed. A small program stands in for OpenCode: the alternate screen, mouse modes 1000, 1002, 1003 and 1006 as
# OpenCode turns on, and a dialog with Skip and Update buttons three rows tall. It writes down every mouse report it gets.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-mouse.sh APK COMPANION [screenshot.png]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; shot=${3:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7791; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
export HC_SESSION="hmmouse-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"

cat > "$work/dialog.py" <<'PY'
import os, re, select, signal, sys, tty
out = sys.argv[1]
layout = {}
def draw(*_):
    cols, rows = os.get_terminal_size()
    mid = rows // 2
    skip = (cols // 2 - 12, cols // 2 - 3)
    update = (cols // 2 + 2, cols // 2 + 11)
    layout.update(cols=cols, rows=rows, top=mid - 1, bottom=mid + 1, skip=skip, update=update)
    s = '\x1b[2J\x1b[%d;%dHUpdate available' % (mid - 4, skip[0])
    for r in range(mid - 1, mid + 2):
        s += '\x1b[%d;%dH\x1b[7m%s\x1b[0m' % (r, skip[0], ('Skip' if r == mid else '').center(skip[1] - skip[0] + 1))
        s += '\x1b[%d;%dH\x1b[7m%s\x1b[0m' % (r, update[0], ('Update' if r == mid else '').center(update[1] - update[0] + 1))
    os.write(1, s.encode())
    with open(out + '.layout', 'w') as f:
        f.write('%d %d %d %d %d %d\n' % (cols, rows, mid - 1, mid + 1, skip[0], skip[1]))
os.write(1, b'\x1b[?1049h\x1b[?25l\x1b[?2004h\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h')
signal.signal(signal.SIGWINCH, draw)
tty.setraw(0)
draw()
def note(line):
    with open(out, 'a') as f: f.write(line + '\n')
buf = b''
while True:
    try: ready = select.select([0], [], [], 0.1)[0]
    except InterruptedError: continue
    if ready:
        buf += os.read(0, 1024)
        continue
    if not buf: continue
    # What came in a burst: mouse reports as press/release lines, everything else as one line of keys.
    for m in re.finditer(rb'\x1b\[<(\d+);(\d+);(\d+)([Mm])', buf):
        b, x, y = int(m[1]), int(m[2]), int(m[3])
        on = layout['top'] <= y <= layout['bottom']
        hit = 'skip' if on and layout['skip'][0] <= x <= layout['skip'][1] else 'update' if on and layout['update'][0] <= x <= layout['update'][1] else 'none'
        note('%s %d %d %d %s' % ('press' if m[4] == b'M' else 'release', b, x, y, hit))
    rest = re.sub(rb'\x1b\[<\d+;\d+;\d+[Mm]', b'', buf)
    if rest: note('keys ' + rest.hex())
    buf = b''
PY
events="$work/mouse"; : > "$events"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spmouse --label herdr.tab=tbmouse \
  python3 -I "$work/dialog.py" "$events" >/dev/null
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

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
tap() {
  b=""; for _ in $(seq 1 30); do b=$(xml | bounds "$1\""); [ -n "$b" ] && break; sleep 1; done
  [ -n "$b" ] || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do "$adb" shell input text "'$(printf %s "$rest" | cut -c1-80)'" < /dev/null; rest=$(printf %s "$rest" | cut -c81-); sleep 0.5; done
tap pair
tap space-spmouse

# The dialog at the phone's size: wait until the program has drawn it for the phone's columns and the page shows it.
box=""; for _ in $(seq 1 30); do box=$(xml | bounds terminal-); [ -n "$box" ] && break; sleep 1; done
[ -n "$box" ] || { echo "FAIL: no terminal on screen"; exit 1; }
set -- $box; left=$1; top=$2; right=$3; bottom=$4
sleep 3
read cols rows first last from to < "$events.layout"
[ "$cols" -lt 80 ] || { echo "FAIL: the terminal is $cols columns, not the phone's"; exit 1; }
# Where a cell is on the phone: the page has no side padding, 12 px above the first row and below the last; the rows
# fill the rest (to within a row, which the three-row button allows for).
dpi=$("$adb" shell wm density | tr -dc '0-9\n' | tail -1)
pad=$((12 * dpi / 160))
cw=$(( (right - left) * 1000 / cols )); ch=$(( (bottom - top - 2 * pad) * 1000 / rows ))
col=$(( (from + to) / 2 )); row=$(( (first + last) / 2 ))
x=$(( left + (col * 1000 - 500) * cw / 1000000 )); y=$(( top + pad + (row * 1000 - 500) * ch / 1000000 ))
: > "$events"
"$adb" shell input tap "$x" "$y"
sleep 2
[ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot" || true
failed=0
if ! grep -q '^release 0 .* skip$' "$events" || ! grep -q '^press 0 .* skip$' "$events"; then
  echo "FAIL: a tap on Skip (column $col, row $row of $cols x $rows, at $x,$y) reached the program as: $(tr '\n' ';' < "$events")"
  failed=1
else
  read _ _ gx gy _ < "$events"
  if [ $((gx - col)) -le 1 ] && [ $((col - gx)) -le 1 ]; then echo "ok: a tap on Skip reached the program as a click at column $gx, row $gy (tapped column $col, row $row)"
  else echo "FAIL: the click landed in column $gx, the tap was on $col"; failed=1; fi
  if grep -q 'update$' "$events"; then echo "FAIL: the tap also clicked Update: $(tr '\n' ';' < "$events")"; failed=1; else echo "ok: and nothing else"; fi
fi
# The keyboard's window on screen (as android.sh checks it): a tap on a button is no reason to bring it up.
if "$adb" shell dumpsys window InputMethod | grep -q "isOnScreen=true"; then echo "FAIL: the tap brought the keyboard up"; failed=1
else echo "ok: the tap leaves the keyboard down"; fi

# The keys with the dialog up: the composer's Esc, and the composer's text (pasted, then Enter).
: > "$events"
tap key-esc
sleep 2
if grep -qx 'keys 1b' "$events"; then echo "ok: the composer's esc reaches the program"
else echo "FAIL: the composer's esc reached the program as: $(tr '\n' ';' < "$events")"; failed=1; fi
: > "$events"
tap compose-input
"$adb" shell input text hello
tap compose-send
for _ in $(seq 1 10); do grep -q '^keys' "$events" && break; sleep 1; done
sleep 1
got=$(grep '^keys' "$events" | awk '{printf "%s", $2}')
if [ "$got" = "$(printf '\033[200~hello\033[201~\r' | xxd -p | tr -d '\n')" ]; then echo "ok: the composer's text reaches the program, pasted, then Enter (which runs the dialog's selected button)"
else echo "FAIL: the composer's text reached the program as $got"; failed=1; fi
exit $failed
