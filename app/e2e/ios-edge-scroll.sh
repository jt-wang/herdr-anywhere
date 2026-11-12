#!/bin/sh
# Scrolling from the screen's left edge, on the iOS simulator (XCUITest), in the four agents the home lists most:
# the installed Claude Code, Codex, OpenCode and Pi, each in a throwaway home against a fake model (the companion's
# fakeapi and fakeresponses), in an isolated herdr-core session, with this branch's Companion. Each answers ROWS (150
# numbered rows) once its space is open on the phone. e2e/ios-uitest's EdgeScroll test then drags slowly up and down
# from 10 pt and 20 pt off the screen's left edge, where iOS's swipe back gets the finger first (ui/terminal-page.ts edgeBack),
# and from the terminal's middle, with a screenshot before and after each drag; e2e/rows.py reads the rows' numbers
# and says how many rows the content moved. Passes when, in every agent, a drag from the edge moves the content as
# far as one from the middle (within 1 row), the middle's drag moved it at least 2 rows, and no drag went back to home.
# A first pair of drags in each space is measured and printed, not compared (see EdgeScroll).
# Then sideways swipes from 10 pt and 20 pt (EdgeScroll: one let go early, two that go back): each passes when the
# agent's screen as hc reads it is the same after as before (nothing was typed, clicked or scrolled in the program), no
# "Copied" came up and the tab stayed; for the one let go early, also the content did not move (rows.py), no
# selection showed (e2e/selection.py, its colour learnt from a hold on the middle) and no keyboard came up. The hold
# and a tap on the middle are controls: the hold must show a selection and Copied, the tap the keyboard.
# Usage: SIM_UDID=<udid> [AGENTS=cc,cx,oc,pi] e2e/ios-edge-scroll.sh [out folder]
# SIM_UDID is required, so the run never lands on another simulator. Needs go, nsh, hc, xcodegen, Xcode, Pillow,
# tesseract, and claude, codex, opencode and pi installed (one missing is skipped and said). Fake data only.
set -eu
: "${SIM_UDID:?set SIM_UDID to the simulator to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
work=$(cd "$(mktemp -d)" && pwd -P) # Claude Code keys its trust by the folder's real path (/private/var/…)
out=${1:-$work/out}; mkdir -p "$out"
run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
h=$work/home
mkdir -p "$h/src/acme-api" "$h/.pi/agent" "$work/claude" "$work/codex" "$work/config/opencode" "$work/data" "$work/state" "$work/cache" "$work/bin"
: > "$h/.zshrc"
port=7691; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
aport=7840; while nc -z 127.0.0.1 "$aport" 2>/dev/null; do aport=$((aport + 1)); done
rport=$((aport + 1)); while nc -z 127.0.0.1 "$rport" 2>/dev/null; do rport=$((rport + 1)); done

# Which agents: a short name, its space, its program; one not installed is skipped.
want=${AGENTS:-cc,cx,oc,pi}
claude=$(command -v claude || echo "$HOME/.local/bin/claude"); codex=$(command -v codex || true)
opencode=$(command -v opencode || echo "$HOME/.opencode/bin/opencode"); pi=$(command -v pi || echo "$HOME/.local/bin/pi")
agents=""
for a in $(echo "$want" | tr ',' ' '); do
  case $a in cc) b=$claude;; cx) b=$codex;; oc) b=$opencode;; pi) b=$pi;; *) echo "FAIL: unknown agent $a"; exit 1;; esac
  if [ -n "$b" ] && [ -x "$b" ]; then agents="$agents $a"; else echo "SKIP: $a is not installed"; fi
done
[ -n "$agents" ] || { echo "FAIL: none of $want is installed"; exit 1; }
name() { case $1 in cc) echo "Claude Code";; cx) echo Codex;; oc) echo OpenCode;; pi) echo Pi;; esac; }

# Each agent on its fake model, with no update check, no login, the folder trusted and nothing to answer first.
# Claude Code gets ANTHROPIC_AUTH_TOKEN (below), not an API key: with a key it first asks whether to use it.
cat > "$work/claude/.claude.json" <<CFG
{"hasCompletedOnboarding": true, "theme": "dark", "autoUpdates": false,
 "projects": {"$h/src/acme-api": {"hasTrustDialogAccepted": true, "hasCompletedProjectOnboarding": true}}}
