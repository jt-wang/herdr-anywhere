#!/bin/sh
# Updating, the whole way on Android: a phone and a computer on the old version, a release that says there is a new one
# (a stand-in for GitHub, on this machine), nothing asked of it until the person says yes the second time the app
# opens, the notice on home, the computer replacing itself, then the app: the system's
# permission screen, a download that doesn't match, a file signed with another key, and the real install.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-update.sh OLD.apk NEW.apk OTHER-KEY.apk OLD-COMPANION NEW-COMPANION NEW-VERSION [folder for screenshots]
# NEW.apk is NEW-VERSION with a higher build number, signed like OLD.apk; OTHER-KEY.apk is the same file under another key.
# Needs python3, hc and the Android SDK.
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
old_apk=$1; new_apk=$2; other_apk=$3; old_bin=$4; new_bin=$5; new_version=$6; shot=${7:-}
app=$(cd "$(dirname "$0")/.." && pwd)
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
hc=$(command -v hc || echo "$HOME/.local/bin/hc")
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")

free_port() { p=$1; while nc -z 127.0.0.1 "$p" 2>/dev/null; do p=$((p + 1)); done; echo "$p"; }
port=$(free_port 7721)
gh=$(free_port 7741)
work=$(mktemp -d)
run=$(mktemp -d /tmp/hmXXXX)
state=$(mktemp -d /tmp/hmtcXXXX)
mkdir -p "$work/home/src/acme-api" "$work/state" "$work/config" "$work/data"
: > "$work/home/.zshrc"
# Ubuntu's /etc/zsh/zshrc runs compinit, which can stop at a y/n question and eat what the phone types first.
echo skip_global_compinit=1 > "$work/home/.zshenv"
export HC_SESSION="hmupd-$$" HC_SOCKET="" HOME="$work/home" SHELL=/bin/zsh
export XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run"
export GIT_CONFIG_GLOBAL="$work/home/.gitconfig"
"$hc" run --bg --keep --cwd "$work/home/src/acme-api" --label herdr.space=spupd --label herdr.tab=tbupd >/dev/null
cp "$old_bin" "$work/herdr-anywhere"

# A release signed with a key made here, as github.com/<repo>/releases serves it: latest/download/ for the newest,
# download/v<version>/ for its files. $work/scenario picks the app's file and the manifest that goes with it.
(cd "$app/../companion" && go build -o "$work/release-manifest" ./cmd/release-manifest)
release_key=$("$work/release-manifest" keygen "$work/release.key")
for s in good bad-digest other-key; do
  mkdir -p "$work/rel-$s/files"
  ln -s "$new_bin" "$work/rel-$s/files/herdr-anywhere-macos-arm64"
  case $s in good) a=$new_apk ;; bad-digest) a=$old_apk ;; other-key) a=$other_apk ;; esac
  ln -s "$a" "$work/rel-$s/files/Herdr-Anywhere.apk"
  "$work/release-manifest" sign -any-key -key "$work/release.key" -version "$new_version" -out "$work/rel-$s" \
    "$work/rel-$s/files/herdr-anywhere-macos-arm64" "$work/rel-$s/files/Herdr-Anywhere.apk" >/dev/null
done
cat > "$work/gh.py" <<'EOF'
import http.server, os, sys
port, version, mac, good, other, work, asked = sys.argv[1:8]
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def file(self, p):
        self.send_response(200); self.send_header('Content-Length', str(os.path.getsize(p))); self.end_headers()
        with open(p, 'rb') as f:
            for b in iter(lambda: f.read(1 << 20), b''): self.wfile.write(b)
    def do_GET(self):
        s = open(work + '/scenario').read().strip()
        if self.path == '/latest/download/latest.json':
            with open(asked, 'a') as f: f.write('asked\n')
            self.file(f'{work}/rel-{s}/latest.json')
        elif self.path == '/latest/download/latest.json.sig': self.file(f'{work}/rel-{s}/latest.json.sig')
        elif self.path == f'/download/v{version}/herdr-anywhere-macos-arm64': self.file(mac)
        elif self.path == f'/download/v{version}/Herdr-Anywhere.apk': self.file(other if s == 'other-key' else good)
        else: self.send_error(404)
http.server.ThreadingHTTPServer(('127.0.0.1', int(port)), H).serve_forever()
EOF
echo good > "$work/scenario"
: > "$work/asked"
python3 "$work/gh.py" "$gh" "$new_version" "$new_bin" "$new_apk" "$other_apk" "$work" "$work/asked" &
gh_pid=$!

