#!/bin/sh
# The app's nsh.aar: the nsh v0.3.0 release's, with the patches beside this script (each file says what it fixes)
# applied to its Java runtime. They change TextLayouts.java only: it is fetched at v0.3.0, checked by its sha256,
# patched, compiled against the release's own classes as nsh compiles it (Java 17), and its classes replace the
# release's in classes.jar. libnsh.so and every other class stay the release's. android/build.sh runs it.
# Usage: app/android/nsh-patches/patch-aar.sh <release nsh.aar> <out.aar>     (needs JAVA_HOME and ANDROID_HOME)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
in=${1:?release nsh.aar}; out=${2:?out.aar}
base=76fa8a7fed47ea51aea01fa85bb40960be15d984 # v0.3.0
file=crates/nsh-platform-android/java/com/longbridge/nsh/TextLayouts.java
sum=013f5ce2a39055ffc095dd0267900814a7d6208b692aa2a3a0a676062399b17f
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir -p "$work/src/$(dirname "$file")" "$work/classes"
curl -fsSL --retry 5 --retry-delay 5 "https://raw.githubusercontent.com/longbridge/nsh/$base/$file" -o "$work/src/$file"
got=$( (sha256sum 2>/dev/null || shasum -a 256) < "$work/src/$file" | cut -d' ' -f1)
[ "$got" = "$sum" ] || { echo "TextLayouts.java at v0.3.0 is not the one these patches are for: $got" >&2; exit 1; }
for p in "$here"/0*.patch; do
  touched=$(sed -n 's#^+++ b/##p' "$p" | sort -u)
  [ "$touched" = "$file" ] || { echo "$p changes more than $file: patch-aar.sh rebuilds only that file" >&2; exit 1; }
  (cd "$work/src" && git apply "$p")
done
# The newest platform's android.jar: the patches call APIs of Android 15 (35); older ones lack them.
jar=$(ls -d "$ANDROID_HOME"/platforms/android-* | sort -V | tail -1)/android.jar
unzip -q -o "$in" classes.jar -d "$work"
"$JAVA_HOME/bin/javac" --release 17 -Xlint:-options -encoding UTF-8 -classpath "$jar:$work/classes.jar" \
  -d "$work/classes" "$work/src/$file"
python3 -I - "$in" "$out" "$work/classes.jar" "$work/classes" <<'PY'
import io, sys, zipfile
from pathlib import Path
src, dst, jar, classes = sys.argv[1:]
prefix = 'com/longbridge/nsh/TextLayouts'
new = sorted(p for p in Path(classes).rglob('*.class'))
def entry(name, data, z):
    info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
    info.compress_type = zipfile.ZIP_DEFLATED
    z.writestr(info, data)
patched = io.BytesIO()
with zipfile.ZipFile(jar) as old, zipfile.ZipFile(patched, 'w') as z:
    names = [n for n in old.namelist() if not (n == prefix + '.class' or n.startswith(prefix + '$'))]
    gone = len(old.namelist()) - len(names)
    for n in names: entry(n, old.read(n), z)
    for p in new: entry(p.relative_to(classes).as_posix(), p.read_bytes(), z)
if not gone or not new or any(not p.relative_to(classes).as_posix().startswith(prefix) for p in new):
    raise SystemExit('patch-aar: unexpected classes: replaced %d with %s' % (gone, [p.name for p in new]))
with zipfile.ZipFile(src) as old, zipfile.ZipFile(dst + '.tmp', 'w') as z:
    for n in old.namelist():
        entry(n, patched.getvalue() if n == 'classes.jar' else old.read(n), z)
Path(dst + '.tmp').replace(dst)
PY
echo "patched $(ls "$here"/0*.patch | wc -l | tr -d ' ') into $out"
