#!/bin/sh
# The chat view of the four agents on an iOS simulator, for crashes and hangs (XCUITest's taps, e2e/ios-uitest
# ChatFour): fake Claude Code, Codex, Pi and OpenCode 2 (companion/internal/testfake/fakeagent, replaying their
# recordings; a conversation of 40 turns from the start, a command that printed 500 lines), in an isolated
# herdr-core, against this branch's Companion. For each agent: open, the long output, a picture, scrolling the long
# conversation, the app in the background and back, the Companion stopped and started again. The app must stay one
# process from launch to the end: a crash fails with its crash report. The app is built from ios-e2e.tsx at this run's
# pairing link (as e2e/ios-chat.sh). Fake data only.
# Usage: SIM_UDID=<udid> [AGENTS="claude-code codex pi opencode"] [AWAY="10 60"] e2e/ios-chat-four.sh [folder for screenshots]
# AWAY: the seconds in the background, each for the first agent, the first for the others (60 is past the Companion's
# ping timeout, so the link is gone by then). Needs go, nsh, hc, python3, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
agents=${AGENTS:-claude-code codex pi opencode}
away=${AWAY:-10 60}
app=$(cd "$(dirname "$0")/.." && pwd)
companion="$app/../companion"
shots=${1:-}
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
chat="$companion/internal/chat"
recordings() {
  case $1 in
    claude-code) d="$chat/claude/testdata/claude-code/2.1.322"; echo "$d/turns.jsonl $d/steps.jsonl $d/image.jsonl" ;;
    codex) d="$chat/codex/testdata/codex/0.167.1"; echo "$d/turns.jsonl $d/steps.jsonl $d/files.jsonl" ;;
    pi) d="$chat/pi/testdata/pi/1.4.0"; echo "$d/turns.jsonl $d/steps.jsonl $d/files.jsonl" ;;
    opencode) d="$chat/opencode/testdata/opencode/2.2.0"; echo "$d/turns.json $d/steps.json $d/files.json" ;;
    *) echo "no agent $1" >&2; exit 2 ;;
  esac
}
port=7762; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX); ctl="$work/ctl"
mkdir -p "$work/h" "$work/state" "$work/config" "$work/data" "$work/claude" "$work/codex" "$work/pi" "$ctl"
: > "$work/h/.zshrc"; date > "$work/started"
(cd "$companion" && CGO_ENABLED=0 go build -buildvcs=false -o "$work/herdr-anywhere" ./cmd/herdr-anywhere &&
  CGO_ENABLED=0 go build -buildvcs=false -o "$work/fakeagent" ./internal/testfake/fakeagent)
placeholder=$(cat "$app/e2e/pair.ts")
# herdr-core gives every terminal the environment of what started it: the agents' folders are set here, for the
# fakes and for the Companion's readers alike.
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  LANG=en_US.UTF-8 HC_SESSION="hmchat4-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" CLAUDE_CONFIG_DIR="$work/claude" CODEX_HOME="$work/codex" \
  PI_CODING_AGENT_DIR="$work/pi" "$@"; }
serve_pid=; ctl_pid=; booted=
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  [ -n "$ctl_pid" ] && kill "$ctl_pid" 2>/dev/null
  [ -n "$serve_pid" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  pkill -f -- "--state $state" 2>/dev/null
  L "$hc" server stop >/dev/null 2>&1
  # A simulator this run started is shut again (one at a time on this computer).
  [ -n "$booted" ] && xcrun simctl shutdown "$SIM_UDID" 2>/dev/null
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  exit "$status"
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP
for a in $agents; do
  s=$(printf %s "$a" | tr -d -)
  mkdir -p "$work/h/src/demo-$s"
  # shellcheck disable=SC2046 # the recordings' paths, one word each
  L "$hc" run --bg --keep --cwd "$work/h/src/demo-$s" --name "$s" --label herdr.space="spc$s" --label herdr.tab="tbc$s" -- \
    "$work/fakeagent" -agent "$a" -preload 40 -long 500 -hc "$hc" $(recordings "$a") >/dev/null
done
serve() {
  L "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" >> "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0; until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; return 1; }; sleep 0.1; done
}
serve
# HM_CHAT4_DRY=1: no simulator. Only the core, the fakes and the Companion: each terminal must be known as its agent.
if [ "${HM_CHAT4_DRY:-}" = 1 ]; then
  sleep 2
  for a in $agents; do
    s=$(printf %s "$a" | tr -d -)
    got=$(L "$hc" get "$s" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("status") or {}).get("app",""), (d.get("labels") or {}).get("herdr-anywhere.chat.session","")[-6:])')
    case $got in "$a "*) echo "ok: $s is $got" ;; *) echo "FAIL: $s is known as '$got', not $a"; exit 1 ;; esac
  done
  echo PASS; exit 0