serve_pid=""
start_serve() {
  "$work/herdr-anywhere" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" --releases "http://127.0.0.1:$gh" --release-key "$release_key" >> "$work/serve.log" 2>&1 &
  serve_pid=$!
  i=0
  until curl -sf "http://127.0.0.1:$port/api/info" >/dev/null; do
    i=$((i + 1)); [ $i -gt 100 ] && { echo "FAIL: serve did not start"; tail -5 "$work/serve.log"; exit 1; }
    sleep 0.1
  done
}
cleanup() {
  [ -n "$serve_pid" ] && kill "$serve_pid" 2>/dev/null
  kill "$gh_pid" 2>/dev/null
  "$adb" reverse --remove "tcp:$gh" >/dev/null 2>&1 || true
  "$hc" server stop >/dev/null 2>&1 || true
  chmod -R u+w "$work" 2>/dev/null; rm -rf "$work" "$run" "$state"
}
trap cleanup EXIT INT TERM

"$adb" uninstall "$id" >/dev/null 2>&1 || true
"$adb" install "$old_apk" >/dev/null
# The phone downloads the app from where the computer says the release is: the same address, through adb.
"$adb" reverse "tcp:$gh" "tcp:$gh" >/dev/null
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
start_serve
invite=$("$work/herdr-anywhere" pair --url "http://10.0.2.2:$port" --state "$state" 2>/dev/null | grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | head -1)
[ -n "$invite" ] || { echo "FAIL: pair printed no invite"; exit 1; }

