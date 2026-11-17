#!/bin/sh
# The self-tests the public CI runs after a device suite (devices.yml, before it uploads the pictures), that need no
# device, built program or extra tool: run here too, so test.sh catches one going red before a CI run spends a
# macOS runner on it. A red one there throws away a passing suite's pictures (ios-failure-logs, after the
# autostart-guard merge, cost two iPhone runs their screenshots).
# Usage: app/scripts/check-ci-selftests.sh [ignored]   HM_CHECK_ROOT=<app folder> checks another tree.
app=${HM_CHECK_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
fail=0
for s in e2e/app-lines.sh e2e/ci/groups.sh e2e/ci/traps.sh e2e/ios-failure-logs.sh e2e/ios-xcresult.sh; do
  out=$(cd "$app" && sh "$s" --self-test 2>&1) || { echo "check-ci-selftests: $s --self-test failed:"; printf '%s\n' "$out" | tail -5; fail=1; }
done
exit $fail
