#!/bin/sh
# Add phone in the Herdr Desktop App on a Mac, as a person gets it: Herdr and herdr-core from Herdr's own installer, the
# companion from INSTALL.md's steps as written (from this commit's build when HM_COMPANION names one, which is how CI
# tests a version before it is released), then the dialog in Herdr's real window, pressed with real keys and clicks.
# Reads each window frame (`herdr-app shot`) with Apple's Vision (desktop-tool.swift) and its QR code with frame.py.
# Runs on a GitHub macOS runner, in three phases, so a phone in another job can pair in between:
#
#   dialog  install; cmd-shift-m does nothing before install-dialog; the dialog offers Start; a click on Start brings up
#           the companion and a code that decodes to an invite; Escape closes the dialog and the companion stays.
#   code    cmd-shift-m opens the dialog again; its new code's invite goes to hm-secret/invite (see below) for the phone's job.
#   phone   waits for the phone: it pairs, the dialog says so, and what it typed runs in a terminal here; then Remove
#           and Remove it? in the dialog remove it, and the companion outlives Herdr.
#
#   HM_COMPANION=<built binary> app/e2e/mac-add-phone.sh dialog <out folder>
#
# It installs into the runner's home and uses Herdr's default app home, so it refuses to run outside GitHub Actions
# (desktop-add-phone.sh is the one for a Mac someone uses). <out> gets masked frames (every QR code painted over) and
# logs with no invite in them; the invite's only file is in hm-secret in the runner's temp folder, never uploaded.
set -eu
phase=${1:-}; out=${2:-}
case $phase in dialog | code | phone) ;; *) sed -n '2,16p' "$0"; exit 2 ;; esac
[ -n "$out" ] || { sed -n '2,16p' "$0"; exit 2; }
fail() { echo "FAIL: $*"; exit 1; }
ok() { echo "ok: $*"; }
# A click that did not land: the run goes on with the key or the CLI, so the steps after it still run, and the phone
# phase fails at its end.
missed() { echo "NOT OK: $*"; echo "$*" | head -1 >>"$secret/missed"; }
[ "${GITHUB_ACTIONS-}" = true ] || fail "only on a CI runner: it installs Herdr and uses its default home"
repo=$(cd "$(dirname "$0")/../.." && pwd)
tmp=${RUNNER_TEMP:?}
secret="$tmp/hm-secret"; frames="$out/frames"
mkdir -p "$out" "$secret" "$frames"
bin="$HOME/.local/bin"
export PATH="$bin:$PATH"
app_home="$HOME/.config/herdr-app"
state="$HOME/.config/herdr-anywhere"
companion="$bin/herdr-anywhere"
tool="$tmp/hm-tool"
project="$HOME/src/acme-api"

# ---- the window: keys and clicks reach the window in front

herdr_app() { # Herdr.app, from the herdr-app link its installer makes
  real=$(readlink -f "$bin/herdr-app") || fail "no herdr-app in $bin"
  echo "${real%/Contents/MacOS/*}"
}
window() {
  w=$(pgrep -x Herdr | head -1) || true
  [ -n "$w" ] || fail "no Herdr window"
  echo "$w"
}
# open on a running app brings it to the front, through LaunchServices: no Automation permission needed.
front() { open "$(herdr_app)"; sleep 0.5; }
# Key codes: 53 Escape, 46 M, 123 left arrow (the dialog ignores it).
key() { front; "$tool" key "$(window)" "$@"; sleep 1; }
cmd_shift_m() { key 46 cmd-shift; }
# A word on the frame, in window points: the frame is the whole window at the screen's pixels per point.
scale() {
  px=$(sips -g pixelWidth "$secret/shot.png" | awk '/pixelWidth/ {print $2}')
  pt=$("$tool" frame "$(window)" | awk '{print $3}')
  awk -v a="$px" -v b="$pt" 'BEGIN { printf "%.3f", a / b }'
}
# Two ways to click: "pid", events sent to Herdr's process only (desktop-tool click), and "hid", a click through the
# window server as a mouse makes it (desktop-tool hidclick). Which one Herdr takes on this runner is measured on Start
# itself (below: only a click that lands brings up Starting, a code or serve) and kept in $secret/click-way. Each click
# logs where it went.
click_way() { cat "$secret/click-way" 2>/dev/null || echo hid; }
press() { # text on the frame just read, [way]
  sc=$(scale)
  xy=$("$tool" find "$secret/shot.png" "$1" "$sc") || fail "no button $1 on the frame; it shows:$(shown)"
  how=${2:-$(click_way)}
  front
  was=$("$tool" cursor); fr=$("$tool" front)
  case $how in pid) "$tool" click "$(window)" $xy ;; *) "$tool" hidclick "$(window)" $xy ;; esac
  echo "click ($how) on $1: at $xy in the window (points; frame $(sips -g pixelWidth -g pixelHeight "$secret/shot.png" | awk '/pixel/ {printf "%s ", $2}')px at $sc px/pt), window $("$tool" frame "$(window)"), cursor $was -> $("$tool" cursor), frontmost pid $fr (Herdr $(window))"
  sleep 1
}
status() { herdr-app status; }
overlays() { status | grep -c '"add-phone"' || true; }

