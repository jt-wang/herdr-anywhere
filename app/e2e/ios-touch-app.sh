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
# And the steps of a tap on a space's row on home, each a request the Companion logs, "GET /hm-e2e/rowtap/STEP/ID/ROW/<the
# app's Date.now(), ms>": the row made (ROW numbers each row as it is made, so a row made again between a finger's down
# and up shows), the row's onPress (press) and the navigation openSpace then pushed (push). These add no handler to the
# row: its onPress is the one it has, called as before. And the row's onLongPress (long), its menu. A first tap that
# opened nothing then says where it stopped (e2e/rowtap-say.sh): no press (lost before the row's onPress), a long
# press, a row made again around the tap, or a press and no push.
# HM_ROWTAP_HANDLERS=1 also adds handlers for the finger's touches on the row (touch-start, touch-end, touch-cancel) and
# NSH's press down and up (press-down, press-up), to tell the touch from the press; handlers of its own may change how
# NSH takes the tap, so a run with them says whether the first tap still opened nothing as often.
# HM_ROWTAP_BROKEN=1, for the suites' self-test (ios_rowtap): the row's onPress returns at once, so a tap opens nothing
# and the record must say: press-down, no press.
python3 -I - "$tree/app/src/ui/home.tsx" "$root" "${HM_ROWTAP_HANDLERS:-}" "${HM_ROWTAP_BROKEN:-}" <<'PY' || exit 1
import sys
home, root, handlers, broken = sys.argv[1], sys.argv[2], sys.argv[3] == "1", sys.argv[4] == "1"
def change(path, pairs):
    s = open(path).read()
    for old, new in pairs:
        if s.count(old) != 1:
            sys.exit(f"FAIL: {path.split('/app/')[-1]} has changed: no single place for the tap's steps ({old[:60]!r})")
        s = s.replace(old, new)
    open(path, "w").write(s)
row = "props.space.id, hmRow"
extra = (f" onPressDown={{() => hmStep('press-down', {row})}} onPressUp={{() => hmStep('press-up', {row})}}"
         f" onTouchStart={{() => hmStep('touch-start', {row})}} onTouchEnd={{() => hmStep('touch-end', {row})}}"
         f" onTouchCancel={{() => hmStep('touch-cancel', {row})}}") if handlers else ""
change(home, [
    ("function SpaceRow(props: { store: Store; space: Space; color: string; actions: HomeActions }) {\n",
     "function SpaceRow(props: { store: Store; space: Space; color: string; actions: HomeActions }) {\n"
     f"  const hmRow = ++hmRows; hmStep('row-made', {row})\n"),
    ("onPress={() => props.actions.openSpace(props.space.id)} onLongPress={menu}",
     f"onPress={{() => {{ {'return; ' if broken else ''}hmStep('press', {row}); props.actions.openSpace(props.space.id) }}}} onLongPress={{menu}}" + extra),
    ("  const menu = async () => {\n", f"  const menu = async () => {{\n    hmStep('long', {row})\n"),
])
with open(home, "a") as f:
    f.write("import { PAIR as hmPair } from '../../e2e/pair.ts'\n"
            "let hmRows = 0\n"
            "function hmStep(step: string, id: string, row: number) {\n"
            "  fetch(new URL(hmPair).origin + '/hm-e2e/rowtap/' + step + '/' + id + '/' + row + '/' + Date.now()).catch(() => {})\n"
            "}\n")
change(root, [("    nav.push({ name: 'space', id })\n  }\n",
               "    nav.push({ name: 'space', id })\n"
               "    fetch(new URL(hmPair).origin + '/hm-e2e/rowtap/push/' + id + '/0/' + Date.now()).catch(() => {})\n  }\n")])
PY
page="$tree/app/src/ui/terminal-page.ts"
[ "$via" = none ] || { [ "$(grep -c '<script>window.__terminal=' "$page")" = 1 ] ||
  { echo "FAIL: the terminal page has changed: no single place for e2e/touch-times.ts" >&2; exit 1; }
sed 's/^export function terminalPage(/function terminalPageAsShipped(/' "$page" > "$page.new" && mv "$page.new" "$page"
# The page's look at how far a wheel report moves a program tells e2e/touch-times.ts what it found and after how long
# (window.hmLook), the way the page goes on as before. HM_TOUCH_LOOK_BROKEN makes a copy whose word and deed differ,
# for the scroll suite's self-test (ios_scroll_selftest): look-says-measured says 3 rows whatever came, and the page goes
# on as if no answer came (a report per row); look-says-missed says no answer, and the page goes on as if 3 came.
python3 -I - "$page" "${HM_TOUCH_LOOK_BROKEN:-}" <<'PY' || exit 1
import sys
page, broken = sys.argv[1], sys.argv[2]
if broken not in ("", "look-says-measured", "look-says-missed"):
    sys.exit(f"FAIL: HM_TOUCH_LOOK_BROKEN is look-says-measured, look-says-missed or empty, not {broken!r}")
s = open(page).read()
for old, new in [("var p = wheel.probe = { before: screenRows(),", "var p = wheel.probe = { hmAt: performance.now(), before: screenRows(),"),
                 ("      if (k) wheel.seen.push(k); else wheel.misses++;\n",
                  {"": "      if (window.hmLook) window.hmLook(k, performance.now() - p.hmAt);\n",
                   "look-says-measured": "      if (window.hmLook) window.hmLook(3, performance.now() - p.hmAt); k = 0;\n",
                   "look-says-missed": "      if (window.hmLook) window.hmLook(0, performance.now() - p.hmAt); k = k || 3;\n"}[broken]
                  + "      if (k) wheel.seen.push(k); else wheel.misses++;\n")]:
    if s.count(old) != 1:
        sys.exit(f"FAIL: the terminal page has changed: no single place for the look's word to e2e/touch-times.ts ({old.strip()!r})")
    s = s.replace(old, new)
open(page, "w").write(s)
PY
cat >> "$page" <<TS
import { touchTimes } from '../../e2e/touch-times.ts'
export function terminalPage(opts: Parameters<typeof terminalPageAsShipped>[0]): string {
  return terminalPageAsShipped(opts).replace('<script>window.__terminal=', () => '<script>' + touchTimes('$via') + '</script><script>window.__terminal=')
}
TS
}
"$tree/app/ios/build.sh" ios-e2e.tsx simulator > "$folder/touch-app-build.log" 2>&1 ||
  { tail -20 "$folder/touch-app-build.log" >&2; echo "FAIL: ios/build.sh (the copy with e2e/touch-times.ts)" >&2; exit 1; }
echo "$tree/app/ios/build/Herdr.app"
