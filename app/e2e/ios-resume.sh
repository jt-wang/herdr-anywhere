#!/bin/sh
# Switching away from the app and back, on an iOS simulator: the app on the chat page of a fake Claude Code (the 2.1.322
# fixtures, e2e/fake-claude.py), paired directly (PAIRING=direct, the companion on 127.0.0.1) or through Tailcat's real
# relays (PAIRING=tailcat), is sent to Settings for each of AWAY seconds and brought back (XCUITest, e2e/ios-uitest
# Resume). It notes every line that shows on screen after the switch back, with when, beside the companion's log of the
# phone's connection, both timed to the millisecond on this Mac. Against an isolated herdr-core; fake data only.
# The reading is checked twice in the same run: STEADY seconds with no switch must see nothing new, and the companion
# stopped at the end must show the banner. Measures; it does not pass or fail on what the switch shows.
# Usage: SIM_UDID=<udid> [PAIRING=direct|tailcat] [AWAY=10,60] [LANG_TAG=en] [APPEARANCE=light|dark]
#        [HM_RESUME_APP=<built Herdr.app>] e2e/ios-resume.sh <out folder>
# HM_RESUME_APP installs that build (from ios-e2e.tsx, e.g. an older commit's, for a before/after) instead of building.
# APPEARANCE is put back as it was after the run.
# Writes <out>/timeline.txt, <out>/screen.txt, <out>/companion.log (token and addresses masked), <out>/probes.txt (each
# check of the line when back in the front: ms, answered; none from a build before it) and <out>/shot-*.png.
# HM_RESUME_EXPECT=calm fails the run when, after a switch back, the screen says the computer doesn't answer (the banner
# with the command and Pair again, en only), though the computer ran the whole time: the user's report, 0.1.26.
# Stops the run when memory free goes under 25% or the swap disk under 5 GB; on a CI runner (GITHUB_ACTIONS) it only
# records them, as the limits are this Mac's. Needs go, nsh, hc, python3, xcodegen, Xcode.
set -eu
mask='s/token=[^ &]+/token=…/g; s/#pair=[A-Za-z0-9_-]+/#pair=…/g; s/herdr1:[A-Za-z0-9_-]+/herdr1:…/g; s/ (127\.0\.0\.1|::1) (GET|POST)/ @loopback:\1@ \2/; s/[0-9a-f:.]{7,} (GET|POST)/<addr> \1/; s/@loopback:([^@]*)@/\1/'
# memory_pressure's free percentage (its last line), on stdin; nothing when it has none.
free_of() { tail -1 | grep -oE '[0-9]+%$' | tr -d %; }
# The English banner's words for a computer that doesn't answer (lostNoAnswer), from the app's own strings.
lost_words() { sed -nE 's/^ *lostNoAnswer: "(.*)",$/\1/p' "$(dirname "$0")/../src/strings/en.ts"; }
# The screen's lines and the companion's, by time, as seconds since the first, masked.
timeline() { sort -n "$@" 2>/dev/null | grep -vE ' GET /assets/' | sed -E "$mask" | awk 'NR == 1 { t0 = $1 } { $1 = sprintf("%8.2f", $1 - t0); print }'; }
# The app's checks of the line when back in the front, from its storage.json (ios-e2e.tsx writes e2e-probes), stamped.
probes_of() {
  python3 -I -c 'import json, os, sys
d = json.load(open(sys.argv[1])) if os.path.exists(sys.argv[1]) else {}
for ms, ok, at in json.loads(d.get("e2e-probes") or "[]"): print("%.3f probe %d ms %s" % (at / 1000, ms, "answered" if ok else "unanswered"))' "$1"
}
# The lines on the screen record ($1) that, after a switch back (phases away<N>), show the words ($2) of the banner
# that says the computer doesn't answer.
lost_after_switch() { grep -E '^[0-9.]+ away[0-9]+ \+[0-9.]+ seen ' "$1" | grep -F -- "$2"; }
# HM_RESUME_SELFTEST=1: checks these on made-up input, with no simulator.
if [ -n "${HM_RESUME_SELFTEST:-}" ]; then
  bad=0
  check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; bad=1; fi; }
  check "free from memory_pressure" "$(printf 'The system has 25769803776 (6291456 pages with a page size of 4096).\nSystem-wide memory free percentage: 44%%\n' | free_of)" 44
  check "free from no output" "$(printf '' | free_of)" ''
  check "free from an error line" "$(printf 'memory_pressure: error\n' | free_of)" ''
  check "free from a last line with no percentage" "$(printf 'Pages free: 12345\n' | free_of)" ''
  d=$(mktemp -d)
  printf '%s\n' '100.250 away5 +0.50 seen 连不上电脑，下面是它上次说的。' '99.000 base 12 lines' > "$d/screen"
  printf '%s\n' '100.000 2026/01/02 03:04:05 192.0.2.7 GET /hc?token=sekrit 101' '100.100 2026/01/02 03:04:05 127.0.0.1 GET /hc?token=sekrit 101' \
    '100.200 pair: http://127.0.0.1:7781/#pair=abcDEF_12 herdr1:Zz9-x' '100.300 GET /assets/app.js' > "$d/serve"
  got=$(timeline "$d/screen" "$d/serve")
  check "timeline order and times" "$(printf '%s\n' "$got" | awk '{ print $1 }' | tr '\n' ' ')" "0.00 1.00 1.10 1.20 1.25 "
  check "timeline masks the token" "$(printf '%s' "$got" | grep -c sekrit)" 0
  check "timeline masks other addresses" "$(printf '%s' "$got" | grep -c '192.0.2.7')" 0
  check "timeline keeps the loopback" "$(printf '%s' "$got" | grep -c '127.0.0.1 GET')" 1
  check "timeline masks pairing" "$(printf '%s' "$got" | grep -cE 'abcDEF|Zz9')" 0
  check "timeline drops assets" "$(printf '%s' "$got" | grep -c assets)" 0
  check "timeline keeps the screen text" "$(printf '%s' "$got" | grep -c '连不上电脑')" 1
  printf '%s' '{"e2e-pair":"x","e2e-probes":"[[412,true,1700000000123],[3001,false,1700000060000]]"}' > "$d/storage.json"
  check "probes from the app's storage" "$(probes_of "$d/storage.json" | tr '\n' '|')" "1700000000.123 probe 412 ms answered|1700000060.000 probe 3001 ms unanswered|"
  printf '%s' '{"e2e-pair":"x"}' > "$d/none.json"
  check "no probes from a build without them" "$(probes_of "$d/none.json")" ''
  check "no probes without storage" "$(probes_of "$d/missing.json")" ''
  no="Your computer doesn't answer. Run this on it:"
  printf '%s\n' "100.0 steady +1.00 seen $no" "101.0 away60 +0.80 seen Reconnecting to your computer…" \
    "102.0 away60 +1.20 seen $no" "103.0 stopped +4.00 seen $no" "104.0 away10 usable +2.00" > "$d/lost"
  check "lost after a switch: only the away phases' banner" "$(lost_after_switch "$d/lost" "$no" | cut -d' ' -f1)" 102.0
  check "lost after a switch: none in a calm record" "$(grep -v '^102' "$d/lost" > "$d/calm"; lost_after_switch "$d/calm" "$no")" ''
  check "lost after a switch: the words from en.ts" "$(lost_words)" "$no"
  rm -rf "$d"
  [ "$bad" = 0 ] && echo "PASS: self-test" || { echo "FAIL: self-test"; exit 1; }
  exit 0
