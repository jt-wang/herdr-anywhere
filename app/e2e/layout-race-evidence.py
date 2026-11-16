#!/usr/bin/env python3
"""Read only this fixture's evidence. Exit 1 is retention failure; exit 2 is invalid evidence."""
import hashlib
import json
from pathlib import Path
import sys


def require(condition, message):
    if not condition:
        raise ValueError(message)


def check(visible, released, sent=None):
    original = visible.get('originalPane')
    terminal = visible.get('terminal')
    require(original and terminal and visible.get('stage') == 'visible', 'missing initial terminal')
    require(visible.get('pane') == original == visible.get('focused'), 'new terminal was never focused')
    require(released.get('stage') == 'released', 'missing conflict release')
    for event in [visible, released]:
        require(event.get('runs') == 1 and event.get('conflicts') == 1, 'creation/conflict count')
        require(event.get('terminal') == terminal and event.get('originalPane') == original, 'changed identity')
        require(event.get('beforeVersion') == visible['beforeVersion'] and event['version'] > visible['beforeVersion'], 'remote version did not advance')
    require(released.get('rename') == released.get('documentRename') == 'grove', 'remote rename missing')
    if not (released.get('pane') == released.get('focused') == original and released.get('documentPanes') == [original]):
        return 1
    if sent is not None:
        require(sent.get('stage') == 'input', 'missing input observation')
        require(sent.get('pane') == sent.get('focused') == original and sent.get('documentPanes') == [original], 'pane lost during input')
        require(sent.get('input') == [terminal, 'layout input ok'] and sent.get('screen') == 'layout input ok', 'input delivered to wrong terminal or no echo')
        require(sent.get('runs') == 1 and sent.get('rename') == sent.get('documentRename') == 'grove', 'layout changed during input')
    return 0


def extract(log, stage):
    found = []
    for line in log.splitlines():
        if 'layout-race-error ' in line:
            raise ValueError('fixture reported an error')
        if 'layout-race-evidence ' in line:
            event = json.loads(line.split('layout-race-evidence ', 1)[1])
            if event.get('stage') == stage:
                found.append(event)
    require(found, 'missing stage ' + stage)
    return found[-1]


def manifest(folder):
    return {p.relative_to(folder).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(folder.rglob('*')) if p.is_file() and p.name != 'evidence-sha256.json'}


def main():
    action, *args = sys.argv[1:]
    if action == 'extract':
        log, stage, destination = args
        Path(destination).write_text(json.dumps(extract(Path(log).read_text(), stage), indent=2) + '\n')
        return 0
    if action == 'manifest':
        folder = Path(args[0])
        (folder / 'evidence-sha256.json').write_text(json.dumps(manifest(folder), indent=2) + '\n')
        return 0
    require(action == 'check' and len(args) in (2, 3), 'usage: check VISIBLE RELEASED [INPUT]')
    rc = check(*(json.loads(Path(p).read_text()) for p in args))
    print('FAIL terminal-retention' if rc else 'PASS terminal-retention' + (' and input' if len(args) == 3 else ''))
    return rc


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, KeyError, OSError) as error:
        print('INVALID layout evidence: ' + str(error), file=sys.stderr)
        sys.exit(2)
