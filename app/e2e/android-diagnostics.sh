#!/bin/sh
# Real native recorder acceptance on the already paired, isolated CI emulator. Never pm clear/uninstall:
# only the diagnostics preference and recorder files are reset; pairing and all other app state stay intact.
# Usage: ANDROID_SERIAL=emulator-5554 sh e2e/android-diagnostics.sh APK [screenshot.png]
# Uses the Google APIs image and adb from e2e/ci/android-up.sh. No emulator is started here.
set -eu
app=$(cd "$(dirname "$0")/.." && pwd)

# Kept in this script so the JSON/privacy assertions can also be exercised without an Android SDK.
validate() {
  python3 -c '
import json, re, sys
mode, selected = sys.argv[1:3]
if mode == "report":
    report = json.load(sys.stdin)
    assert report.get("schemaVersion") == 1, "wrong export schema"
    assert report.get("categories") == [selected], "export categories differ from consent"
    assert isinstance(report.get("limits"), str) and report["limits"], "missing limitations"
    rows = report["events"]
    assert rows, "empty export"
else:
    rows = [json.loads(line) for line in sys.stdin if line.strip()]
assert all(isinstance(r, dict) and r.get("category") == selected and
           isinstance(r.get("event"), str) and r["event"] and isinstance(r.get("fields"), dict)
           for r in rows), "recorded an unselected category or malformed event"
if selected == "none":
    assert not rows, "collected without consent"
if mode == "report":
    # These inputs must never enter a report, even as nested field names. Endpoint templates such as
    # /api/chat/:terminal are deliberately allowed; actual URLs, paths and free-form payloads are not.
    forbidden = {"password", "token", "secret", "authorization", "cookie", "body", "payload",
                 "input", "prompt", "cwd", "path", "url", "address", "pair", "message", "stack"}
    def privacy(value):
        if isinstance(value, dict):
            assert not (set(k.lower() for k in value) & forbidden), "private field in export"
            for v in value.values(): privacy(v)
        elif isinstance(value, list):
            for v in value: privacy(v)
        elif isinstance(value, str):
            assert not re.search(r"https?://|wss?://|#pair=|/data/(?:user|data)/|/Users/|/home/|[A-Za-z]:\\", value), "private value in export"
    privacy(report)
print(len(rows))
' "$1" "$2"
}

if [ "${1:-}" = --self-test ]; then
  printf '' | validate rows none >/dev/null
  good='{"schemaVersion":1,"categories":["network"],"events":[{"category":"network","event":"http-end","fields":{"endpoint":"/api/info","status":"200"}}],"limits":"App only. No automatic upload."}'
  printf '%s' "$good" | validate report network >/dev/null
  rejects() {
    if printf '%s\n' "$1" | validate "$2" "$3" >/dev/null 2>&1; then
      echo "FAIL: diagnostics self-test accepted $4"; exit 1
    fi
  }
  # Deliberately broken results must fail: negative collection checks are not tested only on empty input.
  row='{"category":"network","event":"http-end","fields":{}}'
  rejects "$row" rows none 'collection with no consent'
  rejects "$row" rows interaction 'an unselected category'
  rejects '{"category":"network","event":"http-end"}' rows network 'missing fields'
  rejects "$(printf '%s' "$good" | sed 's/"status":"200"/"token":"diagnostic-canary"/')" report network 'a credential field'
  rejects "$(printf '%s' "$good" | sed 's@/api/info@https://example.invalid/private@')" report network 'a raw URL'
  rejects "$(printf '%s' "$good" | sed 's/"schemaVersion":1/"schemaVersion":2/')" report network 'the wrong schema'
  echo 'PASS: diagnostics JSON checks reject collection without consent, category leaks, malformed rows and private exports'
  exit 0
fi

