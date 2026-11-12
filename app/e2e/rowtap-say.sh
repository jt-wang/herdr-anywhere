#!/bin/sh
# What the test app did with the taps on a space's row on home (e2e/ios-touch-app.sh: a request to the Companion at
# each step, "GET /hm-e2e/rowtap/STEP/ID/ROW/<the app's Date.now(), ms>"), from the Companion's log: each step in
# order with its ms after the first, and what came of the first tap. A tap starts at the row's press-down (or its
# touch-start), with HM_ROWTAP_HANDLERS=1; without them only the row made, press, long and push are there, and the
# first of press and long is the first tap's. The first tap was:
#   press           the row's onPress ran (and push: openSpace pushed the space's page, or not)
#   long press      the row's onLongPress ran (its menu), not its onPress
#   down, no press  NSH pressed the row down and then called neither (press-up says whether it let go)
#   no press-down   touches reached the row's handlers, and NSH made no press of them
#   nothing         no step of a tap at all
# Usage: e2e/rowtap-say.sh SERVE_LOG ID     e2e/rowtap-say.sh --self-test
say() {
  python3 -I - "$1" "$2" <<'PY'
import re, sys
log, want = sys.argv[1], sys.argv[2]
steps = []
try:
    for line in open(log, encoding="utf-8", errors="replace"):
        m = re.search(r"GET /hm-e2e/rowtap/([a-z-]+)/(\S+)/(\d+)/(\d+)(?=[\s?]|$)", line)  # an ID may have a / (box/sp…)
        if m and m.group(2) == want: steps.append((int(m.group(4)), m.group(1), int(m.group(3))))
except OSError: pass
steps.sort()
if not steps:
    print(f"  rowtap {want}: no step (the test app made no row for it, or its requests didn't reach the Companion)"); sys.exit(0)
t0 = steps[0][0]
print(f"  rowtap {want}: " + " ".join(f"{s}{'' if s == 'push' else '#' + str(r)}@{t - t0}" for t, s, r in steps))
names = [s for _, s, _ in steps]
starts = [i for i, s in enumerate(names) if s in ("press-down", "touch-start")]
if starts:
    # The first tap: from its first touch-start or press-down to the next tap's.
    i = starts[0]
    nxt = next((j for j in starts if j > i and names[j] == names[i]), len(names))
    tap = steps[i:nxt]; got = [s for _, s, _ in tap]; at = tap[0][0]
    def ms(name): return next(t for t, s, _ in tap if s == name) - at
    if "press" in got:
        what = f"press, {ms('press')} ms after its first touch" + (", then push" if "push" in got else ", and no push")
    elif "long" in got:
        what = f"long press: the row's menu, {ms('long')} ms after its first touch" + ("" if "press-down" not in got else f" (press-down at {ms('press-down')} ms)")
    elif "press-down" in got:
        what = "down, no press: NSH pressed the row down and called neither onPress nor onLongPress" + (f" (press-up at {ms('press-up')} ms)" if "press-up" in got else " (and no press-up)")
    else:
        what = "no press-down: touches reached the row and NSH made no press of them"
else:
    first = next((s for s in names if s in ("press", "long")), None)
    if first == "press": what = "press" + (", then push" if "push" in names[names.index("press"):] else ", and no push")
    elif first == "long": what = "long press: the row's menu, not its onPress"
    else: what = "nothing: no press and no long press (no handlers for its touches in this build)"
print(f"  rowtap {want}: the first tap: {what}")
PY
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  log() { : > "$t/log"; for s in "$@"; do echo "127.0.0.1 GET /hm-e2e/rowtap/$s 404 0s" >> "$t/log"; done; }
  case_() { want=$1; shift; log "$@"; id=spios; case $1 in */box/*) id=box/sppicture ;; esac; got=$(say "$t/log" "$id" | tail -1)
    case $got in *"$want"*) ;; *) echo "FAIL: rowtap-say self-test: $* says: $got (wanted: $want)"; fail=1 ;; esac; }
  case_ "press, 90 ms after its first touch, then push" row-made/spios/1/1000 touch-start/spios/1/2000 press-down/spios/1/2010 press-up/spios/1/2080 press/spios/1/2090 push/spios/0/2100
  # Out of order in the log (the Companion logs a request once it is done): sorted by the app's time.
  case_ "long press: the row's menu, 600 ms after its first touch (press-down at 0 ms)" row-made/spios/1/1000 long/spios/1/2600 press-down/spios/1/2000 press-up/spios/1/2700
  case_ "down, no press: NSH pressed the row down and called neither onPress nor onLongPress (press-up at 70 ms)" row-made/spios/1/1000 press-down/spios/1/2000 press-up/spios/1/2070 press-down/spios/1/5000 press/spios/1/5080
  case_ "(and no press-up)" row-made/spios/1/1000 press-down/spios/1/2000
  case_ "no press-down: touches reached the row" row-made/spios/1/1000 touch-start/spios/1/2000 touch-end/spios/1/2090
  case_ "press, then push" row-made/spios/1/1000 press/spios/1/2000 push/spios/0/2010
  case_ "long press: the row's menu, not its onPress" row-made/spios/1/1000 long/spios/1/2600 press/spios/1/6000
  case_ "nothing: no press and no long press" row-made/spios/1/1000
  case_ "no step" row-made/other/1/1000
  log row-made/box/sppicture/1/1000 press/box/sppicture/1/2000 push/box/sppicture/0/2010
  case_ "press, then push" row-made/box/sppicture/1/1000 press/box/sppicture/1/2000 push/box/sppicture/0/2010
  got=$(say "$t/log" spios | head -1)
  log row-made/spios/1/1000 row-made/spios/2/1500 press/spios/2/2000 push/spios/0/2010
  [ "$(say "$t/log" spios | head -1)" = "  rowtap spios: row-made#1@0 row-made#2@500 press#2@1000 push@1010" ] ||
    { echo "FAIL: rowtap-say self-test: the steps say: $(say "$t/log" spios | head -1)"; fail=1; }
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: rowtap-say self-test" || exit 1
else
  say "$@"
fi
