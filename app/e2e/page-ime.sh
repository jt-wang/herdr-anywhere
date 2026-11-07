#!/bin/sh
# Runs e2e/page-ime.mjs. Needs Node and Playwright with its Chromium: PLAYWRIGHT_DIR is the playwright-core folder.
set -eu
cd "$(dirname "$0")/.."
: "${PLAYWRIGHT_DIR:=$(find "$HOME/.npm/_npx" -maxdepth 4 -type d -name playwright-core 2>/dev/null | head -1)}"
[ -n "$PLAYWRIGHT_DIR" ] || { echo "no playwright-core: npx playwright install chromium, or set PLAYWRIGHT_DIR" >&2; exit 1; }
export PLAYWRIGHT_DIR
exec node --experimental-transform-types --no-warnings e2e/page-ime.mjs
