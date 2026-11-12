#!/bin/sh
# A test's host.send that nobody awaits resolves once the app is quiet; when the test ends first, the app is closed and
# the promise rejects with no one to hear it ("unhandled rejection: the app is closed"), now and then, and the run still
# passes. Prints file:line for each host.send in a test file without await (or return) before it; fails if there are any.
# Takes no paths: it reads every test file outside e2e/.
set -u
cd "$(dirname "$0")/.."
hits=$(find . \( -name '*.test.ts' -o -name '*.test.tsx' \) | grep -vE '^\./(e2e|node_modules|dist|android)/|/\.' | sort |
  xargs grep -nE 'host\.send\(' | grep -vE '(await|return) [A-Za-z0-9_.]*host\.send\(' | cut -d: -f1,2)
if [ -n "$hits" ]; then
  echo "host.send in a test without await:" >&2
  echo "$hits" >&2
  exit 1
fi
