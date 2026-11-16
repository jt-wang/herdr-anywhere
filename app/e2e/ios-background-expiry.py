"""Capture this test's original app process at background-task expiration."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import shutil
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


def prepare(ios, output):
    ios, output = Path(ios).resolve(), Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    shutil.copytree(ios / 'Sources', output / 'Sources', dirs_exist_ok=True)
    original = (ios / 'Sources/App.swift').read_text()
    anchor = '    hideWebKeyboardBar()'
    if original.count(anchor) != 1:
        raise ValueError('launch insertion point changed')
    marker = """    if Thread.isMainThread {
      var mainThreadID: UInt64 = 0
      if pthread_threadid_np(nil, &mainThreadID) == 0 {
        NSLog("HM_EXPIRY_MAIN pid=%d tid=%llu launch=%@", getpid(), mainThreadID, UUID().uuidString)
      }
    }
"""
    (output / 'Sources/App.swift').write_text('import Darwin\n' + original.replace(anchor, marker + anchor))
    spec = (ios / 'project.yml').read_text()
    if spec.count('      - Sources\n') != 1:
        raise ValueError('project Sources entry changed')
    (output / 'project.yml').write_text(spec.replace('      - Sources\n',
        '      - path: ' + json.dumps(str(output / 'Sources')) + '\n'))


def main_identity(log, pid):
    markers = {(int(p), int(t), launch) for p, t, launch in re.findall(
        r'HM_EXPIRY_MAIN pid=(\d+) tid=(\d+) launch=([A-Fa-f0-9-]{36})\b', log) if int(p) == pid and int(t) > 0}
    if len(markers) != 1:
        raise ValueError('expected one main-thread marker for original PID')
    p, tid, launch = markers.pop()
    return {'pid': p, 'tid': tid, 'launch': launch}


def report_text(data):
    # spindump appends its binary archive after the readable report.
    return data.split(b'\n\nSpindump binary format', 1)[0]


def valid_sample(text, pid, tid):
    processes = {int(p) for p in re.findall(r'^Process:\s+[^\n]*\[(\d+)\]', text, re.M)}
    if processes != {pid}:
        return False
    threads = list(re.finditer(r'^\s*Thread\s+(0x[0-9a-fA-F]+|[0-9]+)\b[^\n]*', text, re.M))
    for i, thread in enumerate(threads):
        value = thread[1]
        if int(value, 16 if value.startswith('0x') else 10) == tid:
            end = threads[i + 1].start() if i + 1 < len(threads) else len(text)
            frames = text[thread.end():end].split('Binary Images:', 1)[0]
            return bool(re.search(r'\+\s*(?:0x[0-9a-fA-F]+|[0-9]+)\b[^\n]*\[0x[0-9a-fA-F]+\]', frames))
    return False


def utc():
    return datetime.now(timezone.utc).isoformat(timespec='milliseconds')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def command(argv, output):
    start = time.monotonic()
    record = {'started_utc': utc(), 'timeout': False}
    proc = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        stdout, stderr = proc.communicate(timeout=10)
    except subprocess.TimeoutExpired:
        record['timeout'] = True
        # sudo relays SIGTERM to spindump; allow it to reap the sampling command.
        proc.terminate()
        try:
            stdout, stderr = proc.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            stdout, stderr = proc.communicate()
    Path(str(output) + '.stdout.txt').write_bytes(stdout)
    Path(str(output) + '.stderr.txt').write_bytes(stderr)
    record.update({'exit': proc.returncode, 'elapsed_seconds': round(time.monotonic() - start, 3),
                   'finished_utc': utc()})
    return record


def take_sample(pid, output, tid):
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.unlink(missing_ok=True)
    record = command(['sudo', '-n', '/usr/sbin/spindump', str(pid), '1', '10', '-onlyTarget',
                      '-noSymbolicate', '-noProcessingWhileSampling', '-timelimit', '8', '-o', str(output)], output)
    data = output.read_bytes() if output.exists() else b''
    readable = report_text(data)
    output.with_suffix('.raw.txt').write_bytes(readable)
    record.update({'pid': pid, 'tid': tid, 'file': output.name, 'sha256': sha(data),
                   'accepted': record['exit'] == 0 and not record['timeout'] and
                               valid_sample(readable.decode(errors='replace'), pid, tid)})
    return record


def decode(raw):
    raw = Path(raw)
    records = []
    for path in sorted(raw.glob('*.spindump')):
        output = path.with_suffix('.decoded')
        result = command(['/usr/sbin/spindump', '-i', str(path), '-symbolicate', '-timelimit', '8',
                          '-o', str(output)], output)
        data = output.read_bytes() if output.exists() else b''
        readable = report_text(data)
        path.with_suffix('.txt').write_bytes(readable)
        result.update({'source': path.name, 'source_sha256': sha(path.read_bytes()),
                       'text_sha256': sha(readable), 'decoded': result['exit'] == 0 and
                       not result['timeout'] and bool(readable)})
        records.append(result)
    write_json(raw / 'decode.json', records)
    return all(r['decoded'] for r in records)


def write_json(path, data):
    Path(path).write_text(json.dumps(data, indent=2) + '\n')


def calibrate(pid, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    record = {'passed': False}
    try:
        identity = main_identity((output / 'app-debug.log').read_text(), pid)
    except (OSError, ValueError) as error:
        record['marker_error'] = str(error)
    else:
        record['identity'] = identity
        positive = take_sample(pid, output / 'calibration-live.spindump', identity['tid'])
        record.update({'positive': positive, 'passed': positive['accepted']})
    write_json(output / 'calibration.json', record)
    return record['passed']


def watch(pid, bundle, source, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    identity = json.loads((output / 'calibration.json').read_text())['identity']
    if identity['pid'] != pid:
        raise ValueError('calibration PID differs from original PID')
    triggers = Triggers(pid, bundle)
    samples = []
    stopping = False

    def stop(signum, frame):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    record = {'pid': pid, 'identity': identity, 'started_utc': utc(), 'log_timezone': time.tzname,
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
                    sample = take_sample(pid, output / (kind + '.spindump'), identity['tid'])
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
        if not path.is_file() or path.suffix in ('.spindump', '.decoded'):
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
    prep = sub.add_parser('prepare'); prep.add_argument('ios'); prep.add_argument('output')
    d = sub.add_parser('decode'); d.add_argument('raw')
    c = sub.add_parser('calibrate'); c.add_argument('pid', type=int); c.add_argument('output')
    w = sub.add_parser('watch'); w.add_argument('pid', type=int); w.add_argument('bundle')
    w.add_argument('source'); w.add_argument('output')
    p = sub.add_parser('publish'); p.add_argument('raw'); p.add_argument('output'); p.add_argument('secret')
    args = parser.parse_args()
    if args.command == 'prepare':
        prepare(args.ios, args.output)
        return 0
    if args.command == 'decode':
        return 0 if decode(args.raw) else 1
    if args.command == 'calibrate':
        return 0 if calibrate(args.pid, args.output) else 1
    if args.command == 'watch':
        return watch(args.pid, args.bundle, args.source, args.output)
    publish(args.raw, args.output, args.secret)
    return 0


if __name__ == '__main__':
    sys.exit(main())
