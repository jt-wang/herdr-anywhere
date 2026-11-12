#!/bin/sh
# A shell script's trap on INT, TERM or HUP must end the script: a handler that only cleans up (trap cleanup EXIT INT
# TERM) returns, and the script goes on from where the signal came, in sh, bash, dash and zsh alike. A cancelled or
# timed-out CI job sends such a signal; a watchdog that stopped a script and let its lock go saw it start a simulator
# again. So each such trap says exit itself, with the signal's status (130 INT, 143 TERM, 129 HUP), and the cleanup
# runs once, from the EXIT trap. A function that happens to exit doesn't count: the check reads only the trap line.
# Usage: app/e2e/ci/traps.sh [FILE…]    each tracked *.sh in the repository when none are given
#        app/e2e/ci/traps.sh --self-test
# HM_TRAPS_EXCEPT: files, one a line, whose traps on INT, TERM or HUP pass as they are; a listed file whose traps all
# end the script fails, so its exception is taken off. The one here: the filing agent's evaluation (run.sh) is in the
# hash its last report was made on (score.py --inputs), so any change to it asks for a new run, with a real agent and
# its login; a person starts it by hand, never CI or a watchdog. Its trap is put right with that next run.
: "${HM_TRAPS_EXCEPT=companion/eval/filing/run.sh}"
export HM_TRAPS_EXCEPT
check() {
  python3 -I -c '
import re, shlex, sys
SIGS = {"INT", "TERM", "HUP", "2", "15", "1"}
def words(text):
    lex = shlex.shlex(text, posix=True, punctuation_chars=";&|()")
    lex.whitespace_split = True; lex.commenters = "#"
    return list(lex)
def bad_traps(ws):
    out, cmd = [], []
    for w in ws + [";"]:
        if w and set(w) <= set(";&|()"):
            if len(cmd) >= 3 and cmd[0] == "trap":
                caught = sorted({x.upper().removeprefix("SIG") for x in cmd[2:]} & SIGS)
                if caught and cmd[1] not in ("", "-") and "exit" not in words(cmd[1]):
                    out.append(" ".join(caught))
            cmd = []
        else: cmd.append(w)
    return out
def heredoc(line):
    """The delimiter of a here-document this line starts, from a << outside quotes and comments; else None."""
    q, k = None, 0
    while k < len(line):
        c = line[k]
        if q:
            if c == "\\" and q == "\"": k += 2; continue
            if c == q: q = None
        elif c == "\\": k += 2; continue
        elif c in "\x27\"": q = c
        elif c == "#" and (k == 0 or line[k - 1] in " \t;&|("): return None
        elif line.startswith("<<", k) and not line.startswith("<<<", k):
            m = re.match(r"<<-?\s*(\x27([^\x27]+)\x27|\"([^\"]+)\"|\\?([A-Za-z_][A-Za-z0-9_]*))", line[k:])
            if m: return m.group(2) or m.group(3) or m.group(4)
        k += 1
    return None
import os
excepted = {x.strip() for x in os.environ.get("HM_TRAPS_EXCEPT", "").split("\n") if x.strip()}
bad = 0
for path in sys.argv[1:]:
    allowed = path in excepted; found = 0
    try: lines = open(path, encoding="utf-8", errors="replace").read().split("\n")
    except OSError: continue
    i, until = 0, None
    while i < len(lines):
        line = lines[i]; no = i + 1; i += 1
        # A here-document is text, not commands: its lines up to the delimiter are passed over.
        if until is not None:
            if line.strip() == until: until = None
            continue
        until = heredoc(line)
        if not re.search(r"(^|[\s;&|(])trap(\s|$)", line): continue
        # A trap line that does not parse alone (a quoted action over several lines, a backslash at the end) is read
        # with the lines after it until it does; one that never does fails, rather than being passed over.
        text, j = line, i
        while True:
            try: ws = words(text.replace("\\\n", " ")); break
            except ValueError:
                if j >= len(lines) or j - i > 30:
                    print(f"FAIL: {path}:{no}: a trap line the check cannot read: {line.strip()}"); bad = 1; ws = None; break
                text += "\n" + lines[j]; j += 1
        if ws is None: continue
        for names in bad_traps(ws):
            found += 1
            if allowed: print(f"note: {path}:{no}: trap on {names} without exit, allowed (HM_TRAPS_EXCEPT): {line.strip()}")
            else: print(f"FAIL: {path}:{no}: trap on {names} without exit: the script goes on after the handler: {line.strip()}"); bad = 1
    if allowed and not found:
        print(f"FAIL: {path}: on the list of exceptions (HM_TRAPS_EXCEPT), but each of its traps ends the script: the exception is no longer needed, take it off"); bad = 1
sys.exit(bad)
' "$@"
}

if [ "${1:-}" = --self-test ]; then
  t=$(mktemp -d); fail=0
  # What the check says of trap lines: these stop the script on a signal…
  cat > "$t/good.sh" <<'EOF'
trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM HUP
trap 'adb_exit $? cleanup' EXIT
trap 'cleanup; exit 143' TERM
trap 'echo "FAIL: stopped by a signal"; exit 1' INT TERM
trap - EXIT INT TERM HUP
trap '' PIPE
# trap cleanup INT TERM, in a comment
EOF
  # …and these don't.
  cat > "$t/bad.sh" <<'EOF'
trap cleanup EXIT INT TERM
trap cleanup INT TERM
trap 'cleanup' SIGTERM
trap "rm -f x" HUP
trap cleanup EXIT; trap 'echo stopped' INT
EOF
  # A trap written over two lines is read whole; one in a here-document is text, passed over, and the line after the
  # document is read again.
  printf '%s\n' "trap 'cleanup" "  exit 143' TERM" "cat > x <<'END'" "trap cleanup INT TERM" "END" "cat <<-END" "	trap cleanup TERM" "	END" >> "$t/good.sh"
  printf '%s\n' "cat > x <<END" "text" "END" "trap 'rm -f x" "  y' INT" >> "$t/bad.sh"
  # A << in quotes or a comment starts no here-document: the trap after it is still read.
  printf '%s\n' "echo \"a <<END b\" # and <<END" "trap cleanup TERM" >> "$t/bad.sh"
  printf '%s\n' "trap 'never closed INT TERM" > "$t/unread.sh"
  check "$t/unread.sh" | grep -q "cannot read" || { echo "FAIL: traps self-test: a trap line it cannot read was passed over"; fail=1; }
  # A file on the list of exceptions passes with its trap, and says so; once its trap ends the script, the exception
  # fails, so it is taken off the list and doesn't let a later bad trap there through.
  printf '%s\n' "trap cleanup EXIT INT TERM" > "$t/excepted.sh"
  HM_TRAPS_EXCEPT="$t/excepted.sh" check "$t/excepted.sh" > "$t/said-except" &&
    grep -q "allowed" "$t/said-except" || { echo "FAIL: traps self-test: a file on the list of exceptions: $(cat "$t/said-except")"; fail=1; }
  printf '%s\n' "trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM" > "$t/excepted.sh"
  HM_TRAPS_EXCEPT="$t/excepted.sh" check "$t/excepted.sh" > "$t/said-except" &&
    { echo "FAIL: traps self-test: an exception no longer needed passed"; fail=1; }
  grep -q "no longer needed" "$t/said-except" || { echo "FAIL: traps self-test: an exception no longer needed says: $(cat "$t/said-except")"; fail=1; }
  check "$t/good.sh" > "$t/said-good" || { echo "FAIL: traps self-test: traps that exit were called bad: $(cat "$t/said-good")"; fail=1; }
  check "$t/bad.sh" > "$t/said-bad" && { echo "FAIL: traps self-test: traps that don't exit passed"; fail=1; }
  [ "$(grep -c '^FAIL: ' "$t/said-bad")" = 7 ] || { echo "FAIL: traps self-test: of 7 traps that don't exit it found: $(cat "$t/said-bad")"; fail=1; }
  # What a TERM does, in each shell here: with a handler that only cleans up, the script goes on after it; with the
  # trap the check wants, it stops, and its EXIT cleanup runs once.
  for sh in sh bash dash zsh; do
    command -v "$sh" >/dev/null || continue
    for kind in old new; do
      rm -f "${t:?}/went-on" "${t:?}/cleaned"
      if [ "$kind" = old ]; then traps="trap cleanup EXIT INT TERM"; else traps="trap cleanup EXIT; trap 'exit 130' INT; trap 'exit 143' TERM"; fi
      printf '%s\n' "cleanup() { echo x >> '$t/cleaned'; }" "$traps" "kill -TERM \$\$" "sleep 0.2" "echo on > '$t/went-on'" > "$t/run.sh"
      "$sh" "$t/run.sh" 2>/dev/null; st=$?
      if [ "$kind" = old ]; then
        [ -e "$t/went-on" ] || echo "note: traps self-test: in $sh the old trap stopped the script anyway"
      else
        [ -e "$t/went-on" ] && { echo "FAIL: traps self-test: in $sh the script went on after TERM"; fail=1; }
        [ "$st" = 143 ] || { echo "FAIL: traps self-test: in $sh a TERM ended the script with $st, not 143"; fail=1; }
        [ "$(wc -l < "$t/cleaned" 2>/dev/null | tr -d ' ')" = 1 ] || { echo "FAIL: traps self-test: in $sh the cleanup ran $(wc -l < "$t/cleaned" 2>/dev/null | tr -d ' ') times"; fail=1; }
      fi
    done
  done
  rm -rf "${t:?}"
  [ "$fail" = 0 ] && echo "PASS: traps self-test" || exit 1
elif [ $# -gt 0 ]; then
  check "$@"
else
  root=$(cd "$(dirname "$0")/../../.." && pwd)
  cd "$root" || exit 1
  git ls-files '*.sh' | { set --; while IFS= read -r f; do set -- "$@" "$f"; done; check "$@"; } && echo "ok: every trap on INT, TERM or HUP ends the script"
fi
