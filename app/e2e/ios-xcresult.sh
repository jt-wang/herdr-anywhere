#!/bin/sh
# What went wrong in an XCUITest run, from its result bundle (xcodebuild test -resultBundlePath): each failure's message,
# which xcodebuild -quiet leaves out of its log (on CI it said only "Failing tests: TypeIn.test()"), and, given a folder,
# the screenshots XCTest took at the failures, for the job to upload; with all, every screenshot the test kept.
# Usage: e2e/ios-xcresult.sh RESULT.xcresult [FOLDER [all]]
#        e2e/ios-xcresult.sh --tidy FOLDER     only the last step: what an export left there that is not to be uploaded
set -u
tidy() {
  # At a failure Xcode 26.6 also exported "UI Snapshot <time>", a binary property list, and "App UI hierarchy for
  # <app>", text, neither with an extension (both have dots in their names), and a video (a keyboard failure). Text
  # keeps its words and gets .txt. Anything else goes, the video too: a macOS runner has no ffmpeg, so the upload
  # check fails on any video and nothing at all is uploaded; the list's strings may be UTF-16, which a byte search
  # misses. Each file by what its bytes are, not its name: a video renamed .txt would be uploaded unread.
  find "$1" -type f ! -name '*.png' ! -name '*.jpg' ! -name '*.jpeg' ! -name '*.txt' | while IFS= read -r f; do
    case $(file -b --mime-type "$f") in
      text/*) mv "$f" "$f.txt" ;;
      *) echo "  left out: $(basename "$f") ($(file -b --mime-type "$f"))"; rm -f "$f" ;;
    esac
  done
}
if [ "${1:-}" = --tidy ]; then tidy "$2"; exit 0; fi
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
  tidy "$folder"
  n=$(find "$folder" -type f | wc -l | tr -d ' ')
  [ "$n" = 0 ] && rmdir "$folder" 2>/dev/null
  echo "  $n files${which:+ at the failures} (screenshots and what XCTest wrote)"
fi
exit 0