# ---- reading a frame: Vision for the words, frame.py for the QR code

n=$(ls "$frames" | wc -l | tr -d ' ')
# Saves a frame: its words in $secret/text, how many codes in $qr, the invite of a code on it in $secret/frame-invite
# until the next frame, and a masked copy in <out>/frames.
frame() {
  n=$((n + 1))
  # A window draws on input (desktop-add-phone.sh); the left arrow asks for a frame.
  key 123; sleep 0.5
  rm -f "$secret/shot.png"
  herdr-app shot "$secret/shot.png" >/dev/null
  [ -f "$secret/shot.png" ] || fail "herdr-app shot saved no frame"
  masked="$frames/$(printf %03d "$n").png"
  codes=$(python3 "$repo/app/e2e/frame.py" "$secret/shot.png" "$secret/frame-invite" "$masked" "$secret/ocr.png") || fail "frame.py failed"
  qr=$(printf '%s\n' "$codes" | grep -c '^qr ' || true)
  # Vision reads codes too: only its words are kept, and how many codes it saw. A frame with a code frame.py missed
  # would show that code unmasked, so it is not kept.
  "$tool" scan "$secret/shot.png" >"$secret/scan" || : >"$secret/scan"
  sed -n 's/^text //p' "$secret/scan" >"$secret/text"
  seen=$(grep -c '^qr ' "$secret/scan" || true); rm -f "$secret/scan"
  [ "$seen" -le "$qr" ] || { rm -f "$masked"; echo "frame $n not kept: Vision saw $seen codes, frame.py masked $qr"; }
}
says() { grep -qF "$1" "$secret/text"; }
shown() { printf '\n'; sed 's/^/  | /' "$secret/text"; }
wait_for() { # text, seconds
  end=$(($(date +%s) + ${2:-60}))
  while [ "$(date +%s)" -lt "$end" ]; do
    frame
    says "$1" && return 0
    sleep 1
  done
  fail "the window never showed \"$1\"; it shows:$(shown)"
}
keep() { cp "$frames/$(printf %03d "$n").png" "$out/$1" 2>/dev/null || echo "not kept: $1"; }
invite() { tr -d '\r\n' <"$secret/frame-invite" 2>/dev/null || true; }
# Every later log line hides it, should anything print it.
mask() { echo "::add-mask::$1"; }
check_invite() {
  i=$(invite)
  [ "$qr" = 1 ] && printf %s "$i" | grep -qE '^herdr1:[A-Za-z0-9_-]+$' || fail "the frame's QR code did not decode to an invite ($qr codes)"
  mask "$i"
}

# ---- a doc's shell block, as an agent runs it: a fresh sh

