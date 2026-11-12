#!/bin/sh
# A sub-agent kept as a tab, on an Android emulator at 360 dp wide, against a real Companion and herdr-core
# (e2e/subkeep-core.sh: an agent with two sub-agents, fake data only). For each theme, light then dark, with a sub-agent
# of its own: its row on home opens a peek, Keep makes it a tab, and back on home its row opens that tab, with no peek
# and no second Keep. Then the switcher. A screenshot at each step, for the before-and-after pictures: run on the commit
# before the fix, the second tap shows the peek again and Keep makes a second tab. Passes when, in both themes, the
# second tap opened the tab, the shared layout has one tab for the sub-agent, and it still runs.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-subkeep.sh APK COMPANION OUT-DIR
#   OUT-DIR gets <theme>-1-home.png, -2-peek.png, -3-kept.png, -4-second-tap.png, -5-switcher.png.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; bin=$2; out=$3
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$app/e2e/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
. "$app/e2e/android-lib.sh"   # at, tap
. "$app/e2e/subkeep-core.sh"  # sk, sk_start, sk_tabs, sk_state, sk_stop
. "$app/e2e/companion.sh"     # companion_start, companion_stop
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7781; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$out"
# Screen density and dark mode as found, put back on the way out.
density=$("$adb" shell wm density | tr -d '\r' | sed -n 's/^Override density: //p')
night=$("$adb" shell cmd uimode night 2>/dev/null | tr -d '\r' | sed -n 's/^Night mode: //p'); case $night in yes|no|auto) ;; *) night=no;; esac
cleanup() {
  trap - EXIT; trap '' INT TERM HUP
  set +e
  status=${1:-0}
  companion_stop || status=1
  if [ -n "$density" ]; then "$adb" shell wm density "$density"; else "$adb" shell wm density reset; fi >/dev/null 2>&1
  "$adb" shell cmd uimode night "$night" >/dev/null 2>&1
  sk_stop
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
  exit "$status"
}
trap 'adb_exit $? cleanup $?' EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

sk_start "$work" "$run"
companion_start "$work/serve.log" sk "$bin" serve --listen "127.0.0.1:$port" --hc "$sk_hc" --state "$work/companion" || exit 1
invite=$(sk "$bin" pair --url "http://10.0.2.2:$port" --state "$work/companion" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }

# 360 dp wide: the density that makes the screen's width 360 dp.
px=$("$adb" shell wm size | tr -d '\r' | tail -1 | awk '{print $NF}' | cut -dx -f1)
"$adb" shell wm density $((px * 160 / 360)) >/dev/null
"$adb" shell cmd uimode night no >/dev/null
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
"$app/e2e/android-type-link.sh" "$invite"
tap pair

shown() { # ID SECONDS: the view shows within SECONDS
  end=$(($(date +%s) + $2))
  while [ "$(date +%s)" -lt "$end" ]; do [ -n "$(at "$1")" ] && return 0; sleep 1; done
  return 1
}
shot() { "$adb" exec-out screencap -p > "$out/$theme-$1.png"; }
home() { # back to home, where the sub-agent's row is: Back while it isn't (Back on home would leave the app)
  for _ in 1 2 3 4; do [ -n "$(at "sub-$kid")" ] && return 0; "$adb" shell input keyevent KEYCODE_BACK; sleep 1.5; done
  shown "sub-$kid" 10
}
# A step that did not come: the screen as it was, the ids on it, and what herdr-core and the Companion had then.
fail() {
  echo "FAIL: $theme: $1"
  "$adb" exec-out screencap -p > "$out/$theme-fail.png" || true
  echo "on screen:"; dump; "$adb" shell cat /sdcard/hm.xml 2>/dev/null | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | grep -vE 'pair=|herdr[0-9]:|token=' | head -40 | sed 's/^/  /'
  echo "herdr-core:"; sk "$sk_hc" ls --json | python3 -c 'import json,sys; [print("  ", t["address"], t.get("parent"), t["state"], t.get("labels")) for t in json.load(sys.stdin)["terminals"]]' || true
  echo "the Companion's log ends:"; grep -vE " GET /assets/" "$work/serve.log" | tail -8 | sed -E 's/(token|pair)=[^ &]+/\1=…/g; s/^/  /'
  exit 1
}
failed=0
keep() { # THEME KID
  theme=$1 kid=$2
  shown "sub-$kid" 60 || fail "home does not list the sub-agent $kid"
  sleep 1; shot 1-home
  tap "sub-$kid"
  shown peek-keep 15 || fail "the sub-agent's row opened no peek"
  sleep 1; shot 2-peek
  tap peek-keep
  shown "terminal-$kid" 15 && kept=shown || kept="not shown"
  sleep 2; shot 3-kept
  home || fail "no way back to home"
  tap "sub-$kid"
  sleep 3
  if [ -n "$(at peek-keep)" ]; then second=peek; elif [ -n "$(at "terminal-$kid")" ]; then second=tab; else second=neither; fi
  shot 4-second-tap
  # As before the fix: the peek again, and Keep once more.
  if [ "$second" = peek ]; then tap peek-keep; sleep 3; fi
  if shown switcher 10; then tap switcher; sleep 2; shot 5-switcher; fi
  sleep 2
  tabs=$(sk_tabs "$kid"); state=$(sk_state "$kid")
  echo "$theme: after Keep its tab $kept; tapping its row again opened: $second; tabs for it in the shared layout: $tabs; it is $state"
  [ "$second" = tab ] && [ "$tabs" = 1 ] && [ "$state" = live ] || failed=1
}
keep light "$sk_kid1"
# Dark: the app opened again on home, the second sub-agent not kept yet.
"$adb" shell am force-stop "$id"
"$adb" shell cmd uimode night yes >/dev/null; sleep 2
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
keep dark "$sk_kid2"
[ "$failed" = 0 ] && echo PASS || { echo "FAIL: a sub-agent kept as a tab was offered as a peek again, or has more than one tab"; exit 1; }
