#!/bin/sh
# What an iOS suite that failed adds to its failure folder, to tell a tap the app never got from one it lost: the
# Companion's whole log (each request the app made, each connect and disconnect; it logs paths, never a query, where
# the token is) and the crash reports simulator processes wrote while the suite ran (a page's WebContent process can
# crash with the app's own process still there). Both as .txt, which the upload keeps and its check reads.
# Usage: e2e/ios-failure-logs.sh SINCE SERVE_LOG FOLDER    SINCE: a file made when the suite began
#        e2e/ios-failure-logs.sh --self-test
#        e2e/ios-failure-logs.sh --sample-load FILE &    started by a suite: the machine's load (sysctl vm.loadavg) with
#          the time, every HM_LOAD_EVERY s (5) while the suite runs, for its notes' times; a failure keeps it as
#          load.txt, from the folder SINCE is in.
# HM_DIAGNOSTIC_REPORTS: where the crash reports are (the Mac's ~/Library/Logs/DiagnosticReports).
failure_logs() {
  [ $# = 3 ] && [ -n "$3" ] || { echo "FAIL: ios-failure-logs.sh: SINCE SERVE_LOG FOLDER, not: $*" >&2; return 2; }
  since=$1 serve=$2 to=$3
  reports=${HM_DIAGNOSTIC_REPORTS:-$HOME/Library/Logs/DiagnosticReports}
  mkdir -p "$to"
  [ -f "$serve" ] && cp "$serve" "$to/serve.log.txt"
  [ -f "$(dirname "$since")/load.txt" ] && cp "$(dirname "$since")/load.txt" "$to/load.txt"
  # A simulator's process says so in its report (launchd_sim starts it, its coalition is the SimDevice's). Names
  # may have spaces (a process's name).
  find "$reports" -maxdepth 1 -name '*.ips' -newer "$since" 2>/dev/null | sort | while IFS= read -r f; do
    grep -q 'CoreSimulator\|launchd_sim' "$f" || continue
    echo "$f"
  done | head -20 > "$to/.reports"
  while IFS= read -r f; do cp "$f" "$to/$(basename "$f").txt"; done < "$to/.reports"
  echo "  kept for the failure: the Companion's log, $(wc -l < "$to/.reports" | tr -d ' ') crash reports from the simulator (20 at most)"
  rm -f "$to/.reports"
  # Names the upload takes (e2e/ios-xcresult.sh: one name with : or " in it loses the whole artifact).
  sh "$(dirname "$0")/ios-xcresult.sh" --tidy "$to"
}
# Ends when the suite that started it is gone (its parent).
sample_load() {
  while kill -0 "$PPID" 2>/dev/null && [ -d "$(dirname "$1")" ]; do
    echo "$(date +%H:%M:%S) $(sysctl -n vm.loadavg 2>/dev/null || cat /proc/loadavg)" >> "$1"
    sleep "${HM_LOAD_EVERY:-5}"
  done
}
if [ "${1:-}" = --sample-load ]; then
  sample_load "$2"
elif [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  mkdir -p "$t/reports" "$t/out"
  printf '{"app_name":"Old"}\n{"coalitionName":"com.apple.CoreSimulator.SimDevice.X"}\n' > "$t/reports/Old-1.ips"
  touch -t 202001010000 "$t/reports/Old-1.ips"
  touch -t 202001020000 "$t/since"
  printf '{"app_name":"Herdr"}\n{"parentProc":"launchd_sim","coalitionName":"com.apple.CoreSimulator.SimDevice.X"}\n' > "$t/reports/Herdr-2.ips"
  printf '{"app_name":"Web Content"}\n{"parentProc":"launchd_sim"}\n' > "$t/reports/Web Content-4.ips"
  printf '{"app_name":"a:b"}\n{"parentProc":"launchd_sim"}\n' > "$t/reports/a:b-5.ips"
  printf '{"app_name":"Finder"}\n{"parentProc":"launchd"}\n' > "$t/reports/Finder-3.ips"
  printf 'listening on http://127.0.0.1:7691\n127.0.0.1 GET /api/info 200 0s\n' > "$t/serve.log"
  printf '00:49:13 { 3.10 2.50 2.00 }\n' > "$t/load.txt"
  HM_DIAGNOSTIC_REPORTS="$t/reports" failure_logs "$t/since" "$t/serve.log" "$t/out" > "$t/said"
  [ -f "$t/out/serve.log.txt" ] || { echo "FAIL: ios-failure-logs self-test: no Companion log"; fail=1; }
  [ -f "$t/out/Herdr-2.ips.txt" ] || { echo "FAIL: ios-failure-logs self-test: the simulator's crash report during the suite is not kept"; fail=1; }
  [ -f "$t/out/Web Content-4.ips.txt" ] || { echo "FAIL: ios-failure-logs self-test: a crash report whose name has a space is not kept"; fail=1; }
  [ -f "$t/out/a_b-5.ips.txt" ] || { echo "FAIL: ios-failure-logs self-test: a name the upload refuses is not renamed: $(ls "$t/out" | tr '\n' ' ')"; fail=1; }
  [ -e "$t/out/Old-1.ips.txt" ] && { echo "FAIL: ios-failure-logs self-test: a crash report from before the suite is kept"; fail=1; }
  [ -e "$t/out/Finder-3.ips.txt" ] && { echo "FAIL: ios-failure-logs self-test: the Mac's own crash report is kept"; fail=1; }
  grep -q "3 crash reports" "$t/said" || { echo "FAIL: ios-failure-logs self-test: it says $(cat "$t/said")"; fail=1; }
  grep -q "3.10" "$t/out/load.txt" 2>/dev/null || { echo "FAIL: ios-failure-logs self-test: the load sampled during the suite is not kept"; fail=1; }
  # The sampler: a line every HM_LOAD_EVERY s while the suite that started it runs, and gone once it ended.
  HM_LOAD_EVERY=0.2 sh -c 'sh "$1" --sample-load "$2" & echo $! > "$3"; sleep 1' sh "$0" "$t/sampled" "$t/sampler"
  n=$(grep -cE '^[0-9]{2}:[0-9]{2}:[0-9]{2} .*[0-9]\.[0-9]' "$t/sampled" 2>/dev/null) || n=0
  [ "$n" -ge 2 ] || { echo "FAIL: ios-failure-logs self-test: the sampler wrote $n load lines in 1 s: $(head -3 "$t/sampled" 2>/dev/null)"; fail=1; }
  sleep 1; kill -0 "$(cat "$t/sampler")" 2>/dev/null && { echo "FAIL: ios-failure-logs self-test: the sampler outlived the suite"; kill "$(cat "$t/sampler")"; fail=1; }
  rm -rf "$t"
  [ "$fail" = 0 ] && echo "PASS: ios-failure-logs self-test" || exit 1
else
  failure_logs "$@"
fi
