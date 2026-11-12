#!/bin/sh
# Cold-starts a built Herdr.app on a simulator of its own and passes when the app is still running 30 s later. Made for
# the iOS 27 check: iOS 27 ends the launch of an app built with its SDK that has no scene (TestFlight 0.1.24 crashed in
# _UIApplicationEvaluateRuntimeIssueForNoSceneLifecycleAdoption on iOS 27.2.1, and ran on iOS 26). On a failure it
# prints the top of the crash report, which macOS can take up to a minute to write.
# Usage: SIM_RUNTIME=com.apple.CoreSimulator.SimRuntime.iOS-27-0 e2e/ios-opens.sh path/to/Herdr.app
# SIM_DEVICE picks the model (default iPhone 17). The simulator is created for this run and deleted after it.
set -eu
: "${SIM_RUNTIME:?set SIM_RUNTIME to a runtime id from xcrun simctl list runtimes}"
app=${1:?usage: e2e/ios-opens.sh path/to/Herdr.app}
device=${SIM_DEVICE:-com.apple.CoreSimulator.SimDeviceType.iPhone-17}
id=$(plutil -extract CFBundleIdentifier raw "$app/Info.plist")
exe=$(plutil -extract CFBundleExecutable raw "$app/Info.plist")
reports=$HOME/Library/Logs/DiagnosticReports
marker=$(mktemp)
# A simulator that can't be made is a failed setup (exit 2), never a failed app (exit 1): the runtime and its device
# types go to the log, so a run that never started the app says so.
echo "runtime $SIM_RUNTIME, device $device"
if ! xcrun simctl list runtimes -j | python3 -c '
import json, sys
r = next((r for r in json.load(sys.stdin)["runtimes"] if r["identifier"] == sys.argv[1]), None)
if r is None: sys.exit("no runtime " + sys.argv[1])
print("runtime", r.get("version"), r.get("buildversion"), "available:", r.get("isAvailable"), r.get("availabilityError", ""))
ids = [d["identifier"] for d in r.get("supportedDeviceTypes", [])]
if sys.argv[2] not in ids: sys.exit("the runtime has no " + sys.argv[2] + "; it has: " + " ".join(i.split(".")[-1] for i in ids))
' "$SIM_RUNTIME" "$device"; then
  echo "SETUP FAIL: no simulator of this kind on this runtime"; xcrun simctl list runtimes -v; exit 2
fi
if ! udid=$(xcrun simctl create "ios-opens $$" "$device" "$SIM_RUNTIME"); then
  echo "SETUP FAIL: simctl create"; xcrun simctl list runtimes -v; xcrun simctl runtime list 2>/dev/null || true; exit 2
fi
cleanup() { xcrun simctl shutdown "$udid" 2>/dev/null || true; xcrun simctl delete "$udid" 2>/dev/null || true; rm -f "$marker"; }
trap cleanup EXIT
trap 'exit 130' INT TERM
xcrun simctl boot "$udid" || { echo "SETUP FAIL: simctl boot"; exit 2; }
xcrun simctl bootstatus "$udid" -b >/dev/null || { echo "SETUP FAIL: simctl bootstatus"; exit 2; }
# The first launch on a simulator that has just booted can be slow: start a system app once first.
xcrun simctl launch "$udid" com.apple.Preferences >/dev/null 2>&1 || true
sleep 5
xcrun simctl terminate "$udid" com.apple.Preferences 2>/dev/null || true
xcrun simctl install "$udid" "$app" || { echo "SETUP FAIL: simctl install"; exit 2; }
touch "$marker"
pid=$(xcrun simctl launch "$udid" "$id" | awk -F': ' '{print $2}')
case $pid in ''|*[!0-9]*) echo "FAIL: simctl launch gave no pid"; exit 1 ;; esac
up=0
while [ "$up" -lt 30 ]; do
  sleep 2
  kill -0 "$pid" 2>/dev/null || break
  up=$((up + 2))
done
if kill -0 "$pid" 2>/dev/null; then
  echo "✓ $exe still running 30 s after a cold start on $SIM_RUNTIME"
  exit 0
fi
echo "✗ $exe ended within $((up + 2)) s of a cold start on $SIM_RUNTIME"
end=$(($(date +%s) + 90))
while [ "$(date +%s)" -lt "$end" ]; do
  report=$(find "$reports" -name "$exe-*.ips" -newer "$marker" 2>/dev/null | head -1)
  if [ -n "$report" ]; then
    echo "crash report: $report"
    # The .ips is a JSON header line, then the report; print what a reader looks for first.
    python3 -c '
import json, sys
lines = open(sys.argv[1]).read().split("\n", 1)
r = json.loads(lines[1])
e = r.get("exception", {})
print("exception:", e.get("type"), e.get("signal"))
t = next((t for t in r.get("threads", []) if t.get("triggered")), None)
imgs = r.get("usedImages", [])
for f in (t or {}).get("frames", [])[:8]:
    name = imgs[f["imageIndex"]].get("name", "?") if f.get("imageIndex") is not None and f["imageIndex"] < len(imgs) else "?"
    print("  ", name, f.get("symbol", hex(f.get("imageOffset", 0))))
' "$report" || head -40 "$report"
    exit 1
  fi
  sleep 5
done
echo "no crash report within 90 s"
exit 1
