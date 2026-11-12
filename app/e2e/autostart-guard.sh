# Sourced by every e2e suite (sh) that runs the Companion's pair, start or stop. Those can set serve to start at login:
# start does, pair does when no serve runs, and stop takes it off again. The entry they write goes under $HOME, but
# launchd's domain is the user's (gui/<uid>), whatever HOME is: a suite with a home of its own and the default name
# still unloaded the person's Companion (launchctl bootout) and loaded its own under the person's label. So a suite
# gives its Companion a name of its own (HERDR_ANYWHERE_AUTOSTART_NAME) as well as a home of its own, and checks at the
# end that the person's entry was not touched.
# Usage: . e2e/autostart-guard.sh
#        autostart_guard NAME   before the suite's first pair, start or stop: exports HERDR_ANYWHERE_AUTOSTART_NAME
#            (io.example.herdranywhere.test.NAME.<pid>), and notes the person's entry: its file's time and size (or
#            that there is none), how the service manager has it (loaded or not, its pid and file), which herdranywhere
#            labels are loaded, and the person's Companions (herdr-anywhere serve run from outside the temporary
#            folders).
#        autostart_guard_end    in the suite's cleanup: unloads and deletes the suite's own entry, then fails, saying
#            why, when any of that changed (other suites' test labels aside), or the suite's entry was written in the
#            account's real home (pair, start or stop ran with the real HOME). The suite passes the name on through
#            any env -i it runs the Companion under, and runs it with a HOME of its own.
#        sh e2e/autostart-guard.sh --self-test COMPANION
# The person's home is the account's (from the user database), not $HOME, which the suite may already have changed;
# HM_AUTOSTART_HOME puts another in its place, for the self-test.
autostart_os() { case $(uname -s) in Darwin) echo darwin ;; Linux) echo linux ;; *) echo other ;; esac; }
autostart_default() { case $(autostart_os) in darwin) echo io.jingtao.herdranywhere.companion ;; *) echo herdr-anywhere ;; esac; }
autostart_entry() { # HOME NAME: where the Companion keeps the entry NAME
  case $(autostart_os) in
    darwin) echo "$1/Library/LaunchAgents/$2.plist" ;;
    linux) echo "${ag_config:-$1/.config}/systemd/user/$2.service" ;;
  esac
}
autostart_stamp() { # FILE: its time and size, or "none"
  [ -e "$1" ] || { echo none; return; }
  if [ "$(autostart_os)" = darwin ]; then stat -f '%m %z' "$1"; else stat -c '%Y %s' "$1"; fi # GNU's -f is the file system
}
autostart_labels() { # the herdranywhere entries loaded now, other suites' test labels aside, one a line
  case $(autostart_os) in
    darwin) launchctl list 2>/dev/null | awk 'NR > 1 { print $3 }' ;;
    linux) systemctl --user list-units --all --no-legend --plain 2>/dev/null | awk '{ sub(/\.service$/, "", $1); print $1 }' ;;
  esac | grep -E 'herdranywhere|^herdr-anywhere$' | grep -v '^io\.example\.herdranywhere\.test\.' | sort -u || true
}
autostart_state() { # how the service manager has the person's entry: loaded or not, its program's pid, its file
  case $(autostart_os) in
    darwin) launchctl print "gui/$(id -u)/$ag_default" 2>/dev/null |
      awk -F' = ' '/^\tpath = / { p = $2 } /^\tpid = / { i = $2 } END { if (p != "") print "loaded, pid " (i == "" ? "none" : i) ", " p; else print "not loaded" }' || true ;;
    linux) systemctl --user show -p ActiveState -p MainPID -p FragmentPath "$ag_default.service" 2>/dev/null | sort | tr '\n' ' ' || true ;;
  esac
}
autostart_companions() { # the person's Companions: serve processes of a herdr-anywhere outside the temporary folders
  ps -eo pid=,args= | awk '$2 ~ /herdr-anywhere(\.exe)?$/ { for (i = 3; i <= NF; i++) if ($i == "serve") { print $1 " " $2; break } }' |
    grep -vE ' (/private)?(/tmp|/var/folders)/' | grep -vF " ${RUNNER_TEMP:-/nonexistent}/" | sort || true
}
autostart_guard() {
  ag_name=io.example.herdranywhere.test.${1:?autostart_guard: give the suite a name}.$$
  export HERDR_ANYWHERE_AUTOSTART_NAME="$ag_name"
  ag_home=${HM_AUTOSTART_HOME:-$(eval "echo ~$(id -un)")}
  # Linux keeps the unit under XDG_CONFIG_HOME: the account's own unless the guard runs after the suite changed it.
  ag_config=
  [ "$(autostart_os)" != linux ] || [ -n "${HM_AUTOSTART_HOME:-}" ] || ag_config=${XDG_CONFIG_HOME:-}
  ag_default=$(autostart_default)
  ag_file=$(autostart_entry "$ag_home" "$ag_default")
  ag_stamp=$(autostart_stamp "$ag_file")
  ag_loaded=$(autostart_labels)
  ag_state=$(autostart_state)
  ag_companions=$(autostart_companions)
  ag_on=1
}
autostart_guard_end() {
  [ -n "${ag_on:-}" ] || return 0
  ag_on= ag_bad=
  case $(autostart_os) in
    darwin) launchctl bootout "gui/$(id -u)/$ag_name" >/dev/null 2>&1 || true ;;
    linux) systemctl --user disable --now "$ag_name.service" >/dev/null 2>&1 || true ;;
  esac
  ag_own=$(autostart_entry "$HOME" "$ag_name")
  [ -z "$ag_own" ] || rm -f "$ag_own"
  ag_real=$(autostart_entry "$ag_home" "$ag_name")
  if [ -n "$ag_real" ] && [ -e "$ag_real" ]; then
    rm -f "$ag_real"
    ag_bad="$ag_bad
  the suite's own entry was written in the account's home ($ag_real, now deleted): pair, start or stop ran with the real HOME"
  fi
  ag_now=$(autostart_stamp "$ag_file")
  [ "$ag_now" = "$ag_stamp" ] || ag_bad="$ag_bad
  the person's entry $ag_file changed: $ag_stamp before, $ag_now after"
  ag_after=$(autostart_labels)
  ag_new=$(printf '%s\n' "$ag_after" | grep -vxF -e "$(printf '%s\n' "$ag_loaded")" -e '' || true)
  ag_gone=$(printf '%s\n' "$ag_loaded" | grep -vxF -e "$(printf '%s\n' "$ag_after")" -e '' || true)
  [ -z "$ag_new" ] || ag_bad="$ag_bad
  loaded during the suite: $(echo $ag_new)"
  [ -z "$ag_gone" ] || ag_bad="$ag_bad
  unloaded during the suite: $(echo $ag_gone)"
  ag_now=$(autostart_state)
  [ "$ag_now" = "$ag_state" ] || ag_bad="$ag_bad
  the person's entry $ag_default in the service manager changed: $ag_state before, $ag_now after"
  ag_after=$(autostart_companions)
  ag_ended=$(printf '%s\n' "$ag_companions" | grep -vxF -e "$(printf '%s\n' "$ag_after")" -e '' || true)
  [ -z "$ag_ended" ] || ag_bad="$ag_bad
  the person's Companion ended during the suite: $(echo $ag_ended)"
  [ -n "$ag_bad" ] || return 0
  echo "FAIL: autostart guard ($ag_name):$ag_bad"
  return 1
}