fi
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
out=${1:?give the folder for the results}
pairing=${PAIRING:-direct}
case $pairing in direct|tailcat) ;; *) echo "PAIRING is direct or tailcat" >&2; exit 2 ;; esac
app=$(cd "$(dirname "$0")/.." && pwd)
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
fixtures="$app/../companion/internal/chat/claude/testdata/claude-code/2.1.322"
mkdir -p "$out"; out=$(cd "$out" && pwd)
port=7781; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/h/src/acme-api" "$work/state" "$work/config" "$work/data" "$work/claude"
: > "$work/h/.zshrc"
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  LANG=en_US.UTF-8 HC_SESSION="hmresume-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" CLAUDE_CONFIG_DIR="$work/claude" "$@"; }
serve_pid=; guard_pid=; test_pid=; was_look=
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  [ -n "$was_look" ] && xcrun simctl ui "$SIM_UDID" appearance "$was_look" && echo "appearance back to $(xcrun simctl ui "$SIM_UDID" appearance)"
  [ -n "$guard_pid" ] && kill "$guard_pid" 2>/dev/null
  [ -n "$test_pid" ] && kill "$test_pid" 2>/dev/null
  [ -n "$serve_pid" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  L "$hc" server stop >/dev/null 2>&1
  [ -s "$work/serve.log" ] && sed -E "$mask" "$work/serve.log" > "$out/companion.log"
  xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

# Memory free (memory_pressure's last line) and the swap disk's space, every 10 s; a reading that is not a number stops
# the run too.
free_pct() { memory_pressure -Q 2>/dev/null | free_of; }
swap_gb() { df -g /System/Volumes/VM 2>/dev/null | awk 'NR == 2 { print $4 }'; }
guard() {
  while :; do
    f=$(free_pct); g=$(swap_gb)
    case "$f$g" in ''|*[!0-9]*)
      [ -n "${GITHUB_ACTIONS:-}" ] && { echo "$(date +%T) memory unreadable ('$f' '$g')" >> "$out/memory.txt"; sleep 10; continue; }
      echo "STOP: memory or swap disk unreadable ('$f' '$g')" >&2; kill -TERM $$; exit ;;
    esac
    echo "$(date +%T) memory free $f% swap disk $g GB" >> "$out/memory.txt"
    if [ -z "${GITHUB_ACTIONS:-}" ] && { [ "$f" -lt 25 ] || [ "$g" -lt 5 ]; }; then echo "STOP: memory free $f%, swap disk $g GB" >&2; kill -TERM $$; exit; fi
    sleep 10
  done
}
: > "$out/memory.txt"
guard & guard_pid=$!

