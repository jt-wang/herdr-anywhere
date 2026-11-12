#!/bin/sh
# What went wrong in an XCUITest run, from its result bundle (xcodebuild test -resultBundlePath): each failure's message,
# which xcodebuild -quiet leaves out of its log (on CI it said only "Failing tests: TypeIn.test()"), and, given a folder,
# the screenshots XCTest took at the failures, for the job to upload; with all, every screenshot the test kept.
# Usage: e2e/ios-xcresult.sh RESULT.xcresult [FOLDER [all]]
#        e2e/ios-xcresult.sh --tidy FOLDER     only the last step: what an export left there that is not to be uploaded
#        e2e/ios-xcresult.sh --self-test       the last step on names Xcode gave, and names the upload takes
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
  # The upload refuses a whole artifact for one name with " : < > | * or ? in it, and Xcode names a debug description
  # after the query it printed ("Debug description for `"picture-image" Any`"): each of them becomes _.
  find "$1" -type f -name '*[":<>|*?]*' | while IFS= read -r f; do
    new=$(dirname "$f")/$(basename "$f" | tr '":<>|*?' '_______')
    [ -e "$new" ] && new="${new%.*}-2.${new##*.}"
    mv "$f" "$new"
  done
}
if [ "${1:-}" = --tidy ]; then tidy "$2"; exit 0; fi
# Names as Xcode 26.6 exported them at an iOS failure, and the names the upload takes: the whole artifact was refused
# for one of them ("Debug description for `"picture-image" Any`", a double quote).
if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  printf 'Other, label: picture\n' > "$t/Debug description for \`\"picture-image\" Any\`"
  printf '\211PNG\r\n\032\n' > "$t/at 1:2 <a> |b| *c* ?d?.png"
  printf 'plain\n' > "$t/kept.txt"
  printf 'one\n' > "$t/two:one.txt"; printf 'two\n' > "$t/two?one.txt"
  sh "$0" --tidy "$t" > /dev/null
  bad=$(find "$t" -type f -name '*[":<>|*?]*')
  [ -z "$bad" ] || { echo "FAIL: ios-xcresult self-test: a name the upload refuses is left: $(basename "$bad")"; fail=1; }
  [ "$(cat "$t/Debug description for \`_picture-image_ Any\`.txt" 2>/dev/null)" = "Other, label: picture" ] ||
    { echo "FAIL: ios-xcresult self-test: the debug description is not kept under a name the upload takes"; fail=1; }
  [ -f "$t/at 1_2 _a_ _b_ _c_ _d_.png" ] || { echo "FAIL: ios-xcresult self-test: the picture is not kept under a name the upload takes"; fail=1; }
  [ "$(cat "$t/kept.txt")" = plain ] || { echo "FAIL: ios-xcresult self-test: a plain name was touched"; fail=1; }
  [ "$(cat "$t/two_one.txt" "$t/two_one-2.txt" 2>/dev/null | sort | tr '\n' ' ')" = "one two " ] ||
    { echo "FAIL: ios-xcresult self-test: two names that become the same did not both stay"; fail=1; }
  [ "$(find "$t" -type f | wc -l | tr -d ' ')" = 5 ] || { echo "FAIL: ios-xcresult self-test: files went missing: $(ls "$t" | tr '\n' ';')"; fail=1; }
  rm -rf "$t"
  [ $fail = 0 ] && echo "ok: ios-xcresult self-test: names the upload refuses are renamed, contents kept"
  exit $fail
fi
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
