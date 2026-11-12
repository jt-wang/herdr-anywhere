#!/bin/sh
# The phone's language on a real device: for each language, Android's per-app language set and the app reopened, the
# pairing screen says its first line in that language and the notification channels are named in it.
# Usage: ANDROID_SERIAL=emulator-5554 e2e/android-locale.sh [apk] [screenshots-dir]
set -eu
: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to use}"
app=$(cd "$(dirname "$0")/.." && pwd)
apk=${1:-$app/android/build/Herdr.apk}
shots=${2:-}
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
. "$(dirname "$0")/adb-note.sh"; adb_note "$adb"  # a failed adb call is written down and shown if the suite fails
trap 'adb_exit $?' EXIT
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$app/nsh.json")
"$adb" install -r "$apk" >/dev/null
"$adb" shell pm clear "$id" >/dev/null # unpaired, so it opens on the pairing screen
"$adb" shell pm grant "$id" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
fail=0
while IFS='|' read -r tag title channel; do
  "$adb" shell cmd locale set-app-locales "$id" --locales "$tag" </dev/null
  "$adb" shell am force-stop "$id" </dev/null
  "$adb" shell am start -n "$id/.MainActivity" </dev/null >/dev/null
  seen=''
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    sleep 1
    "$adb" shell uiautomator dump /sdcard/ui.xml </dev/null >/dev/null 2>&1 || continue
    seen=$("$adb" shell cat /sdcard/ui.xml </dev/null)
    case $seen in *"$title"*) break ;; esac
  done
  case $seen in
    *"$title"*) echo "ok: $tag says \"$title\"" ;;
    *) echo "FAIL: $tag doesn't say \"$title\""; fail=1 ;;
  esac
  if "$adb" shell dumpsys notification --noredact </dev/null | grep -qF "mName=$channel,"; then
    echo "ok: $tag channel \"$channel\""
  else
    echo "FAIL: $tag has no channel \"$channel\""; fail=1
  fi
  if [ -n "$shots" ]; then "$adb" exec-out screencap -p > "$shots/pair-$tag.png" </dev/null; fi
done <<'LANGS'
zh-CN|手机上的 agent|Agent 在等你
zh-TW|手機上的 agent|Agent 在等你
es-ES|Tus agentes, en tu teléfono|Un agente te necesita
ko-KR|휴대폰으로 만나는 에이전트|에이전트가 응답을 기다릴 때
ja-JP|エージェントをスマホで|エージェントが対応待ちのとき
ru-RU|Ваши агенты на телефоне|Агент ждёт вас
en-US|Your agents, on your phone|An agent needs you
LANGS
"$adb" shell cmd locale set-app-locales "$id" --locales "" </dev/null || true
[ "$fail" = 0 ] && echo "PASS: seven languages on the pairing screen and the notification channels"
exit $fail
