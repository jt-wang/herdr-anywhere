#!/bin/sh
# Proves from outside the process that serve --routes lan and a LAN-only phone send nothing off this computer: runs
# TestLiveLANOnly (pair with the emoji, connect, talk to the core, revoke) as a throwaway user and counts, with
# iptables and ip6tables, every packet that user sends anywhere but loopback, DNS included. (Packets to this
# computer's own LAN address go over loopback on Linux.) The count must be 0. As a control, the same count for
# TestLiveTailcat, which pairs over Tailcat's real servers, must be above 0: otherwise the count proves nothing.
# Linux, root through sudo, Go on PATH, herdr-core's hc. Usage: app/e2e/no-tailcat-packets.sh HC
set -eu
hc_src=$1
user=hanotc
work=$(mktemp -d)
trap 'reset off; rm -rf "$work"' EXIT
(cd "$(dirname "$0")/../../companion" && go test -c -o "$work/main.test" ./cmd/herdr-anywhere)
cp "$hc_src" "$work/hc"
chmod 755 "$work" "$work/main.test" "$work/hc"
id -u $user >/dev/null 2>&1 || sudo useradd -M -s /usr/sbin/nologin $user
uid=$(id -u $user)
count() { # packets $user sent off loopback since the chains were made
  for ipt in iptables ip6tables; do sudo $ipt -L HANOTC -v -n -x | awk 'NR>2{s+=$1} END{print s+0}'; done | awk '{s+=$1} END{print s}'
}
reset() {
  for ipt in iptables ip6tables; do
    sudo $ipt -D OUTPUT ! -o lo -m owner --uid-owner "$uid" -j HANOTC 2>/dev/null || true
    sudo $ipt -F HANOTC 2>/dev/null || true
    sudo $ipt -X HANOTC 2>/dev/null || true
    [ "${1:-}" = off ] && continue
    sudo $ipt -N HANOTC
    sudo $ipt -A HANOTC -j RETURN
    sudo $ipt -I OUTPUT 1 ! -o lo -m owner --uid-owner "$uid" -j HANOTC
  done
}
run() { # test name, timeout; the test's own failure fails the script
  st=0
  (cd "$work" && sudo -u $user env HOME="$work" TMPDIR=/tmp HM_TAILCAT_LIVE=1 HM_TEST_HC="$work/hc" \
    "$work/main.test" -test.count=1 -test.v -test.run "^$1\$" -test.timeout "$2") > "$work/$1.log" 2>&1 || st=$?
  grep -E '^(---|ok|FAIL|PASS)|_test.go' "$work/$1.log" || true
  [ "$st" -eq 0 ] || { echo "FAIL: $1 exit $st"; exit 1; }
}
reset
run TestLiveLANOnly 3m
n=$(count); echo "LAN only: $n packets off loopback"
[ "$n" -eq 0 ] || { echo "FAIL: serve --routes lan and a LAN-only phone sent packets off this computer"; exit 1; }
reset
run TestLiveTailcat 5m
c=$(count); echo "control, through Tailcat: $c packets off loopback"
[ "$c" -gt 0 ] || { echo "FAIL: the control sent nothing, so the count proves nothing"; exit 1; }
echo PASS