# Built and installed first: a Tailcat invite lasts 2 minutes.
(cd "$app/../companion" && CGO_ENABLED=0 go build -buildvcs=false -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
built=${HM_RESUME_APP:-}
if [ -z "$built" ]; then
  "$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
  built="$app/ios/build/Herdr.app"
fi
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
if [ -n "${APPEARANCE:-}" ]; then
  was_look=$(xcrun simctl ui "$SIM_UDID" appearance)
  case $was_look in light|dark) ;; *) echo "FAIL: cannot read the appearance ('$was_look')"; was_look=; exit 1 ;; esac
  xcrun simctl ui "$SIM_UDID" appearance "$APPEARANCE"
fi
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$built"
store="$(xcrun simctl get_app_container "$SIM_UDID" "$id" data)/Library/Application Support/nsh/$id/storage.json"
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
xcodebuild build-for-testing -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -destination "id=$SIM_UDID" \
  -derivedDataPath "$work/uitest/derived" > "$work/uitest/build.log" 2>&1 || { tail -20 "$work/uitest/build.log"; echo "FAIL: UITests build"; exit 1; }

L "$hc" run --bg --keep --cwd "$work/h/src/acme-api" --name claude --label herdr.space=spresume --label herdr.tab=tbresume \
  python3 "$app/e2e/fake-claude.py" "$fixtures/image.jsonl" >/dev/null
# Each line of the companion's log stamped with this Mac's clock, to the millisecond, as the UITest stamps its own.
stamp() { perl -MTime::HiRes=time -ne 'BEGIN { $| = 1 } printf "%.3f %s", time, $_'; }
if [ "$pairing" = tailcat ]; then
  (L "$work/herdr-anywhere" serve --tailcat --listen "127.0.0.1:$port" --hc "$hc" --state "$state" 2>&1 | stamp > "$work/serve.log") &
