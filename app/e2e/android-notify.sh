#!/bin/sh
# What each of the app's notifications does on Android, on a fresh install: the channels the app made and what the
# system did with a real one of each, posted from an isolated herdr-core and companion while the app is away. For each
# it reports the channel and its importance, whether it popped up (a heads-up), whether the system played its sound and
# whether the app played one of its own (dumpsys audio). Then Settings: each kind's line opens its page in Android's
# settings. Fails unless both kinds pop up with the system's sound alone, each on its channel with Herdr's sound, and
# the keep-alive is silent (MainActivity). Files with everything it read go to the out folder.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-notify.sh APK [COMPANION] OUT
# COMPANION is a herdr-anywhere binary; without it one is built from companion/. Needs python3, hc and the Android SDK.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
apk=$1
if [ $# -ge 3 ]; then bin=$2; out=$3; else bin=""; out=$2; fi
mkdir -p "$out"
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

port=7781
while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d)
if [ -n "$bin" ]; then cp "$bin" "$work/herdr-anywhere"; else (cd "$app/../companion" && go build -o "$work/herdr-anywhere" ./cmd/herdr-anywhere); fi
run=$(mktemp -d /tmp/hmXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
export HC_SESSION="hmnotify-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export HERDR_ANYWHERE_STATE="$work/companion" GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spnotify --label herdr.tab=tbnotify >/dev/null
cleanup() {
  [ -n "${serve_pid:-}" ] && kill "$serve_pid" 2>/dev/null
  "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run"
}
trap cleanup EXIT INT TERM
"$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" > "$work/serve.log" 2>&1 &
serve_pid=$!
sleep 1
link=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)

at() {
  case $1 in *-) q="resource-id=\"$1" ;; *) q="resource-id=\"$1\"" ;; esac
  "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F "$q" | head -1 |
    sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'
}
tap() {
  xy=""; for _ in 1 2 3 4 5 6 7 8 9 10; do xy=$(at "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: no view $1 on screen"; exit 1; }
  "$adb" shell input tap $xy
}
screen_has() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml | grep -q "$1"; }
notes() { "$adb" shell dumpsys notification --noredact; }

# A fresh install: no channels from an earlier one.
"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$apk" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
"$adb" shell input text "'$link'"
tap pair
ok=""
for _ in $(seq 1 30); do grep -q "device .* connected" "$work/serve.log" && screen_has acme-api && { ok=1; break; }; sleep 1; done
[ -n "$ok" ] || { echo "FAIL: not connected within 30 s"; exit 1; }
echo "ok: paired"
sleep 3
"$adb" shell dumpsys activity services "$id" | grep -q KeepAlive && echo "ok: the keep-alive service runs" || echo "note: no keep-alive service"

# Away from the app: the home screen in front, the shade closed. A second terminal for an agent that asks right after.
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spnotify --label herdr.tab=tbnotify >/dev/null
"$adb" shell input keyevent 3
sleep 3
terms=$("$hc" ls --json | python3 -c 'import json,sys; print(" ".join(t["address"] for t in json.load(sys.stdin)["terminals"]))')
term=${terms%% *}; term2=${terms##* }
uid=$("$adb" shell pm list packages -U "$id" | grep -oE 'uid:[0-9]+' | head -1 | cut -d: -f2)

# The players Android has seen start (dumpsys audio keeps a log of them): the app's own (its uid), and the system's
# notification sounds (usage USAGE_NOTIFICATION, another uid). The emulator has no audio out: a player started, not a
# sound heard.
players() { "$adb" shell dumpsys audio > "$work/audio.txt"; }
app_started() { grep -oE "new player piid:[0-9]+ uid/pid:$uid/" "$work/audio.txt" | grep -oE 'piid:[0-9]+' | while read -r p; do grep -q "player $p event:started" "$work/audio.txt" && echo "$p"; done; }
system_started() { grep -oE "player piid:[0-9]+ new AudioAttributes:AudioAttributes: usage=USAGE_NOTIFICATION " "$work/audio.txt" | grep -oE 'piid:[0-9]+' | while read -r p; do grep -q "player $p event:started" "$work/audio.txt" && echo "$p"; done; }
fresh() { for p in $1; do case " $2 " in *" $p "*) ;; *) echo "$p" ;; esac; done | grep -c . || true; }

