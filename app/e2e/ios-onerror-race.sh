#!/bin/bash
# Diagnostic only: the unchanged OnError assertion, with idb kept/stopped in alternating trials.
# Build the app once using ios-onerror.sh, then relaunch for every trial. Any failure fails the run.
set -euo pipefail
: "${SIM_UDID:?}" "${1:?pass the evidence screenshot path}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export FIRSTTAP_REAL_XCODEBUILD=$(command -v xcodebuild)
export FIRSTTAP_RACE_OUT="${1%.png}-race"
mkdir -p "$FIRSTTAP_RACE_OUT" "$work/bin"
cp "$(dirname "$0")/ios-uitest/tests/OnError.swift" "$FIRSTTAP_RACE_OUT/OnError-used.swift.txt"
cat > "$work/bin/xcodebuild" <<'WRAPPER'
#!/bin/bash
set -euo pipefail
if [ "${1:-}" != test ]; then exec "$FIRSTTAP_REAL_XCODEBUILD" "$@"; fi
args=("$@")
project= result= out=${TEST_RUNNER_OUT:?}
for ((i=0; i<${#args[@]}; i++)); do
  case ${args[i]} in
    -project) project=${args[i+1]} ;;
    -resultBundlePath) result=${args[i+1]}; result_arg=$((i+1)) ;;
  esac
done
serve="$(dirname "$project")/../serve.log"
status=0
chosen=
echo 'trial,idb,exit' > "$FIRSTTAP_RACE_OUT/results.csv"
for trial in 1 2 3 4 5 6 7 8; do
  mode=keep; [ $((trial % 2)) = 0 ] && mode=stop
  dir="$FIRSTTAP_RACE_OUT/trial-$trial-$mode-evidence"
  mkdir -p "$dir"
  start=$(date '+%Y-%m-%d %H:%M:%S')
  count=$(grep -c ' /hm-e2e/throwing ' "$serve" || true)
  xcrun simctl terminate "$SIM_UDID" io.jingtao.herdranywhere || true
  xcrun simctl launch "$SIM_UDID" io.jingtao.herdranywhere > "$dir/launch.txt"
  # Confirm this process reached its error marker, not merely that an earlier process did.
  seen=0
  for _ in $(seq 1 40); do
    now=$(grep -c ' /hm-e2e/throwing ' "$serve" || true)
    if [ "$now" -gt "$count" ]; then seen=1; break; fi
    sleep 1
  done
  [ "$seen" = 1 ] || { echo "missing new throw marker: $trial" >&2; exit 2; }
  sleep 5
  # Same AX helper call in both arms; only whether its companion remains differs.
  perl -e 'alarm 40; exec @ARGV' idb ui describe-all --udid "$SIM_UDID" > "$dir/before-tree.txt"
  if [ "$mode" = stop ]; then idb kill > "$dir/idb-stop.txt" 2>&1; fi
  ps -axo pid,command | grep '[i]db_companion' > "$dir/idb-processes.txt" || true
  args[result_arg]="$dir/raw.xcresult"
  rc=0
  TEST_RUNNER_OUT="$dir/tap.txt" "$FIRSTTAP_REAL_XCODEBUILD" "${args[@]}" > "$dir/xctest.log" 2>&1 || rc=$?
  echo "$trial,$mode,$rc" | tee -a "$FIRSTTAP_RACE_OUT/results.csv"
  data=$(xcrun simctl get_app_container "$SIM_UDID" io.jingtao.herdranywhere data)
  cp "$data/Library/Caches/firsttap-native.txt" "$dir/native.txt"
  cp "$serve" "$dir/trace.log"
  xcrun simctl spawn "$SIM_UDID" log show --info --debug --start "$start" --style compact \
    --predicate '(process == "backboardd" AND (subsystem == "com.apple.BackBoard" OR subsystem == "com.apple.iohid")) OR (process == "Herdr" AND subsystem == "com.apple.UIKit")' > "$dir/touch-system.log"
  xcrun xcresulttool get test-results summary --path "$dir/raw.xcresult" > "$dir/summary.json"
  xcrun xcresulttool export attachments --path "$dir/raw.xcresult" --output-path "$dir/attachments"
  [ -n "$chosen" ] || chosen="$dir"
  if [ "$rc" != 0 ] && [ "$status" = 0 ]; then chosen="$dir"; status=$rc; fi
done
# The outer suite still gets its expected result; all eight originals remain beside it.
cp -R "$chosen/raw.xcresult" "$result"
cat "$FIRSTTAP_RACE_OUT"/trial-*-evidence/tap.txt > "$out"
exit "$status"
WRAPPER
chmod +x "$work/bin/xcodebuild"
# Eight fresh test sessions may each take a minute. Only the outer 300-second watchdog is extended;
# OnError.swift and its 20-second first-tap assertion are byte-for-byte unchanged.
export HM_ONERROR_DRIVER_TIMEOUT=1800
export PATH="$work/bin:$PATH"
exec_script="$(dirname "$0")/ios-onerror.sh"
"$exec_script" "$1"
