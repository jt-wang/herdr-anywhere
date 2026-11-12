#!/bin/sh
# build-nsh-ios.sh stops before any build when the nsh source, CARGO_HOME or CARGO_TARGET_DIR has a space in its path,
# and says which one and why (the compiler flags that rename these paths are split at spaces). Each run names a
# toolchain that doesn't exist, so a script that missed the space stops at the toolchain check instead, with no clone,
# download or build. A run without spaces is the control: it must stop at the toolchain, not at the space check.
set -u
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/a b" "$tmp/plain"
fails=0
run() { # <name> <expect: space|toolchain> <src> [VAR=value ...]
  name=$1 expect=$2 src=$3; shift 3
  err=$(env -u CARGO_HOME -u CARGO_TARGET_DIR RUSTUP_TOOLCHAIN=no-such-toolchain-for-test "$@" \
    sh "$here/build-nsh-ios.sh" "$src" "$tmp/out" 2>&1 >/dev/null); code=$?
  case $expect in
    space) printf %s "$err" | grep -q "has a space" && printf %s "$err" | grep -q "$name" ;;
    toolchain) ! printf %s "$err" | grep -q "has a space" ;;
  esac
  ok=$?
  if [ "$code" -ne 0 ] && [ "$ok" -eq 0 ]; then echo "ok   $name ($expect)"; else
    echo "FAIL $name: expected a stop at the $expect check, got exit $code:"; printf '%s\n' "$err" | sed 's/^/     /'
    fails=$((fails + 1)); fi
}
run src space "$tmp/a b/nsh"
run CARGO_HOME space "$tmp/plain/nsh" CARGO_HOME="$tmp/a b/cargo"
run CARGO_TARGET_DIR space "$tmp/plain/nsh" CARGO_TARGET_DIR="$tmp/a b/target"
run none toolchain "$tmp/plain/nsh" CARGO_HOME="$tmp/plain/cargo" CARGO_TARGET_DIR="$tmp/plain/target"
[ ! -e "$tmp/a b/nsh" ] && [ ! -e "$tmp/plain/nsh" ] || { echo "FAIL a run reached the clone"; fails=$((fails + 1)); }
[ "$fails" -eq 0 ] || exit 1
