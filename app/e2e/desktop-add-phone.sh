#!/bin/sh
# The Add phone dialog in a real Herdr window of its own: its own app home,
# herdr-core session and companion state, nothing of the person's. Reads the
# window's frames, decodes the QR code from the pixels, pairs a phone over
# Tailcat's real relays with what it decoded, and watches the dialog follow.
# Needs internet, Herdr and macOS. A camera on the code is not part of this.
#
# It runs while the person works: the window opens behind theirs and never takes the
# focus. A window in the background takes no keys or clicks, so there Start, Escape,
# cmd-shift-m and Remove go through the CLI, as the dialog's buttons do (main.test.mjs
# presses them under node). HM_DESKTOP_FOCUS=1 presses them in the window instead,
# bringing it to the front again and again: only for a Mac nobody is using.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
companion="$here/../../companion"
work=$(mktemp -d /tmp/hmap.XXXX) # sockets must fit macOS's 104 bytes
mkdir -p "$work/app" "$work/s" "$work/x"
export HERDR_APP_HOME="$work/app" HC_SESSION="hmap-$$" HC_SOCKET="" XDG_STATE_HOME="$work/x/state" XDG_CONFIG_HOME="$work/x/config" XDG_RUNTIME_DIR="$work/x"
unset HERDR_ENV HERDR_PANE_ID HERDR_SOCKET_PATH HERDR_TAB_ID HERDR_WORKSPACE_ID HERDR_BIN_PATH HERDR_ANYWHERE_STATE
# The dialog speaks the Mac's language; the words this script looks for are its English ones.
export HERDR_ANYWHERE_LANG=en

