#!/bin/sh
# The unit and screen tests: every *.test.ts(x) outside e2e/, which run headless with nothing else. The e2e suites need
# a harness: e2e/run.sh (a real core and companion), `nsh test --target web e2e/shots.test.tsx` (screenshots) and
# e2e/android.sh (a device).
set -eu
cd "$(dirname "$0")/.."
fail=0
for f in $(find . -name '*.test.ts' -o -name '*.test.tsx' | grep -vE '^\./(e2e|node_modules|dist|android)/|/\.' | sort); do
  # A failing file prints all it said: its failure may name no test (a crash, a hang the runner ended), and a filter
  # that matched nothing would end the script here under set -e, before the files after it ran.
  if out=$(nsh test "$f" 2>&1); then
    echo "$out" | tail -1 | sed "s|^|$f: |"
  else
    fail=1
    echo "$f: FAILED"
    echo "$out" | sed 's/^/  /'
  fi
done
for check in scripts/check-*.sh; do "$check" src/ui || fail=1; done
exit $fail
