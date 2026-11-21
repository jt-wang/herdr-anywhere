#!/usr/bin/env python3
"""check-public-history.py passes a history source-export writes and fails each way a push can break its rules."""
import os
import pathlib
import subprocess
import sys
import tempfile

CHECK = pathlib.Path(__file__).with_name("check-public-history.py").resolve()
NAME, MAIL = "Jingtao Wang", "7776499+jt-wang@users.noreply.github.com"
NOON = 1795262400  # 2026-11-21 12:00:00 UTC
SRC = "\n\nSource-Rev: " + "a" * 40


def run(repo, *args, env=None):
    return subprocess.run(args, cwd=repo, env={**os.environ, **(env or {})}, capture_output=True, text=True)


def commit(repo, msg, date=f"{NOON} +0000", name=NAME, mail=MAIL, cdate=None, cmail=MAIL):
    env = {"GIT_AUTHOR_NAME": name, "GIT_AUTHOR_EMAIL": mail, "GIT_COMMITTER_NAME": name if cmail != MAIL else NAME,
           "GIT_COMMITTER_EMAIL": cmail, "GIT_AUTHOR_DATE": date, "GIT_COMMITTER_DATE": cdate or f"{NOON} +0000"}
    r = run(repo, "git", "-c", "commit.gpgSign=false", "commit", "-q", "--allow-empty", "-m", msg, env=env)
    assert r.returncode == 0, r.stderr


def check(repo, *extra):
    r = run(repo, sys.executable, "-I", str(CHECK), "--author", f"{NAME} <{MAIL}>", *extra)
    return r.returncode, r.stdout


def repo_with(build):
    d = tempfile.TemporaryDirectory()
    run(d.name, "git", "init", "-q")
    commit(d.name, "first" + SRC)
    build(d.name)
    return d


def tag(when, annotated=True):
    def build(repo):
        env = {"GIT_COMMITTER_NAME": NAME, "GIT_COMMITTER_EMAIL": MAIL, "GIT_COMMITTER_DATE": when}
        a = ["-a", "-m", "v1.0.0"] if annotated else []
        run(repo, "git", "-c", "tag.gpgSign=false", "tag", *a, "v1.0.0", env=env)
    return build


CASES = [
    # (what, how the repository is built, extra arguments, expected to pass)
    ("noon UTC with Source-Rev, an annotated tag", tag(f"{NOON} +0000"), [], True),
    ("the same instant shown as +0900", lambda r: commit(r, "x" + SRC, f"{NOON} +0900"), [], False),
    ("13:00 UTC", lambda r: commit(r, "x" + SRC, f"{NOON + 3600} +0000"), [], False),
    ("committer at 02:00 UTC", lambda r: commit(r, "x" + SRC, cdate=f"{NOON - 36000} +0000"), [], False),
    ("another author", lambda r: commit(r, "x" + SRC, name="Amp", mail="amp@example.com"), [], False),
    ("no Source-Rev", lambda r: commit(r, "x"), [], False),
    ("Amp-Thread-ID beside Source-Rev", lambda r: commit(r, "x" + SRC + "\nAmp-Thread-ID: T-1"), [], False),
    ("Co-authored-by mid-message", lambda r: commit(r, "x\n\nCo-authored-by: Someone\n\nmore" + SRC), [], False),
    ("a private email in the body", lambda r: commit(r, "x\n\nwrite to me@example.com" + SRC), [], False),
    ("a credit line for a contributor", lambda r: commit(r, "x\n\nContributed by @someone in #3." + SRC), [], True),
    ("tagger at +0900", tag(f"{NOON} +0900"), [], False),
    ("a lightweight tag", tag(f"{NOON} +0000", annotated=False), [], False),
    ("an upstream author passed with --upstream",
     lambda r: commit(r, "upstream work", f"{NOON + 77} +0300", "Up", "up@example.com", cmail="up@example.com"),
     ["--upstream", "up@example.com"], True),
]


def main():
    failures = []
    for what, build, extra, ok in CASES:
        with repo_with(build) as repo:
            code, out = check(repo, *extra)
            if (code == 0) != ok:
                failures.append(f"{what}: exit {code}\n{out}")
    for f in failures:
        print("FAIL", f)
    print(f"{len(CASES) - len(failures)}/{len(CASES)} passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
