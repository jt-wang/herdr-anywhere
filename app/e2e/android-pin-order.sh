#!/bin/sh
# The order of Pinned, with real touches on Android, against an isolated herdr-core and the companion on loopback
# (10.0.2.2 from the emulator). Five stand-in Claude panes are pinned: four with their places in the label
# herdr-anywhere.pin-order, one with herdr.pinned alone, as Herdr Desktop App pins. Passes when:
#  - Pinned shows them by place, the one pinned in Herdr Desktop App last, and the phone wrote its place;
#  - a row held still and dragged up, then one dragged down, lands where it was let go, the list standing still, and
#    only the moved agent's label changes; a status change moves nothing;
#  - a tap opens the agent; held still and let go, the row's menu opens and nothing else;
#  - a quick swipe over a row scrolls the list and moves no row; with the top of Pinned scrolled out of sight, a row
#    taken to the top edge scrolls the list itself and lands first;
#  - another phone's move (a label written with hc) shows; the app started again shows the same order, writing nothing;
#  - unpinned in Herdr Desktop App (herdr.pinned removed), the agent's place goes too.
# With a folder for screenshots (and SHOT_TAG, e.g. android360-light): the list at rest, a row held mid-drag, and
# just after the drop. BEFORE=1 takes the screenshots of a build without the drag (at rest, and after a status change)
# and checks nothing else.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-pin-order.sh APK [folder for screenshots]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1; shots=${2:-}; tag=${SHOT_TAG:-android}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
readonly id
port=7891; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmpoXXXX)
mkdir -p "$work/u/src" "$work/state" "$work/config" "$work/data" "$work/bin"
: > "$work/u/.zshrc"
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
(cd "$app/e2e/fake-claude" && GO111MODULE=off go build -o "$work/bin/claude" main.go)
export HC_SESSION="hmpinorder-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh PATH="$work/bin:$PATH"
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/u/.gitconfig"

# Five pinned agents in spaces of their own (names made up), and spaces with shells under them, so the list scrolls.
# name, its status first, the status it goes to after some seconds (or -), those seconds, then its labels.
agent() {
  n=$1 st=$2 then=$3 after=$4; shift 4
  mkdir -p "$work/u/src/$n"
  [ "$then" = - ] && then=
  "$hc" run --bg --keep --cwd "$work/u/src/$n" --name "$n" --label herdr.space="sp$n" --label herdr.tab="tb$n" "$@" -- \
    env FAKE_STATE="$st" FAKE_THEN="$then" FAKE_AFTER="$after" FAKE_MSG="Bash: npm test" "$work/bin/claude" >/dev/null
}
P=herdr-anywhere.pin-order
agent orchard_api idle - 0 --label herdr.pinned=1 --label "$P=i"
agent ledger working blocked "${WAIT_AFTER:-150}" --label herdr.pinned=1 --label "$P=r"
agent field_notes idle - 0 --label herdr.pinned=1 --label "$P=v"
agent tide_mobile blocked - 0 --label herdr.pinned=1 --label "$P=x"
agent lantern_web working - 0 --label herdr.pinned=1
# Enough spaces that the list scrolls well past Pinned on a tall phone too (step 6b).
for s in moss_docs quarry_cli harbor_ops kiln_site birch_sdk ember_bot pine_relay copper_ui willow_db slate_jobs fern_auth dune_maps; do
  mkdir -p "$work/u/src/$s"; "$hc" run --bg --keep --cwd "$work/u/src/$s" --label herdr.space="sp$s" --label herdr.tab="tb$s" >/dev/null
done
addr() { "$hc" get "$1" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["address"])'; }
A=$(addr orchard_api); B=$(addr ledger); C=$(addr field_notes); D=$(addr tide_mobile); E=$(addr lantern_web)
label() { "$hc" get "$1" --json | python3 -c 'import json,sys; print(json.load(sys.stdin).get("labels", {}).get(sys.argv[1], ""))' "$2"; }
labels() { for t in "$@"; do "$hc" get "$t" --json | python3 -c 'import json,sys; print(sorted(json.load(sys.stdin).get("labels", {}).items()))'; done; }

