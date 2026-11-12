#!/bin/bash
# Before a CI job uploads its screenshots and logs, which anyone can download: every QR code on a picture painted over,
# and no pairing secret left in the folder, neither in any file's bytes nor in the words on a picture or video.
# A pairing secret is an invite (herdr1:…), a pairing link's #pair=… or a token=… value, and every line of each
# SECRET file given (the invite or link a test made; lines shorter than 8 characters are skipped).
# Usage: app/e2e/ci-shots-check.sh FOLDER [SECRET…]    exits 1 and names the file when it finds one
#        app/e2e/ci-shots-check.sh --self-test COMPANION
#        app/e2e/ci-shots-check.sh --self-test-words      only what OCR makes of a secret (part of --self-test)
#        app/e2e/ci-shots-check.sh --probe-grey N         measures, prints, never fails: see probe_grey
# --self-test makes a real pairing link with COMPANION (a state of its own in a temporary folder) and checks that the
# check finds it in a log, reads it from a QR code and leaves no code there, and finds it in a picture's words, and
# that a folder with none of it passes. A job runs it before the real check, so a check that finds nothing fails.
# Needs python3 with zxing-cpp and Pillow (shots-check.py), ffmpeg for videos, and tesseract (or a Mac's Vision).
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
py=${HM_SHOTS_PYTHON:-python3}
shapes='herdr[0-9]:[A-Za-z0-9_-]{16,}|#pair=[A-Za-z0-9_-]{8,}|token=[A-Za-z0-9_-]{8,}'
# What OCR makes of the same, matched with the spaces it reads taken out: it splits a long token with spaces at random
# (1 run in 10 here read "pair=…" as two words), reads : as ; and = as - (a Mac runner's tesseract read "#pair-…"),
# _ - as other marks, and letters as others or one more: a Linux runner read "#pairs=…" (and "hitp"), the app's help
# text came out as "herdri:". So: an invite's 1 may be i l | !, pair and token may be misread and have up to 2 marks
# more before the =, and anything with up to 6 marks between a # and an = counts too. Over the words read on every
# picture and video the public CI uploaded in one audit (285,000 lines), none of these matched: the nearest, the help
# text "herdr1:…http://…#pair=…", has only "…" after its =. misread_test keeps both kinds.
seen_shapes='[Hh]erdr[0-9il|!][:;][A-Za-z0-9_-]{16,}|[Pp][a@4][il1|!]r[^ =:;-]{0,2}[=:;-][^ ]{8,}|#[^ ]{0,6}[=:;-][^ ]{10,}|[Tt][o0]k[e3]n[^ =:;-]{0,2}[=:;-][^ ]{8,}'

# The numbers of the lines on standard input (words read on pictures, spaces taken out) that look like a secret.
loose() { grep -nE "$seen_shapes" | cut -d: -f1; }

check() {
  local folder=$1; shift
  [ -d "$folder" ] || { echo "FAIL: no folder $folder"; return 1; }
  "$py" "$here/shots-check.py" mask "$folder" || return 1
  local f bad
  for f in "$@"; do
    [ -s "$f" ] || { echo "FAIL: the secret file $f is empty"; return 1; }
    # The check itself first, as mac-e2e.yml does: it has to find each secret where it is.
    grep -E '.{8,}' "$f" | grep -qFf - "$f" || { echo "FAIL: the check finds nothing in $f itself"; return 1; }
    bad=$(grep -E '.{8,}' "$f" | grep -rlaFf - "$folder")
    [ -z "$bad" ] || { echo "FAIL: a pairing secret from $(basename "$f") is in $(echo $bad)"; return 1; }
  done
  bad=$(grep -rlaE "$shapes" "$folder")
  [ -z "$bad" ] || { echo "FAIL: something shaped like a pairing secret is in $(echo $bad)"; return 1; }
  local words
  words=$("$py" "$here/shots-check.py" words "$folder" | awk -F'\t' '{ gsub(/[ \t]/, "", $2); print $1 "\t" $2 }') || return 1
  # The words only: a file's own name (pair-en-US.png) is not on the picture.
  local hits
  hits=$(printf '%s\n' "$words" | cut -f2 | loose | tr '\n' ' ')
  bad=$(printf '%s\n' "$words" | awk -F'\t' -v hits="$hits" 'BEGIN { n = split(hits, h, " "); for (i = 1; i <= n; i++) at[h[i]] = 1 } NR in at { print $1 }' | sort -u)
  [ -z "$bad" ] || { echo "FAIL: a picture or video shows something shaped like a pairing secret: $(echo $bad)"; return 1; }
  if [ -n "$*" ]; then
    for f in "$@"; do
      bad=$(grep -E '.{8,}' "$f" | while IFS= read -r s; do printf '%s\n' "$words" | grep -F "$s" | cut -f1; done | sort -u)
      [ -z "$bad" ] || { echo "FAIL: a picture or video shows a pairing secret from $(basename "$f"): $(echo $bad)"; return 1; }
    done
  fi
  echo "ok: $folder has no pairing secret and no QR code ($(find "$folder" -type f | wc -l | tr -d ' ') files)"
}