block() { # file, a word in the block
  awk -v want="$2" '/^```sh$/ { b = ""; on = 1; next } /^```$/ && on { if (index(b, want)) { printf "%s", b; exit } on = 0; next } on { b = b $0 "\n" }' "$repo/$1"
}
run_block() {
  b=$(block "$1" "$2")
  [ -n "$b" ] || fail "$1 has no sh block with $2"
  # This commit's companion in place of the latest release's, when one is given: a folder holding it under this Mac's
  # release name, so a step that picks the other Mac's file finds nothing.
  if [ -n "${HM_COMPANION-}" ]; then
    rel="$tmp/hm-release"
    mkdir -p "$rel"
    ln -sf "$HM_COMPANION" "$rel/herdr-anywhere-macos-$(uname -m | sed 's/x86_64/amd64/')"
    b=$(printf '%s\n' "$b" | sed "s|https://github.com/jt-wang/herdr-anywhere/releases/latest/download/|file://$rel/|")
  fi
  echo "--- $1, the block with $2" >&2
  printf '%s\n' "$b" | sh 2>&1
}

if [ "$phase" = dialog ]; then
  (cd "$repo/app/e2e" && swiftc -O desktop-tool.swift -o "$tool" 2>/dev/null) || fail "desktop-tool.swift does not build"
  echo "events from this runner: Accessibility trusted: $("$tool" trusted); screen locked: $("$tool" locked && echo yes || echo no)"
  nohup caffeinate -dimsu -t 3600 >/dev/null 2>&1 & # a sleeping display draws no frames

  # Herdr's installer, as herdr.dev gives it: `curl -fsSL https://releases.herdr.dev/app/install.sh | sh`.
  echo "--- Herdr's installer"
  curl -fsSL https://releases.herdr.dev/app/install.sh | sh
  [ -x "$bin/herdr-app" ] || fail "Herdr's installer left no herdr-app in $bin"
  echo "$bin" >>"${GITHUB_PATH:-/dev/null}"
  echo "herdr-app: $(herdr-app --version), at $(herdr_app); herdr-core: $(herdr-core --version)"

  out1=$(run_block INSTALL.md "herdr-core --version")
  echo "$out1"
  echo "$out1" | grep -qE 'hc [0-9]+\.[0-9]+\.[0-9]+' || fail "INSTALL.md step 1 found no herdr-core"
  ok "INSTALL.md step 1: herdr-core is installed"

  out2=$(run_block INSTALL.md "herdr-anywhere-macos-")
  echo "$out2"
  want=$(sed -n 's/^var version = "\(.*\)"$/\1/p' "$repo/companion/cmd/herdr-anywhere/main.go")
  if [ -n "${HM_COMPANION-}" ]; then
    echo "$out2" | grep -qx "herdr-anywhere $want" || fail "INSTALL.md step 2 did not print this commit's version ($want)"
    cmp -s "$HM_COMPANION" "$companion" || fail "INSTALL.md step 2 did not put this commit's build at $companion"
    ok "INSTALL.md step 2, with this commit's build: herdr-anywhere $want"
  else
    echo "$out2" | grep -qE '^herdr-anywhere [0-9]+\.[0-9]+\.[0-9]+$' || fail "INSTALL.md step 2 printed no version"
    ok "INSTALL.md step 2, the latest release"
  fi

  # The first window skips the welcome screen and What's new, either of which would cover the dialog
  # (desktop-add-phone.sh says what Herdr writes there).
  mkdir -p "$app_home"
  echo '{"frame.welcomed":"skipped"}' >"$app_home/shared.json"
  v=$(herdr-app --version | awk '{print $2}')
  printf '{"last_run":"%s","presented":["%s"]}\n' "$v" "$v" >"$app_home/update-notes.json"
  open "$(herdr_app)"
  for _ in $(seq 1 60); do status >/dev/null 2>&1 && [ -f "$app_home/keys.json" ] && break; sleep 1; done
  status >/dev/null 2>&1 || fail "the Herdr window never answered herdr-app status"
  sleep 3
  echo "window: pid $(window), frame $("$tool" frame "$(window)")"

  # The check that the key opens the dialog, seen failing: before install-dialog, cmd-shift-m opens nothing.
  frame
  [ -s "$secret/text" ] || fail "Vision reads nothing on Herdr's window"
  says "isn't running" && fail "the dialog text is on screen before the dialog"
  [ "$qr" = 0 ] || fail "a QR code is on screen before the dialog"
  keep 0-herdr.png
  cmd_shift_m; sleep 2
  [ "$(overlays)" = 0 ] || fail "cmd-shift-m opened Add phone before install-dialog"
  ok "before install-dialog: Vision reads the window ($(wc -l <"$secret/text" | tr -d ' ') lines), no QR code, and cmd-shift-m opens nothing"

  out3=$(run_block INSTALL.md "install-dialog")
  echo "$out3"
  echo "$out3" | grep -q "Add phone is installed\. Press cmd-shift-m in Herdr" || fail "INSTALL.md step 3 did not install the dialog"
  grep -q '"cmd-shift-m": "mod.open:add-phone"' "$app_home/keys.json" || fail "keys.json has no cmd-shift-m for Add phone"
  herdr-app check "$app_home/mods/add-phone" || fail "herdr-app check mods/add-phone"
  ok "INSTALL.md step 3: install-dialog added cmd-shift-m and the mod, which loads"

  for _ in 1 2 3 4 5; do [ "$(overlays)" = 1 ] && break; cmd_shift_m; sleep 1; done
  [ "$(overlays)" = 1 ] || fail "cmd-shift-m did not open the dialog"
  wait_for "isn't running"
  grep -qx Start "$secret/text" || fail "the dialog offers no Start:$(shown)"
  [ "$qr" = 0 ] || fail "a QR code before the companion runs"
  keep 1-offers-start.png
  ok "cmd-shift-m opens the dialog; no companion yet, and it offers Start"

  # Each way of clicking in turn, until one brings up Starting, a code or serve: what a click that lands does, and
  # nothing else in this run does it.
  started=""
  # hid twice: should the first click only bring the window forward, the second is the one a person makes next.
  for how in pid hid hid; do
    press Start "$how"
    for _ in $(seq 1 10); do
      frame
      if says "Starting" || says "scan this code" || pgrep -f "$companion serve" >/dev/null; then started=yes; break; fi
      sleep 1
    done
    [ -n "$started" ] && { echo "$how" >"$secret/click-way"; break; }
    echo "click ($how) on Start: no Starting, no code, no herdr-anywhere serve in 10 s"
  done
  if [ -z "$started" ]; then
    # Enter is the dialog's Start too (main.js: primary), so the rest still runs; the run fails at its end.
    missed "neither way of clicking on Start did anything; it shows:$(shown)"
    key 36
  fi
  wait_for "scan this code" 90
  pgrep -f "$companion serve" >/dev/null || fail "no herdr-anywhere serve after Start"
  keep 2-code.png
  [ -n "$started" ] && ok "a click ($(click_way)) on Start brought up the companion and a code" || echo "Enter on Start brought up the companion and a code"

  check_invite
  ok "the code on screen decodes to an invite, $(invite | wc -c | tr -d ' ') characters"
  # The check itself first: it must find the invite where it is.
  invite >"$secret/needle"
  grep -qFf "$secret/needle" "$secret/frame-invite" || fail "the on-disk check finds nothing"
  for dir in "$app_home" "$state"; do
    grep -rqFf "$secret/needle" "$dir" 2>/dev/null && fail "the invite is on disk under $dir"
  done
  rm -f "$secret/needle"
  ok "the invite is in no file of Herdr's home or the companion's state"

  for _ in 1 2 3 4 5; do [ "$(overlays)" = 0 ] && break; key 53; done
  [ "$(overlays)" = 0 ] || fail "Escape did not close the dialog"
  sleep 2
  pgrep -f "$companion serve" >/dev/null || fail "the companion stopped with the dialog"
  ok "Escape closes the dialog, and the companion keeps running"

  # A terminal for the phone to find and type in, in a project folder, as Herdr's own spaces are.
  mkdir -p "$project"
  herdr-core run --bg --keep --cwd "$project" --label herdr.space=spmac --label herdr.tab=tbmac >/dev/null || fail "herdr-core run"
  ok "a terminal in acme-api for the phone"
