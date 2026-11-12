#!/bin/sh
# The order of Pinned with real touches on the iOS simulator (XCUITest, e2e/ios-uitest's PinOrder), as
# e2e/android-pin-order.sh does on Android: an isolated herdr-core with five stand-in Claude panes pinned (four with
# places in herdr-anywhere.pin-order, one with herdr.pinned alone, as Herdr Desktop App pins) and the companion on
# loopback. Passes when Pinned goes by place with the one pinned in Herdr Desktop App last and given a place; a row held
# and dragged up, then one dragged down, lands where it was let go, the list standing still, writing only its own label;
# a tap opens the agent; held still and let go, the menu opens and nothing else; a quick swipe scrolls and moves no row;
# with the top of Pinned scrolled out of sight, a row taken to the top edge scrolls the list itself and lands first.
# With a folder for screenshots (and SHOT_TAG, e.g. ios375-light): at rest, mid-drag, after the drop. BEFORE=1 with
# APP_ROOT=<a checkout of the version before>: only the screenshot at rest of that build.
# Usage: SIM_UDID=<udid> e2e/ios-pin-order.sh [folder for screenshots]
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
src=${APP_ROOT:-$app}
shots=${1:-}; tag=${SHOT_TAG:-ios}
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
port=7991; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/u/src" "$work/state" "$work/config" "$work/data" "$work/bin"
: > "$work/u/.zshrc"
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
(cd "$app/e2e/fake-claude" && GO111MODULE=off go build -o "$work/bin/claude" main.go)
placeholder=$(cat "$src/e2e/pair.ts")
# The isolated herdr-core and the companion get a home of their own; the builds below keep the real one (nsh, Go and
# Xcode read theirs from it).
L() { HC_SESSION="hmiospin-$$" HC_SOCKET="" HOME="$work/u" SHELL=/bin/zsh XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" GIT_CONFIG_GLOBAL="$work/u/.gitconfig" "$@"; }
# Keeps the exit status: a step that crashed (set -eu) must not end as a pass.
cleanup() {
  st=$?
  # serve runs under the L shell function, a subshell: its child goes first.
  [ -n "${serve_pid:-}" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  L "$hc" server stop >/dev/null 2>&1 || true
  printf "%s\n" "$placeholder" > "$src/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
  exit $st
}
trap cleanup EXIT INT TERM
P=herdr-anywhere.pin-order
agent() {
  n=$1 st=$2; shift 2
  mkdir -p "$work/u/src/$n"
  L "$hc" run --bg --keep --cwd "$work/u/src/$n" --name "$n" --label herdr.space="sp$n" --label herdr.tab="tb$n" "$@" -- env FAKE_STATE="$st" "$work/bin/claude" >/dev/null
}
agent orchard-api idle --label herdr.pinned=1 --label "$P=i"
agent ledger working --label herdr.pinned=1 --label "$P=r"
agent field-notes idle --label herdr.pinned=1 --label "$P=v"
agent tide-mobile blocked --label herdr.pinned=1 --label "$P=x"
agent lantern-web working --label herdr.pinned=1
# Enough spaces that the list scrolls well past Pinned (the edge step).
for s in moss-docs quarry-cli harbor-ops kiln-site birch-sdk ember-bot pine-relay copper-ui willow-db slate-jobs fern-auth dune-maps; do
  mkdir -p "$work/u/src/$s"; L "$hc" run --bg --keep --cwd "$work/u/src/$s" --label herdr.space="sp$s" --label herdr.tab="tb$s" >/dev/null
done
addr() { L "$hc" get "$1" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["address"])'; }
A=$(addr orchard-api); B=$(addr ledger); C=$(addr field-notes); D=$(addr tide-mobile); E=$(addr lantern-web)
label() { L "$hc" get "$1" --json | python3 -c 'import json,sys; print(json.load(sys.stdin).get("labels", {}).get(sys.argv[1], ""))' "$2"; }
L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0
until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do
  i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; exit 1; }
  sleep 0.1
done
link=$(L env HERDR_ANYWHERE_STATE="$work/companion" "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "pair printed no link" >&2; exit 1; }
printf "export const PAIR: string = '%s'\n" "$link" > "$src/e2e/pair.ts"
"$src/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$src/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$src/ios/build/Herdr.app"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
shot() { [ -n "$shots" ] && { mkdir -p "$shots"; perl -e 'alarm 40; exec @ARGV' xcrun simctl io "$SIM_UDID" screenshot "$shots/$tag-$1.png" >/dev/null 2>&1; } || true; }

cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
out="$work/steps"; : > "$out"
# Screenshots: at rest once the test has started, and 2 s after it marks the drag (the finger still down).
(
  until grep -q '^start ' "$out" 2>/dev/null; do sleep 0.3; [ -f "$work/done" ] && exit 0; done; shot rest
  until grep -q '^mark drag' "$out" 2>/dev/null; do sleep 0.3; [ -f "$work/done" ] && exit 0; done; sleep 2.2; shot drag
  until grep -q '^up ' "$out" 2>/dev/null; do sleep 0.3; [ -f "$work/done" ] && exit 0; done; shot dropped
) &
watcher=$!
rc=0
TEST_RUNNER_OUT="$out" TEST_RUNNER_AGENTS="$A $B $C $D $E" TEST_RUNNER_BEFORE="${BEFORE:-}" perl -e 'alarm 600; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" \
  -scheme UITests -only-testing:UITests/PinOrder -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 || rc=$?
touch "$work/done"; wait "$watcher" 2>/dev/null || true
if [ "$rc" != 0 ]; then echo "FAIL: the PinOrder test (xcodebuild test exit $rc)"; grep -E "error:|XCTAssert|failed" "$work/uitest/test.log" | head -8; cat "$out"; exit 1; fi
if [ "${BEFORE:-}" = 1 ]; then echo "info: before: $(cat "$out")"; exit 0; fi

failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
line() { grep "^$1 " "$out" | head -1 | cut -d' ' -f2-; }
rows() { line "$1" | sed 's/ spaces=.*//'; }
sp() { line "$1" | sed 's/.*spaces=//'; }
is() { [ "$1" = "$2" ] && say "ok: $3" || say "FAIL: $3: got [$1], wanted [$2]"; }
is "$(rows start)" "$A $B $C $D $E" "Pinned is in the order of the places, the one pinned in Herdr Desktop App last"
# The labels are read once the test is over, after every move (the UI test can't read them between steps).
pe=$(label "$E" "$P")
[ -n "$pe" ] && say "ok: the phone gave $E a place ($pe)" || say "FAIL: $E has no place"
is "$(label "$E" herdr.pinned)" 1 "herdr.pinned stays as Herdr Desktop App wrote it"
is "$(rows up)" "$A $D $B $C $E" "a row held and dragged up lands where it was let go"
is "$(sp up)" "$(sp start)" "the list stood still while the row was dragged"
is "$(rows down)" "$D $B $C $A $E" "a row held and dragged down lands where it was let go"
is "$(sp down)" "$(sp start)" "the list stood still again"
is "$(label "$B" "$P") $(label "$C" "$P")" "r v" "the agents passed keep their places"
pd=$(label "$D" "$P"); pa=$(label "$A" "$P")
# In the end order (E D B C A, after the edge step), each place comes after the one above.
python3 -c 'import sys; p = sys.argv[1:]; sys.exit(0 if all(a < b for a, b in zip(p, p[1:])) else 1)' "$pe" "$pd" r v "$pa" && say "ok: the places go in the order of Pinned ($pe $pd r v $pa)" || say "FAIL: places $pe $pd r v $pa"
is "$(line tap)" "opened=true" "a tap on a Pinned row opens its agent"
is "$(line menu)" "shown=true opened=false" "held still and let go, the row's menu opens, and nothing else"
is "$(rows after-menu)" "$D $B $C $A $E" "the menu moved nothing"
set -- $(line swipe | tr '=' ' '); [ "$4" -lt "$2" ] && say "ok: a quick swipe over a Pinned row scrolls the list ($2 → $4)" || say "FAIL: the swipe did not scroll ($2 → $4)"
is "$(rows after-swipe)" "$D $B $C $A $E" "the swipe moved no row"
set -- $(line edge-setup | tr '=' ' '); [ "$2" -le "$4" ] && say "ok: the edge step starts with the first two Pinned rows out of sight" || say "FAIL: the edge step's setup did not scroll the first two Pinned rows out of sight (second row at $2, list top $4)"
is "$(rows edge)" "$E $D $B $C $A" "a row taken to the top edge scrolls the list up and lands first"
exit $failed
