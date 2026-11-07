#!/bin/sh
# The app on an Android device or emulator against a real computer: an isolated herdr-core with a fake project and the
# companion on this computer's loopback, which an emulator reaches as 10.0.2.2. Installs the APK, pairs it by typing the
# link, and passes when the phone is connected (the companion logs it) and its home screen lists the project.
# Then it opens the space and types a command into its terminal, which passes when the file the command makes appears;
# and the input box row's switch: the keyboard in the terminal with no input box, a command typed there runs, and the
# key row's pen brings the box back.
# Last, away from the app: a blocked terminal posts Android's notification and a tap on it opens the pane; a finished one
# pops up too, on its own channel. Then, back in the app, a new space whose folder is picked by browsing the computer, and one
# in a typed folder that doesn't exist, which the app makes after the tap on Create it.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android.sh [apk] [screenshot.png]
# ANDROID_SERIAL is required, so the run never lands on another emulator.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=${1:-$app/android/build/Herdr.apk}
shot=${2:-}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
# The app's package, for every later adb call: a loop variable named id once replaced it, and the sound step then
# looked for the players of a package called to-box (2026-11-07).
readonly id

port=7691
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/home/src/acme-api" "$work/home/src/beta-web" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
# Ubuntu's /etc/zsh/zshrc runs compinit, which can stop at a y/n question and eat what the phone types first.
echo skip_global_compinit=1 > "$work/home/.zshenv"
export HC_SESSION="hmandroid-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export HERDR_ANYWHERE_STATE="$work/companion" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spandroid --label herdr.tab=tbandroid >/dev/null
# On a failure, what is on the phone's screen (ids and texts) and what each terminal of this run's core shows.
show_terms() {
  "$adb" shell uiautomator dump /sdcard/hm.xml 2>&1 | grep -v "dumped to" || true
  echo "--- on the phone's screen:"
  # CI logs are public: no pairing code, even this run's.
  "$adb" shell cat /sdcard/hm.xml 2>/dev/null | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40 | sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g'
  for t in $("$hc" ls --json 2>/dev/null | python3 -c 'import json,sys; print(" ".join(x["address"] for x in json.load(sys.stdin)["terminals"]))' 2>/dev/null); do
    echo "--- terminal $t, last lines:"; "$hc" read "$t" 2>&1 | tail -15
  done
}
cleanup() {
  status=$?
  [ "$status" -ne 0 ] && show_terms
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
}
trap cleanup EXIT INT TERM
"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
sleep 1
link=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)

# The centre of the view with this resource-id (an NSH testID), from the accessibility tree; an id ending in "-" is a
# prefix (space-<id>, terminal-<address>).
at() {
  case $1 in *-) q="resource-id=\"$1" ;; *) q="resource-id=\"$1\"" ;; esac
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "$q" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
# On a slow emulator Android can cover the app with "<some app> isn't responding" (CI, 2026-11-08: "Pixel Launcher
# isn't responding" hid the pairing screen). Wait is the system's own answer to it; the app under test is not touched.
not_responding() {
  w=$(at android:id/aerr_wait); [ -n "$w" ] || return 1
  echo "note: a system \"isn't responding\" dialog covered the screen; pressed Wait"; "$adb" shell input tap $w; sleep 1
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; not_responding; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
screen_has() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | grep -q "$1"; }
xml() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml; }

# Another build of the app, signed with another key, can't be replaced: then it goes first (its data is cleared below
# either way).
"$adb" install -r "$apk" >/dev/null 2>&1 || { "$adb" uninstall "$id" >/dev/null; "$adb" install "$apk" >/dev/null; }
"$adb" shell pm clear "$id" >/dev/null
# Android 13+ posts an app's notifications only once the person allows them (the app asks from Settings).
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS
activity=$("$adb" shell cmd package resolve-activity --brief "$id" | tail -1 | tr -d '\r')
# The app's own activity, which can answer the app's host calls (NSH's AppActivity cannot be extended).
[ "$activity" = "$id/.MainActivity" ] || { echo "FAIL: the launcher opens $activity"; exit 1; }
"$adb" shell am start -W -n "$activity" >/dev/null
# What the link field holds: pair-link is the field's wrapper, whose own text is empty; the text is its child's
# (seen in a CI run's screen dump).
typed() {
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | sed -n '/resource-id="pair-link"/,$p' | grep -oE 'text="[^"]+"' | head -1 | sed -E 's/^text="(.*)"$/\1/'
}
# adb's input text drops a character now and then on a slow emulator (in one CI run it typed 21 of the code's 22
# characters, and the companion said the code was wrong), so the link is typed until the field holds all of it.
for _ in 1 2 3; do
  tap pair-link
  "$adb" shell input keyevent KEYCODE_MOVE_END $(printf 'KEYCODE_DEL %.0s' $(seq 1 80))
  "$adb" shell input text "'$link'"
  got=$(typed); [ "$got" = "$link" ] && break
  echo "the link field holds ${#got} of the link's ${#link} characters; typing it again"