CFG
cat > "$work/codex/config.toml" <<CFG
check_for_update_on_startup = false
model = "gpt-5.5"
model_provider = "fake"
sandbox_mode = "workspace-write"
approval_policy = "never"
[model_providers.fake]
name = "Fake"
base_url = "http://127.0.0.1:$rport/v1"
wire_api = "responses"
env_key = "FAKE_API_KEY"
[projects."$h/src/acme-api"]
trust_level = "trusted"
[tui]
screen_reader_detection_done = true
[notice.model_migrations]
"gpt-5.5" = "gpt-6-sol"
CFG
cat > "$work/config/opencode/opencode.json" <<CFG
{"autoupdate": false, "share": "disabled", "enabled_providers": ["fake"], "disabled_providers": ["opencode"],
 "provider": {"fake": {"npm": "@ai-sdk/openai", "name": "Fake", "options": {"baseURL": "http://127.0.0.1:$rport/v1", "apiKey": "fake"},
   "models": {"gpt-5.5": {"name": "GPT-5.5", "tool_call": true}}}},
 "model": "fake/gpt-5.5"}
CFG
cat > "$h/.pi/agent/models.json" <<CFG
{"providers": {"fake": {"baseUrl": "http://127.0.0.1:$rport/v1", "api": "openai-responses", "apiKey": "fake",
  "models": [{"id": "gpt-5.5", "name": "Fake GPT"}]}}}
CFG
# hc runs the command line as a shell would: a path with spaces goes through a link named as the program.
[ -n "$opencode" ] && ln -s "$opencode" "$work/bin/opencode"
(cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere && go build -o "$work/fakeapi" ./internal/chat/fakeapi/serve \
  && go build -o "$work/fakeresp" ./internal/chat/fakeresponses/serve)
clean() {
  env -i PATH="${codex:+$(dirname "$codex"):}/usr/bin:/bin:/usr/sbin:/sbin" HOME="$h" SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 \
    HC_SESSION="hmes-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" \
    XDG_RUNTIME_DIR="$run" XDG_CACHE_HOME="$work/cache" GIT_CONFIG_GLOBAL="$h/.gitconfig" \
    CLAUDE_CONFIG_DIR="$work/claude" ANTHROPIC_AUTH_TOKEN=fake-token ANTHROPIC_BASE_URL="http://127.0.0.1:$aport" DISABLE_AUTOUPDATER=1 \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 CODEX_HOME="$work/codex" FAKE_API_KEY=fake \
    PI_CODING_AGENT_DIR="$h/.pi/agent" PI_SKIP_VERSION_CHECK=1 PI_OFFLINE=1 \
    OPENCODE_DISABLE_AUTOUPDATE=1 OPENCODE_DISABLE_MODELS_FETCH=1 OPENCODE_DISABLE_SHARE=1 OPENCODE_DISABLE_LSP_DOWNLOAD=1 \
    OPENCODE_DISABLE_CLAUDE_CODE=1 "$@"
}
lhc() { clean "$hc" "$@"; }
placeholder=$(cat "$app/e2e/pair.ts")
serve_pid=; fa=; fr=; test_pid=; answer_pid=
cleanup() {
  trap - EXIT; trap '' INT TERM HUP
  set +e
  [ -n "$test_pid" ] && { pkill -P "$test_pid" 2>/dev/null; kill "$test_pid" 2>/dev/null; }
  [ -n "$answer_pid" ] && kill "$answer_pid" 2>/dev/null
  [ -n "$serve_pid" ] && { pkill -P "$serve_pid" 2>/dev/null; kill "$serve_pid" 2>/dev/null; }
  pkill -f -- "--state $state" 2>/dev/null
  [ -n "$fa" ] && kill "$fa" 2>/dev/null; [ -n "$fr" ] && kill "$fr" 2>/dev/null
  printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"
  lhc server stop >/dev/null 2>&1
  # OpenCode starts a background server of its own, and Codex's TUI leaves an app-server under CODEX_HOME.
  for _ in 1 2 3; do sleep 1; pkill -f -- "$work/bin/opencode" 2>/dev/null; done
  pkill -f -- "$work/codex" 2>/dev/null
  xcrun simctl shutdown "$SIM_UDID" 2>/dev/null
  chmod -R u+w "$work" 2>/dev/null; rm -rf "${work:?}" "${run:?}" "${state:?}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

"$work/fakeapi" "127.0.0.1:$aport" > "$work/fakeapi.log" 2>&1 & fa=$!
"$work/fakeresp" "127.0.0.1:$rport" > "$work/fakeresp.log" 2>&1 & fr=$!
for a in $agents; do
  case $a in cc) set -- "$claude";; cx) set -- "$codex";; oc) set -- "$work/bin/opencode";; pi) set -- "$pi" --model fake/gpt-5.5;; esac
  lhc run --bg --keep --cwd "$h/src/acme-api" --name "$a" --label herdr.space="sp$a" --label herdr.tab="tb$a" "$@" >/dev/null
