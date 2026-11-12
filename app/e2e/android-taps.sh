#!/bin/sh
# Taps on rows inside a Scroll right after a line sent from the composer, on Android: the box keeps its focus (the
# keyboard down, or a question took the box's place), and a Scroll that doesn't persist taps spends the first tap on
# that focus. Four places: the choices row, the switcher's rows, home's rows, the tab strip on a wide screen.
# Every place is tried, then the script fails if any first tap was lost.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-taps.sh APK COMPANION [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?}"
apk=$1; bin=$2; shot=${3:-}; [ -z "$shot" ] || mkdir -p "$shot"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/home/src/beta-web" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"; echo skip_global_compinit=1 > "$work/home/.zshenv"
export HC_SESSION="hmtap-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
serve_pid=; dens_found=; lost=
cleanup() {
  trap - EXIT; trap '' INT TERM HUP; set +e
  [ -n "$dens_found" ] && { if [ "$dens_found" = reset ]; then "$adb" shell wm density reset; else "$adb" shell wm density "$dens_found"; fi; } </dev/null
  [ -n "$serve_pid" ] && kill "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" "${state:?}"
}
trap cleanup EXIT; trap 'cleanup; exit 130' INT; trap 'cleanup; exit 143' TERM HUP
ids() { "$hc" ls --json | python3 -c 'import json,sys; print(" ".join(t["address"] for t in json.load(sys.stdin)["terminals"]))'; }
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spp1 --label herdr.tab=tbp1 >/dev/null; t1=$(ids)
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spp1 --label herdr.tab=tbp2 >/dev/null
t2=$(ids | tr ' ' '\n' | grep -vx "$t1")
"$hc" run --bg --keep --cwd "$work/home/src/beta-web" --label herdr.space=spp2 --label herdr.tab=tbp3 >/dev/null
t3=$(ids | tr ' ' '\n' | grep -vx "$t1" | grep -vx "$t2")
echo "terminals: $t1 $t2 $t3"
# A Claude-Code-style question on the screen, then blocked; one key read raw, then idle again.
cat > "$work/home/src/acme-api/m.sh" <<'SH'
printf 'Do you want to proceed?\n\342\235\257 1. Yes\n  2. No\n'
printf '\033]7501;state=blocked:kind=question:app=claude-code\033\\'
stty -icanon -echo; k=$(dd bs=1 count=1 2>/dev/null); stty icanon echo
echo "picked-$k"; printf '\033]7501;state=idle:app=claude-code\033\\'
SH
"$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 & serve_pid=$!
sleep 1
invite=$("$bin" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
centre() { case $1 in *-) m="resource-id=\"$1" ;; *) m="resource-id=\"$1\"" ;; esac
  tr '>' '\n' | grep -F "$m" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk 'NF == 4 {print int(($1+$3)/2), int(($2+$4)/2)}'; }