done
[ "$got" = "$link" ] || { echo "FAIL: the link field never held the whole link"; exit 1; }
tap pair # once, with the keyboard up: the tap has to reach the button

ok=""
for _ in $(seq 1 30); do
  if grep -q "device .* connected (binary," "$work/serve.log" && screen_has acme-api; then ok=1; break; fi
  sleep 1
done
[ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot"
if [ -z "$ok" ]; then
  echo "FAIL: not connected over binary within 30 s"
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep 'resource-id="pair-error"' | sed -E 's/.*text="([^"]*)".*/  phone says: \1/' || true
  grep -vE " GET /assets/" "$work/serve.log" | tail -5 | sed -E 's/token=[^ &]+/token=…/'
  exit 1
fi
# The keyboard's window on screen (input_method's mInputShown stayed true with the keyboard gone; checked both ways).
keyboard_up() { sleep 1; "$adb" shell dumpsys window InputMethod | grep -q "isOnScreen=true"; }
keyboard_up && { echo "FAIL: the keyboard is still up after pairing"; exit 1; }
echo "ok: paired with one tap, connected over binary, home lists acme-api, keyboard closed"

tap space-spandroid
sleep 2
# Reading the terminal asks for no keyboard: neither a tap on it nor a drag across it.
tap terminal-
keyboard_up && { echo "FAIL: a tap on the terminal brought the keyboard up"; exit 1; }
xy=$(at terminal-); set -- $xy
"$adb" shell input swipe "$1" "$(($2 - 200))" "$1" "$(($2 + 200))" 300
keyboard_up && { echo "FAIL: a drag across the terminal brought the keyboard up"; exit 1; }
echo "ok: a tap and a drag on the terminal leave the keyboard down"
# The composer does, and ⋯ types in the terminal itself.
tap compose-input
keyboard_up || { echo "FAIL: a tap on the composer brought no keyboard"; exit 1; }
# Return in the composer starts a new line, the box grows, and send sends both lines as one.
height() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep 'resource-id="compose-input"' | sed -E 's/.*bounds="\[[0-9]+,([0-9]+)\]\[[0-9]+,([0-9]+)\]".*/\1 \2/' | awk '{print $2-$1}'; }
one=$(height)
"$adb" shell input text "'touch hm-line-one'"
"$adb" shell input keyevent 66
"$adb" shell input text "'touch hm-line-two'"
sleep 1
two=$(height)
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-composer.png"
[ "$two" -gt "$one" ] || { echo "FAIL: the composer did not grow with a second line ($one -> $two)"; exit 1; }
tap compose-send
for _ in $(seq 1 15); do [ -e "$work/home/src/acme-api/hm-line-two" ] && break; sleep 1; done
[ -e "$work/home/src/acme-api/hm-line-one" ] && [ -e "$work/home/src/acme-api/hm-line-two" ] || { echo "FAIL: two composer lines did not both reach the computer"; exit 1; }
echo "ok: the composer grew from $one to $two px and sent both lines"
"$adb" shell input keyevent 4
sleep 1
# The menu's Type in the terminal brings the keyboard up itself, with it down before (the back key put it away). adb's
# typing reaches a focused field with no keyboard on screen, so the keyboard is checked before anything is typed.
typein() {
  tap space-more
  sleep 1
  xy=""; for _ in 1 2 3 4 5; do "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
    xy=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F 'text="Type in the terminal"' | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}')
    [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no Type in the terminal in the menu"; exit 1; }
  "$adb" shell input tap $xy
  up=""; for _ in 1 2 3 4 5; do keyboard_up && { up=1; break; }; done
  [ -n "$up" ] || { echo "FAIL: Type in the terminal $1 did not bring the keyboard up"; exit 1; }
}
keyboard_up && { echo "FAIL: the back key left the keyboard up"; exit 1; }
typein "after the back key"
"$adb" shell input text "'touch hm-typed'"
"$adb" shell input keyevent 66
for _ in $(seq 1 15); do [ -e "$work/home/src/acme-api/hm-typed" ] && break; sleep 1; done
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-terminal.png"
[ -e "$work/home/src/acme-api/hm-typed" ] || { echo "FAIL: typing in the terminal did not reach the computer"; exit 1; }
echo "ok: typed in the terminal and the command ran on the computer"

