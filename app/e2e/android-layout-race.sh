#!/bin/bash
# A real Android Root over FakeCore: the old Store must lose the new pane; the current Store must keep it and input.
# Usage: --package OUT (no device); or BEFORE.apk AFTER.apk OUT. ANDROID_SERIAL selects a dedicated test device.
set -euo pipefail
app=$(cd "$(dirname "$0")/.." && pwd)
check="$app/e2e/layout-race-evidence.py"
old_commit=27cc6b0fb7a895921af95e012ffdcce3ba1e14ff
old_hash=439053b36201e7c43423fd5c0c2b3674aae0a26f6cec2b9bad51bfdd69fbf4fd
new_hash=0e744bc5a5ca2c7594b61ab68aafb8619646a5504c6983664bb039d776e4e3b6
id=io.jingtao.herdranywhere.layoutcheck

if [ "${1:-}" = --package ]; then
  mkdir -p "$2"; out=$(cd "$2" && pwd)
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  # Public history only; a shallow checkout may not contain the pinned control.
  if ! git -C "$app" cat-file -e "$old_commit:app/src/store.ts" 2>/dev/null; then
    git -C "$app" fetch --no-tags origin "$old_commit"
  fi
  git -C "$app" show "$old_commit:app/src/store.ts" > "$work/before-store.ts"
  python3 - "$app" "$work" "$out" "$old_commit" "$old_hash" "$new_hash" <<'PY'
from pathlib import Path
import hashlib, json, shutil, sys
app, work, out = map(Path, sys.argv[1:4])
commit, old_hash, new_hash = sys.argv[4:]
old = (work / 'before-store.ts').read_bytes()
new = (app / 'src/store.ts').read_bytes()
assert hashlib.sha256(old).hexdigest() == old_hash, 'old Store hash mismatch'
assert hashlib.sha256(new).hexdigest() == new_hash, 'new Store hash mismatch'
binding = {'old_commit': commit, 'variants': {}}
# Both app copies resolve src/tunnel.ts's ../../third_party import through this shared directory.
vendor = Path('third_party/tailpocket-8ae23dd/sdk/js')
shutil.copytree(app.parent / vendor, work / vendor)
binding['vendored'] = {}
for source in sorted((app.parent / vendor).rglob('*')):
    if source.is_file():
        relative = source.relative_to(app.parent)
        original = hashlib.sha256(source.read_bytes()).hexdigest()
        assert hashlib.sha256((work / relative).read_bytes()).hexdigest() == original
        binding['vendored'][relative.as_posix()] = original
for name, store in [('before', old), ('after', new)]:
    dest = work / name
    shutil.copytree(app, dest, ignore=shutil.ignore_patterns('android', 'ios', 'build', '.dev', '.git', 'node_modules'))
    (dest / 'src/store.ts').write_bytes(store)
    (dest / 'e2e/layout-params.ts').write_text("import { host } from 'nsh'\nObject.defineProperty(host, 'params', { value: { scene: 'layout-race', prefs: JSON.stringify({ opened: 2 }) }, configurable: true })\n")
    (dest / '.layout-check.tsx').write_text("import './src/fonts.ts'\nimport './e2e/layout-params.ts'\nimport './fixtures/fake.tsx'\n")
    sources = ['src/store.ts', 'fixtures/fake.tsx', 'fixtures/layout-race.tsx', 'e2e/layout-params.ts', '.layout-check.tsx', 'e2e/android-layout-race.sh', 'e2e/layout-race-evidence.py']
    binding['variants'][name] = {p: hashlib.sha256((dest / p).read_bytes()).hexdigest() for p in sources}
    (out / (name + '-store.ts')).write_bytes(store)
for path in binding['variants']['before']:
    if path != 'src/store.ts': assert binding['variants']['before'][path] == binding['variants']['after'][path]
(out / 'source-binding.json').write_text(json.dumps(binding, indent=2) + '\n')
PY
  for variant in before after; do
    (cd "$work/$variant" && nsh package --target android .layout-check.tsx --id "$id" --name "Layout regression" -o "$work/$variant-apk")
    mapfile -t apks < <(find "$work/$variant-apk" -maxdepth 1 -type f -name '*.apk')
    [ "${#apks[@]}" = 1 ] || { echo 'FAIL: expected one fixture APK'; exit 1; }
    cp "${apks[0]}" "$out/$variant.apk"
  done
  python3 "$check" manifest "$out"
  exit 0