else
  (L "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" 2>&1 | stamp > "$work/serve.log") &
fi
serve_pid=$!
ready=.; [ "$pairing" = tailcat ] && ready='listening on Tailcat'
i=0; until grep -qE "$ready" "$work/serve.log" 2>/dev/null && curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do
  i=$((i + 1)); [ $i -gt 300 ] && { echo "FAIL: the companion did not start"; sed -E "$mask" "$work/serve.log" | tail -5; exit 1; }; sleep 0.1
done
if [ "$pairing" = tailcat ]; then
  link=$(L "$work/herdr-anywhere" pair --state "$state" 2>/dev/null | grep -o 'herdr1:[A-Za-z0-9_-]*' | head -1)
else
  link=$(L "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
fi
[ -n "$link" ] || { echo "FAIL: no pairing link or invite"; exit 1; }
mkdir -p "$(dirname "$store")"
python3 -c 'import json,sys; json.dump({"e2e-pair": sys.argv[2], "e2e-open-settings": "99"}, open(sys.argv[1], "w"))' "$store" "$link"
xcrun simctl launch "$SIM_UDID" "$id" -AppleLanguages "(${LANG_TAG:-en})" >/dev/null
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do
  i=$((i + 1)); [ $i -gt 120 ] && { echo "FAIL: the app never connected"; sed -E "$mask" "$work/serve.log" | tail -8; exit 1; }; sleep 1
done

screen="$out/screen.txt"; rm -f "$screen" "$screen.stop" "$out"/shot-*.png
TEST_RUNNER_OUT="$screen" TEST_RUNNER_SHOTS="$out/shot" TEST_RUNNER_SPACE=spresume TEST_RUNNER_AWAY="${AWAY:-10,60}" \
  TEST_RUNNER_STEADY="${STEADY:-20}" perl -e 'alarm 900; exec @ARGV' xcodebuild test-without-building -quiet \
  -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/Resume -destination "id=$SIM_UDID" \
  -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 &
test_pid=$!
# The UITest's last check: it asks for the companion to stop, and the banner must say so.
stopped=
while kill -0 "$test_pid" 2>/dev/null; do
  if [ -z "$stopped" ] && [ -e "$screen.stop" ]; then
    stopped=1; pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null
    perl -MTime::HiRes=time -e 'printf "%.3f companion stopped by the script\n", time' >> "$work/serve.log"
  fi
  sleep 0.2
done
rc=0; wait "$test_pid" || rc=$?; test_pid=
# The app's own record of its checks of the line, from its storage once it has stopped.
xcrun simctl terminate "$SIM_UDID" "$id" 2>/dev/null || true
probes_of "$store" > "$out/probes.txt" || true
cat "$out/probes.txt" >> "$screen"
timeline "$screen" "$work/serve.log" > "$out/timeline.txt"
sed 's/^/  /' "$out/timeline.txt" | tail -60
[ "$rc" = 0 ] || { echo "FAIL: the UITest's own checks failed (exit $rc)"; grep -E "error:|XCTAssert|failed" "$work/uitest/test.log" | head -8; exit 1; }
if [ "${HM_RESUME_EXPECT:-}" = calm ]; then
  [ "${LANG_TAG:-en}" = en ] || { echo "FAIL: HM_RESUME_EXPECT=calm reads the English banner; LANG_TAG is ${LANG_TAG}"; exit 1; }
  words=$(lost_words); [ -n "$words" ] || { echo "FAIL: no lostNoAnswer in src/strings/en.ts"; exit 1; }
  lost=$(lost_after_switch "$screen" "$words" || true)
  [ -z "$lost" ] || { echo "FAIL: after a switch back the app said the computer doesn't answer, while it ran:"; echo "$lost" | head -4; exit 1; }
  echo "ok: no 'doesn't answer' after a switch back"
fi
echo "DONE: $pairing, timeline in $out/timeline.txt"
