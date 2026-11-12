# Sourced by the Android suites (sh): $adb runs adb and, when a call fails, writes down which call, its exit status and
# when, so a suite that stops on it (set -e) says which one. A device-side command killed on the phone (exit 137,
# SIGKILL: Android kills tools like uiautomator and input when short of memory) also gets the phone log's lines about
# kills just then. A call ended by SIGPIPE (exit 141: "$adb" shell … | grep -q, the grep done reading) is only counted:
# it is how those checks work, and the list stays for the one that stopped the suite. adb_failures prints it all.
# A suite's EXIT trap is adb_exit: trap 'adb_exit $? cleanup' EXIT (or a command, or nothing). It prints the list when
# the suite failed, then runs the command with $? still the suite's status (a cleanup that reads it first sees the
# failure), and exits with that status. Not "adb_done $?; cleanup": under set -e a trap's failed command ends the
# shell there, so on a failure the cleanup never ran.
# Usage: . e2e/adb-note.sh; adb_note "$ADB_PATH" [FILE]    then "$adb" … as before
#        adb_gone && exit 1    in a loop that waits on the phone
#        sh e2e/adb-note.sh --self-test
adb_note() {
  adb_real=$1 adb_file=${2:-$(mktemp)}
  : > "$adb_file"; : > "$adb_file.141"
  adb=adb_noted
}
adb_noted() {
  adb_rc=0
  "$adb_real" "$@" || adb_rc=$?
  if [ "$adb_rc" = 141 ]; then
    echo >> "$adb_file.141"
  elif [ "$adb_rc" != 0 ]; then
    echo "$(date -u +%T) adb $* -> exit $adb_rc" >> "$adb_file"
    if [ "$adb_rc" = 137 ]; then
      "$adb_real" logcat -d -t 300 2>/dev/null | grep -iE 'lowmemorykiller|lmkd|kill(ing)? |uiautomator|InputManager' | tail -20 | sed 's/^/    /' >> "$adb_file"
    fi
  fi
  return "$adb_rc"
}
# True, and says so, once adb has not found the phone for HM_ADB_GONE s (20) in a row: the emulator went, and a suite
# that waits on the phone (an upload, 600 s) should stop then, not run out its wait. adb root and unroot lose it for
# about 2 s. Call it from the suite's own shell (not in $(…)), where it keeps the time it first missed the phone.
adb_gone() {
  if [ "$("$adb_real" get-state 2>/dev/null | tr -d '\r')" = device ]; then adb_since=; return 1; fi
  [ -n "${adb_since:-}" ] || adb_since=$(date +%s)
  [ $(($(date +%s) - adb_since)) -ge "${HM_ADB_GONE:-20}" ] || return 1
  echo "FAIL: adb has not found the phone for $(($(date +%s) - adb_since)) s: the emulator went (the job's emulator-lost notes say more)"
}
adb_failures() {
  [ -n "${adb_file:-}" ] || return 0
  if [ -s "$adb_file" ]; then
    echo "--- adb calls that failed (the last 15):"
    tail -15 "$adb_file" | sed -E 's/#pair=[A-Za-z0-9_-]+/#pair=…/g; s/token=[A-Za-z0-9_-]+/token=…/g'
  fi
  pipes=$(wc -l < "$adb_file.141" 2>/dev/null | tr -d ' ')
  [ "${pipes:-0}" = 0 ] || echo "--- and $pipes adb calls ended by SIGPIPE (141) from a reader that stopped early, left out"
}
adb_exit() {
  adb_st=$1; shift
  set +e
  [ "$adb_st" = 0 ] || adb_failures
  [ -n "${adb_file:-}" ] && rm -f "${adb_file:?}" "${adb_file:?}.141"
  if [ $# -gt 0 ]; then (exit "$adb_st"); "$@"; fi
  exit "$adb_st"
}

if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d)
  # A stand-in adb: input is killed (137), anything else works, and its log has one kill line.
  fake() { case $1 in shell) [ "$2" = input ] && return 137; [ "$2" = dumpsys ] && return 141; return 0 ;; logcat) echo "I lowmemorykiller: Kill 'uiautomator'"; echo "I other: line" ;; esac; }
  adb_note fake "$t/failed"
  # Not followed by || : a test of its status turns set -e off inside it.
  ( set -e; "$adb" shell uiautomator dump; "$adb" shell dumpsys window || :; "$adb" shell dumpsys window || :; "$adb" shell input tap 10 20; echo "went on" > "$t/on" )
  fail=0
  [ -e "$t/on" ] && { echo "FAIL: adb-note self-test: a killed call did not stop a set -e script"; fail=1; }
  grep -q "adb shell input tap 10 20 -> exit 137" "$t/failed" || { echo "FAIL: adb-note self-test: the killed call is not written down"; fail=1; }
  grep -q "lowmemorykiller: Kill 'uiautomator'" "$t/failed" || { echo "FAIL: adb-note self-test: no kill lines from the phone's log"; fail=1; }
  grep -q "other: line" "$t/failed" && { echo "FAIL: adb-note self-test: unrelated log lines written down"; fail=1; }
  grep -q "uiautomator dump -> " "$t/failed" && { echo "FAIL: adb-note self-test: a call that worked is written down"; fail=1; }
  adb_failures | grep -q "input tap 10 20" || { echo "FAIL: adb-note self-test: adb_failures does not show it"; fail=1; }
  grep -q "dumpsys" "$t/failed" && { echo "FAIL: adb-note self-test: a SIGPIPE call is listed"; fail=1; }
  adb_failures | grep -q "and 2 adb calls ended by SIGPIPE" || { echo "FAIL: adb-note self-test: the SIGPIPE calls are not counted"; fail=1; }
  # A set -eu suite with this trap: on a failure its cleanup runs and reads the failure's status, and the suite ends
  # with it; on success the same with 0.
  here=$(cd "$(dirname "$0")" && pwd)
  for want in 137 0; do
    sh -c 'set -eu; d=$2; . "$1/adb-note.sh"; adb_note fake_ "$d/f"
      fake_() { [ "$1" = boom ] && return 137; return 0; }
      cleanup() { status=$?; echo "cleanup ran, status=$status" > "$d/cleanup"; }
      trap '"'"'adb_exit $? cleanup'"'"' EXIT
      "$adb" fine; [ "$3" = 0 ] || "$adb" boom; echo went on > "$d/on"' _ "$here" "$t" "$want" > "$t/out" 2>&1
    got=$?
    [ "$got" = "$want" ] && grep -q "cleanup ran, status=$want" "$t/cleanup" 2>/dev/null ||
      { echo "FAIL: adb-note self-test: a suite that ends with $want: exit $got, $(cat "$t/cleanup" 2>/dev/null || echo "no cleanup")"; fail=1; }
    if [ "$want" = 137 ]; then grep -q "adb boom -> exit 137" "$t/out" || { echo "FAIL: adb-note self-test: the failed call is not shown at the end"; fail=1; }
      [ -e "$t/on" ] && { echo "FAIL: adb-note self-test: the suite went on after the failure"; fail=1; }; fi
    [ -e "$t/f" ] && { echo "FAIL: adb-note self-test: the list file is left behind"; fail=1; }
    rm -f "${t:?}/cleanup" "${t:?}/on"
  done
  # adb_gone: the phone gone (adb no longer finds it) for HM_ADB_GONE s in a row; a blip (adb root) is not.
  echo device > "$t/state"
  fake_state() { [ "$1" = get-state ] && cat "$t/state"; }
  adb_note fake_state "$t/g"
  HM_ADB_GONE=1
  adb_gone > "$t/said" && { echo "FAIL: adb-note self-test: gone with the phone there"; fail=1; }
  echo "error: device 'emulator-5554' not found" > "$t/state"
  adb_gone > "$t/said" && { echo "FAIL: adb-note self-test: gone at once, a blip would stop the suite"; fail=1; }
  sleep 1.2
  adb_gone > "$t/said" || { echo "FAIL: adb-note self-test: not gone after the phone was away for 1 s"; fail=1; }
  grep -q "has not found the phone for" "$t/said" || { echo "FAIL: adb-note self-test: adb_gone says: $(cat "$t/said")"; fail=1; }
  echo device > "$t/state"
  adb_gone > "$t/said" && { echo "FAIL: adb-note self-test: still gone with the phone back"; fail=1; }
  echo "error: device offline" > "$t/state"
  adb_gone > "$t/said" && { echo "FAIL: adb-note self-test: gone at once after the phone was back"; fail=1; }
  unset HM_ADB_GONE
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: adb-note self-test" || exit 1
fi