: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the isolated CI emulator}"
case "$ANDROID_SERIAL" in emulator-*) ;; *) echo 'FAIL: diagnostics acceptance requires an isolated emulator'; exit 1 ;; esac
apk=${1:-$app/android/build/Herdr.apk}
shot=${2:-}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
base="/data/user/0/$id"
prefs="$base/shared_prefs/diagnostics.xml"
events="$base/no_backup/diagnostics"
exports="$base/cache/opened"
activity="$id/.MainActivity"
scratch="$app/../.amp/in/orb-backup"
mkdir -p "$scratch"
work=$(mktemp -d "$scratch/android-diagnostics.XXXXXX")
. "$app/e2e/adb-note.sh"; adb_note "$adb" "$work/adb-failed"
. "$app/e2e/android-lib.sh"
was_root=$("$adb" shell id -u | tr -d '\r')
root_ready=0
record_pid=
paused_pid=
show_touches=$("$adb" shell settings get system show_touches | tr -d '\r')
fail() { echo "FAIL: diagnostics: $*"; exit 1; }
device_ready() {
  for _ in $(seq 1 30); do
    [ "$("$adb_real" get-state 2>/dev/null | tr -d '\r')" = device ] && return 0
    sleep 1
  done
  return 1
}
remove_exports() {
  # Do not delete other Opened-provider attachments or their directories.
  "$adb" shell "if [ -d '$exports' ]; then find '$exports' -type f -name 'Herdr Anywhere Diagnostics.json' -delete; fi"
}
cleanup() {
  if [ -n "$record_pid" ]; then wait "$record_pid" || true; fi
  if [ "$root_ready" = 1 ]; then
    if [ -n "$paused_pid" ]; then "$adb" shell kill -CONT "$paused_pid" >/dev/null 2>&1 || true; fi
    "$adb" shell am force-stop "$id" >/dev/null 2>&1 || true
    "$adb" shell "rm -f '$prefs' '$prefs.bak'; rm -rf '$events'" >/dev/null 2>&1 || true
    remove_exports >/dev/null 2>&1 || true
    "$adb" shell rm -f /sdcard/herdr-diagnostics-e2e.mp4 >/dev/null 2>&1 || true
    if [ "$show_touches" = null ]; then "$adb" shell settings delete system show_touches >/dev/null 2>&1 || true
    else "$adb" shell settings put system show_touches "$show_touches" >/dev/null 2>&1 || true; fi
  fi
  if [ "$was_root" != 0 ]; then "$adb" unroot >/dev/null 2>&1 || true; device_ready || true; fi
  rm -rf "$work"
}
trap 'adb_exit $? cleanup' EXIT
trap 'exit 130' INT; trap 'exit 143' TERM
"$adb" root >/dev/null
device_ready || fail 'adb did not return after root'
[ "$("$adb" shell id -u | tr -d '\r')" = 0 ] || fail 'Google APIs rootable CI image required'
root_ready=1
"$adb" shell am force-stop "$id"
"$adb" shell "rm -f '$prefs' '$prefs.bak'; rm -rf '$events'"
remove_exports

# All reads are restricted to diagnostics, never the app preferences that contain pairing credentials.
snapshot() {
  "$adb" shell "if [ -d '$events' ]; then find '$events' -type f -name 'events-*.jsonl'; fi" > "$work/generations"
  python3 -c '
import sys
files = [s.strip().rsplit("/", 1)[-1] for s in open(sys.argv[1]) if s.strip()]
assert len(files) <= 3 and set(files) <= {"events-0.jsonl", "events-1.jsonl", "events-2.jsonl"}, "unexpected diagnostic generations"
' "$work/generations"
  # All three paths are fixed and contain no spaces; escape only the remote variable expansion.
  "$adb" shell "for f in '$events'/events-0.jsonl '$events'/events-1.jsonl '$events'/events-2.jsonl; do if [ -f \$f ]; then cat \$f; fi; done" > "$work/rows"
}
assert_rows() { snapshot; validate rows "$1" < "$work/rows" > "$work/count"; }
assert_prefs() {
  "$adb" shell "if [ -f '$prefs' ]; then cat '$prefs'; else echo '<map />'; fi" |
    python3 -c '
import sys, xml.etree.ElementTree as E
p = {e.attrib["name"]: e.attrib.get("value") for e in E.parse(sys.stdin).getroot()}
assert p.get("enabled", "false") == sys.argv[1], "master preference mismatch"
for c in ("interaction", "performance", "network", "resources"):
    assert p.get("category." + c, "false") == str(c == sys.argv[2]).lower(), "saved category mismatch: " + c
' "$1" "$2"
}
tree() {
  # Unlike android-lib.sh, do not inspect a stale dump after a failed uiautomator call.
  "$adb" shell rm -f /sdcard/hm.xml
  dump
  "$adb" shell cat /sdcard/hm.xml > "$work/tree.xml"
}
assert_switches() {
  for _ in $(seq 1 10); do
    tree
    if python3 -c '
import sys, xml.etree.ElementTree as E
nodes = {n.get("resource-id"): n for n in E.parse(sys.argv[1]).iter("node")}
on, selected = sys.argv[2] == "true", sys.argv[3]
for c in ("toggle", "interaction", "performance", "network", "resources"):
    n = nodes.get("diagnostic-" + c)
    assert n is not None, "missing switch: " + c
    assert n.get("checked") == str(on if c == "toggle" else on and c == selected).lower(), "checked: " + c
    assert n.get("enabled") == str(c == "toggle" or on).lower(), "enabled: " + c
    assert n.get("checkable") == "true", "not an accessible switch: " + c
' "$work/tree.xml" "$1" "$2" 2>/dev/null; then return 0; fi
    sleep 1
  done
  fail "accessible switch states do not match master=$1 category=$2"
}
open_diagnostics() {
  "$adb" shell am start -W -n "$activity" >/dev/null
  # Startup after force-stop restores home; settings can start below the fold from android.sh.
  for _ in $(seq 1 12); do
    [ -n "$(at diagnostic-toggle)" ] && return 0
    if [ -n "$(at settings-diagnostics)" ]; then tap settings-diagnostics
    elif [ -n "$(at settings)" ]; then tap settings
    else "$adb" shell input swipe 500 1800 500 400 250
    fi
    sleep 1
  done
  fail 'cannot reach the diagnostics page from home/settings'
}
shot_state() {
  assert_switches "$2" "$3"
  if [ -n "$shot" ]; then "$adb" exec-out screencap -p > "${shot%.png}-diagnostics-$1.png"; fi
}
restart() { "$adb" shell am force-stop "$id"; open_diagnostics; }
upgrade() { "$adb" shell am force-stop "$id"; "$adb" install -r "$apk" >/dev/null; open_diagnostics; }
exercise() {
  # Actual input and a native lifecycle/reconnect; no direct writes into the recorder and no debug host calls.
  tap diagnostic-back
  open_diagnostics
  "$adb" shell input swipe 500 1100 500 1000 200
  restart
  sleep 3
}
no_collection() { exercise; assert_rows none; }

