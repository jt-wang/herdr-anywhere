#!/bin/sh
# A picture's path in a terminal on another machine, asked for as the phone asks (GET /api/image with the terminal's
# box/tN address): the Companion reads it there through a real hc, over the connection hc keeps to the machine. The
# other machine is a container (no network, no host folders), reached with `hc machine add --via "docker exec"`; the
# hc here runs in a fake home. Each reply is checked: the picture's bytes and full path, and every refusal (outside,
# symlink out, symlinked folder out, not a picture, a folder, over 8 MB, missing, unknown terminal, the machine gone).
# The path comes from the phone: paths written to run a command there ($( ), ;, quotes, backticks, a line break, a
# leading -) are each asked for, as real files and as missing ones, and must run nothing, neither on that machine nor
# here: the marker file they would touch must not appear. Fake data only.
# Usage: e2e/remote-picture.sh COMPANION [IMAGE…]   (default image alpine:latest; each image is one machine's sh)
set -eu
. "$(dirname "$0")/autostart-guard.sh"; autostart_guard remote-picture # a login entry of its own, never the person's
bin=$1; shift
[ $# -gt 0 ] || set -- alpine:latest
app=$(cd "$(dirname "$0")/.." && pwd)
hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
docker=$(command -v docker)
port=7795; while nc -z 127.0.0.1 "$port" 2>/dev/null; do port=$((port + 1)); done
work=$(mktemp -d); run=$(mktemp -d /tmp/hmXXXX); state=$(mktemp -d /tmp/hmtcXXXX)
box=hmpicture-$$
# What an injected command would make: "cd;touch $mark" makes it in the home of whatever shell ran it (a file name
# holds no "/", so the command can't name a folder).
mark=hmpwned-$$
mkdir -p "$work/home" "$work/state" "$work/config" "$work/data" "$work/files"
: > "$work/home/.zshrc"
python3 -c 'import base64,re,sys; d=re.search(r"base64,([A-Za-z0-9+/=]+)", open(sys.argv[1]).read()).group(1); open(sys.argv[2],"wb").write(base64.b64decode(d))' \
  "$app/fixtures/picture.ts" "$work/files/shot.png"
# Every hc call, the Companion's too, sees this home only; docker keeps the caller's own settings to find its daemon.
dockerhost=${DOCKER_HOST:-$("$docker" context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)}
. "$app/e2e/companion.sh"  # companion_start, companion_stop
L() {
  env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin:"$(dirname "$docker")" HOME="$work/home" HERDR_ANYWHERE_AUTOSTART_NAME="$HERDR_ANYWHERE_AUTOSTART_NAME" SHELL=/bin/sh DOCKER_HOST="$dockerhost" \
    HC_SESSION="hmpicture-$$" HC_SOCKET="" XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/config" \
    XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$run" "$@"
}
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e # every step runs, even after one fails
  # The Companion this test started must be gone, or it outlives the test.
  companion_stop || status=1
  L "$hc" server stop >/dev/null 2>&1
  "$docker" rm -f "$box" >/dev/null 2>&1
  rm -rf "$work" "$run" "$state"
  autostart_guard_end || exit 1
  exit "$status"
}
trap cleanup EXIT; trap 'cleanup; exit 130' INT; trap 'cleanup; exit 143' TERM HUP
failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }

