#!/bin/sh
# A run step in .github/workflows that pipes one command into another must run with pipefail, or the step takes the
# last command's exit: `go test … | tee log` showed a job green over "--- FAIL" (GitHub runs a step with no shell: as
# `bash -e {0}`, and `shell: bash` as `bash --noprofile --norc -eo pipefail {0}`). Each workflow sets
# `defaults: run: shell: bash`; this fails on a step with a pipe whose shell has no pipefail (none given, sh, or a
# custom command without it), unless the step sets pipefail itself. pwsh and powershell pipe objects, not exit codes.
# Usage: app/scripts/check-pipefail.sh [ignored]     app/scripts/check-pipefail.sh --self-test
# The check runs its self-test first, so a check that finds nothing can't pass.
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)

check() { # workflow files…: prints file:line for each step that loses a failure in a pipe; exit 1 if any
  python3 -I - "$@" <<'PY'
import re, sys

def blocks(lines, start, indent):
    """Indexes of the lines after start that are more indented than indent (blank lines kept)."""
    out = []
    for k in range(start + 1, len(lines)):
        s = lines[k]
        if s.strip() == "" or len(s) - len(s.lstrip()) > indent:
            out.append(k)
        else:
            break
    while out and lines[out[-1]].strip() == "":
        out.pop()
    return out

def ind(s):
    return len(s) - len(s.lstrip())

def key(s, name):
    m = re.match(r"^(\s*)(?:- )?" + name + r":\s*(.*?)\s*$", s)
    return m

def default_shell(lines, at, indent):
    """shell under defaults: run: in the mapping at lines[at] (a defaults: key with that indent)."""
    for k in blocks(lines, at, indent):
        m = key(lines[k], "shell")
        if m:
            return m.group(2).strip("'\"")
    return None

def unquoted(line):
    """The line with comments, quoted strings and ${{ }} taken out, so a | there is not a pipe."""
    s = line.strip()
    if s.startswith("#"):
        return ""
    s = re.sub(r"\$\{\{.*?\}\}", "", s)
    s = re.sub(r"'[^']*'", "''", s)
    s = re.sub(r'"(?:[^"\\]|\\.)*"', '""', s)
    return re.sub(r"(^|\s)#.*$", "", s)

def has_pipe(text):
    return any("|" in unquoted(l).replace("||", "") for l in text.split("\n"))

def sets_pipefail(text):
    return re.search(r"\bset\s+(-[A-Za-z]*o\s+pipefail|-o\s+pipefail|-[A-Za-z]+\s+-o\s+pipefail)", text) is not None

def ok_shell(sh):
    if sh is None:
        return False  # bash -e {0}: no pipefail (sh -e {0} where bash is missing)
    if sh in ("bash", "pwsh", "powershell", "python", "cmd"):
        return True
    return "pipefail" in sh

bad = 0
for path in sys.argv[1:]:
    lines = open(path, encoding="utf-8").read().split("\n")
    wf_shell = None
    for k, s in enumerate(lines):
        if re.match(r"^defaults:\s*$", s):
            wf_shell = default_shell(lines, k, 0)
    jobs_at = next((k for k, s in enumerate(lines) if re.match(r"^jobs:\s*$", s)), None)
    if jobs_at is None:
        continue
    for jk in blocks(lines, jobs_at, 0):
        if not re.match(r"^  [A-Za-z0-9_-]+:\s*$", lines[jk]):
            continue
        body = blocks(lines, jk, 2)
        job_shell = None
        steps_at = None
        for j in body:
            if re.match(r"^    defaults:\s*$", lines[j]):
                job_shell = default_shell(lines, j, 4)
            if re.match(r"^    steps:\s*$", lines[j]):
                steps_at = j
        if steps_at is None:
            continue
        under = blocks(lines, steps_at, 4)
        starts = [j for j in under if re.match(r"^\s+- ", lines[j])]
        if not starts:
            continue
        step_indent = ind(lines[starts[0]])
        for sk in starts:
            if ind(lines[sk]) != step_indent:
                continue
            # The step: its "- " line and the lines indented under it. Its keys sit at step_indent + 2.
            step = [sk] + blocks(lines, sk, step_indent)
            shell, run, run_line, run_line_inline = None, None, None, True
            for j in step:
                t = lines[j].replace("- ", "  ", 1) if j == sk else lines[j]
                if ind(t) != step_indent + 2:
                    continue
                m = key(t, "shell")
                if m:
                    shell = m.group(2).strip("'\"")
                m = key(t, "run")
                if m:
                    run_line = j + 1
                    v = m.group(2)
                    run_line_inline = not re.match(r"^[|>][-+0-9]*$", v)
                    if not run_line_inline:
                        run = "\n".join(lines[x] for x in blocks(lines, j, step_indent + 2))
                    else:
                        run = v.strip("'\"")
            if run is None:
                continue
            eff = shell or job_shell or wf_shell
            if eff in ("pwsh", "powershell", "python", "cmd"):
                continue
            if has_pipe(run) and not ok_shell(eff) and not sets_pipefail(run):
                print(f"{path}:{run_line}: a pipe in a step whose shell ({eff or 'none given: bash -e'}) has no pipefail")
                bad = 1
            # Under pipefail a writer killed by SIGPIPE fails the step: the reader stopped early, nothing went wrong.
            for n, line in enumerate(run.split("\n")):
                s = unquoted(line)
                why = None
                if re.search(r"(^|[;&(]\s*|\s)yes\s*\|(?!\|)", s):
                    why = "yes | … ends in SIGPIPE: write { yes || true; } |"
                elif re.search(r"\|\s*head\b", s):
                    why = "| head stops reading: write | sed -n 1p (or -print -quit for find)"
                elif re.search(r"\|\s*grep\b[^|]*\s-[A-Za-z]*[qm]", s):
                    why = "| grep -q or -m stops reading: write | grep … >/dev/null"
                if why:
                    print(f"{path}:{run_line + n + (0 if run_line_inline else 1)}: {why}")
                    bad = 1
sys.exit(bad)
PY
}