has() { xml | grep -qF "$1"; }
snap() { [ -z "$shot" ] || "$adb" exec-out screencap -p > "$shot/$1.png" || true; }
wait_for() { for _ in $(seq 1 30); do has "$1" && return 0; sleep 1; done; echo "FAIL: $2"; snap fail; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -40; exit 1; }
at() { xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done; [ -n "$xy" ] || { echo "FAIL: no $1"; snap fail; exit 1; }; echo "$xy"; }
tap() { "$adb" shell input tap $(at "$1"); }
type_text() { "$adb" shell input text "'$(printf %s "$1" | sed 's/ /%s/g')'" < /dev/null; }
focus() { "$adb" shell dumpsys input_method | tr -d '\r' | grep -oE 'mInputShown=[a-z]+|mServedView=[^ ]+' | head -2 | tr '\n' ' '; }
hide_keys() { for _ in 1 2; do "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && { "$adb" shell input keyevent KEYCODE_BACK; sleep 1; }; done; return 0; }
# After a send: the keyboard down with one BACK, and only while it is up (it can go down by itself; a BACK then leaves the pane).
# When the send made the composer go (a question replaced it with the choices row), no BACK: the keyboard is left as
# the user would find it, and a BACK would leave the pane.
down_once() { sleep 1.5; has 'resource-id="compose-input"' || { echo "  composer gone after the send; no BACK"; return 0; }
  "$adb" shell dumpsys input_method | grep -q 'mInputShown=true' && "$adb" shell input keyevent KEYCODE_BACK; sleep 1.5
  has 'resource-id="compose-input"' || { echo "FAIL: BACK left the pane"; snap fail-back; exit 1; }; }
# A line from the composer, sent, keyboard down: the state every probe starts from.
say() { tap compose-input; type_text "$1"; has "text=\"$1\"" || { echo "FAIL: typed text changed"; exit 1; }; tap compose-send; echo "  focus after the send: $(focus)"; down_once; echo "  focus before the tap: $(focus)"; }
# $1 place, $2 the id to tap, $3 a shell test that passes when the tap was taken.
probe() {
  xy=$(at "$2"); "$adb" shell input tap $xy
  for _ in 1 2 3 4 5 6; do sleep 1; if eval "$3"; then echo "RESULT $1: TAKEN (first tap)"; return 0; fi; done
  snap "lost-$1"
  "$adb" shell input tap $xy
  for _ in 1 2 3 4 5 6; do sleep 1; if eval "$3"; then echo "RESULT $1: LOST (first tap did nothing; the second was taken)"; lost="$lost $1"; return 0; fi; done
  echo "RESULT $1: LOST (neither tap taken)"; lost="$lost $1"; return 0
}

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link; type_text "$invite"; hide_keys; tap pair
wait_for 'resource-id="space-' "the phone did not pair"

echo "== 1. choices row"
tap space-spp1
wait_for "resource-id=\"terminal-$t1\"" "space 1 did not open on tab 1"
say "sh m.sh"
wait_for 'resource-id="choice-1"' "no choices row"
echo "  focus with the choices row up: $(focus)"
probe choices choice-1 '"$hc" read "$t1" | grep -q picked-1'
wait_for 'resource-id="compose-input"' "the composer did not come back"

echo "== 2. switcher row"
say "echo two"
tap switcher
wait_for 'resource-id="switch-tab-tbp2"' "no switcher row for tab 2"
probe switcher switch-tab-tbp2 'has "resource-id=\"terminal-$t2\"" && ! has "resource-id=\"switch-tab-tbp2\""'

echo "== 3. home row"
wait_for "resource-id=\"terminal-$t2\"" "tab 2 is not shown"
say "echo three"
tap back
wait_for 'resource-id="space-spp2"' "home did not show"
probe home space-spp2 'has "resource-id=\"terminal-$t3\""'

echo "== 4. wide tab strip"
d=$("$adb" shell wm density | tr -d '\r'); echo "  density was: $(echo "$d" | tr '\n' ' ')"
case $d in *Override*) dens_found=$(echo "$d" | sed -nE 's/Override density: ([0-9]+)/\1/p') ;; *) dens_found=reset ;; esac
w=$("$adb" shell wm size | tr -d '\r' | sed -nE 's/.*size: ([0-9]+)x.*/\1/p' | tail -1)
"$adb" shell wm density $((w * 160 / 760)) # 760 dp wide: past the 720 dp where the strip shows
sleep 4
# Started again, so it lays out at the new width (brought to the front, it kept the phone layout).
"$adb" shell am force-stop "$id"; "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null; sleep 2
for _ in 1 2 3 4 5; do has 'resource-id="space-spp1"' && break; has 'resource-id="back"' && tap back; sleep 1; done
tap space-spp1
wait_for 'resource-id="tab-tbp1"' "no tab strip at 760 dp"
tap tab-tbp1
wait_for "resource-id=\"terminal-$t1\"" "tab 1 did not show"
say "echo four"
snap wide-before
probe tabstrip tab-tbp2 'has "resource-id=\"terminal-$t2\"" && ! has "resource-id=\"terminal-$t1\""'
[ -z "$lost" ] || { echo "FAIL: a first tap was lost:$lost"; exit 1; }
echo "ok: every first tap was taken"
