#!/bin/sh
# The Android app on an emulator against a computer somewhere else, over Tailcat's real relays: CI's Windows or macOS
# runner, with the invite decoded from the Herdr dialog's QR code there (windows-add-phone.ps1, mac-add-phone.sh). Both
# computers need the internet only. HM_REMOTE_SPACE names the computer's space (the herdr.space label of its terminal;
# default spwin, Windows').
#
#   ANDROID_SERIAL=emulator-5554 e2e/android-remote.sh open <apk>
#       installs the app with no data, opens it on the pairing screen, and readies the emulator's clipboard (below)
#   ANDROID_SERIAL=emulator-5554 e2e/android-remote.sh pair <invite file> <screenshot folder>
#       pastes the invite, pairs, and passes when home lists acme-api (the computer's terminal), a command sent from the
#       terminal's input box runs there (the computer's job checks), and then, once the computer removes this phone, the app asks to
#       pair again
#
# Nothing printed holds the invite: the screen is only read for ids and the texts below, and pairing codes are masked.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
trap 'adb_exit $?' EXIT
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
readonly id
space=${HM_REMOTE_SPACE:-spwin}
fail() { echo "FAIL: $*"; exit 1; }

xml() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml; }
# The centre of the view with this resource-id (an NSH testID).
at() {
  xml | tr '>' '\n' | grep -F "resource-id=\"$1\"" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || fail "no view $1 on screen"
  "$adb" shell input tap $xy
}
screen_has() { xml | grep -qF "$1"; }
# The invite goes in as a person pastes it: on the phone's clipboard, then Paste in the field. The clipboard is set
# through the emulator's own gRPC endpoint (setClipboard in $ANDROID_HOME/emulator/lib/emulator_controller.proto), with
# the port and token the emulator writes to its pid_*.ini. Typing it with `adb shell input text` is no good here: CI's
# emulator types 4 characters a second and drops some, so the 554-character invite took 5 rounds of 2 minutes and was
# still wrong, while the code lasts 2 minutes.
clip_dir=${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hm-clip
pid_ini() {
  grep -lx "port.serial=${ANDROID_SERIAL#emulator-}" \
    "${XDG_RUNTIME_DIR:-/nonexistent}"/avd/running/pid_*.ini /tmp/android-"$(id -un)"/avd/running/pid_*.ini \
    "$HOME"/Library/Caches/TemporaryItems/avd/running/pid_*.ini "$HOME"/.android/avd/running/pid_*.ini 2>/dev/null | head -1
}
clip() { # set <file> | get
  ini=$(pid_ini)
  # An emulator without a window writes the file once its gRPC endpoint is on (seen on a Mac); the console turns it on.
  [ -n "$ini" ] || { "$adb" emu grpc $((${ANDROID_SERIAL#emulator-} + 3000)) >/dev/null; sleep 1; ini=$(pid_ini); }
  [ -n "$ini" ] || fail "no running emulator's pid_*.ini for $ANDROID_SERIAL"
  HM_GRPC_PORT=$(sed -n 's/^grpc.port=//p' "$ini") HM_GRPC_TOKEN=$(sed -n 's/^grpc.token=//p' "$ini") \
    "$clip_dir/py/bin/python" -I "$clip_dir/clip.py" "$clip_dir" "$@"
}
# For a failure: the screen's ids and texts, any invite or pairing link masked.
screen() {
  echo "--- on the phone's screen:"
  xml | tr '>' '\n' | grep -oE '(resource-id|text)="[^"]+"' | head -40 | sed -E 's/herdr1:[A-Za-z0-9_-]+/herdr1:…/g; s/#pair=[A-Za-z0-9_-]+/#pair=…/g'
}

case "${1:-}" in
  open)
    apk=$2
    "$adb" install -r "$apk" >/dev/null 2>&1 || { "$adb" uninstall "$id" >/dev/null; "$adb" install "$apk" >/dev/null; }
    "$adb" shell pm clear "$id" >/dev/null
    "$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
    "$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
    for _ in $(seq 1 30); do screen_has 'resource-id="pair-link"' && break; sleep 1; done
    if ! screen_has 'resource-id="pair-link"'; then
      screen
      echo "--- in front:"; "$adb" shell dumpsys activity activities | grep -E 'ResumedActivity' | head -3
      echo "--- crashes:"; "$adb" logcat -d -b crash | tail -40
      echo "--- the app's log:"; "$adb" logcat -d | grep -iE 'herdranywhere|AndroidRuntime|ActivityTaskManager' | tail -40
      fail "the app did not open on the pairing screen"
    fi
    echo "ok: the app is open on the pairing screen"
    rm -rf "$clip_dir"; mkdir -p "$clip_dir"
    python3 -m venv "$clip_dir/py"
    "$clip_dir/py/bin/pip" install --disable-pip-version-check -q grpcio==1.84.1 grpcio-tools==1.84.1 protobuf==7.36.2
    proto="${ANDROID_HOME:-$HOME/Library/Android/sdk}/emulator/lib"
    "$clip_dir/py/bin/python" -m grpc_tools.protoc -I "$proto" --python_out="$clip_dir" --grpc_python_out="$clip_dir" \
      "$proto/emulator_controller.proto"
    cat >"$clip_dir/clip.py" <<'PY'
import os, sys
import grpc
sys.path.insert(0, sys.argv[1])
import emulator_controller_pb2 as pb, emulator_controller_pb2_grpc as rpc
from google.protobuf import empty_pb2
stub = rpc.EmulatorControllerStub(grpc.insecure_channel("localhost:" + os.environ["HM_GRPC_PORT"]))
auth = [("authorization", "Bearer " + os.environ["HM_GRPC_TOKEN"])]
if sys.argv[2] == "set":
    with open(sys.argv[3]) as f:
        stub.setClipboard(pb.ClipData(text=f.read()), metadata=auth, timeout=10)
else:
    print(len(stub.getClipboard(empty_pb2.Empty(), metadata=auth, timeout=10).text))
PY
    # The clipboard check, seen working both ways: what is set is read back, and then an empty one.
    printf 'hm-clip-check' >"$clip_dir/check"
    clip set "$clip_dir/check"; [ "$(clip get)" = 13 ] || fail "the emulator's clipboard did not take 13 characters"
    : >"$clip_dir/check"; clip set "$clip_dir/check"; [ "$(clip get)" = 0 ] || fail "the emulator's clipboard did not empty"
    rm -f "$clip_dir/check"
    echo "ok: the emulator's clipboard can be set"
    ;;
  pair)
    invite=$(cat "$2")
    shots=$3
    mkdir -p "$shots"
    case $invite in herdr1:*) ;; *) fail "$2 holds no invite" ;; esac
    # The check that home lists the computer's project, seen failing: before pairing it doesn't.
    screen_has acme-api && fail "the screen shows acme-api before pairing"
    echo "ok: before pairing, the screen shows no acme-api"
    # What the link field holds: pair-link is the field's wrapper; the text is its child's.
    typed() { xml | tr '>' '\n' | sed -n '/resource-id="pair-link"/,$p' | grep -oE 'text="[^"]+"' | head -1 | sed -E 's/^text="(.*)"$/\1/'; }
    started=$(date +%s)
    clip set "$2"
    [ "$(clip get)" = "${#invite}" ] || fail "the clipboard does not hold the invite's ${#invite} characters"
    tap pair-link
    for _ in 1 2 3; do
      "$adb" shell input keyevent KEYCODE_PASTE
      got=$(typed); [ "$got" = "$invite" ] && break
      echo "after Paste the field holds ${#got} characters, not the invite's ${#invite}; again"
      "$adb" shell input keyevent KEYCODE_MOVE_END $(printf 'KEYCODE_DEL %.0s' $(seq 1 $((${#got} + 1))))
    done
    : >"$clip_dir/empty"; clip set "$clip_dir/empty"
    echo "pasted in $(($(date +%s) - started)) s"
    [ "$got" = "$invite" ] || fail "the field never held the whole invite"
    started=$(date +%s)
    tap pair
    ok=""
    for _ in $(seq 1 120); do screen_has acme-api && { ok=1; break; }; sleep 1; done
    # No screenshot before home shows: until then the field on screen holds the invite.
    if [ -z "$ok" ]; then
      xml | tr '>' '\n' | grep 'resource-id="pair-error"' | sed -E 's/.*text="([^"]*)".*/  phone says: \1/' || true
      screen; fail "within 120 s of the tap on Pair, home does not list acme-api"
    fi
    "$adb" exec-out screencap -p > "$shots/1-home.png"
    echo "ok: paired over Tailcat; home lists the computer's acme-api $(($(date +%s) - started)) s after the tap on Pair"

    tap "space-$space"
    sleep 3
    tap compose-input
    # Spaces around >: Windows' terminal here is pwsh, which reads `phone>` as a word (on CI's Windows runner); cmd and pwsh
    # both write the file this way. adb's input text loses characters on CI's emulator, so Send waits until the box holds
    # the whole command; until then the box is cleared and the command typed again.
    cmd='echo phone > hm-typed'
    boxed() { xml | tr '>' '\n' | sed -n '/resource-id="compose-input"/,$p' | grep -oE 'text="[^"]+"' | head -1 | sed -E 's/^text="(.*)"$/\1/' | sed 's/&gt;/>/g'; }
    whole=""
    for _ in 1 2 3 4; do
      "$adb" shell input text "'$cmd'"
      got=$(boxed); [ "$got" = "$cmd" ] && { whole=1; break; }
      echo "the input box holds '$got', not '$cmd'; typing it again"
      "$adb" shell input keyevent KEYCODE_MOVE_END $(printf 'KEYCODE_DEL %.0s' $(seq 1 $((${#got} + 1))))
    done
    [ -n "$whole" ] || { screen; fail "the input box never held the whole command"; }
    tap compose-send
    "$adb" shell input keyevent 4 # the keyboard away, so the terminal's rows show
    sleep 3
    "$adb" exec-out screencap -p > "$shots/2-terminal.png"
    echo "ok: sent 'echo phone > hm-typed' from the terminal's input box (the computer checks that it ran)"

    # The computer's job removes this phone once it has seen the file; the app is then told on its connection.
    gone=""
    for _ in $(seq 1 600); do screen_has 'This computer no longer knows this phone' && { gone=1; break; }; sleep 1; done
    "$adb" exec-out screencap -p > "$shots/3-removed.png"
    [ -n "$gone" ] || { screen; fail "the app never said the computer no longer knows this phone"; }
    echo "ok: removed on the computer, the app asks to pair again"
    echo PASS
    ;;
  *) sed -n '2,13p' "$0"; exit 2 ;;
esac
