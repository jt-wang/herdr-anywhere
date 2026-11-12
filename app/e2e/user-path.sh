#!/bin/sh
# INSTALL.md's steps as they are written, from the release files, with a throwaway home and no Go: downloads the
# companion from a local copy of the release, adds the dialog to a Herdr that has been opened once, starts the companion
# through Tailcat, and gets a pairing code. Pairing a phone with that companion: HERDR_ANYWHERE_BIN=<file> e2e/android-tailcat.sh
# Usage: e2e/user-path.sh <folder with the release files>   Needs hc, curl and python3, and the internet.
set -eu
files=$(cd "${1:?give the folder with the release files}" && pwd)
app=$(cd "$(dirname "$0")/.." && pwd)
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
core=$(command -v herdr-core || echo "$HOME/.local/bin/herdr-core")
# Short: the companion's control socket path must stay under 104 bytes.
work=$(mktemp -d /tmp/hmupXXXX)
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$files" >/dev/null 2>&1 &
web=$!
cleanup() {
  pkill -f "$work/home/.local/bin/herdr-anywhere" 2>/dev/null || true
  "$hc" server stop >/dev/null 2>&1 || true
  kill "$web" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"
}
trap cleanup EXIT INT TERM
for _ in 1 2 3 4 5 6 7 8 9 10; do curl -fsS -o /dev/null "http://127.0.0.1:$port/" 2>/dev/null && break; python3 -c 'import time; time.sleep(0.3)'; done

# What a Mac with Herdr has: hc and herdr-core in ~/.local/bin, and Herdr's keys.json from opening it once. No Go.
mkdir -p "$work/home/.local/bin" "$work/home/.config/herdr-app" "$work/bin" "$work/state" "$work/config" "$work/data" "$work/run"
ln -s "$hc" "$work/home/.local/bin/hc"; ln -s "$core" "$work/home/.local/bin/herdr-core"
printf '{"global":{}}\n' > "$work/home/.config/herdr-app/keys.json"
export HOME="$work/home" HC_SESSION="hmup-$$" HC_SOCKET="" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$work/run"
export PATH="$HOME/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
if command -v go >/dev/null 2>&1; then echo "FAIL: go is on this path: $(command -v go)"; exit 1; fi
echo "ok: no go on the path"

# The commands in INSTALL.md's sections 2 and 3, verbatim, except that the release is local.
block() { python3 - "$app/../INSTALL.md" "$1" <<'PY'
import re, sys
text = open(sys.argv[1], encoding='utf-8').read()
section = text.split('\n## ' + sys.argv[2] + '.', 1)[1].split('\n## ', 1)[0]
print(re.search(r'```sh\n(.*?)```', section, re.S).group(1))
PY
}
step=$(block 2 | sed "s#https://github.com/jt-wang/herdr-anywhere/releases/latest/download#http://127.0.0.1:$port#")
out=$(sh -eu -c "$step")
echo "$out" | grep -q '^arm64$' || { echo "FAIL: this path is for arm64: $out"; exit 1; }
want=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$app/nsh.json")
echo "$out" | grep -qx "herdr-anywhere $want" || { echo "FAIL: section 2 did not print version $want: $out"; exit 1; }
echo "ok: section 2 downloaded and ran herdr-anywhere $want"
# A cgo build declares the macOS it was built on as its minimum (the version of the Mac that built the file); a Go-only one declares Go's, 13.0.
minos=$(otool -l "$HOME/.local/bin/herdr-anywhere" | sed -n '/LC_BUILD_VERSION/,+3p' | awk '/minos/ { print int($2) }')
[ "$minos" -le 13 ] || { echo "FAIL: it asks for macOS $minos or newer"; exit 1; }
echo "ok: it runs on macOS $minos and newer"
xattr "$HOME/.local/bin/herdr-anywhere" | grep -q com.apple.quarantine && { echo "FAIL: curl left it quarantined"; exit 1; }
echo "ok: no quarantine mark"
out=$(sh -eu -c "$(block 3)")
echo "$out" | grep -q 'Add phone is installed' || { echo "FAIL: section 3: $out"; exit 1; }
grep -q 'cmd-shift-m' "$HOME/.config/herdr-app/keys.json" || { echo "FAIL: the dialog's key was not added"; exit 1; }
echo "ok: section 3 added the dialog"

# What the dialog's Start and its code do.
ha="$HOME/.local/bin/herdr-anywhere"
"$hc" run --bg --keep --cwd "$HOME" --label herdr.space=spup --label herdr.tab=tbup >/dev/null
"$ha" start > "$work/start.out" 2>&1 || { echo "FAIL: start"; cat "$work/start.out"; exit 1; }
out=$("$ha" pair --json)
echo "$out" | grep -q '"invite":"herdr1:' || { echo "FAIL: no pairing code: $out"; exit 1; }
echo "ok: the companion runs through Tailcat and gave a pairing code"
echo "PASS: installed from the release files without Go, and running"
