#!/bin/bash
# What the device suites need on a GitHub runner (Linux x64 or macOS Arm), each at a fixed version checked by its
# sha256. Tells the later steps where each is through $GITHUB_ENV and $GITHUB_PATH.
# Usage: app/e2e/ci/tools.sh core|nsh|zsh|shots|ios …
#   core   herdr-core, as HM_TEST_HC, and hc beside it on PATH as Herdr's installer puts it (a link to herdr-core)
#   nsh    nsh in ~/.nsh (ios/build.sh takes NativeShell from there), as NSH_HOME
#   zsh    zsh (the e2e terminals run zsh, as on a Mac)
#   shots  what ci-shots-check.sh needs: python with zxing-cpp and Pillow as HM_SHOTS_PYTHON, tesseract and ffmpeg
#   ios    xcodegen and idb (idb-companion and the fb-idb client)
# herdr-core and nsh are the versions computers.yml pins; update both files together.
set -euo pipefail
tmp=${RUNNER_TEMP:?run on a GitHub runner}
env_=${GITHUB_ENV:?}; path_=${GITHUB_PATH:?}
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) os=linux ;;
  Darwin/arm64) os=mac ;;
  *) echo "no tools for $(uname -s)/$(uname -m)"; exit 2 ;;
esac
if command -v sha256sum >/dev/null; then sum() { sha256sum -c -; }; else sum() { shasum -a 256 -c -; }; fi
get() { # url, file, sha256
  curl -fsSL --retry 5 --retry-delay 10 "$1" -o "$tmp/$2"
  echo "$3  $tmp/$2" | sum
}

core() {
  local v=0.1.18 f s
  if [ $os = linux ]; then f=herdr-core-linux-x86_64.tar.gz; s=5b2f97cb4b2a9d7a6b2cc5942628203ed9fbb7cc0c2b4b09ad8a6f5648e246b7
  else f=herdr-core-macos-aarch64.tar.gz; s=77d8af092e25ae650f4424d6dad27514f6916a11d807fb0752858cd27c21e4fc; fi
  get "https://releases.herdr.dev/core/$v/$f" "$f" "$s"
  mkdir -p "$tmp/herdr-core/bin"
  tar -xzf "$tmp/$f" -C "$tmp/herdr-core"
  ln -sf "$tmp/herdr-core/herdr-core" "$tmp/herdr-core/bin/hc"
  ln -sf "$tmp/herdr-core/herdr-core" "$tmp/herdr-core/bin/herdr-core"
  "$tmp/herdr-core/bin/hc" --version
  echo "HM_TEST_HC=$tmp/herdr-core/herdr-core" >> "$env_"
  echo "$tmp/herdr-core/bin" >> "$path_"
}

nsh() {
  local v=v0.3.0 f s
  if [ $os = linux ]; then f=nsh-linux-x86_64.tar.gz; s=539ecf9d76d156b5afa5a086212f23764bfc3945c7395a2119b73a3c66effed0
  else f=nsh-macos-arm64.tar.gz; s=34fdd2ec43d5e22986723e7633acf9223e365bd5bace1a502b5ae24fb75c266c; fi
  get "https://github.com/longbridge/nsh/releases/download/$v/$f" "$f" "$s"
  rm -rf "$HOME/.nsh" "$tmp/nsh"
  tar -xzf "$tmp/$f" -C "$tmp" && mv "$tmp/nsh" "$HOME/.nsh"
  "$HOME/.nsh/bin/nsh" --version
  echo "NSH_HOME=$HOME/.nsh" >> "$env_"
  echo "$HOME/.nsh/bin" >> "$path_"
}

zsh_() {
  if [ $os = linux ]; then sudo apt-get install -y -qq zsh >/dev/null; fi
  zsh --version
}

shots() {
  python3 -m venv "$tmp/shots-py"
  "$tmp/shots-py/bin/pip" install --disable-pip-version-check -q zxing-cpp==3.1.1 pillow==13.0.0
  echo "HM_SHOTS_PYTHON=$tmp/shots-py/bin/python" >> "$env_"
  # tesseract on both: a Mac runner's Vision read no word of the self-test's picture, which Vision on a Mac at a desk
  # reads. Homebrew has no versioned tesseract formula, so the runner image's formula is taken (no update) and a
  # major version other than 5 stops here.
  if [ $os = linux ]; then sudo apt-get install -y -qq tesseract-ocr ffmpeg >/dev/null
  else HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 brew install -q tesseract >/dev/null; fi
  v=$(tesseract --version 2>&1 | sed -n 1p); echo "$v"
  case $v in "tesseract 5."*) ;; *) echo "FAIL: tesseract is not 5.x: $v"; exit 1 ;; esac
  command -v ffmpeg >/dev/null || echo "no ffmpeg: a video in an upload fails the check"
}

ios() {
  [ $os = mac ] || { echo "ios tools are for a Mac"; exit 2; }
  get https://github.com/yonaskolb/XcodeGen/releases/download/2.45.4/xcodegen.zip xcodegen.zip \
    090ec29491aad50aec10631bf6e62253fed733c50f3aab0f5ffc86bc170bdbef
  rm -rf "$tmp/xcodegen" && unzip -q "$tmp/xcodegen.zip" -d "$tmp"
  echo "$tmp/xcodegen/bin" >> "$path_"
  "$tmp/xcodegen/bin/xcodegen" --version
  get https://github.com/meta/idb/releases/download/v1.1.8/idb-companion.universal.tar.gz idb-companion.tar.gz \
    3b72cc6a9a5b1a22a188205a84090d3a294347a846180efd755cf1a3c848e3e7
  tar -xzf "$tmp/idb-companion.tar.gz" -C "$tmp"
  echo "$tmp/idb-companion.universal/bin" >> "$path_"
  # fb-idb 1.1.7 needs Python 3.13 or older: on 3.14 asyncio.get_event_loop() raises before it does anything.
  python3 -c 'import sys; sys.exit(sys.version_info >= (3, 14))' || { echo "FAIL: idb needs Python 3.13 or older, not $(python3 --version)"; exit 1; }
  python3 -m venv "$tmp/idb-py"
  "$tmp/idb-py/bin/pip" install --disable-pip-version-check -q fb-idb==1.1.7 grpclib==0.4.8 protobuf==6.33.6 \
    aiofiles==25.1.0 treelib==1.8.0 h2==4.3.0 hpack==4.1.0 hyperframe==6.1.0 multidict==6.7.1
  echo "$tmp/idb-py/bin" >> "$path_"
}

for what in "$@"; do
  case $what in
    core) core ;; nsh) nsh ;; zsh) zsh_ ;; shots) shots ;; ios) ios ;;
    *) echo "unknown: $what"; exit 2 ;;
  esac
done