fi
link=$(L "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: no pairing link"; exit 1; }
printf "export const COMPANION: number = 0\nexport const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

# The test stops and starts the Companion through files in $ctl (the simulator writes to this Mac's folders).
(
  while :; do
    if [ -e "$ctl/stop" ]; then
      rm -f "$ctl/stop"; pkill -f -- "--state $state" 2>/dev/null; sleep 1
      echo "--- stopped by the test $(date +%T)" >> "$work/serve.log"; : > "$ctl/stopped"
    fi
    if [ -e "$ctl/start" ]; then
      rm -f "$ctl/start"; echo "--- started by the test $(date +%T)" >> "$work/serve.log"
      serve && : > "$ctl/started"
    fi
    sleep 0.3
  done
) &
ctl_pid=$!

xcrun simctl list devices booted | grep -q "$SIM_UDID" || booted=1
xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl ui "$SIM_UDID" appearance light
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
# simctl launch prints "<id>: <pid>" once the process is there (as e2e/ios-onerror.sh reads it).
launched=$(xcrun simctl launch "$SIM_UDID" "$id" 2>&1) || { echo "FAIL: the app did not start: $launched"; exit 1; }
before=${launched##*: }
case $before in ''|*[!0-9]*) echo "FAIL: the app did not start: simctl launch said '$launched'"; exit 1 ;; esac
pid() { xcrun simctl spawn "$SIM_UDID" launchctl list 2>/dev/null | awk -v id="UIKitApplication:$id" 'index($3, id) == 1 {print $1}'; }
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do i=$((i + 1)); [ $i -gt 60 ] && { echo "FAIL: the app never connected"; exit 1; }; sleep 1; done
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
[ -n "$shots" ] && mkdir -p "$shots"
rc=0
TEST_RUNNER_OUT="${shots:-$work}/chat-four.txt" TEST_RUNNER_SHOTS="${shots:-$work}/chat-four" TEST_RUNNER_CTL="$ctl" \
  TEST_RUNNER_AGENTS="$agents" TEST_RUNNER_AWAY="$away" \
  perl -e 'alarm 1800; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -only-testing:UITests/ChatFour -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" > "$work/uitest/test.log" 2>&1 || rc=$?
sed 's/^/  /' "${shots:-$work}/chat-four.txt" 2>/dev/null || true
# One process all through: another pid, or none, is a crash (or iOS ending the app), with its report when there is one.
now=$(pid); [ "$now" = "$before" ] || { sleep 2; now=$(pid); }
if [ "$now" != "$before" ]; then
  echo "FAIL: the app is not the process launched (pid $before at launch, '${now}' now)"
  reports=""
  for _ in $(seq 1 24); do
    reports=$(find "$HOME/Library/Logs/DiagnosticReports" -maxdepth 1 -name 'Herdr-*.ips' -newer "$work/started" 2>/dev/null)
    [ -n "$reports" ] && break
    sleep 5
  done
  for f in $reports; do echo "  crash report: $f"; grep -o '"type" *: *"[A-Z_]*"' "$f" | sort -u | sed 's/^/    /'; [ -n "$shots" ] && cp "$f" "$shots/"; done
  [ -n "$reports" ] || echo "  no crash report from the app within 120 s"
  [ -n "$shots" ] && cp "$work/serve.log" "$shots/serve.log"
  exit 1
fi
if [ "$rc" != 0 ]; then
  echo "FAIL: xcodebuild test exit $rc"
  # Its failures, else its end: what XCUITest said went wrong.
  grep -iE "error|fail|XCT" "$work/uitest/test.log" | head -20 | sed 's/^/  /'
  grep -qiE "error|fail|XCT" "$work/uitest/test.log" || tail -30 "$work/uitest/test.log" | sed 's/^/  /'
  [ -n "$shots" ] && { cp "$work/serve.log" "$shots/serve.log"; cp "$work/uitest/test.log" "$shots/test.log"; }
  exit 1
fi
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
echo PASS
