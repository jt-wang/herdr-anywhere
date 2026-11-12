#!/bin/sh
# For e2e/ios-tui-scroll.sh: what the first drag on the program (e2e/tui-panels.py, 3 rows a wheel report) should have
# sent, from what the page says of its look. The page doesn't know how far a report moves a program, so in a drag it
# sends one alone and waits up to 600 ms for the program's redraw to say (look=K@MS in the drag's touch line, from
# e2e/touch-times.ts: K rows, after MS ms; 0 when no answer came in time). Measured, the drag sends a report per K rows
# of finger travel; not, one per row. A busy runner can leave the look unanswered, so either is right, as long as the
# reports follow what the page measured.
# Usage: e2e/ios-scroll-drag.sh WHEELS FINGER    WHEELS: the program's file, FINGER: the drag's travel in rows
#        e2e/ios-scroll-drag.sh --self-test
drag_check() {
  wheels=$1 finger=$2
  n=$(awk '/^mark /{on = ($2 == "drag"); next} on && !/^touch /' "$wheels" | grep -c . || true)
  look=$(awk '/^mark /{on = ($2 == "drag"); next} on && /^touch /' "$wheels" | head -1 | sed -nE 's/.* look=([0-9]+)@([0-9]+).*/\1 \2/p')
  if [ -z "$look" ]; then
    echo "FAIL: the drag's touch line says nothing of the page's look (look=K@MS, e2e/touch-times.ts); it sent $n reports"; return 1
  fi
  set -- $look; k=$1 ms=$2
  if [ "$k" = 0 ]; then
    want=$(python3 -c "import sys; print(int(float(sys.argv[1])))" "$finger")
    what="one per row: the page's look had no answer after $ms ms"
  elif [ "$k" = 3 ]; then
    want=$(python3 -c "import sys; print(int(float(sys.argv[1]) // 3))" "$finger")
    what="one per 3 rows, as the page measured in $ms ms"
  else
    echo "FAIL: the page measured $k rows a report in $ms ms; the program moves 3"; return 1
  fi
  if [ "$n" -ge $((want - 1)) ] && [ "$n" -le $((want + 1)) ]; then
    echo "ok: a drag of $finger rows sends $n reports, $what"
  else
    echo "FAIL: a drag of $finger rows sent $n reports ($what is $want)"; return 1
  fi
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  # A drag's file: N reports, then the touch line with LOOK (or none).
  drag() { { echo "mark drag 686.0"; i=0; while [ "$i" -lt "$1" ]; do echo "up 14 3 left"; i=$((i + 1)); done
    echo "touch start=0/40 move=133/151 hold=- end=1549/1567 moves=45 stopped=0 posts=tail${2:+ look=$2}"; echo "mark right 686.0"; echo "up 40 3 right"; } > "$t/wheels"; }
  case_() { exp=$1 say=$2; shift 2; drag "$@"
    drag_check "$t/wheels" 11.6 > "$t/said"; rc=$?
    { [ "$exp" = ok ] && [ "$rc" = 0 ]; } || { [ "$exp" = FAIL ] && [ "$rc" = 1 ]; } ||
      { echo "FAIL: ios-scroll-drag self-test: $* gave exit $rc: $(cat "$t/said")"; fail=1; }
    grep -qF "$say" "$t/said" || { echo "FAIL: ios-scroll-drag self-test: $* says: $(cat "$t/said") (wanted: $say)"; fail=1; }
  }
  case_ ok "sends 3 reports, one per 3 rows, as the page measured in 120 ms" 3 3@120
  case_ ok "sends 11 reports, one per row: the page's look had no answer after 601 ms" 11 0@601
  # The page says it measured, yet the drag went a report per row; it says it didn't, yet the reports went per 3 rows.
  case_ FAIL "sent 11 reports (one per 3 rows, as the page measured in 120 ms is 3)" 11 3@120
  case_ FAIL "sent 3 reports (one per row: the page's look had no answer after 601 ms is 11)" 3 0@601
  case_ FAIL "says nothing of the page's look" 3 ""
  case_ FAIL "measured 1 rows a report" 11 1@90
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: ios-scroll-drag self-test" || exit 1
else
  drag_check "$@"
fi
