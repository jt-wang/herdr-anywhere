#!/bin/sh
# The Add phone dialog in a real Herdr window of its own: its own app home,
# herdr-core session and companion state, nothing of the person's. Reads the
# window's frames, decodes the QR code from the pixels, pairs a phone over
# Tailcat's real relays with what it decoded, and watches the dialog follow.
# Needs internet, Herdr and macOS. A camera on the code is not part of this.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
companion="$here/../../companion"
work=$(mktemp -d /tmp/hmap.XXXX) # sockets must fit macOS's 104 bytes
mkdir -p "$work/app" "$work/s" "$work/x"
export HERDR_APP_HOME="$work/app" HC_SESSION="hmap-$$" HC_SOCKET="" XDG_STATE_HOME="$work/x/state" XDG_CONFIG_HOME="$work/x/config" XDG_RUNTIME_DIR="$work/x"
unset HERDR_ENV HERDR_PANE_ID HERDR_SOCKET_PATH HERDR_TAB_ID HERDR_WORKSPACE_ID HERDR_BIN_PATH HERDR_ANYWHERE_STATE

window=""; awake=""
before=$(osascript -e 'tell application "System Events" to get unix id of first process whose frontmost is true' 2>/dev/null || true)
finish() {
  status=$?
  [ -z "$before" ] || osascript -e "tell application \"System Events\" to set frontmost of (first process whose unix id is $before) to true" >/dev/null 2>&1 || true
  [ -z "$awake" ] || kill "$awake" 2>/dev/null || true
  pkill -f "$work/herdr-anywhere serve" 2>/dev/null || true
  [ -z "$window" ] || kill "$window" 2>/dev/null || true
  hc server stop >/dev/null 2>&1 || true
  rm -rf "${work:?}"
  [ "$status" = 0 ] && echo "PASS" || echo "FAIL"
  exit "$status"
}
trap finish EXIT
# A run stopped from outside is no pass (before 2026-11-08 a TERM went to finish with the last command's 0: PASS).
trap 'echo "FAIL: stopped by a signal"; exit 1' INT TERM
fail() {
  echo "FAIL: $1"
  echo "clicks: ${clicks-unset}, screen locked now: $("$work/tool" locked 2>/dev/null && echo yes || echo no), state: $(ls "$work/s" 2>/dev/null | tr '\n' ' ')"
  [ -f "$work/app.log" ] && { echo "app.log, last lines:"; tail -15 "$work/app.log"; }
  exit 1
}

(cd "$companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
swiftc -O "$here/desktop-tool.swift" -o "$work/tool" 2>/dev/null
caffeinate -u -t 600 & awake=$! # a sleeping display draws no frames

echo '{"frame.welcomed":"skipped"}' >"$work/app/shared.json" # no first-run welcome over the dialog
# Nor What's new: on a fresh app home Herdr opens it over everything for a release whose notes it shows by itself
# (0.1.13, 2026-11-08: it covered the dialog, and neither Got it nor Enter closed it from under the dialog). Herdr
# records shown notes in update-notes.json; this is what it wrote after showing them (read back 2026-11-08).
herdr_version=$(herdr-app --version | awk '{print $2}')
printf '{"last_run":"%s","presented":["%s"]}\n' "$herdr_version" "$herdr_version" >"$work/app/update-notes.json"
herdr-app >"$work/app.log" 2>&1 & window=$!
for _ in $(seq 1 30); do [ -f "$work/app/keys.json" ] && [ -S "$work/app/herdr-app.sock" ] && break; sleep 1; done
[ -f "$work/app/keys.json" ] || fail "no window"

hm() { "$work/herdr-anywhere" "$@" --state "$work/s"; }
"$work/herdr-anywhere" install-dialog --state "$work/s" >/dev/null
herdr-app check "$work/app/mods/add-phone" >/dev/null || fail "herdr-app check"
grep -q '"cmd-shift-m": "mod.open:add-phone"' "$work/app/keys.json" || fail "no key"

# Measured 2026-11-03: behind a locked screen the window keeps its state but draws
# only on input (a state set at 13:54:58 was drawn at 13:56:02, on a key press). So
# every frame is asked for with the left arrow, which the dialog ignores, in a frontmost window.
front() { osascript -e "tell application \"System Events\" to set frontmost of (first process whose unix id is $window) to true" >/dev/null 2>&1 || true; }
frame() {
  front; sleep 0.3
  "$work/tool" key "$window" 123; sleep 1.5
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

# Clicks and keys reach only a frontmost window.
press() { # text of a button on the frame just read
  xy=$("$work/tool" find "$work/f.png" "$1") || fail "no button $1 on screen"
  front; sleep 0.3
  "$work/tool" click "$window" $xy; sleep 1
}
# Behind a locked screen a mouse click never arrives (tried 2026-11-03): use the keyboard and the CLI there.
clicks=yes; "$work/tool" locked && clicks=""
# Behind a locked screen Herdr 0.1.13's dialog takes no keys either (2026-11-08: eight Enters, it stayed on Start;
# 0.1.12 took them on 2026-11-03). There the companion is started from the CLI, as the button does, and the dialog
# opened again; main.test.mjs presses Start under node.
reopen() { herdr-app do overlay.close >/dev/null; sleep 1; herdr-app do mod.open --name add-phone >/dev/null; }
if [ -n "$clicks" ]; then press Start; else
  for _ in 1 2 3; do
    front; sleep 0.5; "$work/tool" key "$window" 36; sleep 2
    [ -S "$work/s/serve.sock" ] && break
  done
  if [ ! -S "$work/s/serve.sock" ]; then
    echo "not checked: Start from the dialog, the screen is locked and the window takes no keys (main.test.mjs presses it under node)"
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
for _ in 1 2 3 4 5; do front; sleep 0.5; "$work/tool" key "$window" 53; sleep 1; [ "$(overlays)" = 0 ] && break; done
if [ "$(overlays)" != 0 ] && [ -z "$clicks" ]; then
  echo "not checked: Escape, the screen is locked and the window takes no keys"
  herdr-app do overlay.close >/dev/null; sleep 1
else
  [ "$(overlays)" = 0 ] || fail "Escape did not close the dialog"
  echo "ok: Escape closes the dialog"
fi
[ "$(overlays)" = 0 ] || fail "the dialog did not close"
hm pair --json | grep -q '"invite"' || fail "the companion died with the dialog"
echo "ok: the companion outlives the dialog"

for _ in 1 2 3 4 5; do front; sleep 0.5; "$work/tool" key "$window" 46 cmd-shift; sleep 1; [ "$(overlays)" = 1 ] && break; done
if [ "$(overlays)" != 1 ] && [ -z "$clicks" ]; then
  echo "not checked: cmd-shift-m, the screen is locked and the window takes no keys"
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
  echo "not checked: clicks on Start and Remove, the screen is locked (main.test.mjs presses them under node)"
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
