#!/bin/sh
# The chat view to look at in a browser: an isolated herdr-core and the companion serving the PWA on 127.0.0.1, with
#   acme-api   a fake Claude Code (e2e/fake-claude.py) replaying every turn Claude Code 2.1.319 wrote against a fake API:
#              commands, edits, thinking, an image read, searches, a helper agent and its notice, Markdown, an API
#              error, a prompt with an image; a prompt goes in every 20 s, and anything typed in the composer is one
#   acme-web   the same agent, but its record has a line no reader knows: the chat says so and shows no turn
#   notes      a shell: no chat button
# Prints the page and a pairing link; Ctrl-C (or kill) removes it all. Fake data only.
# Usage: e2e/chat-demo.sh [port]   (needs dist/web: nsh package --target web -o dist/web)
set -eu
app=$(cd "$(dirname "$0")/.." && pwd)
companion="$app/../companion"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
fixtures="$companion/internal/chat/claude/testdata/claude-code/2.1.319"
port=${1:-7790}
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
(cd "$companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere)
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/home/src/acme-api" "$work/home/src/acme-web" "$work/home/notes" "$work/state" "$work/config" "$work/data" "$work/claude"
printf 'a\n' > "$work/home/src/acme-api/notes.txt"; printf 'b\n' > "$work/home/src/acme-api/todo.txt"
# The blue square the image turns show is in the fixtures themselves.
: > "$work/home/.zshrc"
# herdr-core passes the environment of whatever first started it to every terminal: started from inside a Claude Code
# session, every Claude in it inherits CLAUDE_CODE_CHILD_SESSION=1 and saves no transcript (seen with Claude Code 2.1.319).
# So the core, and the companion that asks it, start with only these variables.
. "$app/e2e/companion.sh"  # companion_start, companion_stop
clean() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin HOME="$work/home" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmchat-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" \
    HERDR_ANYWHERE_STATE="$work/companion" CLAUDE_CONFIG_DIR="$work/claude" "$@"
}
lhc() { clean "$hc" "$@"; }
lhc run --bg --keep --cwd "$work/home/src/acme-api" --name claude --label herdr.space=spchat1 --label herdr.tab=tbchat1 \
  python3 "$app/e2e/fake-claude.py" "$fixtures/turns.jsonl" "$fixtures/gallery.jsonl" "$fixtures/image.jsonl" >/dev/null
lhc run --bg --keep --cwd "$work/home/src/acme-web" --name claude2 --label herdr.space=spchat2 --label herdr.tab=tbchat2 \
  python3 "$app/e2e/fake-claude.py" --unreadable "$fixtures/turns.jsonl" >/dev/null
lhc run --bg --keep --cwd "$work/home/notes" --label herdr.space=spchat3 --label herdr.tab=tbchat3 >/dev/null

cleanup() {
  [ -n "${feed_pid:-}" ] && kill "$feed_pid" 2>/dev/null
  companion_stop
  lhc server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
}
trap 'trap - EXIT; cleanup' EXIT
trap 'trap - EXIT; cleanup; exit 130' INT
trap 'trap - EXIT; cleanup; exit 143' TERM HUP
companion_start "$work/serve.log" clean "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --web "$app/dist/web/Herdr Anywhere" --hc "$hc" --state "$work/companion" || exit 1
# The prompts in the order of the turns fake-claude.py replays; the first round goes in quickly, then one every 20 s.
( sleep 2
  lhc send claude2 "HELLO there" >/dev/null 2>&1 || true
  n=0
  while :; do
    for p in "LIST the files here" "EDIT a file for me" "HELLO again" "THINK about a plan" "LOOK at shot.png" "FIND the todos" \
      "HELP me with a helper" "WRITE up the notes" "FAIL on purpose" "HELLO, what is in this picture?"; do
      lhc send claude "$p" >/dev/null 2>&1 || true
      n=$((n + 1)); [ "$n" -le 10 ] && sleep 6 || sleep 20
    done
  done ) &
feed_pid=$!
sleep 1
echo "page: http://127.0.0.1:$port/"
clean "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1
echo "log: $work/serve.log"
wait "$companion_job"