done
sleep 12
# Nothing is typed into an agent unless its screen shows the fake model and no question.
for a in $agents; do
  screen=$(lhc read "$a" 2>/dev/null)
  if printf %s "$screen" | grep -qiE "update now|update available|trust (this|the) folder|sign in|log ?in|press enter|use existing model"; then
    echo "STOP: a question on $(name "$a")'s screen"; printf '%s\n' "$screen" | grep -v '^ *$' | tail -6; exit 1
  fi
  case $a in cc) m="API Usage Billing";; cx) m="gpt-5.5";; oc) m="GPT-5.5";; pi) m="gpt-5.5";; esac
  printf %s "$screen" | grep -qi "$m" || { echo "STOP: $(name "$a") is not on the fake model"; printf '%s\n' "$screen" | grep -v '^ *$' | tail -6; exit 1; }
done

clean "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" > "$work/serve.log" 2>&1 &
serve_pid=$!
i=0
until curl -fsS "http://127.0.0.1:$port/api/info" >/dev/null 2>&1; do
  i=$((i + 1)); [ $i -gt 100 ] && { cat "$work/serve.log" >&2; exit 1; }
  sleep 0.1
done
link=$(clean "$work/herdr-anywhere" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$link" ] || { echo "FAIL: pair printed no link"; exit 1; }
printf "export const PAIR: string = '%s'\n" "$link" > "$app/e2e/pair.ts"
"$app/ios/build.sh" ios-e2e.tsx simulator >/dev/null 2>&1 || { echo "FAIL: ios/build.sh"; exit 1; }
printf "%s\n" "$placeholder" > "$app/e2e/pair.ts"

xcrun simctl boot "$SIM_UDID" 2>/dev/null || true
xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null
xcrun simctl uninstall "$SIM_UDID" "$id" 2>/dev/null || true
xcrun simctl install "$SIM_UDID" "$app/ios/build/Herdr.app"
xcrun simctl launch "$SIM_UDID" "$id" >/dev/null
i=0
until grep -q "device .* connected" "$work/serve.log"; do
  i=$((i + 1)); [ $i -gt 60 ] && { echo "FAIL: the app did not connect within 60 s"; tail -5 "$work/serve.log" | sed -E 's/token=[^ &]+/token=…/'; exit 1; }
  sleep 1
done

cp -R "$app/e2e/ios-uitest" "$work/uitest"
(cd "$work/uitest" && xcodegen generate --quiet)
spaces=$(for a in $agents; do printf 'sp%s,' "$a"; done); spaces=${spaces%,}
: > "$out/drags.txt"
TEST_RUNNER_OUT="$out" TEST_RUNNER_SPACES="$spaces" perl -e 'alarm 1200; exec @ARGV' xcodebuild test -quiet \
  -project "$work/uitest/KeyboardCheck.xcodeproj" -scheme UITests -only-testing:UITests/EdgeScroll -destination "id=$SIM_UDID" \
  -derivedDataPath "$work/uitest/derived" -clonedSourcePackagesDirPath "$work/uitest/spm" -packageCachePath "$work/uitest/spm-cache" \
  -resultBundlePath "$out/test.xcresult" > "$work/test.log" 2>&1 &
test_pid=$!
# The test asks for an agent's screen around each sideways swipe (OUT/ask-<agent>-<tag>): hc reads it into hc-<tag>.txt.
(while kill -0 "$test_pid" 2>/dev/null; do
  for f in "$out"/ask-*; do
    [ -e "$f" ] || continue
    tag=${f##*/ask-}
    lhc read "${tag%%-*}" > "$out/.hc-$tag" 2>&1; mv "$out/.hc-$tag" "$out/hc-$tag.txt"; rm -f "$f"
  done
  sleep 0.2
done) &
answer_pid=$!
# The answer goes in once the phone shows the space, so the phone has every row of it, history included.
for a in $agents; do
  i=0; until grep -q "^ready sp$a\$" "$out/drags.txt" || ! kill -0 "$test_pid" 2>/dev/null; do i=$((i + 1)); [ $i -gt 300 ] && break; sleep 1; done
  grep -q "^ready sp$a\$" "$out/drags.txt" || break
  lhc send "$a" "ROWS" >/dev/null
  i=0; until lhc read "$a" 2>/dev/null | grep -q "acme-api line 150" || [ $i -gt 30 ]; do i=$((i + 1)); sleep 1; done
  lhc read "$a" | grep -q "acme-api line 150" || echo "note: $(name "$a") does not show row 150 after 30 s"
  : > "$out/sent-sp$a"
done
rc=0; wait "$test_pid" || rc=$?; test_pid=
if [ "$rc" != 0 ]; then
  echo "FAIL: the EdgeScroll test (xcodebuild test exit $rc)"; grep -E "error:|XCTAssert|failed|Failed" "$work/test.log" | head -12
  # -quiet leaves out why a test failed; the result bundle has it.
  xcrun xcresulttool get test-results tests --path "$out/test.xcresult" 2>/dev/null | grep -E '"(failureText|message)"' | head -6
  cp "$work/test.log" "$out/xcodebuild.log"; exit 1
fi

# How far each drag moved the content, in rows (e2e/rows.py), and how far the finger went, in rows of the same height.
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
grep "^drag " "$out/drags.txt" | while read -r _ sp at dir _ x y w hh _ _ _ travel back; do
  set -- $(python3 -I "$app/e2e/rows.py" "$out/$sp-$at-$dir-0.png" "$out/$sp-$at-$dir-1.png" "$x" "$y" "$w" "$hh")
  finger=$(python3 -c "import sys; p = float(sys.argv[2]); print(f'{float(sys.argv[1]) / p:.1f}' if p else '?')" "$travel" "$4")
  echo "${sp#sp} $at $dir $2 $finger $back"
done > "$out/moves.txt"
echo "agent   from    way      rows moved  finger rows  went back"
awk '{ printf "%-7s %-7s %-8s %10s  %11s  %s\n", $1, $2, $3, $4, $5, $6 }' "$out/moves.txt"
# OpenCode 1.19.0 ignores wheel reports on its first two columns, where a finger 10 pt off the edge lands (from column 3
# it scrolls; seen with a relay that wrote the reports straight to its terminal): its drag from 10 pt is printed, not
# compared. From 20 pt, still in the strip, every agent is compared.
for a in $agents; do
  for at in edge edge20; do
    for dir in back forward; do
      ev=$(awk -v a="$a" -v t="$at" -v d="$dir" '$1 == a && $2 == t && $3 == d { print $4 }' "$out/moves.txt")
      mv=$(awk -v a="$a" -v d="$dir" '$1 == a && $2 == "middle" && $3 == d { print $4 }' "$out/moves.txt")
      what="$(name "$a"), $dir, from $([ "$at" = edge ] && echo 10 || echo 20) pt"
      if grep "^$a " "$out/moves.txt" | grep -q "back=1"; then say "FAIL: $(name "$a"): a drag went back to home"; continue 3; fi
      if [ "$a" = oc ] && [ "$at" = edge ]; then echo "note: $what: $ev rows (middle $mv), not compared"; continue; fi
      if [ -z "$ev" ] || [ -z "$mv" ] || [ "$ev" = none ] || [ "$mv" = none ]; then say "FAIL: $what: the rows' numbers could not be read (edge ${ev:-?}, middle ${mv:-?})"; continue; fi
      if python3 -c "import sys; e, m = float(sys.argv[1]), float(sys.argv[2]); sys.exit(not (abs(m) >= 2 and abs(e - m) <= 1))" "$ev" "$mv"; then
        say "ok: $what: $ev rows, from the middle $mv"
      else say "FAIL: $what: $ev rows, from the middle $mv"; fi
    done
  done
done

# Sideways from the strip. The controls first: without them a check that sees nothing proves nothing.
sel=$app/e2e/selection.py
for a in $agents; do
  set -- $(awk -v s="sp$a" '$1 == "control" && $2 == s' "$out/drags.txt")
  [ $# -ge 9 ] || { say "FAIL: $(name "$a"): no control line"; continue; }
  bx="$4 $5 $6 $7"; cp=$8; kbd=$9 # control sp<a> box x y w h copied=<0|1> keyboard=<0|1>
  set -- $(python3 -I "$sel" learn "$out/sp$a-hold-0.png" "$out/sp$a-hold-1.png" $bx)
  if [ "$2" = none ]; then color=; grew=0; else color="$2 $3 $4"; grew=$6; fi
  if [ "$grew" -lt 1000 ]; then say "FAIL: $(name "$a"): control: a hold showed no selection ($*)"; color=; fi
  [ "$cp" = copied=1 ] || say "FAIL: $(name "$a"): control: a hold did not say Copied"
  [ "$kbd" = keyboard=1 ] || say "FAIL: $(name "$a"): control: a tap brought no keyboard"
  awk -v s="sp$a" '$1 == "side" && $2 == s' "$out/drags.txt" > "$work/side"
  [ -s "$work/side" ] || { say "FAIL: $(name "$a"): no sideways swipes"; continue; }
  while read -r _ sp x0 kind _ x y w hh back kb cpd tab; do
    what="$(name "$a"), sideways $kind from $x0 pt"; tag="side$x0-$kind"; bad=
    h0=$out/hc-$a-$tag-0.txt; h1=$out/hc-$a-$tag-1.txt
    if [ ! -s "$h0" ] || [ ! -s "$h1" ]; then bad="$bad, hc read nothing"
    elif ! sed 's/[[:space:]]*$//' "$h0" > "$work/h0" || ! sed 's/[[:space:]]*$//' "$h1" > "$work/h1" || ! cmp -s "$work/h0" "$work/h1"; then
      bad="$bad, the agent's screen changed: $(diff "$work/h0" "$work/h1" | grep '^[<>]' | head -2 | tr '\n' ' ')"
    fi
    [ "$cpd" = copied=1 ] && bad="$bad, Copied came up"
    [ "$tab" = tab=same ] || bad="$bad, the tab changed"
    if [ "$kind" = part ]; then
      [ "$back" = back=0 ] || bad="$bad, it went back"
      [ "$kb" = kb=01 ] && bad="$bad, the keyboard came up"
      set -- $(python3 -I "$app/e2e/rows.py" "$out/$sp-$tag-0.png" "$out/$sp-$tag-1.png" "$x" "$y" "$w" "$hh")
      [ "$2" = 0 ] || bad="$bad, the content moved $2 rows"
      if [ -n "$color" ]; then
        n0=$(python3 -I "$sel" count "$out/$sp-$tag-0.png" "$x" "$y" "$w" "$hh" $color)
        n1=$(python3 -I "$sel" count "$out/$sp-$tag-1.png" "$x" "$y" "$w" "$hh" $color)
        [ $((n1 - n0)) -gt $((grew / 3)) ] && bad="$bad, a selection showed ($n0 -> $n1 px of its colour; a hold: +$grew)"
      fi
    else
      [ "$back" = back=1 ] || bad="$bad, it did not go back"
    fi
    if [ -n "$bad" ]; then say "FAIL: $what: ${bad#, }"; else say "ok: $what: nothing in the terminal"; fi
  done < "$work/side"
done
exit $failed