open_diagnostics
assert_prefs false none
shot_state fresh false none
no_collection
restart; assert_switches false none; assert_prefs false none; assert_rows none
# Old Android had no recorder/preferences: remove only diagnostics state, then install -r, never pm clear.
"$adb" shell am force-stop "$id"
"$adb" shell "rm -f '$prefs' '$prefs.bak'"
upgrade
assert_switches false none; assert_prefs false none
no_collection
shot_state upgrade-off false none
echo 'ok: fresh, restart and no-prior-preferences APK upgrade are all off and collect zero events'

tap diagnostic-toggle
assert_switches true none; assert_prefs true none
no_collection
shot_state master-only true none
restart; assert_switches true none; assert_rows none

# Each category is selected through its accessible switch, not seeded preferences. Every category must
# produce real events on its own, and no other category may appear in the actual JSONL generations.
for category in interaction performance resources network; do
  tap "diagnostic-$category"
  assert_switches true "$category"; assert_prefs true "$category"
  exercise
  if [ "$category" = performance ]; then
    # A healthy main loop need not emit a gap. Cause a real, bounded scheduling interruption instead
    # of assuming a loaded emulator or inventing events through a debug API. Resume even on failure.
    paused_pid=$("$adb" shell pidof "$id" | tr -d '\r')
    case "$paused_pid" in ''|*[!0-9]*) fail 'expected one app process for the main-loop-gap control' ;; esac
    "$adb" shell "kill -STOP '$paused_pid'; sleep 2; kill -CONT '$paused_pid'"
    paused_pid=
  fi
  found=0
  for _ in $(seq 1 30); do
    assert_rows "$category"
    [ "$(cat "$work/count")" -gt 0 ] && { found=1; break; }
    sleep 1
  done
  [ "$found" = 1 ] || fail "$category opt-in produced no real events in 30 s"
  echo "ok: $category alone recorded $(cat "$work/count") events, with no other categories"
  if [ "$category" != network ]; then
    tap "diagnostic-$category"; assert_switches true none
    tap diagnostic-clear
    assert_rows none
  fi
done
shot_state network true network
restart; assert_switches true network; assert_prefs true network; assert_rows network
upgrade; assert_switches true network; assert_prefs true network
exercise; assert_rows network
[ "$(cat "$work/count")" -gt 0 ] || fail 'upgrade lost the selected network recorder'
shot_state on-upgrade true network
echo 'ok: restart and install -r preserve master consent and only the selected network category'

