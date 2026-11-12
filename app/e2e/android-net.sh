#!/bin/sh
# The phone's networks as Android sees them (dumpsys connectivity), one line each: its number, kind and state, whether
# Android last found the internet through it (validated), since when it is up, and which one is the default. On CI
# the phone has two, WiFi and mobile data, and the default is not always the same one; once a phone on CI showed
# WiFi without internet, and its Tailcat suites reached neither the computer nor Tailcat's relays.
# Sourced with $adb set: nets prints them. The lines are read in the form of Android 15's NetworkAgentInfo.toString
# (AOSP packages/modules/Connectivity): " lastValidated <time>" is there only while the network is validated.
#        e2e/android-net.sh --self-test    reads made-up dumpsys lines and checks what it makes of them
net_read() {
  awk '
    /Active default network:/ { def = $NF; next }
    /NetworkAgentInfo[{]network[{][0-9]+[}]/ {
      match($0, /network[{][0-9]+[}]/); n = substr($0, RSTART + 8, RLENGTH - 9)
      if (seen[n]++) next
      ni = "?"; if (match($0, /ni[{][^}]*[}]/)) { ni = substr($0, RSTART + 3, RLENGTH - 4); sub(/ extra:.*/, "", ni) }
      v = "not validated"
      if ($0 ~ / partialConnectivity /) v = "partly validated"
      if ($0 ~ / lastValidated /) v = "validated"
      since = ""; if (match($0, /created=[^ ]*T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]/)) since = ", up since " substr($0, RSTART + RLENGTH - 8, 8)
      k++; num[k] = n; text[k] = "network " n ": " ni ", " v since
    }
    END {
      for (i = 1; i <= k; i++) print text[i] (num[i] == def ? ", the default" : "")
      if (def == "" || def == "none") print "no default network"
    }'
}
nets() { "$adb" shell dumpsys connectivity 2>/dev/null | tr -d '\r' | net_read | sed 's/^/    /'; }

if [ "${1:-}" = --self-test ]; then
  # Two networks, WiFi the default and validated, mobile data not; each listed twice, as dumpsys does in its sections.
  # The day in created= is DAY: only the time of day is read.
  wifi='  NetworkAgentInfo{network{100}  handle{432902426637}  ni{WIFI CONNECTED extra: } created=DAYT00:57:24.129Z Score(Policies : TRANSPORT_PRIMARY&EVER_EVALUATED&IS_VALIDATED)  created 5 firstValidated 6 lastValidated 7  lp{{InterfaceName: wlan0}}  nc{[ Transports: WIFI Capabilities: INTERNET&VALIDATED ]}  factorySerialNumber=1}'
  mobile='  NetworkAgentInfo{network{101}  handle{437197393933}  ni{MOBILE[LTE] CONNECTED extra: example.apn} created=DAYT00:57:37.528Z Score(Policies : EVER_EVALUATED)  created 8  lp{{InterfaceName: eth0}}  nc{[ Transports: CELLULAR Capabilities: INTERNET ]}  factorySerialNumber=2}'
  got=$(printf '%s\n' 'Active default network: 100' "$wifi" "$mobile" "$wifi" "$mobile" | net_read)
  want='network 100: WIFI CONNECTED, validated, up since 00:57:24, the default
network 101: MOBILE[LTE] CONNECTED, not validated, up since 00:57:37'
  [ "$got" = "$want" ] || { printf 'FAIL: android-net self-test: two networks read as\n%s\n' "$got"; exit 1; }
  # WiFi up but without internet, and no default at all.
  bad=$(printf '%s\n' "$wifi" | sed 's/ lastValidated 7//; s/ firstValidated 6//')
  got=$(printf '%s\n' 'Active default network: none' "$bad" | net_read)
  want='network 100: WIFI CONNECTED, not validated, up since 00:57:24
no default network'
  [ "$got" = "$want" ] || { printf 'FAIL: android-net self-test: WiFi without internet read as\n%s\n' "$got"; exit 1; }
  echo "PASS: android-net self-test"
fi
