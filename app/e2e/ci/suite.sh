#!/bin/bash
# Runs one suite in a CI job and goes on: its output to the log and to $RUNNER_TEMP/out/<name>.log, its result and
# time to $RUNNER_TEMP/out/results.txt and the job's summary. The job's last step, --finish, fails if any failed, so
# one failing suite doesn't hide the ones after it.
# Usage: app/e2e/ci/suite.sh NAME COMMAND…     app/e2e/ci/suite.sh --finish
set -uo pipefail
out=${RUNNER_TEMP:?}/out
mkdir -p "$out"
if [ "${1:-}" = --finish ]; then
  [ -s "$out/results.txt" ] || { echo "FAIL: no suite ran"; exit 1; }
  cat "$out/results.txt"
  ! grep -q ' FAIL ' "$out/results.txt"
  exit
fi
name=$1; shift
start=$(date +%s)
echo "::group::$name"
"$@" 2>&1 | tee "$out/$name.log"
rc=${PIPESTATUS[0]}
echo "::endgroup::"
took=$(($(date +%s) - start))
if [ "$rc" = 0 ]; then r=PASS; else r=FAIL; echo "::error title=$name::exit $rc (log: $name.log)"; fi
echo "$name $r ${took}s" | tee -a "$out/results.txt"
echo "- $name: $r in ${took} s" >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
exit 0
