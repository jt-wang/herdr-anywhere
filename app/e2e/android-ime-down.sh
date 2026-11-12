#!/bin/sh
# For e2e/android.sh: after the back key, the keyboard goes down and stays down; after a tap on an input box, it comes
# up and stays up. One look a second or two after can't tell a change that is only slow (a busy runner) from one that
# never comes or is undone, so this looks every HM_IME_POLL s (0.5) up to 20 times and passes once it has seen the
# keyboard as wanted 5 times in a row, saying after how many looks. It fails when the keyboard never got there, or
# left again after it did, with each look and the system's record of each show and hide (ime_log).
# Usage (sourced): ime_stays_down WHAT | ime_comes_up WHAT, with functions ime_now (prints up or down) and ime_log
#        e2e/android-ime-down.sh --self-test
ime_settles() { # want (up or down), what, the words for: got there, left again, never got there
  ime_want=$1 ime_what=$2 ime_got=$3 ime_left=$4 ime_never=$5 ime_looks="" ime_n=0 ime_in=0 ime_first=""
  ime_began=$(date +%s)
  while [ "$ime_n" -lt 20 ]; do
    ime_s=$(ime_now 2>/dev/null) || ime_s=unknown
    ime_n=$((ime_n + 1)); ime_looks="$ime_looks $ime_s"
    if [ "$ime_s" = "$ime_want" ]; then
      ime_in=$((ime_in + 1)); [ -n "$ime_first" ] || ime_first=$ime_n
      [ "$ime_in" -ge 5 ] && { echo "ok: $ime_what $ime_got ($ime_want at look $ime_first, one every ${HM_IME_POLL:-0.5} s; $(($(date +%s) - ime_began)) s of looking)"; return 0; }
    elif [ -n "$ime_first" ] && [ "$ime_s" != unknown ]; then
      echo "FAIL: $ime_what $ime_got and it $ime_left (looks:$ime_looks)"; ime_log; return 1
    fi
    sleep "${HM_IME_POLL:-0.5}"
  done
  echo "FAIL: $ime_what $ime_never ($ime_n looks in $(($(date +%s) - ime_began)) s:$ime_looks)"; ime_log
  return 1
}
ime_stays_down() { ime_settles down "$1" "put the keyboard away" "came back up" "left the keyboard up"; }
ime_comes_up() { ime_settles up "$1" "brought the keyboard up" "went down again" "brought no keyboard"; }
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
  # ime_comes_up: the same looks for a keyboard that a tap should bring up and keep up.
  up_() { want=$1 say=$2; shift 2; states "$@"
    ime_comes_up "a tap on the composer" > "$t/said"; rc=$?
    { [ "$want" = ok ] && [ "$rc" = 0 ]; } || { [ "$want" = FAIL ] && [ "$rc" = 1 ]; } ||
      { echo "FAIL: android-ime-down self-test: up, states $* gave exit $rc: $(cat "$t/said")"; fail=1; }
    grep -qF "$say" "$t/said" || { echo "FAIL: android-ime-down self-test: up, states $* say: $(cat "$t/said") (wanted: $say)"; fail=1; }
  }
  up_ ok "up at look 1" up
  up_ ok "up at look 3" down down up
  up_ ok "up at look 2" unknown up
  up_ FAIL "went down again (looks: up up down)" up up down
  up_ FAIL "brought no keyboard (20 looks" down
  # A look that failed (adb) is neither: it neither counts as there nor as gone again.
  up_ ok "up at look 1" up unknown up up up up
  case_ ok "down at look 1" down down unknown down down down
  up_ FAIL "(the record)" down
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: android-ime-down self-test" || exit 1
fi
