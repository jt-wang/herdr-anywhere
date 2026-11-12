#!/bin/sh
# For the iOS suites: waits for the app on the simulator to connect to the suite's Companion, whose log has a line
# "device … connected (binary," once it has. When that doesn't come in time, it says what may explain it: whether the
# app's process is there on the simulator, and the Companion's last lines; with a FOLDER, it also keeps there what
# e2e/ios-failure-logs.sh keeps (the Companion's whole log, the simulator's crash reports, the machine's load). Then it
# exits 1, after "FAIL: the app never connected", and the suite stops.
# Usage: e2e/ios-connect-wait.sh SIM_UDID BUNDLE_ID SERVE_LOG STARTED [FOLDER] || exit 1
#        e2e/ios-connect-wait.sh --self-test
# STARTED and FOLDER as e2e/ios-failure-logs.sh takes them. HM_CONNECT_WAIT: seconds to wait (60).
# HM_XCRUN: the xcrun to ask the simulator with (the self-test gives one that answers nothing).
set -u
here=$(cd "$(dirname "$0")" && pwd)
wait_connected() {
  udid=$1 id=$2 log=$3 started=$4 folder=${5:-} i=0
  while [ "$i" -lt "${HM_CONNECT_WAIT:-60}" ]; do
    grep -q "device .* connected (binary," "$log" 2>/dev/null && { echo "  the app connected after $i s"; return 0; }
    i=$((i + 1)); sleep 1
  done
  echo "FAIL: the app never connected (${HM_CONNECT_WAIT:-60} s)"
  proc=$("${HM_XCRUN:-xcrun}" simctl spawn "$udid" launchctl list 2>/dev/null | grep -F "$id")
  echo "  the app's process on the simulator: ${proc:-not running}"
  echo "  the Companion's last lines:"; tail -10 "$log" 2>/dev/null | sed 's/^/    /'
  [ -z "$folder" ] || { mkdir -p "$folder"; sh "$here/ios-failure-logs.sh" "$started" "$log" "$folder"; }
  return 1
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  : > "$t/started"; mkdir -p "$t/reports"
  # 1. The app connects after about a second: it waits for it, and says so.
  printf 'listening on http://127.0.0.1:7691\n' > "$t/serve.log"
  (sleep 1; echo "device ab12 connected (binary, 2 streams)" >> "$t/serve.log") &
  HM_CONNECT_WAIT=5 HM_XCRUN=false sh "$0" UDID app.example "$t/serve.log" "$t/started" "$t/out1" > "$t/said1" 2>&1 ||
    { echo "FAIL: ios-connect-wait self-test: a connect after 1 s is not seen: $(cat "$t/said1")"; fail=1; }
  grep -q "the app connected after" "$t/said1" || { echo "FAIL: ios-connect-wait self-test: it does not say it connected: $(cat "$t/said1")"; fail=1; }
  [ -e "$t/out1" ] && { echo "FAIL: ios-connect-wait self-test: a connect that came keeps a failure folder"; fail=1; }
  # 2. It never connects: FAIL, the process not there, the Companion's last line, and the folder with its whole log.
  printf 'listening on http://127.0.0.1:7691\n127.0.0.1 GET /api/info 200 0s\n' > "$t/serve.log"
  if HM_CONNECT_WAIT=1 HM_XCRUN=false HM_DIAGNOSTIC_REPORTS="$t/reports" sh "$0" UDID app.example "$t/serve.log" "$t/started" "$t/out2" > "$t/said2" 2>&1; then
    echo "FAIL: ios-connect-wait self-test: no connect still exits 0"; fail=1
  fi
  grep -q "^FAIL: the app never connected" "$t/said2" || { echo "FAIL: ios-connect-wait self-test: no FAIL line: $(cat "$t/said2")"; fail=1; }
  grep -q "process on the simulator: not running" "$t/said2" || { echo "FAIL: ios-connect-wait self-test: it does not say the app is not running: $(cat "$t/said2")"; fail=1; }
  grep -q "GET /api/info" "$t/said2" || { echo "FAIL: ios-connect-wait self-test: the Companion's last lines are not shown: $(cat "$t/said2")"; fail=1; }
  [ -f "$t/out2/serve.log.txt" ] || { echo "FAIL: ios-connect-wait self-test: the folder has no Companion log: $(ls "$t/out2" 2>&1)"; fail=1; }
  wait
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: ios-connect-wait self-test" || exit 1
else
  wait_connected "$@"
fi