# The keys a phone keyboard lacks sit above the keyboard while it is up: the keyboard's top is its window's top plus the
# content inset it gives (dumpsys window InputMethod: frame=[0,top]…, mGivenContentInsets=[0,inset]…).
keyboard_up || { echo "FAIL: the keyboard is not up in the terminal"; exit 1; }
# The bar follows the keyboard as it opens, so give it a few seconds (one run read it mid-way: bottom 2326, top 1517).
above=""
for _ in 1 2 3 4 5; do
  ime=$("$adb" shell dumpsys window InputMethod)
  top=$(echo "$ime" | grep -oE ' frame=\[0,[0-9]+\]' | head -1 | grep -oE '[0-9]+\]$' | tr -d ']')
  inset=$(echo "$ime" | grep -oE 'mGivenContentInsets=\[0,[0-9]+\]' | head -1 | grep -oE '[0-9]+\]$' | tr -d ']')
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  esc=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F 'resource-id="key-esc"' | sed -E 's/.*bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,([0-9]+)\]".*/\1/')
  [ -n "$esc" ] && [ "$esc" -le $((top + inset)) ] && { above=1; break; }
  sleep 1
done
[ -n "$above" ] || { echo "FAIL: the key bar (bottom $esc) is under the keyboard (top $((top + inset)))"; exit 1; }
echo "ok: the key bar sits above the keyboard"
escs=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -cF 'content-desc="esc"' || true)
[ "$escs" = 1 ] || { echo "FAIL: $escs Esc keys on screen with the keyboard up"; exit 1; }
echo "ok: one Esc on screen"
# The key bar's button puts the keyboard away; the menu brings it back (until 0.1.10 it didn't).
tap hide-keyboard
sleep 1
keyboard_up && { echo "FAIL: the key bar's button left the keyboard up"; exit 1; }
typein "after the key bar's hide button"
echo "ok: Type in the terminal brings the keyboard up after the back key and after the key bar's hide button"

