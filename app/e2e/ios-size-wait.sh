#!/bin/sh
# For e2e/ios-picture.sh: waits for the phone to set the terminal's size, then writes it ("cols rows") to OUT for the
# test, which waits 30 s for it once it has opened the space. It waits as long as the test (PID) runs: the test opens
# the space only after xcodebuild has built and launched it, which on a CI runner took over 4 min, past the 120 tries
# the wait used to have (the test then said "the script never wrote the terminal's size"). Only a size read as two
# numbers counts; it says when the size came, or that the test ended first.
# Usage (sourced): size_wait BEFORE OUT PID, with a function size_now that prints the terminal's "cols rows"
#        e2e/ios-size-wait.sh --self-test
size_ok() { printf '%s\n' "$1" | grep -Eqx '[0-9]+ [0-9]+'; }
size_wait() {
  before=$1 out=$2 pid=$3 began=$(date +%s)
  while kill -0 "$pid" 2>/dev/null; do
    now=$(size_now 2>/dev/null) || now=
    if size_ok "$now" && [ "$now" != "$before" ]; then
      sleep "${HM_SIZE_SETTLE:-2}" # the phone may set it twice; the second one stands
      now=$(size_now 2>/dev/null) || now=
      if size_ok "$now"; then
        echo "$now" > "$out.new" && mv "$out.new" "$out"
        echo "  the phone set the terminal to $now ($(($(date +%s) - began)) s after the test was started)"
        return 0
      fi
    fi
    sleep "${HM_SIZE_POLL:-1}"
  done
  echo "  the test ended with the terminal's size never changed from $before ($(($(date +%s) - began)) s)"
}
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  export HM_SIZE_POLL=0 HM_SIZE_SETTLE=0
  # The terminal's size as a list of answers, one per call, the last one for every call after.
  size_now() {
    n=$(($(cat "$t/calls" 2>/dev/null || echo 0) + 1)); echo "$n" > "$t/calls"
    line=$(sed -n "${n}p" "$t/sizes"); [ -n "$line" ] || line=$(tail -1 "$t/sizes")
    [ "$line" = fail ] && return 1
    echo "$line"
  }
  sizes() { rm -f "${t:?}/calls" "${t:?}/out"; : > "$t/sizes"; for s in "$@"; do echo "$s" >> "$t/sizes"; done; }
  # 1. The test is built and launched for minutes before it opens the space: the size changes after 150 calls.
  sizes $(i=0; while [ $i -lt 150 ]; do echo 80x24; i=$((i + 1)); done) 40x30
  sed -i.bak 's/x/ /' "$t/sizes"
  sleep 60 & p=$!
  size_wait "80 24" "$t/out" "$p" > "$t/said"
  [ "$(cat "$t/out" 2>/dev/null)" = "40 30" ] ||
    { echo "FAIL: ios-size-wait self-test: the size set after 150 calls is not written ($(cat "$t/out" 2>/dev/null))"; fail=1; }
  kill "$p" 2>/dev/null; wait "$p" 2>/dev/null
  # 2. Reading the size fails a few times (the core busy): nothing is written until a real size comes.
  sizes "80 24" fail fail "40 30"
  sleep 60 & p=$!
  size_wait "80 24" "$t/out" "$p" > "$t/said"
  [ "$(cat "$t/out" 2>/dev/null)" = "40 30" ] ||
    { echo "FAIL: ios-size-wait self-test: after reads that failed, it wrote '$(cat "$t/out" 2>/dev/null)', not 40 30"; fail=1; }
  kill "$p" 2>/dev/null; wait "$p" 2>/dev/null
  # 3. The test ends with the size never changed: the wait ends too, writes nothing, and says so.
  sizes "80 24"
  sleep 1 & p=$!
  HM_SIZE_POLL=0.05 size_wait "80 24" "$t/out" "$p" > "$t/said"
  [ -e "$t/out" ] && { echo "FAIL: ios-size-wait self-test: a size was written though it never changed"; fail=1; }
  grep -q "never changed" "$t/said" || { echo "FAIL: ios-size-wait self-test: it says: $(cat "$t/said")"; fail=1; }
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: ios-size-wait self-test" || exit 1
fi
