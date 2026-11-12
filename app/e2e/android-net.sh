#!/bin/sh
# The phone's networks as Android sees them (dumpsys connectivity), one line each: its number, kind and state, whether
# Android last found the internet through it (validated), since when it is up, and which one is the default. On CI
# the phone has two, WiFi and mobile data, and the default is not always the same one; once a phone on CI showed
# WiFi without internet, and its Tailcat suites reached neither the computer nor Tailcat's relays.
# Sourced with $adb set: nets prints them. The lines are read in the form of Android 15's NetworkAgentInfo.toString
# (AOSP packages/modules/Connectivity): " lastValidated <time>" is there only while the network is validated.
#        e2e/android-net.sh --mobile-only [seconds]    WiFi off, and waits for mobile data (mobile_only)
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

# The emulator's WiFi is simulated by another program (netsim). On CI it has failed three ways: WiFi up without
# internet for a whole job (every Tailcat suite reached nothing), its link to the emulator broken under a 100 MB
# upload and the emulator gone with it, and, by the look of two runs where the phone's pair request never reached the
# computer, WiFi without internet before mobile data was up. Mobile data goes through the emulator's own network,
# which reaches this computer at 10.0.2.2 and the internet. So the suites turn WiFi off and wait for mobile data.
# mobile_ready: whether net_read's lines (on stdin) have mobile data the default and validated, and no WiFi.
mobile_ready() {
  awk '/^network [0-9]+: WIFI/ { wifi = 1 } /^network [0-9]+: MOBILE[^,]* CONNECTED, validated,.*, the default$/ { ok = 1 }
    END { exit !(ok && !wifi) }'
}
# WiFi off, then up to $1 seconds (120) for mobile data alone, validated and the default; says how long it took.
mobile_only() {
  "$adb" shell svc wifi disable
  i=0
  while [ "$i" -lt "${1:-120}" ]; do
    "$adb" shell dumpsys connectivity 2>/dev/null | tr -d '\r' | net_read | mobile_ready && {
      echo "the phone goes out through mobile data alone, WiFi off ($i s):"; nets; return 0; }
    sleep 1; i=$((i + 1))
  done
  echo "FAIL: ${1:-120} s after WiFi was turned off, mobile data is not the phone's validated default network:"; nets
  return 1
}

# e2e/android-net.sh --mobile-only [seconds]: mobile_only, with adb from the PATH (ANDROID_SERIAL picks the phone).
if [ "${1:-}" = --mobile-only ]; then adb=${adb:-adb}; mobile_only "${2:-120}"; exit; fi
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
  # Ready for the suites only with mobile data the default and validated, and WiFi gone.
  mob_default=$(printf '%s\n' "$mobile" | sed 's/ created 8 / created 8 firstValidated 9 lastValidated 10 /')
  ready() { printf '%s\n' "$@" | net_read | mobile_ready; }
  ready 'Active default network: 100' "$wifi" "$mob_default" && { echo "FAIL: android-net self-test: ready with WiFi up and the default"; exit 1; }
  ready 'Active default network: 101' "$wifi" "$mob_default" && { echo "FAIL: android-net self-test: ready with WiFi still up"; exit 1; }
  ready 'Active default network: 101' "$mobile" && { echo "FAIL: android-net self-test: ready with mobile data not validated"; exit 1; }
  ready 'Active default network: none' && { echo "FAIL: android-net self-test: ready with no network"; exit 1; }
  ready 'Active default network: 101' "$mob_default" || { echo "FAIL: android-net self-test: not ready with mobile data alone, validated, the default"; exit 1; }
  echo "PASS: android-net self-test"
fi
