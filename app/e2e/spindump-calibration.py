"""Calibrate spindump on one owned Python process, then its reaped PID."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


def inspect_report(data, pid, tid):
    text = data.decode('utf-8', errors='replace')
    processes = {int(n) for n in re.findall(r'^Process:\s+[^\n]*\[(\d+)\]', text, re.M)}
    threads = list(re.finditer(r'^\s*Thread\s+(0x[0-9a-fA-F]+|[0-9]+)\b[^\n]*', text, re.M))
    main = ''
    for i, thread in enumerate(threads):
        value = thread[1]
        if int(value, 16 if value.startswith('0x') else 10) == tid:
            end = threads[i + 1].start() if i + 1 < len(threads) else len(text)
            main = text[thread.end():end].split('Binary Images:', 1)[0]
            break
    uuid = bool(re.search(r'\b[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\b', text))
    frames = bool(re.search(r'\+\s*(?:0x[0-9a-fA-F]+|[0-9]+)\b[^\n]*\[0x[0-9a-fA-F]+\]', main))
    checks = {'only_owned_pid': processes == {pid}, 'main_tid_found': bool(main),
              'main_frames_with_offset': frames, 'uuid_found': uuid}
    return {'accepted': all(checks.values()), 'checks': checks, 'processes': sorted(processes)}


def command(argv, output, timeout=10):
    start = time.monotonic()
    record = {'argv': argv, 'started_utc': datetime.now(timezone.utc).isoformat(), 'timeout': False}
    proc = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        stdout, stderr = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        record['timeout'] = True
        # sudo relays SIGTERM to its command; SIGKILL alone would not be relayed.
        proc.terminate()
        try:
            stdout, stderr = proc.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            stdout, stderr = proc.communicate()
    Path(str(output) + '.stdout.txt').write_bytes(stdout)
    Path(str(output) + '.stderr.txt').write_bytes(stderr)
    record.update({'exit': proc.returncode, 'elapsed_seconds': round(time.monotonic() - start, 3)})
    return record


def capture(pid, tid, output):
    run = command(['sudo', '-n', '/usr/sbin/spindump', str(pid), '1', '10', '-onlyTarget',
                   '-noSymbolicate', '-noProcessingWhileSampling', '-timelimit', '8', '-o', str(output)], output)
    data = output.read_bytes() if output.exists() else b''
    run.update({'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(),
                'report': inspect_report(data, pid, tid)})
    run['accepted'] = run['exit'] == 0 and not run['timeout'] and run['report']['accepted']
    return run


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    result = {'passed': False, 'stage': 'help'}
    child = None
    try:
        help_run = command(['/usr/sbin/spindump', '-h'], out / 'help')
        help_text = (out / 'help.stdout.txt').read_text() + (out / 'help.stderr.txt').read_text()
        flags = ['-onlyTarget', '-noSymbolicate', '-noProcessingWhileSampling', '-timelimit', '-o ']
        result['help'] = help_run
        result['missing_options'] = [f for f in flags if f not in help_text]
        if help_run['timeout'] or result['missing_options']:
            return 1
        result['stage'] = 'live'
        code = ('import json,os,threading,time; '
                'print(json.dumps({"pid":os.getpid(),"ppid":os.getppid(),'
                '"main_tid":threading.get_native_id()}),flush=True); time.sleep(90)')
        child = subprocess.Popen([sys.executable, '-c', code], stdout=subprocess.PIPE, text=True,
                                 env={'PATH': '/usr/bin:/bin', 'PYTHONNOUSERSITE': '1'})
        identity = json.loads(child.stdout.readline())
        result['owned_process'] = identity
        if identity['pid'] != child.pid or identity['ppid'] != os.getpid() or child.poll() is not None:
            result['failure'] = 'child identity mismatch'
            return 1
        pid, tid = child.pid, identity['main_tid']
        result['live'] = capture(pid, tid, out / 'live.spindump')
        if not result['live']['accepted']:
            return 1
        child.terminate()
        child.wait(timeout=3)
        result['child_reaped'] = True
        # A recycled PID must not turn the negative control into a different target.
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            pass
        else:
            result['failure'] = 'reaped PID is already in use'
            return 1
        result['stage'] = 'dead'
        result['dead'] = capture(pid, tid, out / 'dead.spindump')
        dead = result['dead']
        result['passed'] = (dead['exit'] != 0 and not dead['timeout'] and not dead['report']['accepted']
                            and not dead['report']['processes'])
        result['stage'] = 'complete'
        return 0 if result['passed'] else 1
    finally:
        if child is not None:
            if child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
            result['child_reaped'] = child.poll() is not None
        (out / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        # Console contains status only; raw reports stay in the artifact.
        print(json.dumps({'passed': result['passed'], 'stage': result['stage']}))


if __name__ == '__main__':
    sys.exit(main())
