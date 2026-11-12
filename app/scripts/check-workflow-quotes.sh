#!/bin/sh
# Each ${{ … }} in the workflows ends on its line with its strings closed: a string in an expression is in single
# quotes, and a quote in it is written twice (''). An apostrophe written once ("Oh My Pi's", in a fromJSON('{…}') name
# map) ends the string early: GitHub then finds no closing }} and the workflow runs no job at all, on every push.
# Prints file:line for each; fails if there are any. Any argument (test.sh passes src/ui) is not used.
set -u
root=$(cd "$(dirname "$0")/../.." && pwd)
python3 -I - "$root"/.github/workflows/*.yml <<'PY'
import sys
bad = 0
for path in sys.argv[1:]:
    for n, line in enumerate(open(path, encoding='utf-8'), 1):
        at = 0
        while (start := line.find('${{', at)) >= 0:
            i, quoted, closed = start + 3, False, False
            while i < len(line):
                c = line[i]
                if quoted:
                    if c == "'":
                        if line[i + 1:i + 2] == "'":
                            i += 1
                        else:
                            quoted = False
                elif c == "'":
                    quoted = True
                elif line.startswith('}}', i):
                    closed = True
                    break
                i += 1
            if not closed:
                print(f'check-workflow-quotes: {path.split("/.github/")[-1]}:{n}: a ${{{{ with no closing }}}} after its strings (a quote not written twice?)')
                bad = 1
                break
            at = i + 2
sys.exit(bad)
PY
