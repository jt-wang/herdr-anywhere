#!/bin/sh
# After e2e/ios-uitest/tests/Hold.swift: one line per press on the space, how long UIKit held it down (from
# e2e/ios-touch-log.sh's report) and whether the space opened (the test's notes).
# The test itself notes, for each hold asked, how many opened nothing. The n-th "tapping the space" goes with the
# n-th "hold …:" result.
# Usage: e2e/ios-hold-say.sh NOTES REPORT
#        e2e/ios-hold-say.sh --self-test
say() {
  awk -F'\t' '
    FILENAME == ARGV[1] && / tapping the space \(round / { n++; sub(/.*\(round /, ""); sub(/\)$/, ""); asked[n] = $0 }
    FILENAME == ARGV[1] && / hold [0-9.]+ s: opened/ { r++; got[r] = ($0 ~ /opened nothing/) ? "opened nothing" : "opened" }
    FILENAME == ARGV[2] && /\(tapping the space \(round / { k++; held[k] = "no touch event"
      if (match($0, /UIKit held it [0-9]+ ms/)) held[k] = "UIKit held it " substr($0, RSTART + 14, RLENGTH - 14) }
    END {
      for (i = 1; i <= n; i++) print "  round " asked[i] ": " (i in held ? held[i] : "not in the report") ", " (i in got ? got[i] : "no result")
    }' "$1" "$2"
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  cat > "$t/notes" <<'EON'
10:00:01.000 +1.0 s load 9 tapping the space (round 1, hold 0.05 s)
10:00:01.300 +1.3 s load 9 tapped the space (hold 0.05 s)
10:00:01.900 +1.9 s load 9 hold 0.05 s: opened
10:00:02.500 +2.5 s load 9 back home: true
10:00:03.000 +3.0 s load 9 tapping the space (round 1, hold 0.16 s)
10:00:03.400 +3.4 s load 9 tapped the space (hold 0.16 s)
10:00:06.400 +6.4 s load 9 hold 0.16 s: opened nothing in 3 s
10:00:07.000 +7.0 s load 9 tapping the space (round 2, hold 0.05 s)
10:00:07.300 +7.3 s load 9 tapped the space (hold 0.05 s)
10:00:10.300 +10.3 s load 9 hold 0.05 s: opened nothing in 3 s
EON
  cat > "$t/report" <<'EOR'
  the app's UIKit log: 12 lines
  UIKit sent the app 4 touch events from 10:00:01.000 to 10:00:01.300 (tapping the space (round 1, hold 0.05 s)); the page's own record says whether it got them; UIKit held it 61 ms
    app window: phase=began
  UIKit sent the app 4 touch events from 10:00:03.000 to 10:00:03.400 (tapping the space (round 1, hold 0.16 s)); the page's own record says whether it got them; UIKit held it 168 ms
  UIKit sent the app 0 touch events from 10:00:07.000 to 10:00:07.300 (tapping the space (round 2, hold 0.05 s)); the page's own record says whether it got them
EOR
  say "$t/notes" "$t/report" > "$t/said"
  printf '%s\n' "  round 1, hold 0.05 s: UIKit held it 61 ms, opened" "  round 1, hold 0.16 s: UIKit held it 168 ms, opened nothing" \
    "  round 2, hold 0.05 s: no touch event, opened nothing" > "$t/want"
  cmp -s "$t/said" "$t/want" || { echo "FAIL: ios-hold-say self-test: said"; cat "$t/said"; echo "wanted"; cat "$t/want"; fail=1; }
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: ios-hold-say self-test" || exit 1
  exit 0
fi
echo "each press on the space (e2e/ios-uitest/tests/Hold.swift):"
say "$1" "$2"