companion_start "$work/serve.log" L "$bin" serve --listen "127.0.0.1:$port" --hc "$hc" --state "$state" || exit 1
code=$(L "$bin" pair --url "http://127.0.0.1:$port" --state "$state" 2>/dev/null | grep -o '#pair=[A-Za-z0-9_-]*' | head -1 | cut -d= -f2)
token=$(curl -s -X POST "http://127.0.0.1:$port/api/pair" -d "{\"code\":\"$code\",\"device\":\"e2e\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
[ -n "$token" ] || { echo "FAIL: no token"; exit 1; }

for image in "$@"; do
  echo "== $image"
  "$docker" rm -f "$box" >/dev/null 2>&1 || true
  "$docker" run -d --name "$box" --network none --entrypoint sleep "$image" infinity >/dev/null
  # The machine's files: a project with a picture, a text file named .png, a folder named .png, one over 8 MB, a
  # symlink in and one out, a symlinked folder leading out, and a picture outside home and temp.
  "$docker" exec "$box" sh -c 'mkdir -p "$HOME/proj/sub" "$HOME/proj/dir.png" /srv/away && printf "just text\n" > "$HOME/proj/notes.png" &&
    ln -s ../shot.png "$HOME/proj/sub/up.png" && ln -s /srv/away/secret.png "$HOME/proj/out.png" && ln -s /srv/away "$HOME/proj/away"'
  "$docker" cp "$work/files/shot.png" "$box:/root/proj/shot.png"
  "$docker" cp "$work/files/shot.png" "$box:/srv/away/secret.png"
  "$docker" exec "$box" sh -c 'cat "$HOME/proj/shot.png" > "$HOME/proj/huge.png"; head -c 9437184 /dev/zero >> "$HOME/proj/huge.png"'
  L "$hc" machine rm box >/dev/null 2>&1 || true
  L "$hc" machine add --via "$docker exec -i $box sh -c" --name box --install --json > "$work/add.json" 2>&1 ||
    { echo "FAIL: hc machine add: $(cat "$work/add.json")"; failed=1; continue; }
  L "$hc" run --on box --bg --keep --cwd /root/proj -- sh >/dev/null
  t=$(L "$hc" ls --json | python3 -c 'import json,sys; d=json.load(sys.stdin); ts=d if isinstance(d,list) else d.get("terminals",[]); print([x["address"] for x in ts if x["address"].startswith("box/")][-1])')
  L "$hc" send "$t" "cd sub" >/dev/null
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    cwd=$(L "$hc" get "$t" --json | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("foreground") or {}).get("cwd") or "")')
    [ "$cwd" = /root/proj/sub ] && break; sleep 0.5
  done
  [ "$cwd" = /root/proj/sub ] && say "ok: herdr-core says $t is in /root/proj/sub after cd" || say "FAIL: $t's folder is '$cwd'"
  # ask PATH TERMINAL → "CODE WHY PATH SAME" (SAME: the bytes are shot.png's)
  ask() {
    curl -s -G "http://127.0.0.1:$port/api/image" -H "Authorization: Bearer $token" --data-urlencode "path=$1" \
      --data-urlencode "terminal=$2" -o "$work/reply.json" -w '%{http_code}' > "$work/code"
    python3 - "$work/reply.json" "$work/files/shot.png" "$(cat "$work/code")" <<'PY'
import base64, json, sys
r = json.load(open(sys.argv[1])); u = r.get("url", "")
same = u.startswith("data:image/png;base64,") and base64.b64decode(u.split(",", 1)[1]) == open(sys.argv[2], "rb").read()
print(sys.argv[3], r.get("why", "-"), r.get("path", "-"), "same" if same else "-")
PY
  }
  expect() { # PATH WANT…
    p=$1; shift; got=$(ask "$p" "$t")
    [ "$got" = "$*" ] && say "ok: $p → $*" || say "FAIL: $p → $got, want $*"
  }
  expect "shot.png" 404 missing /root/proj/sub/shot.png -
  expect "../shot.png" 200 - /root/proj/shot.png same
  expect "up.png" 200 - /root/proj/sub/up.png same
  expect "~/proj/shot.png" 200 - /root/proj/shot.png same
  expect "/root/proj/shot.png" 200 - /root/proj/shot.png same
  expect "/srv/away/secret.png" 403 outside /srv/away/secret.png -
  expect "~/../srv/away/secret.png" 403 outside /srv/away/secret.png -
  expect "~/proj/out.png" 403 outside /root/proj/out.png -
  expect "~/proj/away/secret.png" 403 outside /root/proj/away/secret.png -
  expect "~/proj/notes.png" 415 not_image /root/proj/notes.png -
  expect "~/proj/dir.png" 415 not_image /root/proj/dir.png -
  expect "~/proj/huge.png" 413 too_big /root/proj/huge.png -
  got=$(ask "a.png" "box/t999"); case $got in "404 no_terminal "*) say "ok: an unknown terminal → $got";; *) say "FAIL: an unknown terminal → $got";; esac
  # Paths written to run a command: each must come back as the picture (a real file of that name) or a refusal, and
  # nothing may run. docker exec passes argv as given, so these names are made without a shell.
  for name in "\$(cd;touch $mark).png" "a;cd;touch $mark;.png" "it's 'quoted'.png" "\`cd;touch $mark\`.png" "\"\$(cd;touch $mark)\".png" \
    "a b|cd;touch $mark&.png" "-rf.png" "--help.png"; do
    "$docker" exec "$box" cp /root/proj/shot.png "/root/proj/sub/$name"
    expect "$name" 200 - "/root/proj/sub/$name" same
  done
  expect "\$(cd;touch $mark)-gone.png" 404 missing "/root/proj/sub/\$(cd;touch $mark)-gone.png" -
  expect "\`cd;touch $mark\`-gone.png" 404 missing "/root/proj/sub/\`cd;touch $mark\`-gone.png" -
  expect "~/proj/sub/\$(cd;touch $mark).png" 200 - "/root/proj/sub/\$(cd;touch $mark).png" same
  # The check the tap and the underline use: every path at once, in one call there, the same answers without the
  # bytes. A picture too big to send is a picture.
  python3 - "$mark" > "$work/check.json" <<'PY'