window=""; awake=""; watch=""; start=""; focus=""; why_focus=""; window_pid=""
frontmost() { osascript -e 'tell application "System Events" to get unix id of first process whose frontmost is true' 2>/dev/null || echo -1; }
name() { ps -p "$1" -o comm= 2>/dev/null | sed 's|.*/||' || echo "pid $1"; }
# The person works on this Mac during the run: Herdr's window may show but must never take the focus.
# A watcher reads the frontmost app from System Events about four times a second ("ms pid" per line),
# from just before the launch; it stops the run as soon as the window (its pid, once known) is frontmost
# after its first second.
watch_js='ObjC.import("Foundation");
function run(a) {
  var se = Application("System Events"), me = Application.currentApplication(); me.includeStandardAdditions = true;
  var w = -2, t0 = Date.now(), told = false;
  for (;;) {
    if (w < 0) { var f = $.NSString.stringWithContentsOfFileEncodingError(a[0], $.NSUTF8StringEncoding, null); if (!f.isNil()) w = parseInt(f.js) || -2; }
    var p = -1; try { p = se.processes.whose({frontmost: true})[0].unixId(); } catch (e) {}
    var t = Date.now() - t0; console.log(t + " " + p);
    if (p === w && t > 1000 && !told) { told = true; me.doShellScript("kill -USR1 " + a[1]); }
    delay(0.2);
  }
}'
focus_check() { # after the watcher stopped: Herdr never frontmost after 1 s, and the focus is where the person left it
  [ -z "$focus" ] || { echo "not checked: the focus, this run takes it ($why_focus)"; return 0; }
  [ -n "$start" ] && [ -f "$work/focus.log" ] || return 0
  end=$(frontmost)
  echo "focus changes (ms pid): $(awk -v s="$start" 'BEGIN { prev = s } $2 != prev { printf "%s %s, ", $1, $2; prev = $2 }' "$work/focus.log")end $end; Herdr under test was $window_pid"
  took=$(awk -v w="$window_pid" '$2 == w && $1 > 1000' "$work/focus.log" | wc -l | tr -d ' ')
  # The person may switch apps meanwhile: a change between two other apps moves what the end should be.
  want=$(awk -v w="$window_pid" -v s="$start" 'BEGIN { prev = s; want = s }
    $2 != prev && prev != w && $2 != w && $2 != -1 { want = $2 } $2 != -1 { prev = $2 } END { print want }' "$work/focus.log")
  [ "$took" = 0 ] || { echo "FAIL: Herdr under test was frontmost in $took of $(wc -l <"$work/focus.log" | tr -d ' ') samples after its first second"; return 1; }
  [ "$end" = "$want" ] || { echo "FAIL: the focus ends on $(name "$end") ($end), it was on $(name "$want") ($want)"; return 1; }
  echo "ok: Herdr under test never took the focus ($(wc -l <"$work/focus.log" | tr -d ' ') samples), and it ends on $(name "$end") as it was"
}
finish() {
  status=$?
  [ -z "$watch" ] || kill "$watch" 2>/dev/null || true
  [ -z "$awake" ] || kill "$awake" 2>/dev/null || true
  pkill -f "$work/herdr-anywhere serve" 2>/dev/null || true
  [ -z "$window" ] || kill "$window" 2>/dev/null || true
  sleep 1; focus_check || status=1
  hc server stop >/dev/null 2>&1 || true
  rm -rf "${work:?}"
  [ "$status" = 0 ] && echo "PASS" || echo "FAIL"
  exit "$status"
}
trap finish EXIT
# A run stopped from outside is no pass (a TERM once went to finish with the last command's 0: PASS).
trap 'echo "FAIL: stopped by a signal"; exit 1' INT TERM
trap 'fail "Herdr took the focus"' USR1
fail() {
  echo "FAIL: $1"
  echo "focus: ${focus:-no}, clicks: ${clicks:-no}, screen locked now: $("$work/tool" locked 2>/dev/null && echo yes || echo no), state: $(ls "$work/s" 2>/dev/null | tr '\n' ' ')"
  [ -f "$work/app.log" ] && { echo "app.log, last lines:"; tail -15 "$work/app.log"; }
  exit 1
}

(cd "$companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
swiftc -O "$here/desktop-tool.swift" -o "$work/tool" 2>/dev/null
caffeinate -u -t 600 & awake=$! # a sleeping display draws no frames

echo '{"frame.welcomed":"skipped"}' >"$work/app/shared.json" # no first-run welcome over the dialog
# Nor What's new: on a fresh app home Herdr opens it over everything for a release whose notes it shows by itself
# (0.1.13: it covered the dialog, and neither Got it nor Enter closed it from under the dialog). Herdr
# records shown notes in update-notes.json; this is what it wrote after showing them.
herdr_version=$(herdr-app --version | awk '{print $2}')
printf '{"last_run":"%s","presented":["%s"]}\n' "$herdr_version" "$herdr_version" >"$work/app/update-notes.json"
# Behind a locked screen nobody works, and the window draws only on input that reaches it in front (below).
focus=""; why_focus=""
if "$work/tool" locked; then focus=yes; why_focus="the screen is locked"
elif [ "${HM_DESKTOP_FOCUS-}" = 1 ]; then focus=yes; why_focus="HM_DESKTOP_FOCUS=1"; fi
start=$(frontmost)
[ -n "$focus" ] || { osascript -l JavaScript -e "$watch_js" "$work/window.pid" "$$" 2>"$work/focus.log" & watch=$!; }
# Started from a shell, Herdr comes to the front within a second; through LaunchServices with -g it
# opens behind and stays there (the focus check below fails if that changes). -n: a second Herdr beside the person's.
open -g -n /Applications/Herdr.app --stdout "$work/app.log" --stderr "$work/app.log" \
  --env HERDR_APP_HOME="$HERDR_APP_HOME" --env HC_SESSION="$HC_SESSION" --env HC_SOCKET= --env XDG_STATE_HOME="$XDG_STATE_HOME" \
  --env XDG_CONFIG_HOME="$XDG_CONFIG_HOME" --env XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" --env HERDR_ANYWHERE_LANG=en
for _ in $(seq 1 30); do [ -f "$work/app/keys.json" ] && [ -S "$work/app/herdr-app.sock" ] && break; sleep 1; done
[ -f "$work/app/keys.json" ] || fail "no window"
window=$(lsof -t "$work/app/herdr-app.sock" 2>/dev/null | head -1); window_pid=$window
[ -n "$window" ] || fail "no process holds the window's socket"
echo "$window" >"$work/window.pid"

hm() { "$work/herdr-anywhere" "$@" --state "$work/s"; }
"$work/herdr-anywhere" install-dialog --state "$work/s" >/dev/null
herdr-app check "$work/app/mods/add-phone" >/dev/null || fail "herdr-app check"
grep -q '"cmd-shift-m": "mod.open:add-phone"' "$work/app/keys.json" || fail "no key"

# Measured: behind a locked screen the window keeps its state but draws
# only on input (a new state was drawn about a minute later, on a key press). There
# every frame is asked for with the left arrow, which the dialog ignores, in a frontmost window.
# Unlocked, a window covered by the person's windows draws nothing either, and
# takes no keys or clicks sent to its pid (with or without the window number in the event).
# Resizing it by a point and back through Accessibility draws a frame and leaves the focus alone.
front() { [ -n "$focus" ] && osascript -e "tell application \"System Events\" to set frontmost of (first process whose unix id is $window) to true" >/dev/null 2>&1 || true; }
nudge() {
  osascript -e "tell application \"System Events\" to tell window 1 of (first process whose unix id is $window)" \
    -e "set {w, h} to size" -e "set size to {w, h + 1}" -e "delay 0.2" -e "set size to {w, h}" -e "end tell" >/dev/null 2>&1 || true
}
frame() {
  if [ -n "$focus" ]; then front; sleep 0.3; "$work/tool" key "$window" 123; sleep 1.5
  else nudge; sleep 0.5; fi
  herdr-app shot "$work/f.png" >/dev/null
  "$work/tool" scan "$work/f.png" >"$work/f.txt" || : >"$work/f.txt"
}
wait_for() { # text to find in a frame
  for _ in $(seq 1 40); do
    frame
    grep -qF "What's new in Herdr" "$work/f.txt" && { sed 's/^qr .*/qr <decoded>/' "$work/f.txt"; fail "Herdr's What's new window is open: update-notes.json no longer keeps it shut"; }
    grep -qF "$1" "$work/f.txt" && return 0
    sleep 1
  done
  sed 's/^qr .*/qr <decoded>/' "$work/f.txt"; fail "the dialog never showed: $1"
}

herdr-app do mod.open --name add-phone >/dev/null
wait_for "isn't running"
grep -qx 'text Start' "$work/f.txt" || fail "no Start button"
echo "ok: no companion, and the dialog offers Start"

# Clicks and keys reach only a frontmost window (above).
press() { # text of a button on the frame just read
  xy=$("$work/tool" find "$work/f.png" "$1") || fail "no button $1 on screen"
  front; sleep 0.3
  "$work/tool" click "$window" $xy; sleep 1
}
# Behind a locked screen a mouse click never arrives: use the keyboard and the CLI there.
clicks=""; [ -n "$focus" ] && ! "$work/tool" locked && clicks=yes
why_cli="the screen is locked and the window takes no keys"
[ -n "$focus" ] || why_cli="the window is in the background and takes no keys or clicks"
# Behind a locked screen Herdr 0.1.13's dialog takes no keys either (eight Enters, it stayed on Start;
# 0.1.12 took them). There the companion is started from the CLI, as the button does, and the dialog
# opened again; main.test.mjs presses Start under node.
reopen() { herdr-app do overlay.close >/dev/null; sleep 1; herdr-app do mod.open --name add-phone >/dev/null; }
if [ -n "$clicks" ]; then press Start; else
  for _ in 1 2 3; do
    [ -n "$focus" ] || break
    front; sleep 0.5; "$work/tool" key "$window" 36; sleep 2
    [ -S "$work/s/serve.sock" ] && break
  done
  if [ ! -S "$work/s/serve.sock" ]; then
    echo "not checked: Start from the dialog, $why_cli (main.test.mjs presses it under node)"
    hm start >/dev/null || fail "herdr-anywhere start"
    reopen
  fi
fi
wait_for "scan this code"
echo "ok: the companion is up and the dialog shows a code"
grep -q '^qr herdr1:' "$work/f.txt" || fail "the frame's QR code did not decode to an invite"
( umask 077; sed -n 's/^qr //p' "$work/f.txt" >"$work/invite" )
echo "ok: the code on screen decodes to an invite, $(wc -c <"$work/invite" | tr -d ' ') characters"

# The check itself first: it must find the invite where it is.
grep -qFf "$work/invite" "$work/invite" || fail "the on-disk check finds nothing"
for dir in "$work/app" "$work/s" "$work/x"; do
  grep -rqFf "$work/invite" "$dir" 2>/dev/null && fail "the invite is on disk under $dir"
done
echo "ok: the invite is in no file of the app home, the state or the core"

overlays() { herdr-app status | grep -c '"add-phone"' || true; }
# The first key after coming to the front can be lost, so a key gets a few tries.
for _ in 1 2 3 4 5; do [ -n "$focus" ] || break; front; sleep 0.5; "$work/tool" key "$window" 53; sleep 1; [ "$(overlays)" = 0 ] && break; done
if [ "$(overlays)" != 0 ] && [ -z "$clicks" ]; then
  echo "not checked: Escape, $why_cli"
  herdr-app do overlay.close >/dev/null; sleep 1
else
  [ "$(overlays)" = 0 ] || fail "Escape did not close the dialog"
  echo "ok: Escape closes the dialog"
fi
[ "$(overlays)" = 0 ] || fail "the dialog did not close"
hm pair --json | grep -q '"invite"' || fail "the companion died with the dialog"
echo "ok: the companion outlives the dialog"

for _ in 1 2 3 4 5; do [ -n "$focus" ] || break; front; sleep 0.5; "$work/tool" key "$window" 46 cmd-shift; sleep 1; [ "$(overlays)" = 1 ] && break; done
if [ "$(overlays)" != 1 ] && [ -z "$clicks" ]; then
  echo "not checked: cmd-shift-m in the window, $why_cli (keys.json binds it, checked above)"
  herdr-app do mod.open --name add-phone >/dev/null; sleep 1
else
  [ "$(overlays)" = 1 ] || fail "cmd-shift-m did not open the dialog"
  echo "ok: cmd-shift-m opens the dialog"
fi
[ "$(overlays)" = 1 ] || fail "the dialog did not open again"
wait_for "scan this code"
sed -n 's/^qr //p' "$work/f.txt" >"$work/invite"
(cd "$companion" && HM_SCREEN_INVITE="$work/invite" go test ./cmd/herdr-anywhere -run TestPairFromScreen -count=1 >"$work/pair.log" 2>&1) || { cat "$work/pair.log"; fail "pairing with the decoded invite"; }
hm devices --json | grep -q '"Test Pixel"' || fail "no device after pairing"
wait_for "Test Pixel is paired"
grep -qF 'Paired phones' "$work/f.txt" || fail "no list of phones"
echo "ok: a phone paired with what was on screen, and the dialog named it"

if [ -n "$clicks" ]; then
  press Remove
  wait_for "Remove it?"
  press "Remove it?"
else
  echo "not checked: Remove from the dialog, $why_cli (main.test.mjs presses it under node)"
  id=$(hm devices --json | sed -E 's/.*"id":"([^"]+)".*/\1/')
  "$work/herdr-anywhere" revoke --state "$work/s" "$id" >/dev/null
fi
for _ in $(seq 1 20); do frame; grep -qF 'Paired phones' "$work/f.txt" || break; sleep 1; done
grep -qF 'Paired phones' "$work/f.txt" && fail "the removed phone is still listed"
[ "$(hm devices --json)" = "[]" ] || fail "the removed phone is still paired"
echo "ok: a removed phone is gone from the list and the store"

kill "$window"; window=""; sleep 1
hm pair --json | grep -q '"invite"' || fail "the companion died with Herdr"
echo "ok: the companion outlives Herdr"
