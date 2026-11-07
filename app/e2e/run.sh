#!/bin/sh
# Runs e2e/live.test.ts against a real companion and a real herdr-core in a session of their own:
# its own HC_SESSION, HOME, XDG dirs and companion state, a free port on 127.0.0.1, all removed after.
# Needs: go, nsh, and herdr-core's hc (HM_TEST_HC, or on PATH, or ~/.local/bin).
set -eu
app=$(cd "$(dirname "$0")/.." && pwd)
companion="$app/../companion"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
[ -x "$hc" ] || { echo "no hc: install herdr-core first" >&2; exit 1; }

work=$(mktemp -d)
(cd "$companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere) # before HOME moves: go caches under it
run=$(mktemp -d /tmp/hmXXXX) # the core's sockets must fit macOS's 103-byte path limit
mkdir -p "$work/home" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"

export HC_SESSION="hme2e-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/sh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export HERDR_ANYWHERE_STATE="$work/companion"

placeholder=$(cat "$app/e2e/pair.ts")
cleanup() {
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null && wait "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  rm -rf "$work" "$run"
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
}
trap cleanup EXIT INT TERM

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0
until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do
  i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; exit 1; }
  sleep 0.1
done
HM_PAIR=$("$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$HM_PAIR" ] || { echo "pair printed no link" >&2; exit 1; }
cd "$app"
# nsh test passes the test no environment, so the link goes in as a module, put back to the placeholder after.
printf 'export const PAIR = %s\nexport const COMPANION = %s\n' "'$HM_PAIR'" "$serve_pid" > e2e/pair.ts
status=0
nsh test e2e/live.test.ts || status=$?
[ $status -eq 0 ] || { echo "--- companion log" >&2; cat "$work/serve.log" >&2; }
exit $status