import json, sys
m = sys.argv[1]
want = [["../shot.png", True, ""], ["up.png", True, ""], ["shot.png", False, "missing"], ["~/proj/notes.png", False, "not_image"],
  ["~/proj/dir.png", False, "not_image"], ["~/proj/out.png", False, "outside"], ["/srv/away/secret.png", False, "outside"],
  ["~/proj/huge.png", True, "too_big"], ["$(cd;touch %s).png" % m, True, ""], ["`cd;touch %s`-gone.png" % m, False, "missing"],
  ["a;cd;touch %s;.png" % m, True, ""], ["--help.png", True, ""], ["a\ntouch %s\n.png" % m, False, "path"]]
print(json.dumps({"want": want}))
PY
  python3 -c 'import json,sys; w=json.load(open(sys.argv[1]))["want"]; print(json.dumps({"terminal": sys.argv[2], "paths": [x[0] for x in w]}))' "$work/check.json" "$t" > "$work/ask.json"
  ms=$(python3 -c 'import time; print(int(time.time()*1000))')
  curl -s -X POST "http://127.0.0.1:$port/api/image/check" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
    --data-binary @"$work/ask.json" -o "$work/checked.json" -w '%{http_code}' > "$work/code"
  ms=$(( $(python3 -c 'import time; print(int(time.time()*1000))') - ms ))
  python3 - "$work/check.json" "$work/checked.json" "$(cat "$work/code")" "$ms" > "$work/check.out" <<'PY'
import json, sys
want = json.load(open(sys.argv[1]))["want"]
got = json.load(open(sys.argv[2])).get("paths") or []
if sys.argv[3] != "200" or len(got) != len(want):
    print("FAIL: check → %s, %d answers for %d paths" % (sys.argv[3], len(got), len(want))); sys.exit()
bad = [(w[0], g) for w, g in zip(want, got) if (g.get("asked"), g.get("picture"), g.get("why", "")) != (w[0], w[1], w[2]) or "url" in g]
for p, g in bad: print("FAIL: check %r → %r" % (p, g))
if not bad: print("ok: check: %d paths, one call, every answer as /api/image's, no bytes (%s ms)" % (len(want), sys.argv[4]))
PY
  while IFS= read -r l; do say "$l"; done < "$work/check.out" # not in a pipe: say marks the run failed
  got=$(ask "$(printf 'a\ntouch %s\n.png' "$mark")" "$t"); case $got in "400 path "*) say "ok: a line break in the path → 400 path";; *) say "FAIL: a line break in the path → $got";; esac
  if "$docker" exec "$box" test -e "/root/$mark" || [ -e "$work/home/$mark" ] || [ -e "$HOME/$mark" ]; then
    say "FAIL: a path ran a command: $mark was made"; rm -f "$work/home/$mark" "$HOME/$mark"
  else say "ok: no path ran a command (no $mark in that machine's home or here)"; fi

  # The machine goes away: the terminal is still listed, the picture can't be fetched.
  "$docker" rm -f "$box" >/dev/null
  got=$(ask "~/proj/shot.png" "$t"); case $got in "502 unreachable "*) say "ok: the machine gone → $got";; *) say "FAIL: the machine gone → $got";; esac
done
[ $failed = 0 ] && echo "PASS" || { echo "FAILED"; tail -5 "$work/serve.log"; exit 1; }