"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
# Keeps the exit status: a step that crashed (set -eu) must not end as a pass.
cleanup() { st=$?; kill "$serve_pid" 2>/dev/null; "$hc" server stop >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" "${state:?}"; exit $st; }
trap cleanup EXIT
trap 'exit 1' INT TERM
sleep 1
invite=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: no invite"; exit 1; }
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true

xml() { "$adb" shell rm -f /sdcard/hm.xml; "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml 2>/dev/null || true; }
bounds() { tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/'; }
wait_for() { b=""; for _ in $(seq 1 ${2:-30}); do b=$(xml | bounds "$1"); [ -n "$b" ] && return 0; sleep 1; done; return 1; }
tap() {
  wait_for "$1" || { echo "FAIL: no $1 on screen"; xml | tr '>' '\n' | grep -oE 'resource-id="[^"]+"' | head -30; exit 1; }
  set -- $b; "$adb" shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2))
}
shot() { [ -n "$shots" ] && { mkdir -p "$shots"; "$adb" exec-out screencap -p > "$shots/$tag-$1.png"; } || true; }
# The pinned agents' addresses, top to bottom, by where their rows are on screen.
order() {
  xml > "$work/ui.xml"
  python3 -I - "$work/ui.xml" <<'EOF'
import re, sys
x = open(sys.argv[1]).read()
rows = []
for m in re.finditer(r'<node [^>]*resource-id="pinned-agent-([^"]+)"[^>]*bounds="\[\d+,(\d+)\]\[\d+,(\d+)\]"', x):
    rows.append((int(m.group(2)), m.group(1)))
print(' '.join(a for _, a in sorted(rows)))
EOF
}
mid() { m=$(xml | bounds "pinned-agent-$1" | awk '{print int(($1+$3)/2), int(($2+$4)/2), $4-$2}'); [ -n "$m" ] || { echo "FAIL: no row pinned-agent-$1 on screen" >&2; kill -TERM $$; }; echo "$m"; }
# The top of the Spaces header (its + button), to tell whether the list scrolled.
spaces_y() { xml | bounds space-new | awk '{print $2}'; }
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
ev() { "$adb" shell input motionevent "$@"; }
# A finger down on row $1, held $2 ms still, moved by $3 px in 8 steps; `hold` (an action) runs before it lifts.
drag() {
  set -- $(mid "$1") "$2" "$3" "${4:-}"; x=$1 y=$2 hold=$4 dy=$5 before=$6
  ev DOWN "$x" "$y"
  python3 -c "import time; time.sleep($hold/1000)"
  i=1; while [ $i -le 8 ]; do ev MOVE "$x" $((y + dy * i / 8)); i=$((i + 1)); sleep 0.05; done
  sleep 0.3; [ -n "$before" ] && $before
  ev UP "$x" $((y + dy)); sleep 2
}
is() { [ "$1" = "$2" ] && say "ok: $3" || say "FAIL: $3: got [$1], wanted [$2]"; }

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
# A slow emulator drops typed characters (the companion then refuses the code): e2e/android-type-link.sh types the
# invite in short pieces and reads the field back until it holds all of it.
"$(dirname "$0")/android-type-link.sh" "$invite"
# On a narrow screen (360 dp) the keyboard covers the Pair button: close it first (back closes only the keyboard).
"$adb" shell dumpsys window InputMethod | grep -q "isOnScreen=true" && { "$adb" shell input keyevent 4; sleep 1; }
tap pair
# The pairing screen shows the invite: what is on screen goes to the log with the secret after #pair= cut out.
wait_for "pinned-agent-$E" 40 || {
  x=$(xml)
  if printf '%s' "$x" | grep -qF 'resource-id="pair-link"'; then echo "FAIL: Pinned did not show $E, still on the pairing screen"
  else echo "FAIL: Pinned did not show $E"; fi
  printf '%s' "$x" | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/' | head -20
  exit 1
}
sleep 3
shot rest

if [ "${BEFORE:-}" = 1 ]; then
  echo "info: before: Pinned reads [$(order)] for [$A $B $C $D $E]"
  # The working one asks for something: before, Pinned moved it to the top.
  for _ in $(seq 1 200); do xml | grep -q "resource-id=\"needs-$B\"" && break; sleep 1; done
  sleep 2; echo "info: before, after $B started waiting: [$(order)]"; shot status
  exit 0
fi

# 1. By place; the agent pinned in Herdr Desktop App last, and given a place after the others.
is "$(order)" "$A $B $C $D $E" "Pinned is in the order of the places, the one pinned in Herdr Desktop App last"
pe=$(label "$E" "$P")
python3 -c 'import sys; sys.exit(0 if sys.argv[1] > "x" else 1)' "$pe" && say "ok: the phone gave $E a place after the last ($pe)" || say "FAIL: $E's place is [$pe], not after x"
is "$(label "$E" herdr.pinned)" 1 "herdr.pinned stays as Herdr Desktop App wrote it"

# 2. Held still, then dragged up from 4th to 2nd: lands there, the list doesn't move, only its label changes.
y0=$(spaces_y)
set -- $(mid "$D"); dy4=$2; set -- $(mid "$B"); dy2=$2
drag "$D" 800 $((dy2 - dy4 - 10)) "shot drag"
shot dropped
is "$(order)" "$A $D $B $C $E" "a row held and dragged up lands where it was let go"
is "$(spaces_y)" "$y0" "the list stood still while the row was dragged"
is "$(label "$A" "$P") $(label "$B" "$P") $(label "$C" "$P")" "i r v" "the agents it passed keep their places"
pd=$(label "$D" "$P")
python3 -c 'import sys; sys.exit(0 if "i" < sys.argv[1] < "r" else 1)' "$pd" && say "ok: its own place is now between its neighbours' ($pd)" || say "FAIL: its place is [$pd]"

# 3. Dragged down, from 1st to 4th.
# Its middle onto the middle of the row now 4th: it takes that row's place.
set -- $(mid "$A"); y1=$2; set -- $(mid "$C"); y3=$2
drag "$A" 800 $((y3 - y1))
is "$(order)" "$D $B $C $A $E" "a row held and dragged down lands where it was let go"
is "$(spaces_y)" "$y0" "the list stood still again"

# 4. A tap opens the agent.
tap "pinned-agent-$C"
wait_for pane-header 10 && say "ok: a tap on a Pinned row opens its agent" || say "FAIL: a tap did not open the agent"
"$adb" shell input keyevent 4; sleep 2
wait_for "pinned-agent-$C" 10 || { echo "FAIL: home did not come back"; exit 1; }

# 5. Held still and let go: the menu, and nothing opens behind it.
set -- $(mid "$C")
ev DOWN "$1" "$2"; sleep 1.2; ev UP "$1" "$2"; sleep 2
xml | grep -q 'text="Unpin"' && say "ok: held still and let go, the row's menu opens" || say "FAIL: no menu after a hold"
xml | grep -q 'resource-id="pane-header"' && say "FAIL: the hold opened the agent too" || say "ok: the hold opened nothing else"
"$adb" shell input keyevent 4; sleep 1
is "$(order)" "$D $B $C $A $E" "the menu moved nothing"

# 6. A quick swipe up over a row scrolls the list and moves no row; then back.
set -- $(mid "$B")
"$adb" shell input swipe "$1" "$2" "$1" $(($2 - 600)) 250; sleep 2
y1=$(spaces_y)
[ -n "$y1" ] && [ "$y1" -lt "$y0" ] && say "ok: a quick swipe over a Pinned row scrolls the list" || say "FAIL: the swipe did not scroll ($y0 → $y1)"
"$adb" shell input swipe 540 600 540 1800 250; sleep 2
is "$(order)" "$D $B $C $A $E" "the swipe moved no row"

# 6b. The list scrolled so the top of Pinned is out of sight: a row held and taken to the top edge scrolls the list
# itself and lands first.
# Scrolled by as much as puts the first two Pinned rows above the list's top (Needs you may be above Pinned), so E
# lands first only if the list scrolls back while it is held; with a finger put down on the Spaces header (nothing
# there to press), moved up and rested, so no fling.
top=$(xml | bounds settings | awk '{print $4}')
set -- $(mid "$B"); by=$(($2 - top + $3))
sy=$(spaces_y); ev DOWN 300 $((sy + 20))
i=1; while [ $i -le 8 ]; do ev MOVE 300 $((sy + 20 - by * i / 8)); i=$((i + 1)); sleep 0.05; done
sleep 0.4; ev UP 300 $((sy + 20 - by)); sleep 2
shot edge-scrolled
# The second Pinned row is gone or cut by the list's top (uiautomator clips bounds to what shows).
d=$(xml | bounds "pinned-agent-$B" | awk '{print $2}')
[ -z "$d" ] || [ "$d" -le $((top + 2)) ] || say "FAIL: 6b's setup: the list did not scroll the first two Pinned rows out of sight (second row's top at $d, the list's at $top)"
set -- $(mid "$E"); x=$1 y=$2
echo "info: edge: E at y=$y, the list's top at $top, order [$(order)]"
ev DOWN "$x" "$y"; sleep 0.8
i=1; while [ $i -le 8 ]; do ev MOVE "$x" $((y + (top + 20 - y) * i / 8)); i=$((i + 1)); sleep 0.05; done
sleep 1; shot edge-held; sleep 2; ev MOVE "$x" $((top + 18)); sleep 0.5; ev UP "$x" $((top + 18)); sleep 2
shot edge-dropped
"$adb" shell input swipe 540 600 540 1800 250; sleep 2
is "$(order)" "$E $D $B $C $A" "a row taken to the top edge scrolls the list up and lands first"

# 7. A status change moves nothing (ledger starts waiting WAIT_AFTER, 150, seconds after it started).
for _ in $(seq 1 180); do xml | grep -q "resource-id=\"needs-$B\"" && break; sleep 1; done
xml | grep -q "resource-id=\"needs-$B\"" || say "FAIL: $B never started waiting"
sleep 2; shot status
is "$(order)" "$E $D $B $C $A" "an agent that starts waiting keeps its place in Pinned"

# 8. Another phone moves one: this one follows.
"$hc" label "$C" "$P=1" >/dev/null; sleep 3
is "$(order)" "$C $E $D $B $A" "a move on another phone shows here"

# 9. Started again: the same order, and nothing written.
before=$(labels "$A" "$B" "$C" "$D" "$E")
"$adb" shell am force-stop "$id"; "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
wait_for "pinned-agent-$E" 40 || say "FAIL: Pinned did not come back after a restart"
sleep 3
is "$(order)" "$C $E $D $B $A" "the app started again shows the same order"
[ "$before" = "$(labels "$A" "$B" "$C" "$D" "$E")" ] && say "ok: starting again wrote no label" || say "FAIL: starting again changed labels"

# 10. Unpinned in Herdr Desktop App: its place goes too.
"$hc" label "$A" herdr.pinned= >/dev/null; sleep 3
is "$(label "$A" "$P")" "" "an agent unpinned in Herdr Desktop App loses its place"
is "$(order)" "$C $E $D $B" "and leaves Pinned"
exit $failed