xml() { "$adb" shell uiautomator dump /sdcard/hm.xml >/dev/null 2>&1; "$adb" shell cat /sdcard/hm.xml; }
centre() { tr '>' '\n' | grep -F "$1" | head -1 | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/' | awk '{print int(($1+$3)/2), int(($2+$4)/2)}'; }
tap_where() {
  xy=""; for _ in $(seq 1 30); do xy=$(xml | centre "$1"); [ -n "$xy" ] && break; sleep 1; done
  [ -n "$xy" ] || { echo "FAIL: nothing on screen with $1"; xml | tr '>' '\n' | grep -oE ' text="[^"]+"' | head -20; exit 1; }
  "$adb" shell input tap $xy
}
tap() { tap_where "resource-id=\"$1\""; }
tap_text() { tap_where "text=\"$1\""; }
wait_for() { # what, seconds
  for _ in $(seq 1 "$2"); do xml | grep -qF "$1" && return 0; sleep 1; done
  echo "FAIL: \"$1\" did not show within $2 s"; xml | tr '>' '\n' | grep -oE ' text="[^"]+"' | head -20; exit 1
}
snap() { [ -n "$shot" ] && "$adb" exec-out screencap -p > "$shot/update-$1.png" || true; }
version() { "$adb" shell dumpsys package "$id" | grep -m1 versionName | sed 's/.*versionName=//' | tr -d '\r'; }
info() { curl -s "http://127.0.0.1:$port/api/info" | python3 -c "import json,sys; print(json.load(sys.stdin).get('$1'))"; }
installed_before=$(version)
pid_before=$serve_pid

"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
tap pair-link
rest=$invite
while [ -n "$rest" ]; do
  piece=$(printf %s "$rest" | cut -c1-80); rest=$(printf %s "$rest" | cut -c81-)
  "$adb" shell input text "'$piece'" < /dev/null
  sleep 0.5
done
tap pair

echo "--- nothing goes to GitHub until the person says yes"
wait_for 'resource-id="space-spupd"' 60
sleep 5
[ ! -s "$work/asked" ] || { echo "FAIL: the computer asked GitHub before anyone said yes"; exit 1; }
xml | grep -qF 'resource-id="checks-question"' && { echo "FAIL: asked the first time the app opened"; exit 1; }
echo "ok: the first time the app opens, nothing is asked of the person or of GitHub"
"$adb" shell am force-stop "$id"
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
wait_for "Check for new versions?" 60
snap question
tap checks-yes
[ "$(info checks)" = "on" ] || { sleep 2; [ "$(info checks)" = "on" ] || { echo "FAIL: the computer says checks $(info checks)"; exit 1; }; }
echo "ok: the second time it asks, and yes reaches the computer"

echo "--- the notice, on the old version"
wait_for "Version $new_version is available" 60
snap notice
echo "ok: home says $new_version is out (app $installed_before, computer $(info version))"

echo "--- the computer, from the phone"
tap update-now
wait_for "The computer is now on $new_version" 90
[ "$(info version)" = "$new_version" ] || { echo "FAIL: the computer says $(info version)"; exit 1; }
kill -0 "$pid_before" 2>/dev/null || { echo "FAIL: the process ended"; exit 1; }
cmp -s "$work/herdr-anywhere" "$new_bin" || { echo "FAIL: the file wasn't replaced"; exit 1; }
snap computer-updated
echo "ok: the same process is now $new_version, from the new file, and the phone came back to it"

echo "--- the app: the system must allow this app to install first"
wait_for "Version $new_version is available" 30
tap update-now
tap_text "Download"
sleep 2
"$adb" shell dumpsys window | grep -E 'mCurrentFocus' | grep -qi 'settings' || { echo "FAIL: the system's screen did not open"; "$adb" shell dumpsys window | grep mCurrentFocus; exit 1; }
snap permission
echo "ok: the system's permission screen opened"
"$adb" shell appops set "$id" REQUEST_INSTALL_PACKAGES allow
# Back in the app, the download starts without another tap. The file served now doesn't match the release, so it ends
# in the app's own refusal, and nothing is installed yet for the steps below.
echo other-key > "$work/scenario"
"$adb" shell input keyevent KEYCODE_BACK
wait_for "Couldn't update the app" 240
[ "$(version)" = "$installed_before" ] || { echo "FAIL: installed $(version)"; exit 1; }
echo "ok: back from allowing installs, the download started by itself"

restart_with() {
  echo "$1" > "$work/scenario"
  kill "$serve_pid"; wait "$serve_pid" 2>/dev/null || true
  start_serve
  sleep 2
  wait_for "Version $new_version is available" 60
}

echo "--- a download that doesn't match"
restart_with bad-digest
tap update-now
tap_text "Download"
wait_for "Couldn't update the app" 240
[ "$(version)" = "$installed_before" ] || { echo "FAIL: installed $(version)"; exit 1; }
snap bad-digest
echo "ok: refused, still $(version)"

echo "--- a file signed with another key"
restart_with other-key
tap update-now
tap_text "Download"
# Android refuses at once or after its own prompt, depending on the version. The notice lasts seconds: seen once is seen.
"$adb" logcat -c
said=""
for _ in $(seq 1 240); do
  screen=$(xml)
  if printf %s "$screen" | grep -qF "Couldn't update the app"; then said=1; break; fi
  if printf %s "$screen" | grep -qE 'resource-id="android:id/button1"'; then snap other-key-prompt; tap_where 'resource-id="android:id/button1"'; fi
  sleep 1
done
"$adb" logcat -d -s Herdr | tail -3
[ -n "$said" ] || { echo "FAIL: nothing said it failed"; xml | tr '>' '\n' | grep -oE ' text="[^"]+"' | head -20; exit 1; }
[ "$(version)" = "$installed_before" ] || { echo "FAIL: installed $(version) signed with another key"; exit 1; }
snap other-key
echo "ok: refused, still $(version)"

echo "--- the real install"
"$adb" shell am start -W -n "$id/.MainActivity" >/dev/null
restart_with good
tap update-now
tap_text "Download"
done_install=""
for _ in $(seq 1 300); do
  [ "$(version)" = "$new_version" ] && { done_install=1; break; }
  screen=$(xml)
  if printf %s "$screen" | grep -qE 'resource-id="android:id/button1"'; then snap confirm; tap_where 'resource-id="android:id/button1"'; fi
  sleep 1
done
[ -n "$done_install" ] || { echo "FAIL: still $(version)"; exit 1; }
echo "ok: the system installed $new_version over $installed_before"

# The system ends the app and leaves the person on the home screen. The new version says it is installed, and the
# notification opens it.
said=""
for _ in $(seq 1 20); do "$adb" shell dumpsys notification --noredact | grep -qE "NotificationRecord.*pkg=$id .*tag=update" && { said=1; break; }; sleep 1; done
[ -n "$said" ] || { echo "FAIL: no notification that the app is updated"; exit 1; }
"$adb" shell cmd statusbar expand-notifications
sleep 2
snap updated-notification
tap_text "The app is updated"
wait_for "acme-api" 60
echo "ok: a notification says the app is updated, and opens it"
sleep 3
if xml | grep -qF 'update-banner'; then echo "FAIL: it still offers an update"; exit 1; fi
snap after
echo "ok: the pairing survived, and nothing more is offered"
echo PASS
