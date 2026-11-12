# Sourced by the e2e suites (sh): start and stop the suite's own Companion (herdr-anywhere serve).
# A Companion started in the background through a shell function (clean "$bin" serve … &) is not $!: $! is the
# subshell that runs the function, and kill $! left the Companion running, for days on a Mac, and once on its port
# while the suite asked a new one it thought it had started. So the Companion is found by its --state dir.
# Usage: . e2e/companion.sh
#        companion_start LOG COMMAND...    COMMAND runs serve with --state (and --listen), directly or through env
#            or a wrapper function. It is started in the background, its output added to LOG. Returns once LOG has a
#            new "listening" line (on Tailcat with --tailcat); fails, saying why, when a Companion with that state dir
#            already runs, something already answers on its port, or it ends or is not listening within 30 s.
#            Sets companion_pid (the Companion itself), companion_job (the background job, which a script can wait
#            on) and companion_state.
#        companion_stop [STATE]            TERM to every serve with that --state (the last started's), then up to
#            HM_COMPANION_WAIT tenths of a second (100) for them to end and the port to stop answering; fails, saying
#            so, when they did not (and kills them).
#        sh e2e/companion.sh --self-test COMPANION
# The state dir is matched as one argument: no spaces in it.
companion_arg() { # NAME ARGS…: the argument after NAME
  co_n=$1; shift
  while [ $# -gt 1 ]; do [ "$1" = "$co_n" ] && { echo "$2"; return; }; shift; done
}
companion_pids() { # STATE: the serve processes with this --state
  ps -eo pid=,args= | awk -v s="$1" '{ sv = 0; for (i = 2; i < NF; i++) {
    if ($i == "serve") sv = 1; if (sv && $i == "--state" && $(i + 1) == s) { print $1; break } } }'
}
companion_answers() { [ -n "$1" ] && curl -s -o /dev/null --max-time 2 "http://$1/api/info"; }
companion_start() {
  companion_log=$1; shift
  companion_state=$(companion_arg --state "$@"); companion_listen=$(companion_arg --listen "$@"); companion_pid=
  [ -n "$companion_state" ] || { echo "FAIL: companion_start: no --state in: $*"; return 1; }
  co_old=$(companion_pids "$companion_state")
  [ -z "$co_old" ] || { echo "FAIL: companion_start: a Companion with --state $companion_state already runs (pid $co_old)"; return 1; }
  ! companion_answers "$companion_listen" || { echo "FAIL: companion_start: something already answers on $companion_listen"; return 1; }
  case " $* " in *" --tailcat "*) co_want="listening on Tailcat" ;; *) co_want="listening on http" ;; esac
  co_before=$(grep -c "^$co_want" "$companion_log" 2>/dev/null || true)
  "$@" >> "$companion_log" 2>&1 &
  co_bg=$! co_i=0; companion_job=$co_bg
  until [ "$(grep -c "^$co_want" "$companion_log" 2>/dev/null || true)" -gt "${co_before:-0}" ]; do
    co_i=$((co_i + 1))
    if [ $co_i -gt 300 ] || { ! kill -0 "$co_bg" 2>/dev/null && [ -z "$(companion_pids "$companion_state")" ]; }; then
      echo "FAIL: companion_start: no \"$co_want\" from the Companion with --state $companion_state; its log ends:"
      tail -5 "$companion_log" | sed 's/^/  /'
      companion_stop "$companion_state" >/dev/null || true
      return 1
    fi
    sleep 0.1
  done
  companion_pid=$(companion_pids "$companion_state")
}
companion_stop() {
  co_state=${1:-${companion_state:-}}
  [ -n "$co_state" ] || return 0
  co_pids=$(companion_pids "$co_state")
  [ -z "$co_pids" ] || kill $co_pids 2>/dev/null
  co_i=0
  while [ -n "$(companion_pids "$co_state")" ] || { [ "$co_state" = "${companion_state:-}" ] && companion_answers "${companion_listen:-}"; }; do
    co_i=$((co_i + 1))
    if [ $co_i -gt "${HM_COMPANION_WAIT:-100}" ]; then
      co_pids=$(companion_pids "$co_state")
      echo "FAIL: companion_stop: the Companion with --state $co_state did not end in $((co_i / 10)) s (pid ${co_pids:-none}; ${companion_listen:-no port} $(companion_answers "${companion_listen:-}" && echo answers || echo is free)); it is killed"
      [ -z "$co_pids" ] || kill -9 $co_pids 2>/dev/null
      return 1
    fi
    sleep 0.1
  done
}

