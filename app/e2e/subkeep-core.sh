# Sourced by e2e/android-subkeep.sh and e2e/ios-subkeep.sh (sh): the computer side of a sub-agent kept as a tab. An
# isolated herdr-core (its own session and folders under WORK and RUN) with an agent (e2e/fake-claude, as claude) in the
# space spsk, and two sub-agents of it: one for the light run, one for the dark. Fake data only.
# Usage: . e2e/subkeep-core.sh
#        sk_start WORK RUN       builds the agent into WORK/bin, starts the core and the three; sets sk_kid1, sk_kid2
#        sk COMMAND…             runs COMMAND against that core (as e2e/ios-picture.sh's L)
#        sk_tabs KID             how many tabs of the shared layout (the herdr-app doc) show KID
#        sk_state KID            KID's state in herdr-core (live while it runs)
#        sk_stop                 stops the core
#        sh e2e/subkeep-core.sh --self-test     the above on this computer, no phone: two sub-agents of the agent,
#                                live, and no tab for either in a layout no phone wrote
sk() {
  HC_SESSION="hmsk-$$" HC_SOCKET="" HOME="$sk_work/fakehome" SHELL=/bin/sh XDG_STATE_HOME="$sk_work/state" \
    XDG_CONFIG_HOME="$sk_work/config" XDG_DATA_HOME="$sk_work/data" XDG_RUNTIME_DIR="$sk_run" "$@"
}
sk_hc=${HM_TEST_HC:-$(command -v hc || echo "$HOME/.local/bin/hc")}
sk_start() {
  sk_work=$1 sk_run=$2
  sk_e2e=$(cd "$(dirname "$0")" && pwd)
  mkdir -p "$sk_work/fakehome/src/acme-api" "$sk_work/state" "$sk_work/config" "$sk_work/data" "$sk_work/bin"
  (cd "$sk_e2e/fake-claude" && GO111MODULE=off go build -o "$sk_work/bin/claude" main.go) || { echo "FAIL: building e2e/fake-claude"; return 1; }
  sk_parent=$(sk "$sk_hc" run --bg --keep --json --cwd "$sk_work/fakehome/src/acme-api" --label herdr.space=spsk --label herdr.tab=tbsk \
    -- "$sk_work/bin/claude" | python3 -c 'import json,sys; print(json.load(sys.stdin)["address"])') || { echo "FAIL: starting the agent"; return 1; }
  sk_kid1=$(sk_sub "helper for the light run") && sk_kid2=$(sk_sub "helper for the dark run") || { echo "FAIL: starting the sub-agents"; return 1; }
  echo "agent $sk_parent, sub-agents $sk_kid1 (light) and $sk_kid2 (dark)"
}
sk_sub() { # NAME: a sub-agent of the agent; prints its address
  sk "$sk_hc" run --bg --json --cwd "$sk_work/fakehome/src/acme-api" --parent "$sk_parent" -- sh -c "echo '$1'; sleep 3600" |
    python3 -c 'import json,sys; t=json.load(sys.stdin); print(t["address"]) if t.get("parent") else sys.exit("no parent: %s" % t)'
}
sk_tabs() {
  sk python3 -c '
import json, signal, subprocess, sys
signal.alarm(20)
p = subprocess.Popen([sys.argv[1], "connect"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": 1, "method": "doc.get", "params": {"name": "herdr-app"}}) + "\n")
p.stdin.flush()
while True:
    r = json.loads(p.stdout.readline())
    if r.get("id") == 1: break
p.stdin.close(); p.wait()
want = "\"terminal\":\"%s\"" % sys.argv[2]
v = r["result"]["value"] or {}
print(sum(1 for s in v.get("spaces", []) for t in s.get("tabs", []) if want in json.dumps(t.get("root"), separators=(",", ":"))))
' "$sk_hc" "$1"
}
sk_state() { sk "$sk_hc" get "$1" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["state"])'; }
sk_stop() { [ -z "${sk_work:-}" ] || sk "$sk_hc" server stop >/dev/null 2>&1 || true; }

if [ "${1:-}" = --self-test ]; then
  set -eu
  w=$(mktemp -d); r=$(mktemp -d /tmp/hmXXXX)
  trap 'sk_stop; rm -rf "$w" "$r"' EXIT
  sk_start "$w" "$r"
  fail=0
  for k in "$sk_kid1" "$sk_kid2"; do
    s=$(sk_state "$k"); n=$(sk_tabs "$k")
    [ "$s" = live ] && [ "$n" = 0 ] || { echo "FAIL: subkeep-core self-test: $k is $s with $n tabs (wanted live, 0)"; fail=1; }
  done
  [ "$sk_kid1" != "$sk_kid2" ] || { echo "FAIL: subkeep-core self-test: one sub-agent for both runs"; fail=1; }
  # A layout with the first in two tabs (as an earlier version left it) and the agent in one: sk_tabs counts the two.
  sk python3 -c '
import json, subprocess, sys
tab = lambda i, t: {"id": i, "root": {"id": "p" + i, "type": "block", "content": {"terminal": t}}}
v = {"spaces": [{"id": "spsk", "tabs": [tab("tbsk", sys.argv[2]), tab("tbk1", sys.argv[3]), tab("tbk2", sys.argv[3])]}]}
p = subprocess.Popen([sys.argv[1], "connect"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": 1, "method": "doc.set", "params": {"name": "herdr-app", "version": 0, "value": v}}) + "\n")
p.stdin.flush(); r = p.stdout.readline(); p.stdin.close(); p.wait()
"error" in json.loads(r) and sys.exit("doc.set: " + r)
' "$sk_hc" "$sk_parent" "$sk_kid1"
  n1=$(sk_tabs "$sk_kid1"); n2=$(sk_tabs "$sk_kid2")
  [ "$n1" = 2 ] && [ "$n2" = 0 ] || { echo "FAIL: subkeep-core self-test: in a layout with $sk_kid1 in two tabs, sk_tabs gave $n1 and $n2 (wanted 2, 0)"; fail=1; }
  [ "$fail" = 0 ] && echo "PASS: subkeep-core self-test" || exit 1
fi
