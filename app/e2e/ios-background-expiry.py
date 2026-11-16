"""Capture this test's original app process at background-task expiration."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from urllib.parse import urlsplit, parse_qs


class Triggers:
    def __init__(self, pid, bundle):
        self.background = re.compile(r'Application process state changed for ' + re.escape(bundle) +
                                     r': .*\bpid: ' + str(pid) + r';.*\bvisibility: Background>')
        self.expiry = re.compile(r'Assertions for process will expire soon: \[app<' + re.escape(bundle) +
                                 r'\([^\n]*?\)>:' + str(pid) + r'\]')
        self.background_at = None
        self.expiry_at = None
        self.sent = set()
        self.events = []

    def feed(self, line, now):
        if self.background_at is None and self.background.search(line):
            self.background_at = now
            self.events.append({'kind': 'background', 'time': now, 'line': line.rstrip()})
        if self.background_at is not None and self.expiry_at is None and self.expiry.search(line):
            self.expiry_at = now
            self.events.append({'kind': 'expiry', 'time': now, 'line': line.rstrip()})

    def due(self, now):
        out = []
        for name, at in [('background-25s', None if self.background_at is None else self.background_at + 25),
                         ('expiry', self.expiry_at)]:
            if at is not None and now >= at and name not in self.sent:
                self.sent.add(name)
                out.append(name)
        return out


def valid_sample(text, pid):
    if not re.search(r'^Process:\s+.*\[' + str(pid) + r'\]\s*$', text, re.M):
        return False
    if 'Call graph:' not in text:
        return False
    lines = text.split('Call graph:', 1)[1].splitlines()
    for i, line in enumerate(lines):
        if 'com.apple.main-thread' in line or re.search(r'\bmain thread\b', line, re.I):
            for frame in lines[i + 1:]:
                if re.search(r'\bThread[_\s]', frame) or frame.startswith('Total number'):
                    break
                if re.search(r'\d+\s+\S.*\(in\s+\S', frame):
                    return True
    return False


def utc():
    return datetime.now(timezone.utc).isoformat(timespec='milliseconds')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def text(value):
    return value.decode(errors='replace') if isinstance(value, bytes) else (value or '')


def take_sample(pid, output):
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.unlink(missing_ok=True)
    record = {'pid': pid, 'started_utc': utc(), 'timeout': False}
    command = ['/usr/bin/sample', str(pid), '1', '10', '-mayDie', '-file', str(output)]
    try:
        run = subprocess.run(command, capture_output=True, text=True, timeout=10)
        record['exit'] = run.returncode
        console = run.stdout + run.stderr
    except subprocess.TimeoutExpired as error:
        # subprocess.run kills and waits for its own sample child on timeout.
        record.update({'exit': None, 'timeout': True})
        console = text(error.stdout) + text(error.stderr)
    except OSError as error:
        record['exit'] = None
        console = str(error)
    record['finished_utc'] = utc()
    output.with_suffix('.stderr.txt').write_text(console)
    data = output.read_bytes() if output.exists() else b''
    record.update({'file': output.name, 'sha256': sha(data),
                   'accepted': record['exit'] == 0 and valid_sample(data.decode(errors='replace'), pid)})
    return record


def write_json(path, data):
    Path(path).write_text(json.dumps(data, indent=2) + '\n')


def calibrate(pid, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    positive = take_sample(pid, output / 'calibration-live.txt')
    record = {'positive': positive, 'passed': False}
    if positive['accepted']:
        child = subprocess.Popen([sys.executable, '-c', 'pass'])
        child.wait()
        try:
            os.kill(child.pid, 0)
            record['negative_error'] = 'reaped child PID is already in use'
        except ProcessLookupError:
            negative = take_sample(child.pid, output / 'calibration-exited.txt')
            record['negative'] = negative
            record['passed'] = (negative['exit'] is not None and negative['exit'] != 0
                                and not negative['timeout'] and not negative['accepted'])
    write_json(output / 'calibration.json', record)
    return record['passed']


def watch(pid, bundle, source, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    triggers = Triggers(pid, bundle)
    samples = []
    stopping = False

    def stop(signum, frame):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    record = {'pid': pid, 'started_utc': utc(), 'log_timezone': time.tzname,
              'events': triggers.events, 'samples': samples}
    try:
        with open(source) as stream:
            while not stopping:
                line = stream.readline()
                if line:
                    # compact log timestamps use the runner's local timezone, as does timestamp().
                    try:
                        at = datetime.strptime(line[:23], '%Y-%m-%d %H:%M:%S.%f').timestamp()
                    except ValueError:
                        at = time.time()
                    triggers.feed(line, at)
                for kind in triggers.due(time.time()):
                    if stopping:
                        break
                    sample = take_sample(pid, output / (kind + '.txt'))
                    sample['trigger'] = kind
                    samples.append(sample)
                    write_json(output / 'capture.json', record)
                if not line:
                    time.sleep(0.1)
    finally:
        record['finished_utc'] = utc()
        write_json(output / 'capture.json', record)
    return 0


def publish(raw, output, secret):
    raw, output = Path(raw), Path(output)
    link = Path(secret).read_text().strip()
    token = parse_qs(urlsplit(link).fragment).get('pair', [''])[0]
    if not link or not token or not link.isascii():
        raise ValueError('missing ASCII pairing link')
    values = [('link', link), ('pair-fragment', '#pair=' + token), ('token-field', 'token=' + token),
              ('pair-value', token)]
    output.mkdir(parents=True, exist_ok=True)
    manifest = {'files': {}}
    for path in sorted(raw.iterdir()):
        if not path.is_file():
            continue
        data = path.read_bytes()
        clean = data
        counts = {}
        for label, value in values:
            value = value.encode('ascii')
            count = clean.count(value)
            if count:
                counts[label] = count
                clean = clean.replace(value, b'x' * len(value))
        (output / path.name).write_bytes(clean)
        manifest['files'][path.name] = {'raw_sha256': sha(data), 'public_sha256': sha(clean),
                                       'replacements': counts}
    write_json(output / 'manifest.json', manifest)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    c = sub.add_parser('calibrate'); c.add_argument('pid', type=int); c.add_argument('output')
    w = sub.add_parser('watch'); w.add_argument('pid', type=int); w.add_argument('bundle')
    w.add_argument('source'); w.add_argument('output')
    p = sub.add_parser('publish'); p.add_argument('raw'); p.add_argument('output'); p.add_argument('secret')
    args = parser.parse_args()
    if args.command == 'calibrate':
        return 0 if calibrate(args.pid, args.output) else 1
    if args.command == 'watch':
        return watch(args.pid, args.bundle, args.source, args.output)
    publish(args.raw, args.output, args.secret)
    return 0


if __name__ == '__main__':
    sys.exit(main())