# Shift is held from the key row for the next key: Shift then Tab sends back-tab (ESC [ Z), which Claude Code reads as
# its mode switch. cat -v on the computer writes the line that arrives.
"$adb" shell input text "'head -1 | cat -v > hm-keys'"
"$adb" shell input keyevent 66
sleep 1
tap row-shift
tap row-tab
tap row-enter
for _ in $(seq 1 10); do grep -qF '^[[Z' "$work/home/src/acme-api/hm-keys" 2>/dev/null && break; sleep 1; done
grep -qF '^[[Z' "$work/home/src/acme-api/hm-keys" 2>/dev/null || { echo "FAIL: Shift then Tab did not send back-tab ($(cat -v "$work/home/src/acme-api/hm-keys" 2>/dev/null))"; ls "$work/home/src/acme-api"; "$hc" read "$("$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')" | tail -8; exit 1; }
echo "ok: Shift then Tab from the key row sends back-tab"

# The input box row's switch: the box goes, the keyboard comes up in the terminal under the key row (esc and the pen),
# and what is typed there runs on the computer. The key row's pen brings the box back.
"$adb" shell input keyevent 4
sleep 1
tap to-terminal
up=""; for _ in 1 2 3 4 5; do keyboard_up && { up=1; break; }; sleep 1; done
[ -n "$up" ] || { echo "FAIL: the switch to the terminal brought no keyboard"; exit 1; }
screen=$(xml)
echo "$screen" | grep -q 'resource-id="compose-input"' && { echo "FAIL: the input box is still there after the switch"; exit 1; }
for view in key-row row-esc to-box; do echo "$screen" | grep -q "resource-id=\"$view\"" || { echo "FAIL: no $view after the switch"; exit 1; }; done
"$adb" shell input text "'touch hm-switched'"
"$adb" shell input keyevent 66
for _ in $(seq 1 15); do [ -e "$work/home/src/acme-api/hm-switched" ] && break; sleep 1; done
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-switched.png"
[ -e "$work/home/src/acme-api/hm-switched" ] || { echo "FAIL: typed in the terminal after the switch, nothing ran"; exit 1; }
echo "ok: the switch hides the input box and what is typed in the terminal runs"
tap to-box
box=""; for _ in 1 2 3 4 5; do xml | grep -q 'resource-id="compose-input"' && { box=1; break; }; sleep 1; done
[ -n "$box" ] || { echo "FAIL: the key row's pen did not bring the input box back"; exit 1; }
echo "ok: the key row's pen brings the input box back"
# As before the switch: the keyboard in the terminal from the menu (once the switch's toast has gone from the header).
"$adb" shell input keyevent 4
sleep 6
typein "after the switch back"

# An agent's TUI wraps a paragraph at the phone's width itself, one row at a time, so the terminal sees a newline at
# every row's end. This program writes one paragraph that way, longer than the screen, then writes what it is sent. A
# hold and a drag on it copies; pasted into the composer and sent, the text is the paragraph's words on one line.
cat > "$work/home/src/acme-api/hm-wrap.py" <<'PY'
import os, termios, tty
cols = os.get_terminal_size().columns
rows, line = [], '⏺'
for i in range(1, 601):
    w = 'w%03d' % i
    if len(line) + 1 + len(w) > cols: rows.append(line); line = '  ' + w
    else: line += ' ' + w
rows.append(line)
saved = termios.tcgetattr(0); tty.setraw(0)
os.write(1, ('\r\n'.join(rows) + '\r\n').encode())
with open('hm-pasted', 'ab', 0) as f:
    while True:
        d = os.read(0, 4096)
        if b'\x03' in d: break
        f.write(d)
termios.tcsetattr(0, termios.TCSADRAIN, saved)
PY
"$adb" shell input text "'python3 hm-wrap.py'"
"$adb" shell input keyevent 66
term=$("$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')
for _ in $(seq 1 10); do "$hc" read "$term" | grep -q 'w600' && break; sleep 1; done
tap hide-keyboard
sleep 2
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-wrapped.png"
box=$("$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F 'resource-id="terminal-' | head -1 |
  sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/')
set -- $box
# Away from the screen's edges: a drag that starts at the left edge is the system's back gesture.
x0=$(($1 + ($3 - $1) / 4)); x1=$(($3 - ($3 - $1) / 4)); y0=$((($2 + $4) / 2 - 120)); y1=$((($2 + $4) / 2 + 120))
"$adb" shell input motionevent DOWN "$x0" "$y0"
sleep 1
"$adb" shell input motionevent MOVE "$x1" "$y1"
"$adb" shell input motionevent UP "$x1" "$y1"
sleep 1
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-copy.png"
# Android 13+ shows what was copied over the bottom left corner, where the composer is, for a few seconds.
for _ in $(seq 1 20); do "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | grep -q 'clipboard' || break; sleep 1; done
[ -n "$(at compose-input)" ] || { echo "FAIL: no composer after copying; on screen: $("$adb" shell cat /sdcard/hm.xml | grep -oE '(resource-id|text)="[^"]+"' | sort -u | head -30 | tr '\n' ' ')"; exit 1; }
tap compose-input
sleep 1
"$adb" shell input keyevent 279 # paste
sleep 1
[ -n "$(at compose-send)" ] || { echo "FAIL: nothing pasted into the composer; it says: $("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F 'resource-id="compose-input"' | grep -oE ' text="[^"]*"' | head -c 300)"; exit 1; }
tap compose-send
for _ in $(seq 1 10); do [ -s "$work/home/src/acme-api/hm-pasted" ] && break; sleep 1; done
sleep 1
"$hc" keys "$term" ctrl+c >/dev/null
pasted=$(tr -d '\r' < "$work/home/src/acme-api/hm-pasted" 2>/dev/null | sed -e 's/\x1b\[20[01]~//g' -e 's/[[:space:]]*$//')
echo "$pasted" | python3 -c '
import re, sys
t = sys.stdin.read().strip()
words = t.split(" ")
ok = "\n" not in t and len(words) >= 15 and all(re.fullmatch(r"w\d{3}", w) for w in words[:-1]) and \
  all(int(b[1:]) == int(a[1:]) + 1 for a, b in zip(words, words[1:-1]))
sys.exit(0 if ok else 1)' || { echo "FAIL: a paragraph the TUI wrapped did not copy as one line ($(printf %s "$pasted" | cat -v | head -c 300))"; exit 1; }
echo "ok: a paragraph the TUI wrapped copies as one line ($(echo "$pasted" | wc -w | tr -d ' ') words)"
# The composer has the keyboard now; the next steps type in the terminal.
"$adb" shell input keyevent 4
sleep 1
typein "after the composer"

# The terminal reports idle, then blocked (OSC 7501, as agents do): the app plays Herdr's request sound. Android logs
# each player the app starts (dumpsys audio, "playback activity"); the emulator has no audio out, so this is the
# player starting, not a sound heard.
printf '%s\n' "touch hm-alert-ran; printf '\033]7501;state=idle\033\\\\'; sleep 3; printf '\033]7501;state=blocked\033\\\\'" > "$work/home/src/acme-api/hm-alert.sh"
uid=$("$adb" shell pm list packages -U "$id" | grep -oE 'uid:[0-9]+' | head -1 | cut -d: -f2)
[ -n "$uid" ] || { echo "FAIL: no uid for $id, which the audio log names the app's players by"; exit 1; }
# Android keeps earlier runs' players in that log; count only the ones that appear after this point.
before=$("$adb" shell dumpsys audio | grep -oE "new player piid:[0-9]+ uid/pid:$uid/" | grep -oE 'piid:[0-9]+' | tr '\n' ' ')
"$adb" shell input text "'sh hm-alert.sh'"
"$adb" shell input keyevent 66
played=""
for _ in $(seq 1 20); do
  log=$("$adb" shell dumpsys audio)
  for piid in $(echo "$log" | grep -oE "new player piid:[0-9]+ uid/pid:$uid/" | grep -oE 'piid:[0-9]+'); do
    case " $before " in *" $piid "*) continue ;; esac
    echo "$log" | grep -q "player $piid event:started" && played=1
  done
  [ -n "$played" ] && break
  sleep 1
done
if [ -z "$played" ]; then
  [ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-alert.png"
  [ -e "$work/home/src/acme-api/hm-alert-ran" ] || { echo "FAIL: sh hm-alert.sh, typed in the terminal, did not run"; exit 1; }
  echo "FAIL: no player started after the terminal became blocked (app uid: $uid)"
  "$adb" shell dumpsys audio | grep -E "new player|event:started" | tail -6 | sed 's/^/  audio: /'
  "$adb" logcat -d -t 600 | grep -iE "mediaplayer|exoplayer|audiotrack|video|sound|nsh" | grep -viE "chatty" | tail -25 | sed 's/^/  logcat: /'
  exit 1
fi
echo "ok: a blocked terminal starts the app's sound player"

# Away from the app (the home screen in front), the terminal asks a question: the notification says who asks, what,
# and then where, on the loud channel (dumpsys notification: the record's line names its channel and importance, the lines
# after it the text).
"$adb" shell input keyevent 3
sleep 1
term=$("$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')
printf '%s\n' "printf '\033]7501;state=working\033\\\\'; sleep 2; printf '\033]7501;state=blocked:kind=question:msg=$(printf 'Which port?' | base64)\033\\\\'" > "$work/home/src/acme-api/hm-ask.sh"
"$hc" send --force "$term" 'sh hm-ask.sh' >/dev/null
posted() { "$adb" shell dumpsys notification --noredact | grep -A40 "pkg=$id" | grep -E "$1" >/dev/null; }
record() { "$adb" shell dumpsys notification --noredact | grep -E "NotificationRecord.*pkg=$id .*tag=$term .*$1" >/dev/null; }
asked=""
for _ in $(seq 1 15); do posted "android.text=String \(Which port\?" && posted "^acme-api › " && { asked=1; break; }; sleep 1; done
[ -n "$asked" ] || { echo "FAIL: no notification with what it asks, then where it is"; "$adb" shell dumpsys notification --noredact | grep -A40 "pkg=$id" | grep -E "android\.(title|text)|channel" | head -6; exit 1; }
posted "android.title=String \(.* needs you\)" || { echo "FAIL: the notification does not say who needs you"; exit 1; }
record "importance=4 .*channel=agent-needs-you" || { echo "FAIL: a question is not on the agent-needs-you channel (heads-up)"; exit 1; }
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-notification.png"
echo "ok: away from the app, a blocked terminal posts a notification with who, where and what"

# A tap on it opens that pane.
"$adb" shell cmd statusbar expand-notifications
sleep 1
"$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
xy=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F 'needs you' | head -1 |
  sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}')
[ -n "$xy" ] || { echo "FAIL: the notification is not in the shade"; exit 1; }
"$adb" shell input tap $xy
opened=""
for _ in $(seq 1 10); do screen_has "resource-id=\"terminal-$term\"" && screen_has 'resource-id="pane-header"' && { opened=1; break; }; sleep 1; done
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-opened.png"
[ -n "$opened" ] || { echo "FAIL: tapping the notification did not open the pane of $term"; exit 1; }
echo "ok: tapping the notification opens the pane"

# Away again, the terminal finishes: it pops up too, on its own channel (e2e/android-notify.sh). As Claude Code does (2.1.319, measured
# 2026-11-06), it reports done with no words first and says what it did a few seconds later: the notification takes them.
"$adb" shell input keyevent 3
sleep 1
printf '%s\n' "printf '\033]7501;state=working\033\\\\'; sleep 2; printf '\033]7501;state=done\033\\\\'; sleep 4; printf '\033]7501;state=done:msg=$(printf 'Ran the tests' | base64)\033\\\\'" > "$work/home/src/acme-api/hm-done.sh"
"$hc" send --force "$term" 'sh hm-done.sh' >/dev/null
done=""
for _ in $(seq 1 15); do posted "android.title=String \(.* finished\)" && { done=1; break; }; sleep 1; done
[ -n "$done" ] || { echo "FAIL: no notification when the terminal finished"; exit 1; }
said=""
for _ in $(seq 1 15); do posted "android.text=String \(Ran the tests" && { said=1; break; }; sleep 1; done
[ -n "$said" ] || { echo "FAIL: the finished notification does not say what it did"; "$adb" shell dumpsys notification --noredact | grep -A40 "pkg=$id" | grep -E "android\.(title|text)" | head -4; exit 1; }
record "importance=4 .*channel=agent-finished" || { echo "FAIL: a finished terminal is not on the agent-finished channel"; exit 1; }
echo "ok: notifications away from the app: a question with who, what and where opens its pane; a finish says what it did"

# A new space's folder, picked by browsing: home, then src, then beta-web. The terminal starts there (hc ls: its cwd).
started_in() {
  for _ in $(seq 1 15); do
    "$hc" ls --json | python3 -c 'import json,sys; sys.exit(0 if any(t["cwd"].endswith(sys.argv[1]) for t in json.load(sys.stdin)["terminals"]) else 1)' "$1" && return 0
    sleep 1
  done
  return 1
}
# The pane's Back until home shows (a tap right as the app comes to the front was seen to be lost).
to_home() {
  for _ in 1 2 3 4 5; do screen_has 'resource-id="new"' && return 0; tap back; sleep 1; done
  echo "FAIL: Back does not lead home"; exit 1
}
"$adb" shell am start -n "$activity" >/dev/null
sleep 1
# The switcher sheet: its rows fit, so a drag leaves them where they are (NSH 0.3.0 sized the sheet's scroll content far
# past its rows, and a drag left the sheet blank; measured 2026-11-06).
tap switcher
sleep 2
before=$(at switch-new-folder)
[ -n "$before" ] || { echo "FAIL: no switcher rows"; exit 1; }
# From a row itself: a fixed point can land above a short sheet, where a touch closes it (seen on the CI emulator).
set -- $before
"$adb" shell input swipe "$1" "$2" "$1" "$(($2 - 500))" 300
sleep 2
after=""; for _ in 1 2 3 4 5; do after=$(at switch-new-folder); [ -n "$after" ] && break; sleep 1; done # a dump can fail while the screen settles
[ -n "$before" ] && [ "$before" = "$after" ] || { echo "FAIL: a drag moved the switcher's rows (from '$before' to '$after')"; exit 1; }
"$adb" shell input keyevent KEYCODE_BACK
sleep 1
echo "ok: a drag on the switcher leaves its rows in place"
to_home # from the pane the notification opened
tap new
# New space opens on the folder browser; the program comes after the folder.
for _ in $(seq 1 10); do screen_has 'resource-id="folder-search"' && break; sleep 1; done
screen_has 'resource-id="folder-search"' || { echo "FAIL: New space does not open on the folder browser"; exit 1; }
screen_has 'resource-id="new-terminal"' && { echo "FAIL: the program shows before a folder is picked"; exit 1; }
tap folder-src
for _ in $(seq 1 10); do screen_has 'resource-id="crumb-1"' && break; sleep 1; done
# A row answers the first tap while the search still has the keyboard up.
tap folder-search
"$adb" shell input text beta
for _ in $(seq 1 10); do screen_has 'resource-id="folder-acme-api"' || break; sleep 1; done
tap folder-beta-web
sleep 2
screen_has 'resource-id="crumb-2"' || { echo "FAIL: a folder row needs a second tap while the keyboard is up"; exit 1; }
"$adb" shell am force-stop com.google.android.inputmethod.latin # its floating bar can sit over the sheet
tap folder-use
tap new-terminal
started_in /src/beta-web || { echo "FAIL: the new space did not start in the folder picked"; exit 1; }
echo "ok: a new space starts in the folder picked by browsing"

# A typed folder that doesn't exist gets a card; Create folder makes it on the computer, and the space starts there.
to_home
tap new
tap folder-search
"$adb" shell input text "'~/src/gamma-new'"
for _ in $(seq 1 10); do screen_has "doesn't exist" && break; sleep 1; done
screen_has "doesn't exist" || { echo "FAIL: the app did not say the typed folder doesn't exist"; exit 1; }
[ -e "$work/home/src/gamma-new" ] && { echo "FAIL: the folder was made before Create folder"; exit 1; }
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-missing.png"
tap folder-create
for _ in $(seq 1 10); do [ -d "$work/home/src/gamma-new" ] && break; sleep 1; done
[ -d "$work/home/src/gamma-new" ] || { echo "FAIL: Create folder did not make the folder"; exit 1; }
tap new-terminal
started_in /src/gamma-new || { echo "FAIL: the new space did not start in the folder made"; exit 1; }
# A split on the phone: ⋯ lists it, a warning comes first, then both panes are on the screen. A tap on a pane gives it
# the row under the panes: the new pane goes into a folder, the old one makes a file, and the file lands outside it.
tap_text() {
  xy=""; for _ in 1 2 3 4 5 6 7 8; do "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
    xy=$("$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "text=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}')
    [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: nothing says $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
compose() { tap compose-input; sleep 1; "$adb" shell input text "'$1'"; sleep 1; tap compose-send; sleep 2; }
terminals() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -c 'resource-id="terminal-' || true; }
sleep 2
[ "$(terminals)" = 1 ] || { echo "FAIL: one pane expected before the split, saw $(terminals)"; exit 1; }
tap space-more
sleep 1
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-pane-menu.png"
tap_text "Split right"
sleep 1
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-split-warning.png"
tap_text "Split anyway"
for _ in $(seq 1 15); do [ "$(terminals)" = 2 ] && break; sleep 1; done
[ "$(terminals)" = 2 ] || { echo "FAIL: both panes should be on screen after the split, saw $(terminals)"; exit 1; }
sleep 3
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-split.png"
echo "ok: a split asks first, then shows both panes"
compose "mkdir hm-right; cd hm-right"
for _ in $(seq 1 10); do [ -d "$work/home/src/gamma-new/hm-right" ] && break; sleep 1; done
[ -d "$work/home/src/gamma-new/hm-right" ] || { echo "FAIL: the composer did not reach the new pane"; exit 1; }
xy=$(at terminal-)
"$adb" shell input tap $xy
sleep 2
compose "touch hm-left"
for _ in $(seq 1 10); do [ -e "$work/home/src/gamma-new/hm-left" ] && break; sleep 1; done
[ -e "$work/home/src/gamma-new/hm-left" ] || { echo "FAIL: a tap on the first pane did not give it the composer"; exit 1; }
echo "ok: a tap on a pane gives it the composer"
# Settings, for a look: its top and its end.
tap space-more
sleep 1
tap_text "Settings"
sleep 2
screen_has 'text="Settings"' || { echo "FAIL: Settings did not open"; exit 1; }
"$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { echo "FAIL: the keyboard stayed up over Settings"; exit 1; }
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-settings.png"
"$adb" shell input swipe 500 1800 500 300 200
sleep 2
[ -n "$shot" ] && "$adb" exec-out screencap -p > "${shot%.png}-settings-end.png"
echo "PASS: notifications, and new spaces in a folder browsed to or made on the computer"