if [ "${1:-}" = --self-test ]; then
  bin=${2:?the Companion to test with}
  here=$(cd "$(dirname "$0")" && pwd)
  t=$(mktemp -d) || exit 1
  trap 'ps -eo pid=,args= | grep -F -- "$t/" | grep -F " serve " | grep -v grep | awk "{print \$1}" | xargs kill -9 2>/dev/null; rm -rf "${t:?}"' EXIT
  trap 'exit 1' INT TERM
  # herdr-core's stand-in (companion/internal/testfake/fakehc): serve asks it its version, and nothing else here.
  go -C "$here/../../companion" build -o "$t/hc" ./internal/testfake/fakehc || exit 1
  printf '{"version":"hc 0.1.19"}\n' > "$t/hc.json"
  free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }
  # Whether a serve with this state dir runs, read from the process list as a person would check after a run.
  left() { ps -eo pid=,args= | grep -F -- "--state $1" | grep -F " serve " | grep -v grep | awk '{print $1}'; }
  answers() { curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$1/api/info"; }
  fail=0
  # A wrapper function, as the chat suites' clean: env -i with a home of the suite's own.
  clean() { env -i PATH=/usr/bin:/bin:/usr/local/bin HOME="$t" "$@"; }
  for how in clean direct; do
    port=$(free_port); state="$t/state-$how"
    if [ $how = clean ]; then set -- clean "$bin"; else set -- "$bin"; fi
    companion_start "$t/serve-$how.log" "$@" serve --listen "127.0.0.1:$port" --hc "$t/hc" --state "$state" ||
      { echo "FAIL: companion self-test: started $how: $(tail -2 "$t/serve-$how.log")"; fail=1; continue; }
    answers "$port" || { echo "FAIL: companion self-test: started $how, nothing answers on $port"; fail=1; }
    companion_stop || { echo "FAIL: companion self-test: stopped $how, companion_stop failed"; fail=1; }
    [ -z "$(left "$state")" ] || { echo "FAIL: companion self-test: started $how and stopped, a serve with --state $state still runs (pid $(left "$state"))"; fail=1; }
    answers "$port" && { echo "FAIL: companion self-test: started $how and stopped, port $port still answers"; fail=1; }
  done
  # Another started in its place, on the same port and state dir: a new process, and its own listening line.
  port=$(free_port); state="$t/state-again"
  companion_start "$t/again.log" clean "$bin" serve --listen "127.0.0.1:$port" --hc "$t/hc" --state "$state" >/dev/null; first=$companion_pid
  companion_stop
  companion_start "$t/again.log" clean "$bin" serve --listen "127.0.0.1:$port" --hc "$t/hc" --state "$state" ||
    { echo "FAIL: companion self-test: a second start on the same port failed"; fail=1; }
  [ -n "$companion_pid" ] && [ "$companion_pid" != "$first" ] && [ "$(grep -c '^listening on http' "$t/again.log")" = 2 ] ||
    { echo "FAIL: companion self-test: the second start is pid '$companion_pid' (the first was $first), with $(grep -c '^listening on http' "$t/again.log") listening lines"; fail=1; }
  companion_stop
  # An old Companion still on the port (one a suite left): the new start fails at once and says so, so the suite
  # does not go on talking to the old one.
  port=$(free_port)
  "$bin" serve --listen "127.0.0.1:$port" --hc "$t/hc" --state "$t/state-old" > "$t/old.log" 2>&1 &
  i=0; until answers "$port" || [ $i -gt 50 ]; do i=$((i + 1)); sleep 0.1; done
  companion_start "$t/new.log" "$bin" serve --listen "127.0.0.1:$port" --hc "$t/hc" --state "$t/state-new" > "$t/said" &&
    { echo "FAIL: companion self-test: started with an old Companion on the port"; fail=1; }
  grep -q "something already answers on 127.0.0.1:$port" "$t/said" || { echo "FAIL: companion self-test: with an old Companion on the port it said: $(cat "$t/said")"; fail=1; }
  # One that does not end on TERM (stopped, here): companion_stop fails, says so, and kills it.
  companion_stop "$t/state-old" || { echo "FAIL: companion self-test: the old one did not stop"; fail=1; }
  port=$(free_port); state="$t/state-stuck"
  companion_start "$t/stuck.log" "$bin" serve --listen "127.0.0.1:$port" --hc "$t/hc" --state "$state" >/dev/null
  kill -STOP "$companion_pid"
  HM_COMPANION_WAIT=10 companion_stop > "$t/said" && { echo "FAIL: companion self-test: stopping one that did not end succeeded"; fail=1; }
  grep -q "did not end in 1 s" "$t/said" || { echo "FAIL: companion self-test: stopping one that did not end said: $(cat "$t/said")"; fail=1; }
  sleep 0.5; [ -z "$(left "$state")" ] || { echo "FAIL: companion self-test: one that did not end on TERM is left running"; fail=1; }
  # No --state: no way to find it again, so it is not started.
  companion_start "$t/x.log" "$bin" serve --listen "127.0.0.1:$(free_port)" --hc "$t/hc" > "$t/said" &&
    { echo "FAIL: companion self-test: started without --state"; fail=1; }
  [ "$fail" = 0 ] && echo "PASS: companion self-test" || exit 1
fi
