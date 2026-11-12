#!/bin/sh
# No test may set the person's Companion to start at login, or take it off. pair (with no serve running), start and
# stop reach the service manager, whose domain is the user's whatever HOME is: a suite that ran pair with a home of its
# own but the default name unloaded the person's Companion and loaded its own under the person's label.
# - An e2e script (app/e2e) that runs the Companion's pair, start or stop sources e2e/autostart-guard.sh and calls
#   autostart_guard NAME before the first of them and autostart_guard_end in its cleanup, or sets
#   HERDR_ANYWHERE_AUTOSTART_NAME= and HOME= on that line itself.
# - The Go tests (companion) never use autostart.Default(); package main's tests swap atLogin for a fake in init and
#   check the person's entry around the run (TestMain); an autostart.Service a test makes has a name under
#   io.example.herdranywhere.test., unless the test runs launchctl and systemctl stand-ins (testfake/fakeservice); a
#   test that runs a built herdr-anywhere's pair, start or stop sets HERDR_ANYWHERE_AUTOSTART_NAME.
# Usage: app/scripts/check-autostart.sh [ignored]   HM_CHECK_ROOT=<folder> checks another tree (its self-test).
set -u
# Each run first checks trees made to fail, and one made to pass: a check that finds nothing fails.
if [ -z "${HM_CHECK_ROOT:-}" ]; then
  t=$(mktemp -d) || exit 1
  trap 'rm -rf "$t"' EXIT
  self=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
  tree() { # NAME SCRIPT [GO_TEST]: a tree with one e2e script, package main's fake and guard, and one Go test
    mkdir -p "$t/$1/app/e2e" "$t/$1/companion/cmd/herdr-anywhere"
    printf '%s\n' "$2" > "$t/$1/app/e2e/suite.sh"
    echo 'func init() { atLogin = func() loginStart { return &fakeLogin{} } }' > "$t/$1/companion/cmd/herdr-anywhere/login_test.go"
    echo '	guard := testfake.GuardAutostart(own)' > "$t/$1/companion/cmd/herdr-anywhere/dialog_start_test.go"
    printf '%s\n' "${3:-}" > "$t/$1/companion/cmd/herdr-anywhere/x_test.go"
  }
  guarded='set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard suite
cleanup() { rm -rf "$work"; autostart_guard_end || exit 1; }
invite=$("$work/herdr-anywhere" pair --state "$state")'
  tree ok "$guarded"
  tree no-guard 'set -eu
invite=$("$work/herdr-anywhere" pair --state "$state")'
  tree late-guard 'set -eu
"$bin" start --state "$state"
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard suite
cleanup() { autostart_guard_end || exit 1; }'
  tree no-end 'set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard suite
hm stop'
  tree only-xdg-home 'set -eu
HERDR_ANYWHERE_AUTOSTART_NAME=io.example.herdranywhere.test.x XDG_CONFIG_HOME="$t/c" "$companion" pair --state "$t/s"'
  tree env-i-drops-name 'set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard suite
L() { env -i PATH=/usr/bin HOME="$work/h" "$@"; }
cleanup() { autostart_guard_end || exit 1; }
link=$(L "$work/herdr-anywhere" pair --state "$state")'
  tree default-in-go "$guarded" '	s := autostart.Default()'
  tree persons-name-in-go "$guarded" '	s := &autostart.Service{Name: "io.jingtao.herdranywhere.companion", Home: t.TempDir()}'
  tree exec-without-name "$guarded" '	cmd := exec.Command(bin, "start", "--state", dir)'
  bad=0
  HM_CHECK_ROOT=$t/ok "$self" 2>"$t/said" || { echo "check-autostart: self-test: a tree that should pass failed: $(cat "$t/said")" >&2; bad=1; }
  for c in no-guard late-guard no-end only-xdg-home env-i-drops-name default-in-go persons-name-in-go exec-without-name; do
    HM_CHECK_ROOT=$t/$c "$self" 2>/dev/null && { echo "check-autostart: self-test: the $c tree passed" >&2; bad=1; }
  done
  [ "$bad" = 0 ] || exit 1
