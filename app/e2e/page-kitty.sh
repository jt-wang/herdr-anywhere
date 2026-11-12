#!/bin/sh
# Runs e2e/page-kitty.mjs. Needs Node and Playwright with its Chromium: PLAYWRIGHT_DIR is the playwright-core folder.
set -eu
cd "$(dirname "$0")/.."
[ -n "${PLAYWRIGHT_DIR:-}" ] || { echo "set PLAYWRIGHT_DIR to the playwright-core folder of a Playwright install that has its Chromium" >&2; exit 1; }
export PLAYWRIGHT_DIR
exec node --experimental-transform-types --no-warnings e2e/page-kitty.mjs
