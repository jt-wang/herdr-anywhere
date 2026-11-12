#!/bin/sh
# This commit's app installed over the previous release's, as a phone updates: the previous one installed and opened,
# then this one with install -r and opened. Passes when it runs and says its version. Both APKs must share a key
# (update-files.sh and the previous tag's build, on one runner).
# Usage: ANDROID_SERIAL=emulator-5554 app/e2e/ci/over-previous.sh PREVIOUS.apk THIS.apk
set -eu
: "${ANDROID_SERIAL:?}"
adb="${ANDROID_HOME:?}/platform-tools/adb"
app=$(cd "$(dirname "$0")/../.." && pwd)
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$1" | tail -1
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null; sleep 4
"$adb" install -r "$2" | tail -1
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null; sleep 5
v=$("$adb" shell dumpsys package "$id" | grep -m1 versionName | tr -d ' \r')
[ -n "$("$adb" shell pidof "$id" | tr -d '\r')" ] || { echo "FAIL: not running after the update ($v)"; exit 1; }
echo "PASS: this commit's app runs, installed over the previous release ($v)"
