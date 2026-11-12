#!/bin/bash
# Watches the CI emulator from android-up.sh until the job ends: the phone's log (adb logcat) goes to LOGCAT as it
# comes, and each time adb loses the phone for 10 s or more (offline, not found: three jobs failed so, the suites after
# it too; adb root and unroot, which the quiet suite runs, lose it for about 2 s and are left out) a note in
# FOLDER/lost-N.txt (the first 3 times): what adb said, the runner's free memory and disk, whether the emulator's qemu
# process is still there and how big, the kernel's last lines (an out-of-memory kill shows there), the emulator's own
# output (EMULATOR_LOG) and the phone's log before it went. Lines shaped like a pairing secret are left out of the
# copies, so the upload check can let the note through. The job uploads FOLDER with the rest.
# Usage: app/e2e/ci/android-watch.sh ADB FOLDER EMULATOR_LOG LOGCAT    (android-up.sh starts it in the background)
#        app/e2e/ci/android-watch.sh --self-test
set -uo pipefail
secret='herdr[0-9]:|#pair=|token='

note() { # file, what adb said, emulator log, logcat
  local f=$1
  mkdir -p "$(dirname "$f")"
  {
    echo "$(date -u +%FT%TZ) adb lost the phone: $2"
    echo "== adb devices"; "$adb" devices -l 2>&1
    echo "== uptime"; uptime
    echo "== free -m"; free -m 2>&1
    echo "== df -h"; df -h / "${RUNNER_TEMP:-/tmp}" 2>&1
    echo "== qemu"; pgrep -af qemu-system | cut -c1-160 || echo "no qemu-system process"
    ps -o pid,rss,vsz,etime,stat,comm -C qemu-system-x86_64 2>/dev/null
    echo "== kernel (last 40 lines)"; sudo -n dmesg -T 2>/dev/null | tail -40 || echo "(no dmesg)"
    echo "== emulator output (last 200 lines)"; tail -200 "$3" 2>/dev/null | grep -vE "$secret"
    echo "== phone log (last 400 lines)"; tail -400 "$4" 2>/dev/null | grep -vE "$secret"
  } > "$f" 2>&1
}

watch() { # folder, emulator log, logcat
  local folder=$1 elog=$2 logcat=$3 every=${HM_WATCH_EVERY:-5} grace=${HM_WATCH_GRACE:-10} n=0 lost="" noted="" s
  # adb logcat ends when the phone goes; it starts again when it is back.
  ( while :; do "$adb" logcat -v threadtime >> "$logcat" 2>/dev/null; sleep "$every"; done ) &
  trap 'kill $! 2>/dev/null' EXIT
  while sleep "$every"; do
    s=$("$adb" get-state 2>&1 | tr -d '\r')
    if [ "$s" = device ]; then
      [ -n "$noted" ] && [ "$n" -le 3 ] && echo "$(date -u +%FT%TZ) back after $(($(date +%s) - lost)) s" >> "$folder/lost-$n.txt"
      lost=""; noted=""
      continue
    fi
    [ -n "$lost" ] || lost=$(date +%s)
    [ -z "$noted" ] && [ $(($(date +%s) - lost)) -ge "$grace" ] || continue
    noted=1; n=$((n + 1))
    [ "$n" -le 3 ] && note "$folder/lost-$n.txt" "$s (for $(($(date +%s) - lost)) s)" "$elog" "$logcat"
  done
}

self_test() {
  local t fail=0; t=$(mktemp -d)
  echo device > "$t/state"
  # A made-up secret, made here each run (a fixed one in this file reads as a real key to a secret scanner).
  local fake; fake=$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
  printf 'emulator: INFO: boot completed\nemulator: link http://192.0.2.1:1/#pair=%s\n' "$fake" > "$t/emulator.log"
  # A stand-in adb: the state in $t/state, and a phone log of a few lines a call.
  fake_adb() {
    case $1 in
      get-state) cat "$t/state" ;;
      devices) echo "List of devices attached" ;;
      logcat) [ "$(cat "$t/state")" = device ] || return 1; echo "01-01 12:00:00.000 I hm: phone log line"; echo "01-01 12:00:00.100 I hm: token=$fake"; sleep 0.3 ;;
    esac
  }
  adb=fake_adb
  HM_WATCH_EVERY=0.2 HM_WATCH_GRACE=1 watch "$t/out" "$t/emulator.log" "$t/logcat.txt" & local w=$!
  sleep 1.5
  echo "error: device offline" > "$t/state"; sleep 0.3; echo device > "$t/state"; sleep 1.5  # adb root's blip
  [ -e "$t/out/lost-1.txt" ] && { echo "FAIL: self-test: a note while the phone was there, or for a blip"; fail=1; }
  echo "error: device offline" > "$t/state"; sleep 3
  echo device > "$t/state"; sleep 1.5
  echo "error: device 'emulator-5554' not found" > "$t/state"; sleep 3
  kill "$w"; wait "$w" 2>/dev/null
  local one="$t/out/lost-1.txt" two="$t/out/lost-2.txt"
  for want in "adb lost the phone: error: device offline" "== free -m" "== qemu" "emulator: INFO: boot completed" "I hm: phone log line" "back after"; do
    grep -qF "$want" "$one" 2>/dev/null || { echo "FAIL: self-test: the first note has no \"$want\""; fail=1; }
  done
  grep -qF "not found" "$two" 2>/dev/null || { echo "FAIL: self-test: no second note when the phone went again"; fail=1; }
  grep -qE "$secret" "$t/out"/* 2>/dev/null && { echo "FAIL: self-test: a note holds something shaped like a pairing secret"; fail=1; }
  rm -rf "$t"
  [ "$fail" = 0 ] && echo "PASS: android-watch self-test" || { echo "FAIL: android-watch self-test"; return 1; }
}

if [ "${1:-}" = --self-test ]; then self_test; exit; fi
[ $# = 4 ] || { echo "usage: $0 ADB FOLDER EMULATOR_LOG LOGCAT | --self-test"; exit 2; }
adb=$1; shift
watch "$@"