fi

: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the dedicated test device}"
before=$1; after=$2; mkdir -p "$3"; out=$(cd "$3" && pwd)
adb="${ANDROID_HOME:?}/platform-tools/adb"
work=$(mktemp -d)
cleanup() {
  status=$?
  trap - EXIT
  "$adb" -s "$ANDROID_SERIAL" shell am force-stop "$id" >/dev/null 2>&1 || true
  rm -rf "$work"
  python3 "$check" manifest "$out" || status=1
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
a() { "$adb" -s "$ANDROID_SERIAL" "$@"; }
dump() { a shell uiautomator dump /sdcard/layout-check.xml >/dev/null; a shell cat /sdcard/layout-check.xml > "$work/ui.xml"; }
at() {
  python3 - "$work/ui.xml" "$1" <<'PY'
import re, sys, xml.etree.ElementTree as ET
for n in ET.parse(sys.argv[1]).iter('node'):
    if n.get('resource-id') == sys.argv[2]:
        x1, y1, x2, y2 = map(int, re.findall(r'\d+', n.attrib['bounds']))
        print((x1 + x2) // 2, (y1 + y2) // 2)
        sys.exit(0)
sys.exit(1)
PY
}
wait_for() {
  for _ in $(seq 1 30); do
    if dump && at "$1" > "$work/point"; then return 0; fi
    sleep 1
  done
  a exec-out screencap -p > "$folder/failure.png"
  echo "FAIL: missing $1"; return 1
}
tap() { wait_for "$1"; read -r x y < "$work/point"; a shell input tap "$x" "$y"; }
shot() { a exec-out screencap -p > "$folder/$1.png"; dump; cp "$work/ui.xml" "$folder/$1.xml"; }
evidence() {
  for _ in $(seq 1 15); do
    a logcat -d --pid="$pid" -v raw > "$work/app.log"
    if python3 "$check" extract "$work/app.log" "$1" "$folder/$1.json" 2> "$work/extract-error"; then return 0; fi
    sleep 1
  done
  cat "$work/extract-error" >&2; return 1
}

for variant in before after; do
  folder="$out/$variant"; mkdir -p "$folder"
  apk=$before; [ "$variant" = before ] || apk=$after
  # Only the fixture package; never the user's Herdr Anywhere package or its storage.
  a uninstall "$id" >/dev/null 2>&1 || true
  a install "$apk" >/dev/null
  activity=$(a shell cmd package resolve-activity --brief "$id" | tr -d '\r' | tail -1)
  a shell am start -W -n "$activity" >/dev/null
  wait_for space-splayout
  pid=$(a shell pidof "$id" | tr -d '\r')
  [[ "$pid" =~ ^[0-9]+$ ]] || { echo 'FAIL: fixture PID missing'; exit 1; }
  tap space-splayout
  tap layout-arm
  tap switcher
  tap switch-new-shell
  wait_for layout-blocked
  tap layout-snapshot
  evidence visible
  terminal=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["terminal"])' "$folder/visible.json")
  wait_for "terminal-$terminal"
  shot 01-new-visible
  tap layout-release
  wait_for layout-released
  evidence released
  shot 02-after-conflict
  rc=0
  python3 "$check" check "$folder/visible.json" "$folder/released.json" > "$folder/retention.log" 2>&1 || rc=$?
  echo "$rc" > "$folder/retention.exit"
  cat "$folder/retention.log"
  if [ "$variant" = before ]; then
    [ "$rc" = 1 ] && grep -qx 'FAIL terminal-retention' "$folder/retention.log" || { echo 'FAIL: old Store did not fail the retention assertion'; exit 1; }
  else
    [ "$rc" = 0 ] || exit "$rc"
    wait_for "terminal-$terminal"
    tap compose-input
    a shell input text 'layout%sinput%sok'
    tap compose-send
    # Hide the keyboard only if it is up; no Back that could leave the terminal page.
    if a shell dumpsys input_method | grep -q 'mInputShown=true'; then a shell input keyevent KEYCODE_BACK; fi
    tap layout-snapshot
    evidence input
    python3 "$check" check "$folder/visible.json" "$folder/released.json" "$folder/input.json" > "$folder/input.log"
    wait_for "terminal-$terminal"
    shot 03-input
  fi
  a shell am force-stop "$id" >/dev/null
done
echo 'PASS: old Store retention assertion failed; new Store retained the terminal and received input'