self_test() {
  t=$(mktemp -d) || return 1
  fail=0
  w() { name=$1; shift; printf '%s\n' "$@" > "$t/$name.yml"; }
  want() { # pass|fail name
    if check "$t/$2.yml" > "$t/out" 2>&1; then got=pass; else got=fail; fi
    [ "$got" = "$1" ] || { echo "FAIL: check-pipefail self-test: $2 gave $got, wanted $1: $(cat "$t/out")"; fail=1; }
  }
  head='on: push'
  w nodefault "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' '      - uses: actions/checkout@v1' \
    '      - name: tests' '        run: |' '          go test ./... | tee log' '          echo done'
  want fail nodefault
  w inline "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' '      - run: go test ./... | tee log'
  want fail inline
  w sh "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    '      - shell: sh' '        run: go test ./... | tee log'
  want fail sh
  w jobsh "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    defaults:' \
    '      run:' '        shell: sh -e {0}' '    steps:' '      - run: go test ./... | tee log'
  want fail jobsh
  w second "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' '      - shell: bash' '        run: a | b' \
    '  b:' '    runs-on: ubuntu-latest' '    steps:' '      - run: echo one' '      - run: c | d'
  want fail second
  w wfbash "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    '      - name: tests' '        run: |' '          go test ./... | tee log'
  want pass wfbash
  w jobbash "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    defaults:' '      run:' '        shell: bash' \
    '    steps:' '      - run: go test ./... | tee log'
  want pass jobbash
  w stepbash "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' '      - shell: bash' '        run: a | b'
  want pass stepbash
  w own "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' '      - run: |' '          set -euo pipefail' \
    '          go test ./... | tee log'
  want pass own
  w pwsh "$head" 'jobs:' '  a:' '    runs-on: windows-latest' '    steps:' '      - shell: pwsh' \
    '        run: New-Item -Force x | Out-Null'
  want pass pwsh
  w quoted "$head" 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    "      - run: grep -E 'a|b' x || true" '      - run: echo "${{ inputs.x || 1 }}" # a | b'
  want pass quoted
  w yes "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    '      - run: |' '          echo one' '          yes | sdkmanager --licenses'
  want fail yes
  w head1 "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    '      - run: tesseract --version | head -1'
  want fail head1
  w grepq "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    '      - run: if adb devices | grep -q emu; then echo up; fi'
  want fail grepq
  w safe "$head" 'defaults:' '  run:' '    shell: bash' 'jobs:' '  a:' '    runs-on: ubuntu-latest' '    steps:' \
    '      - run: |' '          { yes || true; } | sdkmanager --licenses' '          tesseract --version | sed -n 1p' \
    '          if adb devices | grep emu >/dev/null; then echo up; fi' '          grep -q x file && echo "a | head"'
  want pass safe
  # The line it names is the one with the pipe.
  check "$t/yes.yml" 2>&1 | grep -q 'yes.yml:11: yes' || { echo "FAIL: check-pipefail self-test: it names the wrong line: $(check "$t/yes.yml" 2>&1)"; fail=1; }
  rm -rf "$t"
  return $fail
}

if [ "${1:-}" = --self-test ]; then self_test && echo "ok: check-pipefail self-test"; exit; fi
self_test || exit 1
check "$root"/.github/workflows/*.yml
