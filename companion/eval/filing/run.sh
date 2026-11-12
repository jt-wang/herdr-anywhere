#!/bin/sh
# The filing agent's evaluation: the shipped path end to end, with your own agent and its login or key.
# For each of the app's seven languages it queues the made-up batch in cases.json (stopped panes, scanned sessions,
# typed messages), sets the phone's language, and lets the companion hand the batch to the filing pane in an
# isolated herdr-core session, as `serve` does. Then it types a correction into the filing pane, as the user would. Then score.py checks what the agent did against the rules in README.md
# and writes report.json.
#
# Usage: companion/eval/filing/run.sh [out-dir]          (default: a new folder under /tmp)
#   AGENT    the filing agent's command (default: claude --model claude-haiku-5-5, as `progress agents` offers it)
#   LABEL    a name for the run in its report (default: AGENT)
#   LANGS    the languages to run (default: all seven)
#   HC       herdr-core's hc (default: hc on PATH, else ~/.local/bin/hc)
# Needs herdr-core, Go, Python 3 and the agent, logged in or with its key in the environment (ANTHROPIC_API_KEY for
# Claude Code). Claude Code keeps your login in your home folder, so it runs with your HOME; it reads no user
# settings (--setting-sources project) and records each eval folder as trusted in ~/.claude.json. Pi runs with this
# run's own home and reads only ~/.pi/agent of yours.
# Nothing touches your own herdr-core session: the core here runs as its own session with its own folders.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
real_home=$HOME
claude=$(command -v claude || true)
AGENT=${AGENT:-"claude --model claude-haiku-5-5"}
LABEL=${LABEL:-$AGENT}
LANGS=${LANGS:-"en zh-Hans zh-Hant es ja ko ru"}
HC=${HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
# On the internal disk: herdr-core hangs on folders on an external volume on macOS.
w=$(mktemp -d /tmp/hfeval.XXXXXX)
out=${1:-$w/out}
mkdir -p "$out" "$w/bin" "$w/home" "$w/xdg/state" "$w/xdg/config" "$w/xdg/data" "$w/run"
chmod 700 "$w/run"
: > "$w/home/.zshrc"
(cd "$repo/companion" && go build -o "$w/bin/herdr-anywhere" ./cmd/herdr-anywhere)
B=$w/bin/herdr-anywhere
# hc for this run's core only, with a clean environment: the core hands it to every pane.
cat > "$w/bin/hcx" <<EOF
#!/bin/sh
exec env -i HOME=$w/home USER=\${USER:-eval} SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \\
  PATH="$w/bin:$(dirname "$claude"):$(dirname "$(command -v codex || echo /x/codex)"):/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \\
  HC_SESSION=hfeval-$$ DISABLE_AUTOUPDATER=1 XDG_STATE_HOME=$w/xdg/state XDG_CONFIG_HOME=$w/xdg/config \\
  XDG_DATA_HOME=$w/xdg/data XDG_RUNTIME_DIR=$w/run "$HC" "\$@"
EOF
chmod +x "$w/bin/hcx"
H=$w/bin/hcx
panes() { "$H" ls --json 2>/dev/null | python3 -c 'import json,sys; [print(t["address"]) for t in json.load(sys.stdin)["terminals"]]' 2>/dev/null || true; }
cleanup() {
  for a in $(panes); do "$H" kill "$a" >/dev/null 2>&1 || true; done
  "$H" server stop >/dev/null 2>&1 || true
}
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM
printf '%s\n' "$AGENT" > "$out/agent.txt"
python3 -I "$here/score.py" --run-inputs > "$out/run-inputs.txt"  # the rules and code this run works with
printf '%s\n' "$LABEL" > "$out/label.txt"
# The filing pane's command. Each agent finds its login its own way; it gets no more of your home folder than that.
agent_cmd() {
  case $AGENT in
    # Claude Code keeps its login under your home folder, and asks before any command its settings don't allow.
    claude\ *|claude) echo "env HOME=$real_home DISABLE_AUTOUPDATER=1 $claude --setting-sources project ${AGENT#claude}" ;;
    # Pi runs commands without asking: its home is this run's, and only its own folder (login, models) is yours.
    pi\ *|pi) echo "env PI_CODING_AGENT_DIR=$real_home/.pi/agent PI_CODING_AGENT_SESSION_DIR=$1/sessions $(command -v pi) ${AGENT#pi}" ;;
    # Codex asks before commands unless told not to; here it may write only in the filing folder and the progress file's.
    codex\ *|codex) echo "env CODEX_HOME=$real_home/.codex $(command -v codex) --no-daemon -c check_for_update_on_startup=false -a never -s workspace-write --add-dir $1 ${AGENT#codex}" ;;
    *) echo "$AGENT" ;;
  esac
}