misread_test() {
  # What OCR has made of a pairing link, invite or token (spaces taken out): each has to be found. And words the app
  # shows that are close to one but hold no secret: none may be.
  local misread=(
    'hitp://127.0.0.1:9/#pairs=hQbbX2k9Lr0aZ7Qm'
    'http://127.0.0.1:9/#pair-hQbbX2k9Lr0aZ7Qm'
    'herdri:Ab3dE5gH7jK9mN1pQ3sT5vW7'
    'token;Ab3dE5gH7jK9mN1p'
  ) benign=(
    'herdri:../http://../#paire=..'
    'herdr-anywherepair'
    '2.ShowapairingQRcode,thenscanit:'
    'Pairagain'
  ) w
  local bad=0
  for w in "${misread[@]}"; do [ -n "$(printf '%s\n' "$w" | loose)" ] || { echo "FAIL: self-test: a misread secret is not found: ${w:0:24}…"; bad=1; }; done
  for w in "${benign[@]}"; do [ -z "$(printf '%s\n' "$w" | loose)" ] || { echo "FAIL: self-test: words with no secret are taken for one: $w"; bad=1; }; done
  [ "$bad" = 0 ] && echo "ok: self-test of what OCR makes of a secret: ${#misread[@]} misread found, ${#benign[@]} near ones not"
  [ "$bad" = 0 ]
}

self_test() {
  local companion=$1 t
  t=$(mktemp -d)
  trap 'rm -rf "$t"' RETURN
  mkdir -p "$t/home" "$t/s"
  (umask 077; HOME="$t/home" XDG_CONFIG_HOME="$t/home/.config" "$companion" pair --state "$t/state" --url http://127.0.0.1:9 2>/dev/null |
    grep -o 'http://[^ ]*#pair=[A-Za-z0-9_-]*' | sed -n 1p > "$t/s/link")
  [ -s "$t/s/link" ] || { echo "FAIL: self-test: the companion printed no pairing link"; return 1; }
  local link; link=$(cat "$t/s/link")
  [ -n "${GITHUB_ACTIONS:-}" ] && echo "::add-mask::$link"
  local n=0 fail=0
  case_() { # name, expect (pass|fail), folder, secret files…
    local name=$1 want=$2 dir=$3; shift 3
    n=$((n + 1))
    if check "$dir" "$@" > "$t/out" 2>&1; then got=pass; else got=fail; fi
    if [ "$got" = "$want" ]; then echo "ok: self-test $name: $got"; else
      echo "FAIL: self-test $name: wanted $want, got $got"
      local said; said=$(cat "$t/out"); printf '%s\n' "${said//"$link"/<the link>}"
      said=$("$py" "$here/shots-check.py" words "$dir" 2>&1 | cut -f2-); echo "the words read on its pictures:"
      printf '%s\n' "${said//"$link"/<the link>}" | sed 's/^/  /'
      fail=1; fi
  }
  n=$((n + 1)); misread_test || fail=1
  mkdir -p "$t/log" "$t/qr" "$t/words" "$t/words-bare" "$t/clean"
  printf 'serve: phone opened %s\n' "$link" > "$t/log/serve.log"
  case_ "a log with the link" fail "$t/log" "$t/s/link"
  case_ "a log with the link, no secret file given" fail "$t/log"
  "$py" "$here/shots-check.py" sample "$t/sample" "$link" || return 1
  cp "$t/sample/qr.png" "$t/qr/"
  [ "$("$py" "$here/shots-check.py" qr "$t/qr/qr.png")" = "$link" ] || { echo "FAIL: self-test: the sample's QR code does not read back"; return 1; }
  case_ "a picture of the link's QR code" pass "$t/qr" "$t/s/link"
  [ -z "$("$py" "$here/shots-check.py" qr "$t/qr/qr.png")" ] && echo "ok: self-test: the QR code is painted over" ||
    { echo "FAIL: self-test: the QR code still reads after the check"; fail=1; }
  cp "$t/sample/words.png" "$t/words/"; cp "$t/sample/words.png" "$t/words-bare/"
  case_ "a picture showing the link" fail "$t/words" "$t/s/link"
  case_ "a picture showing the link, no secret file given" fail "$t/words-bare"
  mkdir -p "$t/words-grey"; cp "$t/sample/words-grey.png" "$t/words-grey/"
  case_ "a phone screen showing the link in a field's grey, no secret file given" fail "$t/words-grey"
  if [ -f "$t/sample/words.mp4" ]; then
    mkdir -p "$t/video"; cp "$t/sample/words.mp4" "$t/video/"
    case_ "a screen recording showing the link for 2 s, no secret file given" fail "$t/video"
  else echo "skip: self-test of a screen recording: no ffmpeg, so any video fails the check"; fi
  "$py" "$here/shots-check.py" sample "$t/sample2" "acme-api › tab 2" || return 1
  cp "$t/sample2/words.png" "$t/clean/"; printf 'serve: phone connected over binary\n' > "$t/clean/serve.log"
  case_ "a folder with none of it" pass "$t/clean" "$t/s/link"
  [ "$fail" = 0 ] && echo "PASS: self-test, $n cases" || { echo "FAIL: self-test"; return 1; }
}