if [ "${1:-}" = --self-test ]; then
  bin=${2:?the Companion to test with}
  "$bin" --version >/dev/null || { echo "FAIL: autostart guard self-test: $bin does not run"; exit 1; }
  here=$(cd "$(dirname "$0")" && pwd)
  t=$(mktemp -d /tmp/hmagXXXX) || exit 1 # short: serve's control socket lives in the state dir
  # The person's Companion runs from outside the temporary folders: its stand-in lives in the checkout.
  p=$here/../../.autostart-self-test-$$ person=
  trap '[ -z "$person" ] || kill "$person" 2>/dev/null; rm -rf "${t:?}" "${p:?}"' EXIT
  trap 'exit 1' INT TERM
  # launchctl and systemctl are stand-ins here (companion/internal/testfake/fakeservice): nothing reaches this
  # user's service manager, and the person's home is a folder of the test's.
  mkdir -p "$t/bin" "$t/account" "$t/suite-home" "$p"
  go -C "$here/../../companion" build -o "$t/bin/launchctl" ./internal/testfake/fakeservice || exit 1
  cp "$t/bin/launchctl" "$t/bin/systemctl"; cp "$t/bin/launchctl" "$p/herdr-anywhere"
  PATH="$t/bin:$PATH"; export PATH
  unset XDG_CONFIG_HOME HERDR_ANYWHERE_AUTOSTART_NAME
  def=$(autostart_default) manager=systemctl
  [ "$(autostart_os)" = darwin ] && manager=launchctl
  "$p/herdr-anywhere" serve --state "$p/state" & person=$!
  fail=0
  run_case() { # WHAT EXPECT(pass|fail) WORDS COMMAND...: one suite, as a subshell, with the guard around COMMAND
    what=$1 want=$2 words=$3; shift 3
    out=$( (HM_AUTOSTART_HOME=$t/account HOME=$t/suite-home; autostart_guard selftest; "$@"; autostart_guard_end) 2>&1)
    rc=$?
    case $want in
      pass) [ $rc = 0 ] || { echo "FAIL: autostart guard self-test: $what failed: $out"; fail=1; } ;;
      fail) { [ $rc != 0 ] && printf %s "$out" | grep -qF -- "$words"; } ||
        { echo "FAIL: autostart guard self-test: $what: exit $rc, wanted a failure saying \"$words\": $out"; fail=1; } ;;
    esac
  }
  # A case's suite has a home of its own, $t/suite-home, as HOME. Before each case nothing is loaded and the account's home has no entry; person_loaded loads the person's.
  reset() { rm -rf "$t/account/Library" "$t/account/.config" "$t/bin/launchctl.d" "$t/bin/systemctl.d"; }
  person_loaded() { mkdir -p "$t/bin/$manager.d"; echo "gui/0 $(autostart_entry "$t/account" "$def")" > "$t/bin/$manager.d/$def"; }
  # What a Companion up to 0.2.0 did for start (and pair with no serve) and for stop, with the name it was given (the
  # default without HERDR_ANYWHERE_AUTOSTART_NAME) and HOME: each way it reached the person's, which the guard must
  # catch, and the suite's own way, which it must let pass.
  old_start() { # NAME HOME
    f=$(autostart_entry "$2" "$1"); mkdir -p "$(dirname "$f")"
    if [ $manager = launchctl ]; then
      printf '<plist><dict>\n<key>Label</key>\n<string>%s</string>\n</dict></plist>\n' "$1" > "$f"
      launchctl bootout "gui/$(id -u)/$1" 2>/dev/null; launchctl bootstrap "gui/$(id -u)" "$f"
    else
      echo '[Service]' > "$f"; systemctl --user daemon-reload; systemctl --user enable "$1.service"; systemctl --user start "$1.service"
    fi
  }
  old_stop() { # NAME HOME
    if [ $manager = launchctl ]; then launchctl bootout "gui/$(id -u)/$1" 2>/dev/null; else systemctl --user disable --now "$1.service"; fi
    rm -f "$(autostart_entry "$2" "$1")"
  }
  reset; run_case "start, default name, the account's home" fail "the person's entry $(autostart_entry "$t/account" "$def") changed" \
    old_start "$def" "$t/account"
  reset; run_case "start, default name, a home of its own" fail "loaded during the suite: $def" old_start "$def" "$t/suite-home"
  if [ $manager = launchctl ]; then # systemd starts the person's unit by name, from its own folder, whatever HOME is
    reset; person_loaded
    run_case "start, default name, a home of its own, the person's Companion loaded" fail "the person's entry $def in the service manager changed" \
      old_start "$def" "$t/suite-home"
  fi
  reset; person_loaded; run_case "stop, default name, the person's Companion loaded" fail "unloaded during the suite: $def" old_stop "$def" "$t/suite-home"
  own_start() { old_start "$HERDR_ANYWHERE_AUTOSTART_NAME" "$1"; }
  reset; run_case "start, the suite's name, the account's home" fail "written in the account's home" own_start "$t/account"
  reset; person_loaded; run_case "start, the suite's name, a home of its own" pass "" own_start "$t/suite-home"
  [ "$(ls "$t/bin/$manager.d")" = "$def" ] || { echo "FAIL: autostart guard self-test: loaded after the suite's own start: $(ls "$t/bin/$manager.d")"; fail=1; }
  [ -z "$(find "$t/suite-home" -name 'io.example.*')" ] || { echo "FAIL: autostart guard self-test: the suite's own entry is left in its home"; fail=1; }
  end_person() { kill "$person"; wait "$person" 2>/dev/null; }
  reset; run_case "the person's Companion ends" fail "the person's Companion ended during the suite: $person $p/herdr-anywhere" end_person
  person=
  # This Companion: with no name of its own and a home that is not the account's, it leaves the person's entry
  # alone (stop: refused; start and pair: refused, then serve for this session, not run here: it would start a real
  # serve). With the suite's name, pair with no serve loads the suite's own entry (the stand-in runs nothing, so pair
  # waits for serve and fails, which is not what the case looks at).
  new_stop() { HOME=$t/suite-home env -u HERDR_ANYWHERE_AUTOSTART_NAME "$bin" stop --state "$t/state-stop" >/dev/null 2>&1 || true; }
  reset; person_loaded; run_case "this Companion's stop, default name, a home of its own" pass "" new_stop
  new_pair() { HOME=$t/suite-home "$bin" pair --state "$t/state-pair" >/dev/null 2>&1 || true; }
  reset; person_loaded; run_case "this Companion's pair, the suite's name, a home of its own" pass "" new_pair
  grep -q "bootstrap .*io.example.herdranywhere.test.selftest\|enable io.example.herdranywhere.test.selftest" "$t/bin/$manager.calls" ||
    { echo "FAIL: autostart guard self-test: pair with the suite's name loaded nothing: $(cat "$t/bin/$manager.calls")"; fail=1; }
  [ "$fail" = 0 ] && echo "PASS: autostart guard self-test" || exit 1
fi
