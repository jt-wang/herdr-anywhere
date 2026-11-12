#!/bin/sh
# What an iOS suite that failed adds to its failure folder, to tell a tap the app never got from one it lost: the
# Companion's whole log (each request the app made, each connect and disconnect; it logs paths, never a query, where
# the token is) and the crash reports simulator processes wrote while the suite ran (a page's WebContent process can
# crash with the app's own process still there). Both as .txt, which the upload keeps and its check reads.
# Usage: e2e/ios-failure-logs.sh SINCE SERVE_LOG FOLDER    SINCE: a file made when the suite began
#        e2e/ios-failure-logs.sh --self-test
# HM_DIAGNOSTIC_REPORTS: where the crash reports are (the Mac's ~/Library/Logs/DiagnosticReports).
failure_logs() {
  since=$1 serve=$2 to=$3
  reports=${HM_DIAGNOSTIC_REPORTS:-$HOME/Library/Logs/DiagnosticReports}
  mkdir -p "$to"
  [ -f "$serve" ] && cp "$serve" "$to/serve.log.txt"
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
if [ "${1:-}" = --self-test ]; then
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
  HM_DIAGNOSTIC_REPORTS="$t/reports" failure_logs "$t/since" "$t/serve.log" "$t/out" > "$t/said"
  [ -f "$t/out/serve.log.txt" ] || { echo "FAIL: ios-failure-logs self-test: no Companion log"; fail=1; }
  [ -f "$t/out/Herdr-2.ips.txt" ] || { echo "FAIL: ios-failure-logs self-test: the simulator's crash report during the suite is not kept"; fail=1; }
  [ -f "$t/out/Web Content-4.ips.txt" ] || { echo "FAIL: ios-failure-logs self-test: a crash report whose name has a space is not kept"; fail=1; }
  [ -f "$t/out/a_b-5.ips.txt" ] || { echo "FAIL: ios-failure-logs self-test: a name the upload refuses is not renamed: $(ls "$t/out" | tr '\n' ' ')"; fail=1; }
  [ -e "$t/out/Old-1.ips.txt" ] && { echo "FAIL: ios-failure-logs self-test: a crash report from before the suite is kept"; fail=1; }
  [ -e "$t/out/Finder-3.ips.txt" ] && { echo "FAIL: ios-failure-logs self-test: the Mac's own crash report is kept"; fail=1; }
  grep -q "3 crash reports" "$t/said" || { echo "FAIL: ios-failure-logs self-test: it says $(cat "$t/said")"; fail=1; }
  rm -rf "$t"
  [ "$fail" = 0 ] && echo "PASS: ios-failure-logs self-test" || exit 1
else
  failure_logs "$@"
fi
