#!/usr/bin/env python3
"""A stand-in for Claude Code in a terminal, for e2e/android-chat.sh and e2e/chat-demo.sh: it reports to herdr-core as
Claude Code does (OSC 7501, app=claude-code), writes the pid file Claude Code writes (sessions/<pid>.json under
CLAUDE_CONFIG_DIR), and for each line typed appends the next turn of transcripts that Claude Code itself wrote
(companion/internal/chat/claude/testdata/claude-code/<version>/*.jsonl, made against a fake API), with the typed words
as the prompt. A turn another program starts (a background task's notice) follows the one before it at once.
--unreadable adds a line in a format no reader knows, as a new Claude Code version might write.
Usage: fake-claude.py [--unreadable] FIXTURE..."""
import json, os, signal, subprocess, sys, time, uuid

args = sys.argv[1:]
unreadable = "--unreadable" in args
fixtures = [a for a in args if a != "--unreadable"]
cfg = os.environ["CLAUDE_CONFIG_DIR"]
version = os.path.basename(os.path.dirname(fixtures[0]))
sid, cwd, pid = str(uuid.uuid4()), os.getcwd(), os.getpid()

def report(state):
    sys.stdout.write("\033]7501;state=%s:app=claude-code\033\\" % state)
    sys.stdout.flush()

def by_person(d):
    return d.get("turnOrigin") in ("human", "sdk")

# The turns: from each prompt (a user line with turnPosition) up to the next one; what comes before goes with the first.
turns, head = [], []
for fixture in fixtures:
    for raw in open(fixture):
        d = json.loads(raw)
        if d.get("type") == "user" and d.get("turnPosition"):
            turns.append([d])
        elif turns:
            turns[-1].append(d)
        else:
            head.append(d)

start = subprocess.run(["ps", "-o", "lstart=", "-p", str(pid)], env={**os.environ, "TZ": "UTC"}, capture_output=True, text=True).stdout.strip()
os.makedirs(os.path.join(cfg, "sessions"), exist_ok=True)
pidfile = os.path.join(cfg, "sessions", "%d.json" % pid)
json.dump({"pid": pid, "sessionId": sid, "cwd": cwd, "procStart": start, "version": version, "kind": "interactive", "entrypoint": "cli"}, open(pidfile, "w"))
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
record = os.path.join(cfg, "projects", cwd.replace("/", "-").replace(".", "-"), sid + ".jsonl")
fresh = {}  # a replay's lines get new ids, so a turn played twice is two turns

def write(d, said=None):
    d = json.loads(json.dumps(d).replace("/work/acme", cwd))
    if d.get("parentUuid"):
        d["parentUuid"] = fresh.get(d["parentUuid"], d["parentUuid"])
    if d.get("uuid"):
        fresh[d["uuid"]] = str(uuid.uuid4())
        d["uuid"] = fresh[d["uuid"]]
    if "sessionId" in d:
        d["sessionId"] = sid
    if said is not None:
        c = d["message"]["content"]
        if isinstance(c, str):
            d["message"]["content"] = said
        else:
            for b in c:
                if b.get("type") == "text":
                    b["text"] = said
    d["timestamp"] = time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime())
    with open(record, "a") as f:
        f.write(json.dumps(d) + "\n")

def play(lines, said):
    for i, d in enumerate(lines):
        write(d, said if i == 0 else None)
        time.sleep(0.4)
    reply = [b["text"] for d in lines if d.get("type") == "assistant" for b in d["message"]["content"] if b.get("type") == "text"]
    sys.stdout.write("\r\n" + (reply[-1] if reply else "") + "\r\n")

try:
    report("idle")
    n = 0
    while True:
        sys.stdout.write("\r\n> ")
        sys.stdout.flush()
        said = sys.stdin.readline()
        if not said:
            break
        said = said.strip()
        if not said:
            continue
        report("working")
        os.makedirs(os.path.dirname(record), exist_ok=True)
        if n == 0:
            for d in head:
                write(d)
        play(turns[n % len(turns)], said)
        n += 1
        while not by_person(turns[n % len(turns)][0]):
            play(turns[n % len(turns)], None)
            n += 1
        if unreadable:
            with open(record, "a") as f:
                f.write(json.dumps({"type": "assistant", "uuid": str(uuid.uuid4()), "message": {"role": "assistant", "content": [{"type": "hologram"}], "stop_reason": "end_turn"}}) + "\n")
        report("done")
finally:
    os.path.exists(pidfile) and os.remove(pidfile)