fi

if [ "$phase" = code ]; then
  for _ in 1 2 3 4 5; do [ "$(overlays)" = 1 ] && break; cmd_shift_m; sleep 1; done
  [ "$(overlays)" = 1 ] || fail "cmd-shift-m did not open the dialog"
  ok "cmd-shift-m opens the dialog again"
  wait_for "scan this code"
  check_invite
  (umask 077; invite >"$secret/invite")
  keep 3-code-for-the-phone.png
  ok "a new code, $(wc -c <"$secret/invite" | tr -d ' ') characters, for the phone"
fi

if [ "$phase" = phone ]; then
  typed="$project/hm-typed"
  end=$(($(date +%s) + 600)); paired=""
  while [ "$(date +%s)" -lt "$end" ]; do
    frame
    says "is paired" && paired=yes
    [ -n "$paired" ] && [ -f "$typed" ] && break
    sleep 3
  done
  [ -n "$paired" ] || fail "the dialog never said the phone is paired; it shows:$(shown)"
  keep 4-paired.png
  ok "the dialog says: $(grep -m1 'is paired' "$secret/text")"
  [ "$("$companion" devices --json | grep -o '"id"' | wc -l | tr -d ' ')" = 1 ] || fail "not one phone paired"
  grep -qE 'device .* connected \(binary,' "$state/serve.log" || fail "the companion logged no binary connection from the phone"
  ok "the companion has one phone, connected"
  if [ ! -f "$typed" ]; then
    # What reached the terminal, with anything shaped like an invite or pairing link masked; and the folder's files.
    for t in $(herdr-core ls --json 2>/dev/null | python3 -c 'import json,sys; print(" ".join(x["address"] for x in json.load(sys.stdin)["terminals"]))' 2>/dev/null); do
      echo "--- terminal $t, last lines:"
      herdr-core read "$t" 2>&1 | tail -20 | sed -E 's/herdr1:[A-Za-z0-9_-]+/herdr1:…/g; s/#pair=[A-Za-z0-9_-]+/#pair=…/g; s/^/  | /'
    done
    echo "--- files in $project:"; ls -la "$project"
    fail "what the phone typed did not run here (no $typed)"
  fi
  grep -q phone "$typed" || fail "hm-typed holds $(cat "$typed")"
  ok "the phone's command ran in the terminal here: hm-typed holds \"$(cat "$typed")\""

  # The dialog lists the phone with its Remove; a second click confirms.
  frame
  press Remove
  confirm=""
  for _ in $(seq 1 10); do frame; says "Remove it?" && { confirm=yes; break; }; sleep 1; done
  if [ -n "$confirm" ]; then press "Remove it?"; else
    # Removed from the CLI, as the button does, so the phone still sees it; the run fails at its end.
    missed "the click on Remove did nothing in 10 s; it shows:$(shown)"
    "$companion" revoke "$("$companion" devices --json | sed -E 's/.*"id":"([^"]+)".*/\1/')" >/dev/null
  fi
  for _ in $(seq 1 20); do [ "$("$companion" devices --json)" = "[]" ] && break; sleep 1; done
  [ "$("$companion" devices --json)" = "[]" ] || fail "the phone is still paired after Remove"
  for _ in $(seq 1 20); do frame; says "Paired phones" || break; sleep 1; done
  says "Paired phones" && fail "the removed phone is still listed"
  keep 5-removed.png
  [ -n "$confirm" ] && ok "Remove in the dialog removed the phone" || echo "the CLI removed the phone"

  pkill -x Herdr; sleep 2
  pgrep -f "$companion serve" >/dev/null || fail "the companion stopped with Herdr"
  ok "the companion outlives Herdr"
fi
if [ "$phase" = phone ] && [ -s "$secret/missed" ]; then echo "FAIL: clicks that did not land:"; cat "$secret/missed"; exit 1; fi
echo "PASS ($phase)"
