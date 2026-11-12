#!/bin/sh
# For e2e/android.sh: after the back key, the keyboard goes down and stays down. One look a second or two after the
# key can't tell a hide that is only slow (a busy runner) from one that never comes or a keyboard that comes back up,
# so this looks every HM_IME_POLL s (0.5) up to 20 times and passes once it has seen the keyboard down 5 times in a row,
# saying after how many looks it went down. It fails when the keyboard never went down, or came back up after it did,
# with each look and the system's record of each show and hide (ime_log).
# Usage (sourced): ime_stays_down WHAT, with functions ime_now (prints up or down) and ime_log (the system's record)
#        e2e/android-ime-down.sh --self-test
ime_stays_down() {
  ime_what=$1 ime_looks="" ime_n=0 ime_down=0 ime_first=""
  ime_began=$(date +%s)
  while [ "$ime_n" -lt 20 ]; do
    ime_s=$(ime_now 2>/dev/null) || ime_s=unknown
    ime_n=$((ime_n + 1)); ime_looks="$ime_looks $ime_s"
    if [ "$ime_s" = down ]; then
      ime_down=$((ime_down + 1)); [ -n "$ime_first" ] || ime_first=$ime_n
      [ "$ime_down" -ge 5 ] && { echo "ok: $ime_what put the keyboard away (down at look $ime_first, one every ${HM_IME_POLL:-0.5} s; $(($(date +%s) - ime_began)) s of looking)"; return 0; }
    elif [ -n "$ime_first" ]; then
      echo "FAIL: $ime_what put the keyboard away and it came back up (looks:$ime_looks)"; ime_log; return 1
    fi
    sleep "${HM_IME_POLL:-0.5}"
  done
  echo "FAIL: $ime_what left the keyboard up ($ime_n looks in $(($(date +%s) - ime_began)) s:$ime_looks)"; ime_log
  return 1
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  export HM_IME_POLL=0
  # The keyboard's state as a list of answers, one per look, the last one for every look after.
  ime_now() {
    n=$(($(cat "$t/calls" 2>/dev/null || echo 0) + 1)); echo "$n" > "$t/calls"
    line=$(sed -n "${n}p" "$t/states"); [ -n "$line" ] || line=$(tail -1 "$t/states")
    echo "$line"
  }
  ime_log() { echo "  (the record)"; }
  states() { rm -f "${t:?}/calls"; : > "$t/states"; for s in "$@"; do echo "$s" >> "$t/states"; done; }
  case_() { # want (ok or FAIL), what it must say, then the states
    want=$1 say=$2; shift 2; states "$@"
    ime_stays_down "the back key" > "$t/said"; rc=$?
    { [ "$want" = ok ] && [ "$rc" = 0 ]; } || { [ "$want" = FAIL ] && [ "$rc" = 1 ]; } ||
      { echo "FAIL: android-ime-down self-test: states $* gave exit $rc: $(cat "$t/said")"; fail=1; }
    grep -qF "$say" "$t/said" || { echo "FAIL: android-ime-down self-test: states $* say: $(cat "$t/said") (wanted: $say)"; fail=1; }
  }
  case_ ok "down at look 1" down
  case_ ok "down at look 4" up up up down
  case_ ok "down at look 2" unknown down
  case_ FAIL "came back up (looks: down down up)" down down up
  case_ FAIL "came back up" up down down down down up
  case_ FAIL "left the keyboard up (20 looks" up
  case_ FAIL "(the record)" up
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: android-ime-down self-test" || exit 1
fi
