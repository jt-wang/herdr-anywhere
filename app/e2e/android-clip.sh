#!/bin/sh
# The emulator's clipboard, set and read from the computer through the emulator's own gRPC endpoint (setClipboard in
# $ANDROID_HOME/emulator/lib/emulator_controller.proto), with the port and token the emulator writes to its pid_*.ini
# (as android-remote.sh does). For text too long to type: CI's emulator types about 4 characters a second.
# Works from inside a suite that points HOME and XDG_RUNTIME_DIR at its own folders: the emulator's files are looked
# up under the user's real home and runtime folder.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-clip.sh set FILE | get   (get prints the clipboard's length)
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
me=$(id -un)
real_home=$(eval echo "~$me")
dir=${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hm-clip-$me
pid_ini() {
  grep -lx "port.serial=${ANDROID_SERIAL#emulator-}" \
    "/run/user/$(id -u)"/avd/running/pid_*.ini /tmp/android-"$me"/avd/running/pid_*.ini \
    "$real_home"/Library/Caches/TemporaryItems/avd/running/pid_*.ini "$real_home"/.android/avd/running/pid_*.ini 2>/dev/null | head -1
}
if [ ! -x "$dir/py/bin/python" ] || [ ! -f "$dir/clip.py" ]; then
  rm -rf "$dir"; mkdir -p "$dir"
  python3 -m venv "$dir/py"
  HOME=$real_home "$dir/py/bin/pip" install --disable-pip-version-check -q grpcio==1.84.1 grpcio-tools==1.84.1 protobuf==7.36.2
  proto="${ANDROID_HOME:-$real_home/Library/Android/sdk}/emulator/lib"
  "$dir/py/bin/python" -m grpc_tools.protoc -I "$proto" --python_out="$dir" --grpc_python_out="$dir" "$proto/emulator_controller.proto"
  cat >"$dir/clip.py" <<'PY'
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
fi
ini=$(pid_ini)
# An emulator without a window writes the file once its gRPC endpoint is on (seen on a Mac); the console turns it on,
# and adb finds the console's token in the real home.
[ -n "$ini" ] || { HOME=$real_home "$adb" emu grpc $((${ANDROID_SERIAL#emulator-} + 3000)) >/dev/null; sleep 1; ini=$(pid_ini); }
[ -n "$ini" ] || { echo "FAIL: no running emulator's pid_*.ini for $ANDROID_SERIAL"; exit 1; }
HM_GRPC_PORT=$(sed -n 's/^grpc.port=//p' "$ini") HM_GRPC_TOKEN=$(sed -n 's/^grpc.token=//p' "$ini") \
  "$dir/py/bin/python" -I "$dir/clip.py" "$dir" "$@"
