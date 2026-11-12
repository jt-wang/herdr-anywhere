#!/bin/sh
# The unit and screen tests: every *.test.ts(x) outside e2e/, which run headless with nothing else. The e2e suites need
# a harness: e2e/run.sh (a real core and companion), `nsh test --target web e2e/shots.test.tsx` (screenshots) and
# e2e/android.sh (a device).
set -eu
cd "$(dirname "$0")/.."
fail=0
for f in $(find . -name '*.test.ts' -o -name '*.test.tsx' | grep -vE '^\./(e2e|node_modules|dist|android)/|/\.' | sort); do
  out=$(nsh test "$f" 2>&1) || { fail=1; echo "$out" | grep -E "✗|error" ; }
  echo "$out" | tail -1 | sed "s|^|$f: |"
done
for check in scripts/check-*.sh; do "$check" src/ui || fail=1; done
exit $fail