# The self-test's phone screen with a link in a field's grey, for N new links: how many the check misses, with
# tesseract's threads as they come and with one, and how often two reads of one picture differ. A Linux runner's
# check once missed it and read it on the next try; the same Ubuntu, tesseract and Pillow here (arm64 and amd64)
# missed none of 100 and read each picture the same 20 times out of 20. With
# what tesseract was built for and the machine's CPU, to compare. The links are made up (port 9, a random token).
probe_grey() {
  local n=$1 t i link a b miss=0 miss1=0 differ=0
  t=$(mktemp -d)
  tesseract --version 2>&1 | sed 's/^/  /'
  echo "  cpus: $(nproc 2>/dev/null || sysctl -n hw.ncpu); $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null || sysctl -n machdep.cpu.brand_string 2>/dev/null)"
  echo "  avx512: $(grep -o -w 'avx512[a-z0-9_]*' /proc/cpuinfo 2>/dev/null | sort -u | tr '\n' ' ')"
  for i in $(seq 1 "$n"); do
    link="http://127.0.0.1:9/#pair=$("$py" -c 'import secrets; print(secrets.token_urlsafe(16))')"
    rm -rf "$t/s" "$t/g"; mkdir -p "$t/g"
    "$py" "$here/shots-check.py" sample "$t/s" "$link" > /dev/null || return 1
    cp "$t/s/words-grey.png" "$t/g/"
    a=$("$py" "$here/shots-check.py" words "$t/g" 2>/dev/null | cksum); b=$("$py" "$here/shots-check.py" words "$t/g" 2>/dev/null | cksum)
    [ "$a" = "$b" ] || differ=$((differ + 1))
    check "$t/g" > /dev/null 2>&1 && miss=$((miss + 1))
    OMP_THREAD_LIMIT=1 check "$t/g" > /dev/null 2>&1 && miss1=$((miss1 + 1))
  done
  rm -rf "$t"
  echo "probe: $n grey-field links: the check missed $miss (threads as they come) and $miss1 (one thread); two reads of one picture differed $differ times"
}

if [ "${1:-}" = --probe-grey ]; then
  probe_grey "${2:-20}"
elif [ "${1:-}" = --self-test-words ]; then
  misread_test
elif [ "${1:-}" = --self-test ]; then
  [ -x "${2:-}" ] || { echo "usage: $0 --self-test COMPANION"; exit 2; }
  self_test "$2"
else
  [ -n "${1:-}" ] || { echo "usage: $0 FOLDER [SECRET…]"; exit 2; }
  check "$@"
fi
