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
cycles=${CHAT_AWAY_CYCLES:-0}
case $cycles in ''|*[!0-9]*) echo "CHAT_AWAY_CYCLES must be 0..30" >&2; exit 2 ;; esac
[ "$cycles" -le 30 ] || { echo "CHAT_AWAY_CYCLES must be 0..30" >&2; exit 2; }
control=${CHAT_AWAY_SELFTEST:-none}
case $control in none) ;; stop-companion) [ "$cycles" -gt 0 ] || { echo "self-test needs background cycles" >&2; exit 2; } ;;
  *) echo "CHAT_AWAY_SELFTEST must be none or stop-companion" >&2; exit 2 ;; esac
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard ios-chat-four # a login entry of its own, never the person's
uitest=${UITEST:-ChatFour}
case $uitest in
  ChatFour) agents=${AGENTS:-claude-code codex pi opencode}; delay=400ms; tag=chat-four ;;
  ChatFold) agents=${AGENTS:-claude-code}; delay=5s; tag=chat-fold ;;
  *) echo "no test $uitest (ChatFour or ChatFold)" >&2; exit 2 ;;
esac
away=${AWAY:-10 60}
[ "$cycles" = 0 ] || [ "$uitest" = ChatFour ] || { echo "background cycles need ChatFour" >&2; exit 2; }
test_timeout=$((1800 + cycles * 180))
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
L() { env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/h" SHELL=/bin/zsh TERM=xterm-256color \
  HERDR_ANYWHERE_AUTOSTART_NAME="$HERDR_ANYWHERE_AUTOSTART_NAME" \
  LANG=en_US.UTF-8 HC_SESSION="hmchat4-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
  XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" CLAUDE_CONFIG_DIR="$work/claude" CODEX_HOME="$work/codex" \
  PI_CODING_AGENT_DIR="$work/pi" "$@"; }
serve_pid=; ctl_pid=; log_pid=; system_log_pid=; proxy_pid=; booted=; remove_app=0
debug_log_pid=; expiry_pid=; expiry_raw=
cleanup() {
  behavior_status=$?
  status=$behavior_status
  trap - EXIT INT TERM HUP
  set +e
  [ -n "$ctl_pid" ] && kill "$ctl_pid" 2>/dev/null
  if [ -n "${expiry_pid:-}" ]; then
    kill "$expiry_pid" 2>/dev/null
    wait "$expiry_pid" || status=1
  fi
  if [ -n "${debug_log_pid:-}" ]; then
    kill "$debug_log_pid" 2>/dev/null
    wait "$debug_log_pid"
  fi
  [ -n "$log_pid" ] && { kill "$log_pid" 2>/dev/null; wait "$log_pid"; }
  if [ -n "${system_log_pid:-}" ]; then
    # This stops our collector, not the app. Keep its status separate from app exit evidence.
    printf 'collector_stop_requested_utc=%s\n' "$(date -u +%FT%TZ)" >> "$work/app-system-status.txt"
    kill "$system_log_pid" 2>/dev/null
    wait "$system_log_pid"
    printf 'collector_wait_exit=%s\n' "$?" >> "$work/app-system-status.txt"
  fi
  [ -n "$proxy_pid" ] && kill "$proxy_pid" 2>/dev/null
  # The app's diagnostics (src/diag.ts, sent by ios-e2e.tsx) the Companion logged: printed when the run failed.
  [ "$status" != 0 ] && grep -h ' /hm-e2e/' "$work/serve.log" 2>/dev/null | tail -10 | sed 's/^/  diagnostic: /'
  # What the app logged as errors (nsh writes the errors it reports there): printed when the run failed, kept always.
  [ "$status" != 0 ] && [ -s "$work/app.log" ] && { echo "  the app's errors (last 20):"; grep -vE '^(Filtering the log data|Timestamp )' "$work/app.log" | tail -20 | cut -c1-300 | sed 's/^/    /'; }
  [ -n "$shots" ] && [ -s "$work/app.log" ] && cp "$work/app.log" "$shots/app.log"
  if [ -n "$shots" ]; then
    if [ -n "${expiry_raw:-}" ] && [ -d "$expiry_raw" ]; then
      python3 "$app/e2e/ios-background-expiry.py" decode "$expiry_raw"
      decode_status=$?
      diagnostic=complete
      if [ "$decode_status" != 0 ]; then
        diagnostic=incomplete
        echo "diagnostic incomplete: decode exit $decode_status (behavior exit $behavior_status)"
      fi
      printf 'diagnostic=%s\ndecode_exit=%s\nbehavior_exit=%s\n' "$diagnostic" "$decode_status" "$behavior_status" \
        > "$expiry_raw/decode-status.txt" || status=1
      python3 "$app/e2e/ios-background-expiry.py" publish "$expiry_raw" "$shots/background-expiry" \
        "$work/expiry-pair" || status=1
    fi
    [ ! -f "$work/serve.log" ] || cp "$work/serve.log" "$shots/serve.log" || status=1
    [ ! -f "$work/uitest/test.log" ] || cp "$work/uitest/test.log" "$shots/test.log" || status=1
    for file in app-system.log app-system-status.txt; do
      [ ! -f "$work/$file" ] || cp "$work/$file" "$shots/$file" || status=1
    done
    # A pre-test failure or the existing dry cleanup has no result bundle to read.
    if [ -d "$work/uitest/result.xcresult" ]; then
    # Read only this test's lifecycle activities; do not export device metadata, UI queries or attachments.
    python3 - "$work/uitest/result.xcresult" "$uitest/test()" "$id" "$shots/app-activate-activities.json" <<'PY_ACTIVITIES'
import json, subprocess, sys
from pathlib import Path
result, test, bundle, output = sys.argv[1:]
command = ['xcrun', 'xcresulttool', 'get', 'test-results', 'activities',
           '--path', result, '--test-id', test, '--compact']
read = subprocess.run(command, capture_output=True, text=True)
record = {'test': test, 'bundle': bundle, 'xcresulttool_exit': read.returncode, 'activities': []}
if read.returncode == 0:
    data = json.loads(read.stdout)
    titles = {f'{verb} {bundle}' for verb in ('Activate', 'Launch', 'Terminate')}
    def walk(nodes, run):
        for node in nodes:
            if node.get('title') in titles:
                record['activities'].append({'run': run, **{
                    key: node[key] for key in ('title', 'startTime', 'isAssociatedWithFailure') if key in node}})
            walk(node.get('childActivities', []), run)
    for run, item in enumerate(data['testRuns'], 1):
        walk(item['activities'], run)
record['capture'] = 'captured' if record['activities'] else 'unavailable'
Path(output).write_text(json.dumps(record, indent=2) + '\n')
PY_ACTIVITIES
    [ "$?" = 0 ] || { echo "FAIL: lifecycle activity extraction"; status=1; }
    fi
    if [ "${HM_CHAT4_DRY:-}" != 1 ]; then
      container=$(xcrun simctl get_app_container "$SIM_UDID" "$id" data 2>/dev/null)
      if [ -n "$container" ]; then
        python3 "$app/e2e/ios-chat-evidence.py" "$container/Library/Application Support/nsh/$id/storage.json" "$shots/connection-diag.json" || status=1
      fi
    fi
  fi
  # Preserve Storage before a successful run removes the installed app.
  [ "$remove_app" = 0 ] || xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
  [ -n "$serve_pid" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  pkill -f -- "--state $state" 2>/dev/null
  L "$hc" server stop >/dev/null 2>&1
  # A simulator this run started is shut again (one at a time on this computer).
  [ -n "$booted" ] && xcrun simctl shutdown "$SIM_UDID" 2>/dev/null
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
  autostart_guard_end || status=1
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
if [ "$cycles" -gt 0 ]; then
  expiry_raw="$work/background-expiry"
  (umask 077; mkdir -p "$expiry_raw"; printf '%s' "$link" > "$work/expiry-pair")
fi
source_evidence=
if [ "$cycles" -gt 0 ] && [ -n "$shots" ]; then
  source_evidence="$work/subscriber-source-raw"
  (umask 077; printf '%s' "$link" > "$work/subscriber-pair")
fi
HM_TEST_MAIN_THREAD_MARKER="$expiry_raw" HM_TEST_BUNDLE_EVIDENCE="$source_evidence" "$app/ios/build.sh" ios-e2e.tsx simulator > "$work/build.log" 2>&1 || { tail -20 "$work/build.log"; echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

# The test stops and starts the Companion through files in $ctl (the simulator writes to this Mac's folders).
(
  while :; do
    if [ -e "$ctl/arm-cut" ]; then
      rm -f "$ctl/arm-cut"
      # Capture the live socket before Home; a later close must belong to this connection.
      awk '$3 == "device" && $5 == "connected" { line=NR; device=$4; active=1 }
        $3 == "device" && $5 == "disconnected" && $4 == device { active=0 }
        END { if (active) print line, device; else exit 1 }' "$work/serve.log" > "$ctl/cut-socket" && : > "$ctl/cut-armed"
    fi
    if [ -e "$ctl/stop" ]; then
      rm -f "$ctl/stop"; pkill -f -- "--state $state" 2>/dev/null; sleep 1
      if [ -s "$ctl/cut-socket" ]; then
        read -r line device < "$ctl/cut-socket"
        # The killed connection can finish logging after pkill returns.
        i=0
        while [ "$i" -lt 40 ]; do
          if awk -v start="$line" -v device="$device" 'NR > start && $3 == "device" && $4 == device && $5 == "disconnected" { found=1 }
            END { exit !found }' "$work/serve.log"; then
            : > "$ctl/cut-disconnected"; break
          fi
          i=$((i + 1)); sleep 0.25
        done
      fi
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
    # When the proxy (ChatFold) answered "can't tell", held an answer and let it go, in serve.log beside the answers
    # the Companion gave: the "can't tell" never reaches the Companion, so the order is told by these times.
    for e in no-status-sent held released; do
      if [ -e "$ctl/$e" ] && [ ! -e "$ctl/.noted-$e" ]; then
        : > "$ctl/.noted-$e"; echo "--- proxy: $e at $(date +%T)" >> "$work/serve.log"
      fi
    done
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
if [ -n "$expiry_raw" ]; then
  xcrun simctl spawn "$SIM_UDID" log stream --style compact --level debug --predicate 'process == "Herdr"' \
    > "$expiry_raw/app-debug.log" 2>&1 &
  debug_log_pid=$!
fi
xcrun simctl ui "$SIM_UDID" appearance light
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
if [ -n "$source_evidence" ]; then
  cmp "$source_evidence/bundle.nshc" "$app/ios/build/Herdr.app/nsh/bundle.nshc"
  python3 "$app/e2e/ios-subscriber-source.py" publish "$source_evidence" "$app/ios/build/Herdr.app/nsh/bundle.nshc" \
    "$shots/subscriber-source" "$work/subscriber-pair" "$app"
fi
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
# Keep complete target-app system events (including multiline termination contexts), at every level.
# The bundle selector follows replacement PIDs; unrelated app events are not selected.
system_predicate="(process == \"runningboardd\" OR process == \"SpringBoard\" OR process == \"launchd_sim\" OR process == \"launchd\") AND composedMessage CONTAINS \"$id\""
xcrun simctl spawn "$SIM_UDID" log stream --style compact --level debug --predicate "$system_predicate" \
  > "$work/app-system.log" 2>&1 &
system_log_pid=$!
printf 'collector_pid=%s\ncollector_start_requested_utc=%s\n' "$system_log_pid" "$(date -u +%FT%TZ)" > "$work/app-system-status.txt"
# The same one-second startup allowance used by the reviewed scene collector, before app launch.
sleep 1
kill -0 "$system_log_pid" 2>/dev/null || { echo "FAIL: target-app system log collector exited before launch"; exit 1; }
# simctl launch prints "<id>: <pid>" once the process is there (as e2e/ios-onerror.sh reads it).
launched=$(xcrun simctl launch "$SIM_UDID" "$id" 2>&1) || { echo "FAIL: the app did not start: $launched"; exit 1; }
before=${launched##*: }
case $before in ''|*[!0-9]*) echo "FAIL: the app did not start: simctl launch said '$launched'"; exit 1 ;; esac
printf 'app_pid_at_launch=%s\n' "$before" >> "$work/app-system-status.txt"
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
if [ -n "$expiry_raw" ]; then
  kill -0 "$debug_log_pid" 2>/dev/null || { echo "FAIL: app debug collector exited"; exit 1; }
  python3 "$app/e2e/ios-background-expiry.py" calibrate "$before" "$expiry_raw" || {
    echo "FAIL: background sample calibration"; exit 1;
  }
  python3 "$app/e2e/ios-background-expiry.py" watch "$before" "$id" "$work/app-system.log" "$expiry_raw" &
  expiry_pid=$!
fi
cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
rc=0
TEST_RUNNER_OUT="${shots:-$work}/$tag.txt" TEST_RUNNER_SHOTS="${shots:-$work}/$tag" TEST_RUNNER_CTL="$ctl" \
  TEST_RUNNER_CHAT_AWAY_ONLY="${CHAT_AWAY_ONLY:-0}" TEST_RUNNER_AGENTS="$agents" TEST_RUNNER_AWAY="$away" TEST_RUNNER_CHAT_AWAY_CYCLES="$cycles" TEST_RUNNER_CHAT_AWAY_SELFTEST="$control" \
  perl -e 'alarm shift; exec @ARGV' "$test_timeout" xcodebuild test -quiet -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests \
  -only-testing:"UITests/$uitest" -destination "id=$SIM_UDID" -derivedDataPath "$work/uitest/derived" \
  -resultBundlePath "$work/uitest/result.xcresult" > "$work/uitest/test.log" 2>&1 || rc=$?
sed 's/^/  /' "${shots:-$work}/$tag.txt" 2>/dev/null || true
# One process all through: another pid, or none, is a crash (or iOS ending the app), with its report when there is one.
now=$(pid); [ "$now" = "$before" ] || { sleep 2; now=$(pid); }
if [ "$rc" != 0 ] || [ "$now" != "$before" ]; then
  echo "xcodebuild test exit $rc"
  if [ -n "$shots" ]; then
    printf 'at-launch=%s\nafter-test=%s\n' "$before" "$now" > "$shots/process-ids.txt"
    xcrun simctl io "$SIM_UDID" screenshot "$shots/after-failure.png" > "$shots/after-failure-screen.log" 2>&1 ||
      echo "  could not capture the simulator screen after the failure"
  fi
  # Keep XCTest's failure even when it also ended the app: the PID check below exits on that path.
  "$app/e2e/ios-xcresult.sh" "$work/uitest/result.xcresult" ${shots:+"$shots/failure"}
  [ -n "$shots" ] && { cp "$work/serve.log" "$shots/serve.log"; cp "$work/uitest/test.log" "$shots/test.log"; }
  [ -n "$shots" ] && [ -s "$work/proxy.log" ] && sed -E 's/(#pair=|[Bb]earer +|token=)[^ "&]*/\1<hidden>/g' "$work/proxy.log" > "$shots/proxy.log"
fi
if [ "$now" != "$before" ]; then
  echo "FAIL: the app is not the process launched (pid $before at launch, '${now}' now)"
  crash_reports 24
  [ -n "$shots" ] && cp "$work/serve.log" "$shots/serve.log"
  [ -n "$shots" ] && [ -s "$work/proxy.log" ] && sed -E 's/(#pair=|[Bb]earer +|token=)[^ "&]*/\1<hidden>/g' "$work/proxy.log" > "$shots/proxy.log"
  exit 1
fi
[ "$rc" = 0 ] || exit 1
[ "$cycles" = 0 ] || python3 "$app/e2e/chat-away-check.py" "$cycles" "${shots:-$work}/$tag.txt"
remove_app=1
echo PASS
