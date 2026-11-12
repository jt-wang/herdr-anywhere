#!/bin/sh
# The files e2e/android-update.sh takes, made from this checkout: OLD.apk as android/build.sh builds it, NEW.apk the
# same app as version 9.9.9 build 999 under the same key (both built on this machine, so the same debug key signs them
# when no release key is set), OTHER-KEY.apk that one signed again with a key made here, and the companion as the old
# version and as 9.9.9. No release key is read.
# Usage: app/e2e/update-files.sh OUT [OLD.apk]
#   OLD.apk: an APK android/build.sh built on this machine, to skip building it again.
# Writes OUT/old.apk, new.apk, other.apk, old-companion, new-companion. Needs what android/build.sh needs, go and keytool.
set -eu
out=$1; old=${2:-}
app=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$out"
out=$(cd "$out" && pwd)
bt=$(ls -d "${ANDROID_HOME:-$HOME/Library/Android/sdk}"/build-tools/* | sort -V | tail -1)
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$app/nsh.json")

if [ -n "$old" ]; then cp "$old" "$out/old.apk"; else (cd "$app" && android/build.sh) && cp "$app/android/build/Herdr.apk" "$out/old.apk"; fi

cp "$app/nsh.json" "$out/nsh.json.keep"
trap 'cp "$out/nsh.json.keep" "$app/nsh.json"; rm -f "$out/nsh.json.keep"' EXIT
python3 - "$app/nsh.json" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
s = re.sub(r'"version": "[^"]+"', '"version": "9.9.9"', s, count=1)
s = re.sub(r'"build": \d+', '"build": 999', s, count=1)
open(p, 'w').write(s)
PY
(cd "$app" && android/build.sh)
cp "$app/android/build/Herdr.apk" "$out/new.apk"
cp "$out/nsh.json.keep" "$app/nsh.json"

rm -f "$out/other.p12"
keytool -genkeypair -keystore "$out/other.p12" -storetype PKCS12 -storepass otherkey -keypass otherkey -alias other \
  -keyalg RSA -keysize 2048 -validity 365 -dname "CN=Another key" >/dev/null 2>&1
cp "$out/new.apk" "$out/other.apk"
"$bt/apksigner" sign --ks "$out/other.p12" --ks-pass pass:otherkey "$out/other.apk"
rm -f "$out/other.p12" "$out/other.apk.idsig"

a=$("$bt/apksigner" verify --print-certs "$out/old.apk" | grep 'SHA-256 digest')
b=$("$bt/apksigner" verify --print-certs "$out/new.apk" | grep 'SHA-256 digest')
c=$("$bt/apksigner" verify --print-certs "$out/other.apk" | grep 'SHA-256 digest')
[ -n "$a" ] && [ "$a" = "$b" ] || { echo "FAIL: old.apk and new.apk are not signed by the same key"; exit 1; }
[ "$a" != "$c" ] || { echo "FAIL: other.apk is signed by the same key"; exit 1; }

cd "$app/../companion"
CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=$version" -o "$out/old-companion" ./cmd/herdr-anywhere
CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=9.9.9" -o "$out/new-companion" ./cmd/herdr-anywhere
echo "ok: $out has old.apk ($version) and new.apk (9.9.9) under one key, other.apk under another, and both companions"