# A heads-up: SystemUI's HeadsUpManagerPhone lists an entry while one shows (checked against a high and a low channel).
# The entry names no notification, so an alert first waits for the one before to go.
heads_up() { "$adb" shell dumpsys activity service com.android.systemui/.SystemUIService > "$1" 2>/dev/null || true; grep -q "^ *HeadsUpEntry=" "$1"; }
# One alert: the terminal reports it (OSC 7501, as agents do). Then: the record (channel, importance), whether it popped
# up, whether the system and the app started a sound, and a screenshot. A heads-up shows about 5 s, so SystemUI is
# asked as soon as the record is there, again and again for 8 s; the screenshot comes after. Taken first, a loaded
# machine's screenshot (about 1.2 MB) once took long enough that the heads-up had gone before SystemUI was asked.
alert() {
  name=$1; t=$2; osc=$3
  end=$(($(date +%s) + 15)); while heads_up "$work/before-$name.txt" && [ "$(date +%s)" -lt "$end" ]; do sleep 0.5; done
  players; app0=$(app_started | tr '\n' ' '); sys0=$(system_started | tr '\n' ' ')
  printf '%s\n' "printf '\033]7501;state=working\033\\\\'; sleep 2; printf '\033]7501;$osc\033\\\\'" > "$work/home/src/acme-api/hm-$name.sh"
  "$hc" send --force "$t" "sh hm-$name.sh" >/dev/null
  rec=""
  for _ in $(seq 1 30); do
    rec=$(notes | grep -E "NotificationRecord.*pkg=$id .*tag=$t .*channel=" | grep -v "$4" || true)
    [ -n "$rec" ] && break
    sleep 0.5
  done
  A_CH=""; A_IMP=""; A_UP=no; A_SYS=0; A_APP=0
  [ -n "$rec" ] || { echo "$name: nothing posted"; return; }
  asked=$(date +%s); end=$((asked + 8))
  until heads_up "$out/$name-systemui.txt"; do [ "$(date +%s)" -lt "$end" ] || break; sleep 0.5; done
  grep -q "^ *HeadsUpEntry=" "$out/$name-systemui.txt" && A_UP="yes (seen $(($(date +%s) - asked)) s after the record)"
  "$adb" exec-out screencap -p > "$out/$name.png"
  sleep 3
  notes > "$out/$name-notification.txt"
  players; cp "$work/audio.txt" "$out/$name-audio.txt"
  app1=$(app_started | tr '\n' ' '); sys1=$(system_started | tr '\n' ' ')
  A_CH=$(echo "$rec" | head -1 | sed -E 's/.*channel=([^ ]+).*/\1/')
  A_IMP=$(echo "$rec" | head -1 | sed -E 's/.*importance=([0-9]+).*/\1/')
  A_SYS=$(fresh "$sys1" "$sys0"); A_APP=$(fresh "$app1" "$app0")
  echo "$name: channel $A_CH, importance $A_IMP; heads-up: $A_UP; system sound players started: $A_SYS; app's own players started: $A_APP; screenshot $name.png"
}
failed=0
fail() { echo "FAIL: $*"; failed=1; }
# An alert as it should be: on its channel, popped up, the system's sound, none of the app's own.
expect_alert() {
  [ "$A_CH" = "$2" ] || fail "$1 is on channel '$A_CH', not $2"
  [ "$A_IMP" = 4 ] || fail "$1 has importance $A_IMP, not 4 (sound and pop-up)"
  case $A_UP in yes*) ;; *) fail "$1 did not pop up" ;; esac
  [ "$A_SYS" -ge 1 ] || fail "$1 played no system sound"
  [ "$A_APP" = 0 ] || fail "$1 also played the app's own sound ($A_APP players)"
}
# $4: a channel the record must not be on yet (the same terminal's earlier notification).
alert needs-you "$term" "state=blocked:kind=question:msg=$(printf 'Which port?' | base64)" "channel=none"
expect_alert needs-you agent-needs-you
# Android 15 has no notification cooldown, so a second question right after sounds too (Android 16's would lower it).
alert needs-you-second "$term2" "state=blocked:kind=approval:msg=$(printf 'Run npm test?' | base64)" "channel=none"
expect_alert needs-you-second agent-needs-you
"$adb" shell input keyevent 3
sleep 6
alert finished "$term" "state=done:msg=$(printf 'Ran the tests' | base64)" "needs-you"
expect_alert finished agent-finished

# The channels as the system holds them now, and the keep-alive's record.
notes > "$out/final.txt"
grep -E "NotificationChannel\{mId='[^']*'" "$out/final.txt" | grep -v 'mDeleted=true' | grep -E "needs-you|finished|connection|updates" | sed -E "s/.*mId='([^']*)'.*mImportance=([0-9]+).*mSound=([^,]*),.*mVibrationEnabled=([a-z]*).*/channel \1: importance \2, sound \3, vibration \4/" | sort -u | tee "$out/channels.txt"
grep -E "NotificationRecord.*pkg=$id " "$out/final.txt" > "$out/records.txt" || true
grep -qE "^channel agent-needs-you: importance 4, sound android.resource://$id/raw/request" "$out/channels.txt" || fail "agent-needs-you is not sound and pop-up with Herdr's request sound"
grep -qE "^channel agent-finished: importance 4, sound android.resource://$id/raw/done" "$out/channels.txt" || fail "agent-finished is not sound and pop-up with Herdr's done sound"
grep -qE "^channel connection: importance 2," "$out/channels.txt" || fail "the keep-alive's channel is not silent (2)"
grep -qE "^channel (needs-you|finished):" "$out/channels.txt" && fail "the channels before 0.1.14 are still there"

# Settings: each kind's line says what Android does with it; a tap opens that kind's page in Android's settings.
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap settings
xy=""; for _ in $(seq 1 10); do xy=$(at channel-done); [ -n "$xy" ] && break; sleep 1; done
"$adb" exec-out screencap -p > "$out/settings.png"
if [ -z "$xy" ]; then fail "Settings shows no line for An agent finished"
else
  "$adb" shell cat /sdcard/hm.xml | tr '>' '\n' | grep -F 'resource-id="channel-done"' | grep -qF 'An agent finished: Sound and pop-up' || fail "the finished line does not say Sound and pop-up"
  "$adb" shell input tap $xy
  sleep 2
  focus=$("$adb" shell dumpsys window | grep -E 'mCurrentFocus=' | head -1)
  "$adb" exec-out screencap -p > "$out/channel-page.png"
  echo "after the tap: $focus"
  echo "$focus" | grep -q "com.android.settings" || fail "the finished line did not open Android's settings"
  screen_has 'An agent finished' || fail "Android's page is not the finished channel's"
fi
[ "$failed" = 0 ] || { echo "FAIL: see above; collected in $out"; exit 1; }
echo "PASS: both kinds pop up with the system's sound alone, on Herdr's sounds; the keep-alive is silent; Settings opens each kind's page ($out)"
