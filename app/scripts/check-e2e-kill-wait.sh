#!/bin/sh
# The e2e suites' scripts, checked where test.sh runs and not only after an iOS suite on CI: e2e/ios-failure-logs.sh's
# self-test reads every e2e script (a job stopped with kill and then waited on says || true, or set -e ends the suite
# with 143 and no word of why; a load sampler stops before its folder is deleted). On CI it runs in the iOS job's
# upload check, after the suites, where a slip cost a whole macOS run and its upload. Any argument is not used.
exec sh "$(cd "$(dirname "$0")/.." && pwd)/e2e/ios-failure-logs.sh" --self-test
