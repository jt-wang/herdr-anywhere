#!/bin/sh
# What went wrong in an XCUITest run, from its result bundle (xcodebuild test -resultBundlePath): each failure's message,
# which xcodebuild -quiet leaves out of its log (on CI it said only "Failing tests: TypeIn.test()"), and, given a folder,
# the screenshots XCTest took at the failures, for the job to upload; with all, every screenshot the test kept.
# Usage: e2e/ios-xcresult.sh RESULT.xcresult [FOLDER [all]]
set -u
result=$1; folder=${2:-}; which=--only-failures; [ "${3:-}" = all ] && which=
[ -d "$result" ] || { echo "  (no result bundle at $result)"; exit 0; }
xcrun xcresulttool get test-results summary --path "$result" 2>/dev/null | python3 -c '
import json, sys
try:
    failures = json.load(sys.stdin).get("testFailures") or []
except ValueError:
    failures = None
if failures is None:
    print("  (the result bundle could not be read)")
for f in failures or []:
    print("  failed: %s: %s" % (f.get("testIdentifierString", f.get("testName", "?")), f.get("failureText", "")))'
if [ -n "$folder" ]; then
  mkdir -p "$folder"
  xcrun xcresulttool export attachments --path "$result" --output-path "$folder" $which >/dev/null 2>&1 ||
    echo "  (the failure screenshots could not be exported)"
  # Each picture under the name the test gave it (before-drag_0_<uuid>.png goes back to before-drag.png); then the
  # manifest goes, as it names the machine (deviceId).
  [ -f "$folder/manifest.json" ] && python3 -c '
import json, os, re, sys
d = sys.argv[1]
for test in json.load(open(os.path.join(d, "manifest.json"))):
    for a in test.get("attachments", []):
        new = re.sub(r"_\d+_[0-9A-F-]{36}(\.\w+)$", r"\1", a.get("suggestedHumanReadableName", ""))
        old = os.path.join(d, a.get("exportedFileName", ""))
        if new and os.path.isfile(old) and not os.path.exists(os.path.join(d, new)):
            os.rename(old, os.path.join(d, new))' "$folder"
  rm -f "$folder/manifest.json"
  n=$(find "$folder" -type f | wc -l | tr -d ' ')
  [ "$n" = 0 ] && rmdir "$folder" 2>/dev/null
  echo "  $n screenshots${which:+ at the failures}"
fi
exit 0
