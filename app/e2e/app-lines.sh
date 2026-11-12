#!/bin/sh
# The app's own lines in what a suite caught of it (the Companion's log, which has a line for each request the test
# app makes, e2e/ios-touch-app.sh), each as "  app: LINE", or one line saying there is none. Only lines with the
# app's own words count: `log stream`, read here before, starts with a line that repeats its filter ("Filtering the
# log data using …"), which held the words looked for too.
# Usage: e2e/app-lines.sh WORDS FILE…      e2e/app-lines.sh --self-test
app_lines() {
  words=$1; shift
  found=$(grep -hF -- "$words" "$@" 2>/dev/null | grep -v '^Filtering the log data using ')
  [ -n "$found" ] && printf '%s\n' "$found" | sed 's/^/  app: /' || echo "  app: no \"$words\" line (the app opened no space)"
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  printf '%s\n' 'Filtering the log data using "composedMessage CONTAINS "hm-e2e""' > "$t/header"
  printf '%s\n' 'Filtering the log data using "composedMessage CONTAINS "hm-e2e""' > "$t/stream"
  printf '%s\n' 'serve: 127.0.0.1 GET /api/info 200 0s' 'serve: 127.0.0.1 GET /hm-e2e/openSpace/spscroll/{"name":"home"} 404 0s' > "$t/serve"
  got=$(app_lines "hm-e2e" "$t/header")
  [ "$got" = '  app: no "hm-e2e" line (the app opened no space)' ] || { echo "FAIL: app-lines self-test: log stream's own first line taken for the app's: $got"; fail=1; }
  got=$(app_lines "/hm-e2e/openSpace/" "$t/serve")
  [ "$got" = '  app: serve: 127.0.0.1 GET /hm-e2e/openSpace/spscroll/{"name":"home"} 404 0s' ] || { echo "FAIL: app-lines self-test: the app's line in the Companion's log: $got"; fail=1; }
  got=$(app_lines "/hm-e2e/openSpace/" "$t/stream" "$t/missing")
  [ "$got" = '  app: no "/hm-e2e/openSpace/" line (the app opened no space)' ] || { echo "FAIL: app-lines self-test: no line, or a missing file: $got"; fail=1; }
  rm -rf "$t"
  [ "$fail" = 0 ] && echo "PASS: app-lines self-test" || exit 1
else
  app_lines "$@"
fi
