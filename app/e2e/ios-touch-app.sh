#!/bin/sh
# Builds the iOS test app (ios-e2e.tsx) from a copy of app/ in FOLDER whose terminal page also runs e2e/touch-times.ts,
# which tells the test when the page got each touch and what it made of it, by VIA (input or label; see there).
# app/ itself is not changed. PAIR is the e2e/pair.ts the copy gets. Prints where the app is.
# Usage: e2e/ios-touch-app.sh FOLDER PAIR VIA     Needs what ios/build.sh needs, and rsync.
set -eu
folder=$1; pair=$2; via=$3
case $via in input|label) ;; *) echo "FAIL: ios-touch-app.sh: VIA is input or label, not $via" >&2; exit 2 ;; esac
app=$(cd "$(dirname "$0")/.." && pwd)
tree="$folder/tree"
rm -rf "$tree"; mkdir -p "$tree"
rsync -a --exclude /ios/build --exclude /ios/nsh --exclude /android "$app/" "$tree/app/"
ln -s "$app/../third_party" "$tree/third_party"
cp "$pair" "$tree/app/e2e/pair.ts"
# And a line on the app's stderr each time it opens a space (the copy's root.tsx), "hm-e2e openSpace ID top=…":
# the test can tell a tap that never reached the app from one that did and opened nothing.
root="$tree/app/src/ui/root.tsx"
[ "$(grep -c '^  const openSpace = (id: string) => {$' "$root")" = 1 ] ||
  { echo "FAIL: root.tsx has changed: no single openSpace for the log line" >&2; exit 1; }
sed 's/^  const openSpace = (id: string) => {$/&\
    console.log("hm-e2e openSpace " + id + " top=" + JSON.stringify(nav.routes().at(-1)))/' "$root" > "$root.new" && mv "$root.new" "$root"
page="$tree/app/src/ui/terminal-page.ts"
[ "$(grep -c '<script>window.__terminal=' "$page")" = 1 ] ||
  { echo "FAIL: the terminal page has changed: no single place for e2e/touch-times.ts" >&2; exit 1; }
sed 's/^export function terminalPage(/function terminalPageAsShipped(/' "$page" > "$page.new" && mv "$page.new" "$page"
cat >> "$page" <<TS
import { touchTimes } from '../../e2e/touch-times.ts'
export function terminalPage(opts: Parameters<typeof terminalPageAsShipped>[0]): string {
  return terminalPageAsShipped(opts).replace('<script>window.__terminal=', () => '<script>' + touchTimes('$via') + '</script><script>window.__terminal=')
}
TS
"$tree/app/ios/build.sh" ios-e2e.tsx simulator > "$folder/touch-app-build.log" 2>&1 ||
  { tail -20 "$folder/touch-app-build.log" >&2; echo "FAIL: ios/build.sh (the copy with e2e/touch-times.ts)" >&2; exit 1; }
echo "$tree/app/ios/build/Herdr.app"
