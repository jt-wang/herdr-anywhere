#!/bin/sh
# The app's own notifications on an iOS simulator (XCUITest, e2e/ios-uitest Notifications): the question about
# notifications answered Allow, then Don't Allow on a fresh install; an agent's question while the app is away; and how
# long after the app leaves the screen a new version still reaches the person, since without a push server the app
# posts only while iOS lets it run. For each wait the computer's daily check "finds" a new version that many seconds
# after the app left (its update.json written, as the check writes it), and Notification Center is read 15 s later.
# The app is built from ios-e2e.tsx with e2e-ask-notifications (it asks, as a person's app does) and reads the
# computer's answer each second (an hour in the app people get). The Companion asks a closed port for releases:
# nothing goes to GitHub. Fake data only.
# Usage: SIM_UDID=<udid> e2e/ios-update-notify.sh [folder for results and screenshots]
#   WAITS (seconds, "0 5 10 20 30 60") and ROUNDS (3) set the measurement.
# Before the app had this (its words have no updateNotifyTitle), nothing is asked or posted: the suite passes when
# nothing shows, and keeps the same screenshots, as the picture of before.
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, python3, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-}
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
before=$(grep -q updateNotifyTitle "$app/src/strings/en.ts" || echo 1)
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$app/nsh.json")
port=7764; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
[ -n "$out" ] || out=$work/out
mkdir -p "$out"; out=$(cd "$out" && pwd)
(cd "$app/../companion" && CGO_ENABLED=0 go build -buildvcs=false -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
mkdir -p "$work/h/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/h/.zshrc"
placeholder=$(cat "$app/e2e/pair.ts")
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  LANG=en_US.UTF-8 HC_SESSION="hmiosnote-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" "$@"; }
. "$app/e2e/companion.sh"  # companion_start, companion_stop
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e # every step runs, even after one fails (a companion already gone)
  companion_stop || status=1
  L "$hc" server stop >/dev/null 2>&1 || true
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP

# The agent: a shell that asks a question (OSC 7501, as agents report it) once the test creates $work/ask.
L "$hc" run --bg --keep --cwd "$work/h/src/acme-api" --label herdr.space=spnotify --label herdr.tab=tbnotify >/dev/null
term=$(L "$hc" ls --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["terminals"][0]["address"])')
q=$(printf 'Which port?' | base64)
printf '%s\n' "while [ ! -f '$work/ask' ]; do sleep 0.5; done; printf '\033]7501;state=working\033\\\\'; sleep 2; printf '\033]7501;state=blocked:kind=question:msg=$q\033\\\\'" > "$work/h/src/acme-api/hm-ask.sh"

# The person said yes to daily checks; the last check found this version. Releases at a closed port: no GitHub.
printf '{"checks":"on","latest":"%s"}' "$version" > "$state/update.json"
closed=9; while nc -z 127.0.0.1 "$closed" 2>/dev/null; do closed=$((closed + 1)); done
companion_start "$work/serve.log" L "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" \
  --releases "http://127.0.0.1:$closed" || exit 1
link=$(L "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl ui "$SIM_UDID" appearance light
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)

# A fresh install, its storage written before it first opens: it asks about notifications, and reads each second.
fresh() {
  xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
  xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
  store="$(xcrun simctl get_app_container "$SIM_UDID" "$id" data)/Library/Application Support/nsh/$id/storage.json"
  mkdir -p "$(dirname "$store")"
  printf '{"e2e-ask-notifications":"1","e2e-update-every":"1000"}' > "$store"
}
uitest() { # NAME: runs testNAME, its words to $out/NAME.txt
  rc=0
  TEST_RUNNER_OUT="$out/$1.txt" TEST_RUNNER_SHOTS="$out/$1" TEST_RUNNER_UPDATE_JSON="$state/update.json" TEST_RUNNER_ASK="$work/ask" TEST_RUNNER_BEFORE="$before" \
    TEST_RUNNER_WAITS="${WAITS:-0 5 10 20 30 60}" TEST_RUNNER_ROUNDS="${ROUNDS:-3}" \
    perl -e 'alarm 3000; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
    -only-testing:"UITests/Notifications/test$2" -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest-$1.log" 2>&1 || rc=$?
  sed 's/^/  /' "$out/$1.txt" 2>/dev/null || true
  [ "$rc" = 0 ] || { echo "FAIL: $1: xcodebuild test exit $rc"; grep -E "error:|XCTAssert|failed" "$work/uitest-$1.log" | head -8; exit 1; }
}

fresh
L "$hc" send "$term" "sh hm-ask.sh" >/dev/null
if [ -n "$before" ]; then
  echo "--- before: no question, and nothing posted while away"
  WAITS=0 ROUNDS=1 uitest before Before
  echo PASS
  exit 0
fi
echo "--- Allow, an agent's question, and how long a new version still reaches the person"
uitest allow Allow
# What the app read while away: the Companion logs each /api/info with the time, so the last read after the app left
# says how long iOS let it run (a read each second).
python3 - "$out/allow.txt" "$work/serve.log" > "$out/measured.txt" <<'PY'
import re, sys, time
rounds = [dict(kv.split('=') for kv in l.split() if '=' in kv) | {'posted': l.split()[-1] == 'posted'}
          for l in open(sys.argv[1]) if l.startswith('round=')]
reads = []
for l in open(sys.argv[2]):
    m = re.match(r'(\d{4}/\d\d/\d\d \d\d:\d\d:\d\d) \S+ GET /api/info ', l)
    if m: reads.append(int(time.mktime(time.strptime(m.group(1), '%Y/%m/%d %H:%M:%S'))))
print('wait_s posted round last_read_after_leaving_s')
by = {}
for r in rounds:
    away, told = int(r['away']), int(r['told'])
    after = [t - away for t in reads if away <= t <= told + 15]
    print(r['wait'], 'yes' if r['posted'] else 'no', r['round'], max(after) if after else 'none')
    by.setdefault(int(r['wait']), []).append(r['posted'])
print()
for w in sorted(by): print(f'after {w} s away: posted {sum(by[w])} of {len(by[w])}')
PY
cat "$out/measured.txt"
grep -q '^agent alert: ' "$out/allow.txt" && ! grep -q '^agent alert: none' "$out/allow.txt" || { echo "FAIL: no agent alert in Notification Center"; exit 1; }
[ -f "$out/allow/update-home.png" ] || { echo "FAIL: no new-version notification was posted at any wait"; exit 1; }
echo "ok: Allow; an agent's question and a new version were posted while the app ran; a tap opened home with the notice"

echo "--- Don't Allow"
fresh
uitest deny Deny
echo "ok: Don't Allow; the app goes on and asks nothing when opened again"
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
echo PASS
