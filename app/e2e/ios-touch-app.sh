#!/bin/sh
# Builds the iOS test app (ios-e2e.tsx) from a copy of app/ in FOLDER whose terminal page also runs e2e/touch-times.ts,
# which tells the test when the page got each touch and what it made of it, by VIA (input or label; see there), or
# not (none: only the request on opening a space, below).
# app/ itself is not changed. PAIR is the e2e/pair.ts the copy gets. Prints where the app is.
# Usage: e2e/ios-touch-app.sh FOLDER PAIR VIA     Needs what ios/build.sh needs, and rsync.
set -eu
folder=$1; pair=$2; via=$3
case $via in input|label|none) ;; *) echo "FAIL: ios-touch-app.sh: VIA is input, label or none, not $via" >&2; exit 2 ;; esac
app=$(cd "$(dirname "$0")/.." && pwd)
tree="$folder/tree"
rm -rf "${tree:?}"; mkdir -p "$tree"
rsync -a --exclude /ios/build --exclude /ios/nsh --exclude /android "$app/" "$tree/app/"
ln -s "$app/../third_party" "$tree/third_party"
cp "$pair" "$tree/app/e2e/pair.ts"
# And a request to the Companion each time the app opens a space (the copy's root.tsx), which its log shows as
# "GET /hm-e2e/openSpace/ID/<the app's Date.now(), ms>/<the top route>": the test can tell a tap that never reached
# the app from one that did and opened nothing, and one that reached it late (the Companion logs a request once it is
# done, and on CI a request to it took up to 3 s). Not a console.log: NSH's iOS runtime keeps those to itself (a run caught none on the app's stdout,
# its stderr or the simulator's log).
root="$tree/app/src/ui/root.tsx"
[ "$(grep -c '^  const openSpace = (id: string) => {$' "$root")" = 1 ] ||
  { echo "FAIL: root.tsx has changed: no single openSpace for the log line" >&2; exit 1; }
sed 's/^  const openSpace = (id: string) => {$/&\
    fetch(new URL(hmPair).origin + "\/hm-e2e\/openSpace\/" + id + "\/" + Date.now() + "\/" + encodeURIComponent(JSON.stringify(nav.routes().at(-1)))).catch(() => {})/' "$root" > "$root.new" && mv "$root.new" "$root"
echo "import { PAIR as hmPair } from '../../e2e/pair.ts'" >> "$root"
page="$tree/app/src/ui/terminal-page.ts"
[ "$via" = none ] || { [ "$(grep -c '<script>window.__terminal=' "$page")" = 1 ] ||
  { echo "FAIL: the terminal page has changed: no single place for e2e/touch-times.ts" >&2; exit 1; }
sed 's/^export function terminalPage(/function terminalPageAsShipped(/' "$page" > "$page.new" && mv "$page.new" "$page"
cat >> "$page" <<TS
import { touchTimes } from '../../e2e/touch-times.ts'
export function terminalPage(opts: Parameters<typeof terminalPageAsShipped>[0]): string {
  return terminalPageAsShipped(opts).replace('<script>window.__terminal=', () => '<script>' + touchTimes('$via') + '</script><script>window.__terminal=')
}
TS
}
# And the copy's window writes where each touch landed to the app's log (e2e/ios-touch-window.swift), which
# e2e/ios-touch-log.sh shows beside each tap: the view, the gesture recognizers and their states, a picture sheet's views.
# HM_TOUCH_WINDOW=off leaves the window as shipped, to compare taps with and without it.
swift="$tree/app/ios/Sources/App.swift"
[ "${HM_TOUCH_WINDOW:-}" = off ] || {
  [ "$(grep -cE 'UIWindow\((windowScene: scene|frame: UIScreen\.main\.bounds)\)' "$swift")" = 1 ] ||
    { echo "FAIL: App.swift has changed: no single place the window is made, for e2e/ios-touch-window.swift" >&2; exit 1; }
  sed -E 's/UIWindow\((windowScene: scene|frame: UIScreen\.main\.bounds)\)/HMTouchWindow(\1)/' "$swift" > "$swift.new" && mv "$swift.new" "$swift"
  cat "$app/e2e/ios-touch-window.swift" >> "$swift"
}
"$tree/app/ios/build.sh" ios-e2e.tsx simulator > "$folder/touch-app-build.log" 2>&1 ||
  { tail -20 "$folder/touch-app-build.log" >&2; echo "FAIL: ios/build.sh (the copy with e2e/touch-times.ts)" >&2; exit 1; }
echo "$tree/app/ios/build/Herdr.app"
