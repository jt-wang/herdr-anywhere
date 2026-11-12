#!/bin/bash
# An Android emulator on a GitHub Linux runner, the same phone as mac-e2e.yml's and computers.yml's: API 35, Google
# APIs, x86_64, a Pixel 6 screen, drawn with swangle. Started in the background so it stays up across steps; then
# animations off, error dialogs hidden (computers.yml says why), and the screen read back as 1080x2400 at 420 dpi,
# light, font scale 1, before any suite runs: every suite once failed on a phone whose screen another run had changed.
# Usage: app/e2e/ci/android-up.sh    then ANDROID_SERIAL=emulator-5554
# HM_EMULATOR_FEATURE (devices.yml's quiet trial): an emulator feature to turn off, as -NAME (-WiFiPacketStream: no
# netsim WiFi stream; -Wifi: no WiFi). NAME must be one this emulator lists (emulator/lib/advancedFeatures.ini), and
# the emulator's log must then lack its netsim WiFi line, so a feature that changed nothing fails here, not later.
set -euo pipefail
: "${ANDROID_HOME:?}" "${RUNNER_TEMP:?}"
# android-emulator-runner's setup for hardware acceleration on Linux runners.
echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm4all.rules >/dev/null
sudo udevadm control --reload-rules
sudo udevadm trigger --name-match=kvm
image="system-images;android-35;google_apis;x86_64"
bin="$ANDROID_HOME/cmdline-tools/latest/bin"
# yes is cut off once the licenses are taken; with pipefail its SIGPIPE would fail the step.
{ yes || true; } | "$bin/sdkmanager" --licenses >/dev/null
"$bin/sdkmanager" --install platform-tools emulator "$image" >/dev/null
export ANDROID_AVD_HOME="$HOME/.android/avd"
echo "ANDROID_AVD_HOME=$ANDROID_AVD_HOME" >> "${GITHUB_ENV:-/dev/null}"
mkdir -p "$ANDROID_AVD_HOME"
echo no | "$bin/avdmanager" create avd --force -n hm -k "$image" -d pixel_6 -p "$ANDROID_AVD_HOME/hm.avd" >/dev/null
feature=()
if [ -n "${HM_EMULATOR_FEATURE:-}" ]; then
  name=${HM_EMULATOR_FEATURE#-}
  grep -qE "^${name} = " "$ANDROID_HOME/emulator/lib/advancedFeatures.ini" ||
    { echo "FAIL: this emulator ($("$ANDROID_HOME/emulator/emulator" -version 2>/dev/null | head -1)) has no feature $name"; exit 1; }
  feature=(-feature "$HM_EMULATOR_FEATURE")
fi
nohup "$ANDROID_HOME/emulator/emulator" -avd hm -no-window -gpu swangle -noaudio -no-boot-anim -no-snapshot "${feature[@]}" \
  >"$RUNNER_TEMP/emulator.log" 2>&1 &
adb="$ANDROID_HOME/platform-tools/adb"
booted() { [ "$("$adb" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = 1 ]; }
end=$(($(date +%s) + 900))
until booted; do
  [ "$(date +%s)" -lt "$end" ] || { echo "FAIL: the emulator did not boot in 900 s"; "$adb" devices; tail -40 "$RUNNER_TEMP/emulator.log"; exit 1; }
  sleep 5
done
netsim=$(grep -c "Successfully initialized netsim WiFi" "$RUNNER_TEMP/emulator.log" || true)
echo "emulator: $("$ANDROID_HOME/emulator/emulator" -version 2>/dev/null | head -1); feature off: ${HM_EMULATOR_FEATURE:-none}; netsim WiFi in its log: ${netsim} line(s)"
if [ -n "${HM_EMULATOR_FEATURE:-}" ] && [ "$netsim" != 0 ]; then
  echo "FAIL: with $HM_EMULATOR_FEATURE the emulator still started netsim WiFi: the feature changed nothing measured here"; exit 1
fi
# From here on (not while it boots, when adb sees it come and go), a note each time adb loses the phone, uploaded
# with the job's files (android-watch.sh).
here=$(cd "$(dirname "$0")" && pwd)
"$here/android-watch.sh" --self-test
sh "$here/../adb-note.sh" --self-test
nohup "$here/android-watch.sh" "$adb" "${OUT:-$RUNNER_TEMP/out}/emulator-lost" "$RUNNER_TEMP/emulator.log" "$RUNNER_TEMP/logcat.txt" \
  >/dev/null 2>&1 &
# A minute for the phone to settle, as the Mac's release runs wait: without it the first suite after boot failed to
# pair or to show its terminal in three jobs, and the suites after it in the same job passed.
sleep 60
for s in window_animation_scale transition_animation_scale animator_duration_scale; do "$adb" shell settings put global $s 0; done
"$adb" shell settings put global hide_error_dialogs 1
"$adb" shell cmd uimode night no >/dev/null
"$adb" shell settings put system font_scale 1.0
size=$("$adb" shell wm size | tr -d '\r' | tail -1); dpi=$("$adb" shell wm density | tr -d '\r' | tail -1)
night=$("$adb" shell cmd uimode night | tr -d '\r'); font=$("$adb" shell settings get system font_scale | tr -d '\r')
echo "booted: Android $("$adb" shell getprop ro.build.version.release | tr -d '\r'), $("$adb" shell getprop ro.build.fingerprint | tr -d '\r'); $size, $dpi, $night, font_scale $font"
[ "$size" = "Physical size: 1080x2400" ] && [ "$dpi" = "Physical density: 420" ] && [ "$night" = "Night mode: no" ] && [ "$font" = 1.0 ] ||
  { echo "FAIL: the screen is not 1080x2400 at 420 dpi, light, font scale 1"; exit 1; }
# What keyboard this image has: android-typing.sh needs Gboard.
echo "keyboards: $("$adb" shell ime list -s | tr -d '\r' | tr '\n' ' ')"