fi
root=${HM_CHECK_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}
status=0
no() { echo "check-autostart: $1" >&2; status=1; }
# A command word that is the Companion ("$bin", "$work/herdr-anywhere", hm, …), then pair, start or stop. herdr-core's
# hc and adb have their own start and stop.
calls='(^|[[:space:]"'"'"'(|;&=])("?\$\{?[A-Za-z_][A-Za-z0-9_]*\}?(/[^[:space:]"]*)?"?|[^[:space:]"'"'"']*herdr-anywhere[^[:space:]"'"'"']*"?|hm)[[:space:]]+(pair|start|stop)([[:space:]]|$|[";)|])'
for f in $(cd "$root" && find app/e2e -name '*.sh' ! -name autostart-guard.sh | sort); do
  hits=$(grep -nE "$calls" "$root/$f" | grep -vE '^[0-9]+:[[:space:]]*#' | grep -vE '"\$\{?(hc|adb|HM_TEST_HC)\}?"' |
    grep -vE 'HERDR_ANYWHERE_AUTOSTART_NAME=.*(^|[[:space:](;])HOME=|(^|[[:space:](;])HOME=.*HERDR_ANYWHERE_AUTOSTART_NAME=')
  [ -n "$hits" ] || continue
  first=$(echo "$hits" | head -1 | cut -d: -f1)
  guard=$(grep -nE '(^|;)[[:space:]]*autostart_guard[[:space:]]+[A-Za-z0-9_-]+' "$root/$f" | grep -vE '^[0-9]+:[[:space:]]*#' | head -1 | cut -d: -f1)
  if [ -z "$guard" ]; then
    no "$f runs the Companion's pair, start or stop (line $first) without autostart_guard (e2e/autostart-guard.sh)"
  elif [ "$guard" -gt "$first" ]; then
    no "$f calls autostart_guard on line $guard, after its first pair, start or stop (line $first)"
  fi
  grep -qE '^[[:space:]]*[^#]*autostart_guard_end' "$root/$f" ||
    no "$f runs the Companion's pair, start or stop without autostart_guard_end in its cleanup"
  grep -qE '^[[:space:]]*\.[[:space:]].*autostart-guard\.sh' "$root/$f" || no "$f does not source e2e/autostart-guard.sh"
  # A wrapper the call goes through (clean "$bin" pair …) that empties the environment (env -i) passes the name on.
  for w in $(echo "$hits" | grep -oE '(^|[[:space:](;])[A-Za-z_][A-Za-z0-9_]*[[:space:]]+(env[[:space:]]+[^"]*)?"?\$\{?[A-Za-z_]' |
      grep -oE '[A-Za-z_][A-Za-z0-9_]*[[:space:]]' | tr -d ' \t' | sort -u); do
    body=$(awk -v w="$w" '$0 ~ "^[[:space:]]*" w "\\(\\)" { on = 1 } on { print } on && /}[[:space:]]*$/ { exit }' "$root/$f")
    case $body in *"env -i"*) case $body in *HERDR_ANYWHERE_AUTOSTART_NAME*) ;; *) no "$f: $w() empties the environment (env -i) without passing HERDR_ANYWHERE_AUTOSTART_NAME on" ;; esac ;; esac
  done
done
gotests=$(cd "$root" && find companion -name '*_test.go' | sort)
for f in $gotests; do
  bad=$(grep -n 'autostart\.Default(' "$root/$f" | cut -d: -f1 | tr '\n' ' ')
  [ -z "$bad" ] || no "$f uses the person's entry, autostart.Default(), on line $bad"
  # A test that puts launchctl and systemctl stand-ins first on PATH (testfake/fakeservice) may use any name.
  bad=
  grep -q 'testfake/fakeservice' "$root/$f" ||
    bad=$(grep -nE 'autostart\.Service\{' "$root/$f" | grep -v 'io\.example\.herdranywhere\.test\.' | cut -d: -f1 | tr '\n' ' ')
  [ -z "$bad" ] || no "$f makes an autostart.Service not named io.example.herdranywhere.test.… on line $bad"
  if grep -nE 'exec\.Command(Context)?\(.*"(pair|start|stop)"' "$root/$f" | grep -vqE '"server", "stop"'; then
    grep -q 'HERDR_ANYWHERE_AUTOSTART_NAME' "$root/$f" ||
      no "$f runs a herdr-anywhere's pair, start or stop without HERDR_ANYWHERE_AUTOSTART_NAME"
  fi
done
main=companion/cmd/herdr-anywhere
grep -qE '^func init\(\) \{ atLogin = func\(\) loginStart \{ return &fakeLogin' "$root/$main/login_test.go" 2>/dev/null ||
  no "$main/login_test.go no longer swaps atLogin for a fake in init"
grep -qE 'testfake\.GuardAutostart\(' "$root/$main/dialog_start_test.go" 2>/dev/null ||
  no "$main's TestMain does not check the person's entry around the run (testfake.GuardAutostart)"
exit $status
