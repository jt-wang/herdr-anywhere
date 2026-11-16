"""Execute the script's cleanup and success tail against a synthetic app container."""
from pathlib import Path
import json
import os
import shutil
import subprocess
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
        for name in ['pkill', 'hc']:
            shutil.copyfile(cls.bin / 'xcrun', cls.bin / name)
            (cls.bin / name).chmod(0o755)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def check_path(self, status, dry=False):
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
            for helper in ['ios-chat-evidence.py']:
                if (APP / 'e2e' / helper).exists(): shutil.copyfile(APP / 'e2e' / helper, fakeapp / 'e2e' / helper)
            script = '''set -eu
work=$1; shots=$2; app=$3; state=$4; run=$5; rc=$6
id=sample.app; SIM_UDID=sample; cycles=0; tag=chat-four
ctl_pid=; log_pid=; proxy_pid=; serve_pid=; booted=; remove_app=0; placeholder=sample; hc=hc
L() { "$@"; }
autostart_guard_end() { return 0; }
''' + cleanup + '\ntrap cleanup EXIT\n' + tail
            env = dict(os.environ, PATH=str(self.bin) + ':' + os.environ['PATH'], EVIDENCE_TEST_CONTAINER=str(container),
                       EVIDENCE_TEST_CALLS=str(root / 'simulator-calls'), HM_CHAT4_DRY='1' if dry else '0')
            result = subprocess.run(['sh', '-c', script, 'test', str(work), str(shots), str(fakeapp),
                                     str(root / 'state'), str(root / 'run'), str(status)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, status, result.stderr)
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

    def test_dry_has_no_simulator_calls(self): self.check_path(0, dry=True)

    def test_success(self): self.check_path(0)
    def test_failure(self): self.check_path(1)


if __name__ == '__main__': unittest.main()