for lang in $LANGS; do
  S=$w/state-$lang
  mkdir -p "$S" "$out/$lang"
  export HERDR_ANYWHERE_STATE=$S
  python3 -I "$here/seed.py" "$here/cases.json" "$lang" "$S/track.json"
  "$B" progress organizer --cmd "$(agent_cmd "$S")" >/dev/null
  "$B" progress lang "$lang"
  "$B" progress on
  start=$(date +%s)
  while :; do
    "$B" progress once --hc "$H" >> "$out/$lang/once.log" 2>&1
    left=$("$B" track queue --json --hc "$H" --herdr - | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["items"]))')
    st=$("$H" ls --json | python3 -c 'import json,sys; t=[t for t in json.load(sys.stdin)["terminals"] if t.get("name")=="herdr-organizer"]; print((t[0].get("status") or "none") if t else "-")')
    echo "$lang $(( $(date +%s) - start ))s queue=$left organizer=$st"
    # A new folder: Claude Code asks whether to trust it, with "No, exit" first, so the answer is down, then enter.
    if [ "$st" = none ] && "$H" read herdr-organizer 2>/dev/null | grep -q "Yes, I trust this folder"; then
      "$H" keys herdr-organizer down enter >/dev/null; sleep 5; continue
    fi
    [ "$left" = 0 ] && [ "$st" != working ] && break
    [ $(( $(date +%s) - start )) -gt 900 ] && { echo "$lang: gave up after 15 minutes"; break; }
    sleep 10
  done
  # Then the user tells the filing pane itself that a task is finished (R12), and it puts the page right.
  "$H" send herdr-organizer "$(python3 -I -c 'import json,sys; print(next(r for r in json.load(open(sys.argv[1]))["runs"] if r["lang"] == sys.argv[2])["correction"]["text"])' "$here/cases.json" "$lang")" >/dev/null
  sent=$(date +%s)
  while :; do
    sleep 10
    st=$("$H" ls --json | python3 -c 'import json,sys; t=[t for t in json.load(sys.stdin)["terminals"] if t.get("name")=="herdr-organizer"]; print((t[0].get("status") or "none") if t else "-")')
    echo "$lang correction $(( $(date +%s) - sent ))s organizer=$st"
    [ "$st" != working ] && [ $(( $(date +%s) - sent )) -ge 20 ] && break
    [ $(( $(date +%s) - sent )) -gt 300 ] && { echo "$lang: the correction took over 5 minutes"; break; }
  done
  echo $(( $(date +%s) - start )) > "$out/$lang/seconds"
  cp "$S/track.json" "$out/$lang/track.json"
  "$H" read herdr-organizer > "$out/$lang/screen.txt" 2>/dev/null || true
  # The agent's record of the pane's session: every command it ran, for the rule that checks them.
  find "$S/sessions" -name '*.jsonl' -exec cp {} "$out/$lang/" \; 2>/dev/null || true
  # Codex keeps them in your Codex folder: the ones from this run are moved here.
  for f in $(grep -rl "$(basename "$w")/state-$lang/" "$real_home/.codex/sessions" 2>/dev/null); do mv "$f" "$out/$lang/"; done
  # It files them under the folder's real path (/tmp is /private/tmp on macOS); the copy is kept, the original removed.
  key=$(cd "$S/organizer" 2>/dev/null && pwd -P | sed 's/[^A-Za-z0-9]/-/g')
  if [ -n "$key" ] && [ -d "$real_home/.claude/projects/$key" ]; then
    cp "$real_home/.claude/projects/$key"/*.jsonl "$out/$lang/" 2>/dev/null || true
    rm -rf "$real_home/.claude/projects/$key"
  fi
  for a in $(panes); do "$H" kill "$a" >/dev/null 2>&1 || true; done
done
python3 -I "$here/score.py" "$here/cases.json" "$out"
