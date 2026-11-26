#!/bin/sh
# Runs e2e/live.test.ts (or HM_LIVE_TEST) against a real companion and a real herdr-core in a session of their own:
# its own HC_SESSION, HOME, XDG dirs and companion state, a free port on 127.0.0.1, all removed after.
# Needs: go, nsh, and herdr-core's hc (HM_TEST_HC, or on PATH, or ~/.local/bin).
# The app's commands on the computer (Store.exec) see HM_LIVE_BIN, HM_LIVE_URL and HM_LIVE_CTL: a number of seconds
# written to $HM_LIVE_CTL/restart stops the companion, and starts it again that much later on the same port and state.
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard run # a login entry of its own, never the person's
app=$(cd "$(dirname "$0")/.." && pwd)
companion="$app/../companion"
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
[ -x "$hc" ] || { echo "no hc: install herdr-core first" >&2; exit 1; }

work=$(mktemp -d)
(cd "$companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere) # before HOME moves: go caches under it
run=$(mktemp -d /tmp/hmXXXX) # the core's sockets must fit macOS's 103-byte path limit
mkdir -p "$work/home" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
h="$work/home"; (cd "$app/e2e/fake-claude" && GO111MODULE=off go build -o "$h/bin/claude" main.go) # live-app.tsx's made-up agent

export HC_SESSION="hme2e-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/sh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export HERDR_ANYWHERE_STATE="$work/companion"

placeholder=$(cat "$app/e2e/pair.ts")
cleanup() {
  [ -n "${keeper:-}" ] && kill "$keeper" 2>/dev/null && wait "$keeper" 2>/dev/null || true
  [ -s "$work/serve.pid" ] && kill "$(cat "$work/serve.pid")" 2>/dev/null
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null && wait "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  rm -rf "$work" "$run"
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  autostart_guard_end || exit 1
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
export HM_LIVE_BIN="$work/herdr-anywhere" HM_LIVE_URL="http://127.0.0.1:$port" HM_LIVE_CTL="$work/ctl"
mkdir "$HM_LIVE_CTL"
up() {
  i=0
  until curl -fsS "$HM_LIVE_URL/api/info" >/dev/null 2>&1; do
    i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; return 1; }
    sleep 0.1
  done
}
"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
up
# The restarts the app asks for: the first serve is this script's child, the ones after are the keeper's, in serve.pid.
(
  while :; do
    if [ -f "$HM_LIVE_CTL/restart" ]; then
      down=$(cat "$HM_LIVE_CTL/restart"); rm -f "$HM_LIVE_CTL/restart"
      old=$(cat "$work/serve.pid" 2>/dev/null || echo "$serve_pid")
      kill "$old" 2>/dev/null
      while kill -0 "$old" 2>/dev/null; do sleep 0.1; done
      echo "--- stopped for ${down}s" >> "$work/serve.log"
      sleep "$down"
      "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" >> "$work/serve.log" 2>&1 &
      echo $! > "$work/serve.pid"
      up || exit 1
    fi
    sleep 0.2
  done
) &
keeper=$!
HM_PAIR=$("$work/herdr-anywhere" pair --url "$HM_LIVE_URL" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$HM_PAIR" ] || { echo "pair printed no link" >&2; exit 1; }
cd "$app"
# nsh test passes the test no environment, so the link goes in as a module, put back to the placeholder after.
printf 'export const PAIR = %s\nexport const COMPANION = %s\n' "'$HM_PAIR'" "$serve_pid" > e2e/pair.ts
status=0
nsh test "${HM_LIVE_TEST:-e2e/live.test.ts}" || status=$?
[ $status -eq 0 ] || { echo "--- companion log" >&2; cat "$work/serve.log" >&2; }
exit $status
