"""Execute the script's cleanup and success tail against a synthetic app container."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]


class EvidenceTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.bin = Path(cls.build.name)
        subprocess.run(['go', 'build', '-o', str(cls.bin / 'xcrun'), './internal/testfake/chatevidence'],
                       cwd=APP.parent / 'companion', check=True)
        for name in ['pkill', 'hc', 'python3']:
            shutil.copyfile(cls.bin / 'xcrun', cls.bin / name)
            (cls.bin / name).chmod(0o755)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def check_path(self, status, dry=False, decode=None, publish_failure=None):
        source = (APP / 'e2e/ios-chat-four.sh').read_text()
        cleanup = source[source.index('cleanup() {'):source.index('trap cleanup EXIT;')]
        # Execute the actual successful uninstall/exit tail as well as the EXIT trap.
        tail = source[source.index('[ "$rc" = 0 ] || exit 1'):]
        if dry: tail = source[source.index('  echo PASS; exit 0'):].splitlines()[0]
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            work, shots, container = root / 'work', root / 'shots', root / 'container'
            (work / 'uitest').mkdir(parents=True); shots.mkdir()
            store = container / 'Library/Application Support/nsh/sample.app/storage.json'
            store.parent.mkdir(parents=True)
            trace = [{ 'seq': 1, 'time': 123, 'what': 'connection-lost', 'fields': {'attempt': '1'} }]
            store.write_text(json.dumps({'e2e-connection-diag': json.dumps(trace),
                                         'e2e-pair': 'synthetic pairing', 'prefs': 'synthetic preference'}))
            (work / 'serve.log').write_text('synthetic serve\n')
            (work / 'uitest/test.log').write_text('synthetic XCTest\n')
            # pair.ts restoration is directed to a temporary app, never the checkout.
            fakeapp = root / 'app'; (fakeapp / 'e2e').mkdir(parents=True)
            for helper in ['ios-chat-evidence.py', 'ios-background-expiry.py', 'ci-shots-check.sh']:
                if (APP / 'e2e' / helper).exists(): shutil.copyfile(APP / 'e2e' / helper, fakeapp / 'e2e' / helper)
            if decode is not None:
                raw = work / 'expiry'; raw.mkdir()
                (raw / 'app-debug.log').write_text('ordinary debug line\n')
                (work / 'expiry-pair').write_text('invalid' if publish_failure == 'privacy' else
                                                'http://127.0.0.1:9/#pair=aaaaaaaaaaaaaaaa')
                if publish_failure == 'output': (shots / 'background-expiry').write_text('occupied')
            script = '''set -eu
work=$1; shots=$2; app=$3; state=$4; run=$5; rc=$6
id=sample.app; SIM_UDID=sample; cycles=0; tag=chat-four
ctl_pid=; log_pid=; proxy_pid=; serve_pid=; booted=; remove_app=0; placeholder=sample; hc=hc
L() { "$@"; }
autostart_guard_end() { return 0; }
''' + cleanup + '\ntrap cleanup EXIT\n' + (
                'expiry_raw="$work/expiry"\nexit "$rc"' if decode is not None else tail)
            env = dict(os.environ, PATH=str(self.bin) + ':' + os.environ['PATH'], EVIDENCE_TEST_CONTAINER=str(container),
                       EVIDENCE_TEST_CALLS=str(root / 'simulator-calls'), EVIDENCE_TEST_PYTHON=sys.executable,
                       EVIDENCE_TEST_DECODE_EXIT=str(decode or 0), HM_CHAT4_DRY='1' if dry else '0')
            result = subprocess.run(['sh', '-c', script, 'test', str(work), str(shots), str(fakeapp),
                                     str(root / 'state'), str(root / 'run'), str(status)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 1 if publish_failure else status, result.stderr)
            if decode is not None:
                self.assertFalse(work.exists(), 'cleanup must remove private work')
                if publish_failure: return
                saved = shots / 'background-expiry'
                receipt = dict(line.split('=', 1) for line in (saved / 'decode-status.txt').read_text().splitlines())
                self.assertEqual(receipt, {'diagnostic': 'incomplete' if decode else 'complete',
                                          'decode_exit': str(decode), 'behavior_exit': str(status)})
                records = json.loads((saved / 'decode.json').read_text())
                self.assertEqual(records[0]['exit'], -15 if decode else 0)
                self.assertEqual(records[0]['timeout'], bool(decode))
                self.assertEqual((saved / 'app-debug.log').read_text(), 'ordinary debug line\n')
                self.assertEqual('diagnostic incomplete' in result.stdout, bool(decode))
                return
            for name, expected in [('serve.log', 'synthetic serve\n'), ('test.log', 'synthetic XCTest\n')]:
                self.assertTrue((shots / name).is_file(), name)
                self.assertEqual((shots / name).read_text(), expected)
            evidence = shots / 'connection-diag.json'
            if dry:
                calls = root / 'simulator-calls'
                self.assertEqual(calls.read_text() if calls.exists() else '', '', 'DRY must not call simulator')
                self.assertFalse(evidence.exists())
                self.assertTrue(container.exists())
                self.assertFalse(work.exists())
                return
            self.assertTrue(evidence.is_file(), 'connection-diag.json')
            self.assertEqual(json.loads(evidence.read_text()), trace)
            self.assertNotIn('synthetic pairing', evidence.read_text())
            self.assertNotIn('synthetic preference', evidence.read_text())
            self.assertFalse(work.exists(), 'cleanup must still remove work')
            if status == 0: self.assertFalse(container.exists(), 'successful run still uninstalls')
            else: self.assertTrue(container.exists(), 'failed run keeps app for inspection')

    def test_decode_failure_preserves_behavior_success(self): self.check_path(0, dry=True, decode=1)
    def test_decode_failure_preserves_original_behavior_failure(self): self.check_path(7, dry=True, decode=1)
    def test_decode_success_records_complete(self): self.check_path(0, dry=True, decode=0)
    def test_publish_privacy_failure_still_fails(self): self.check_path(0, dry=True, decode=0, publish_failure='privacy')
    def test_publish_output_failure_still_fails(self): self.check_path(0, dry=True, decode=0, publish_failure='output')

    def test_dry_has_no_simulator_calls(self): self.check_path(0, dry=True)

    def test_success(self): self.check_path(0)
    def test_failure(self): self.check_path(1)


class EvidenceLaunchExportTest(unittest.TestCase):
    def test_legacy_single_key_still_exports_unchanged(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); store = root / 'storage.json'; output = root / 'trace.json'
            old = [{'seq': 1, 'time': 100, 'what': 'connection-lost', 'fields': {}}]
            store.write_text(json.dumps({'e2e-connection-diag': json.dumps(old)}))
            result = subprocess.run(['python3', str(APP / 'e2e/ios-chat-evidence.py'), str(store), str(output)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(output.read_text()), old)

    def test_exports_both_launches_with_distinct_launch_numbers(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); store = root / 'storage.json'; output = root / 'trace.json'
            old = {'seq': 1, 'time': 100, 'what': 'connection-lost', 'fields': {}}
            new = {'seq': 1, 'time': 200, 'what': 'connection-connect', 'fields': {}}
            store.write_text(json.dumps({'e2e-connection-diag-2': json.dumps([new]),
                                         'e2e-connection-diag-1': json.dumps([old]),
                                         'e2e-pair': 'synthetic pairing'}))
            result = subprocess.run(['python3', str(APP / 'e2e/ios-chat-evidence.py'), str(store), str(output)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(output.exists(), 'launch traces were not exported')
            self.assertEqual(json.loads(output.read_text()), [{**old, 'launch': 1}, {**new, 'launch': 2}])


if __name__ == '__main__': unittest.main()
