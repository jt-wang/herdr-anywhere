# Sourced by the Tailcat suites. A companion with the emoji check (0.3.1 and later) shows a herdr2: invite, waits for
# the phone, then asks whether the phone shows the same four emoji: tailcat_invite answers y for the suite, from
# pair's stdin, and prints the invite once pair shows it. An older companion prints a herdr1: invite and exits.
#
#   invite=$(tailcat_invite <herdr-anywhere> <state dir> <file for pair's output>)
#   "$bin" serve --tailcat $(tailcat_no_lan "$bin") ...
#
# tailcat_no_lan: these suites are about Tailcat, so a companion from 0.3.1 on gets --no-lan (it would offer this
# computer's own network too); an older one has no such flag, and listens there only with --lan.
tailcat_invite() {
  printf 'y\n' | "$1" pair --state "$2" > "$3" 2>&1 &
  _ti_n=0; _ti_inv=""
  while [ "$_ti_n" -lt 300 ]; do
    _ti_inv=$(grep -o 'herdr[12]:[A-Za-z0-9_-]*' "$3" 2>/dev/null | head -1)
    [ -n "$_ti_inv" ] && break
    _ti_n=$((_ti_n + 1)); sleep 0.1
  done
  printf '%s' "$_ti_inv"
}
tailcat_no_lan() { if "$1" serve -h 2>&1 | grep -q -- '-no-lan'; then echo --no-lan; fi; }