# Freeze the buffer through the real opt-out, retaining the saved category. Then reconnect and wait:
# there must be zero additional events, not just a UI label saying recording stopped.
tap diagnostic-toggle
assert_switches false none; assert_prefs false network
snapshot; cp "$work/rows" "$work/stopped"
exercise; assert_switches false none; assert_prefs false network
snapshot; cmp -s "$work/stopped" "$work/rows" || fail 'master opt-out appended events'
shot_state disabled false none
tap diagnostic-clear; assert_rows none
no_collection; assert_prefs false network
echo 'ok: master opt-out stops all collection while preserving category choices; clear leaves zero events'

# Re-enable saved network consent for a real report. Turn it off again before taking the snapshot so
# expected export counts cannot race a reconnect, periodic sampler, or UI interaction.
tap diagnostic-toggle; assert_switches true network
exercise; assert_rows network
[ "$(cat "$work/count")" -gt 0 ] || fail 'no events for the export'
tap diagnostic-toggle; assert_switches false none
snapshot
expected=$(validate rows network < "$work/rows")
tap_text_part() {
  for _ in $(seq 1 10); do
    tree
    xy=$(python3 -c '
import re, sys, xml.etree.ElementTree as E
for n in E.parse(sys.argv[1]).iter("node"):
    if sys.argv[2] in n.get("text", ""):
        a = list(map(int, re.findall(r"\d+", n.get("bounds", ""))))
        if len(a) == 4: print((a[0]+a[2])//2, (a[1]+a[3])//2); break
' "$work/tree.xml" "$1")
    [ -n "$xy" ] && { "$adb" shell input tap $xy; return 0; }
    sleep 1
  done
  fail "native selector missing: $1"
}
wait_text() {
  for _ in $(seq 1 10); do tree; grep -F "$1" "$work/tree.xml" >/dev/null && return 0; sleep 1; done
  fail "native dialog missing: $1"
}
export_files() {
  "$adb" shell "if [ -d '$exports' ]; then find '$exports' -type f -name 'Herdr Anywhere Diagnostics.json'; fi" | tr -d '\r'
}
tap diagnostic-export
wait_text 'Time range'; tap_text_part 'Last 15 min'
wait_text 'Optional note'; tap_text_part 'None'
wait_text 'Review before sharing'
[ -z "$(export_files)" ] || fail 'export was written before preview confirmation'
if [ -n "$shot" ]; then "$adb" exec-out screencap -p > "${shot%.png}-diagnostics-preview.png"; fi
tap_text_part 'Share'
chooser=0
for _ in $(seq 1 15); do
  # Check the native resolver activity, not the app preview that also contains the word Share.
  if "$adb" shell dumpsys activity activities | grep -E '(mResumedActivity|topResumedActivity).*([Cc]hooserActivity|ResolverActivity)' >/dev/null; then chooser=1; break; fi
  sleep 1
done
[ "$chooser" = 1 ] || fail 'Android share chooser did not open'
file=$(export_files)
[ "$(printf '%s\n' "$file" | grep -c .)" = 1 ] || fail 'expected exactly one diagnostic export'
case "$file" in "$exports"/*/'Herdr Anywhere Diagnostics.json') ;; *) fail 'export outside Opened provider' ;; esac
"$adb" shell "cat '$file'" > "$work/report.json"
actual=$(validate report network < "$work/report.json")
[ "$actual" = "$expected" ] || fail "export has $actual events, expected $expected"
echo "ok: native range/note/preview/share flow exported $actual network events with schema and privacy checks"
"$adb" shell input keyevent KEYCODE_BACK
assert_switches false none
tap diagnostic-clear
assert_rows none
[ -z "$(export_files)" ] || fail 'Clear retained a generated diagnostic export'
assert_prefs false network

# Bounded native recording, only of already-validated states; no pairing UI or arbitrary preferences.
if [ -n "$shot" ]; then
  "$adb" shell settings put system show_touches 1
  "$adb" shell screenrecord --time-limit 12 /sdcard/herdr-diagnostics-e2e.mp4 > "$work/record.log" 2>&1 &
  record_pid=$!
  sleep 2
  tap diagnostic-toggle; assert_switches true network
  tap diagnostic-toggle; assert_switches false none
  if wait "$record_pid"; then
    record_pid=
    "$adb" pull /sdcard/herdr-diagnostics-e2e.mp4 "${shot%.png}-diagnostics.mp4" >/dev/null
  else
    record_pid=
    fail 'native screenrecord failed'
  fi
fi
echo 'PASS: native Android diagnostics consent, category isolation, persistence, export and clear (pairing untouched)'
