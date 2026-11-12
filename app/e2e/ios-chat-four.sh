#!/bin/sh
# The chat view of the four agents on an iOS simulator, for crashes and hangs (XCUITest's taps, e2e/ios-uitest
# ChatFour): fake Claude Code, Codex, Pi and OpenCode 2 (companion/internal/testfake/fakeagent, replaying their
# recordings; a conversation of 40 turns from the start, a command that printed 500 lines), in an isolated
# herdr-core, against this branch's Companion. For each agent: open, the long output, a picture, scrolling the long
# conversation, the app in the background and back, the Companion stopped and started again. The app must stay one
# process from launch to the end: a crash fails with its crash report. The app is built from ios-e2e.tsx at this run's
# pairing link (as e2e/ios-chat.sh). Fake data only.
# UITEST=ChatFold runs e2e/ios-uitest ChatFold instead (devices.yml group ios-chatfold, a change's before and after):
# Claude Code alone, its turns slowed (2 s a line), the app paired through companion/internal/testfake/chatproxy, which
# the test steers by the same folder of files.
# Usage: SIM_UDID=<udid> [AGENTS="claude-code codex pi opencode"] [AWAY="10 60"] [UITEST=ChatFour|ChatFold] e2e/ios-chat-four.sh [folder for screenshots]
# AWAY: the seconds in the background, each for the first agent, the first for the others (60 is past the Companion's
# ping timeout, so the link is gone by then). Needs go, nsh, hc, python3, xcodegen and Xcode.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
uitest=${UITEST:-ChatFour}
case $uitest in
  ChatFour) agents=${AGENTS:-claude-code codex pi opencode}; delay=400ms; tag=chat-four ;;
  ChatFold) agents=${AGENTS:-claude-code}; delay=2s; tag=chat-fold ;;
  *) echo "no test $uitest (ChatFour or ChatFold)" >&2; exit 2 ;;
esac
away=${AWAY:-10 60}
app=$(cd "$(dirname "$0")/.." && pwd)
companion="$app/../companion"
shots=${1:-}
[ -z "$shots" ] || mkdir -p "$shots"
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
  CGO_ENABLED=0 go build -buildvcs=false -o "$work/fakeagent" ./internal/testfake/fakeagent &&
  CGO_ENABLED=0 go build -buildvcs=false -o "$work/chatproxy" ./internal/testfake/chatproxy)
placeholder=$(cat "$app/e2e/pair.ts")
# herdr-core gives every terminal the environment of what started it: the agents' folders are set here, for the
# fakes and for the Companion's readers alike.
# HERDR_ANYWHERE_AUTOSTART_NAME: a pair or start that finds no serve makes this run's own login entry, never the
# person's (a fake HOME alone still reaches the real launchctl).
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  HERDR_ANYWHERE_AUTOSTART_NAME="hm-e2e-ios-chat-four" \
  LANG=en_US.UTF-8 HC_SESSION="hmchat4-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" CLAUDE_CONFIG_DIR="$work/claude" CODEX_HOME="$work/codex" \
  PI_CODING_AGENT_DIR="$work/pi" "$@"; }
serve_pid=; ctl_pid=; log_pid=; proxy_pid=; booted=
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  [ -n "$ctl_pid" ] && kill "$ctl_pid" 2>/dev/null
  [ -n "$log_pid" ] && kill "$log_pid" 2>/dev/null
  [ -n "$proxy_pid" ] && kill "$proxy_pid" 2>/dev/null
  # The app's diagnostics (src/diag.ts, sent by ios-e2e.tsx) the Companion logged: printed when the run failed.
  [ "$status" != 0 ] && grep -h ' /hm-e2e/' "$work/serve.log" 2>/dev/null | tail -10 | sed 's/^/  diagnostic: /'
  # What the app logged as errors (nsh writes the errors it reports there): printed when the run failed, kept always.
  [ "$status" != 0 ] && [ -s "$work/app.log" ] && { echo "  the app's errors (last 20):"; grep -vE '^(Filtering the log data|Timestamp )' "$work/app.log" | tail -20 | cut -c1-300 | sed 's/^/    /'; }
  [ -n "$shots" ] && [ -s "$work/app.log" ] && cp "$work/app.log" "$shots/app.log"
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
    "$work/fakeagent" -agent "$a" -preload 40 -long 500 -delay "$delay" -hc "$hc" $(recordings "$a") >/dev/null
done
serve() {
  L "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" >> "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0; until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; return 1; }; sleep 0.1; done
}
serve
url="http://127.0.0.1:$port"
# ChatFold: the app's link ends at the proxy, which passes all on to the Companion (stopped and started on $port).
if [ "$uitest" = ChatFold ]; then
  pport=$((port + 1)); while nc -z 127.0.0.1 "$pport" 2>/dev/null; do pport=$((pport + 1)); done
  "$work/chatproxy" -listen "127.0.0.1:$pport" -to "$url" -ctl "$ctl" >> "$work/proxy.log" 2>&1 &
  proxy_pid=$!; url="http://127.0.0.1:$pport"
  i=0; until curl -fsS "$url/api/info" >/dev/null 2>&1; do i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/proxy.log" >&2; exit 1; }; sleep 0.1; done
fi
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
# The link goes into the app's build, which comes after it: an hour (pair's default is 10 minutes), as a build with
# Xcode 27.0 on CI took 15 and the app was shown an expired code (POST /api/pair 410), as e2e/ios-onerror.sh found.
link=$(L "$work/herdr-anywhere" pair --ttl 60m --url "$url" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
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
    # A failed step asks what herdr-core says of its agent's terminal (its name in the file): the run's output keeps it.
    if [ -e "$ctl/get" ]; then
      g=$(cat "$ctl/get"); rm -f "$ctl/get"
      { echo "--- hc get $g at $(date +%T)"; L "$hc" get "$g" 2>&1; } >> "${shots:-$work}/hc-get.txt"; : > "$ctl/got"
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
xcrun simctl spawn "$SIM_UDID" log stream --style compact \
  --predicate 'process == "Herdr" AND (messageType == error OR messageType == fault OR composedMessage CONTAINS[c] "nsh") AND NOT composedMessage CONTAINS "XC_kAX" AND NOT composedMessage CONTAINS "nsh_platform_ios::"' \
  > "$work/app.log" 2>&1 &
log_pid=$!
xcrun simctl ui "$SIM_UDID" appearance light
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
# simctl launch prints "<id>: <pid>" once the process is there (as e2e/ios-onerror.sh reads it).
launched=$(xcrun simctl launch "$SIM_UDID" "$id" 2>&1) || { echo "FAIL: the app did not start: $launched"; exit 1; }
before=${launched##*: }
case $before in ''|*[!0-9]*) echo "FAIL: the app did not start: simctl launch said '$launched'"; exit 1 ;; esac
pid() { xcrun simctl spawn "$SIM_UDID" launchctl list 2>/dev/null | awk -v id="UIKitApplication:$id" 'index($3, id) == 1 {print $1}'; }
# The app's crash reports since the launch, each with its type, kept with the screenshots.
crash_reports() {
  reports=""
  for _ in $(seq 1 "$1"); do
    reports=$(find "$HOME/Library/Logs/DiagnosticReports" -maxdepth 1 -name 'Herdr-*.ips' -newer "$work/started" 2>/dev/null)
    [ -n "$reports" ] && break
    sleep 5
  done
  for f in $reports; do echo "  crash report: $f"; grep -o '"type" *: *"[A-Z_]*"' "$f" | sort -u | sed 's/^/    /'; [ -n "$shots" ] && cp "$f" "$shots/"; done
  [ -n "$reports" ] || echo "  no crash report from the app within $(($1 * 5)) s"
}
# A simulator just booted can be slow to start the app the first time: 120 s.
i=0; until grep -q "device .* connected (binary," "$work/serve.log"; do
  i=$((i + 1))
  if [ $i -gt 120 ]; then
    now=$(pid)
    echo "FAIL: the app never connected (pid $before at launch, '${now}' now)"
    [ -n "$shots" ] && { xcrun simctl io "$SIM_UDID" screenshot "$shots/never-connected.png" >/dev/null 2>&1 || true; cp "$work/serve.log" "$shots/serve.log"; }
    tail -20 "$work/serve.log" | sed 's/^/  serve: /'
    [ "$now" = "$before" ] || crash_reports 6
    exit 1
  fi
  sleep 1
done
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
rc=0
TEST_RUNNER_OUT="${shots:-$work}/$tag.txt" TEST_RUNNER_SHOTS="${shots:-$work}/$tag" TEST_RUNNER_CTL="$ctl" \
  TEST_RUNNER_AGENTS="$agents" TEST_RUNNER_AWAY="$away" \
  perl -e 'alarm 1800; exec @ARGV' xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -only-testing:"UITests/$uitest" -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
  -resultBundlePath "$work/uitest/result.xcresult" > "$work/uitest/test.log" 2>&1 || rc=$?
sed 's/^/  /' "${shots:-$work}/$tag.txt" 2>/dev/null || true
# One process all through: another pid, or none, is a crash (or iOS ending the app), with its report when there is one.
now=$(pid); [ "$now" = "$before" ] || { sleep 2; now=$(pid); }
if [ "$now" != "$before" ]; then
  echo "FAIL: the app is not the process launched (pid $before at launch, '${now}' now)"
  crash_reports 24
  [ -n "$shots" ] && cp "$work/serve.log" "$shots/serve.log"
  exit 1
fi
if [ "$rc" != 0 ]; then
  echo "FAIL: xcodebuild test exit $rc"
  # Its failures, else its end: what XCUITest said went wrong.
  # -quiet leaves the failures' messages out of the log: they are read from the result bundle.
  "$app/e2e/ios-xcresult.sh" "$work/uitest/result.xcresult" ${shots:+"$shots/failure"}
  [ -n "$shots" ] && { cp "$work/serve.log" "$shots/serve.log"; cp "$work/uitest/test.log" "$shots/test.log"; }
  exit 1
fi
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
echo PASS
