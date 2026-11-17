"""Execute the real pin cleanup with synthetic Storage and raw logs."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]

class PinEvidenceTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(); cls.bin = Path(cls.build.name)
        subprocess.run(['go', 'build', '-o', str(cls.bin / 'xcrun'), './internal/testfake/chatevidence'], cwd=APP.parent / 'companion', check=True)
        shutil.copyfile(cls.bin / 'xcrun', cls.bin / 'hc'); (cls.bin / 'hc').chmod(0o755)
    @classmethod
    def tearDownClass(cls): cls.build.cleanup()
    def check_path(self, status):
        source = (APP / 'e2e/ios-pin-order.sh').read_text()
        block = source[source.index('cleanup() {'):source.index('trap cleanup EXIT;')]
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); work = root / 'work'; shots = root / 'shots'; container = root / 'container'
            (work / 'uitest').mkdir(parents=True)
            for name, text in [('steps', 'up 1 4 2 3 5\n'), ('uitest/test.log', 'synthetic XCTest\n'), ('serve.log', 'synthetic serve\n')]: (work / name).write_text(text)
            app = root / 'app'; (app / 'e2e').mkdir(parents=True)
            shutil.copyfile(APP / 'e2e/ios-chat-evidence.py', app / 'e2e/ios-chat-evidence.py')
            store = container / 'Library/Application Support/nsh/sample.app/storage.json'; store.parent.mkdir(parents=True)
            trace = [{'seq': 1, 'time': 1, 'what': 'pin-grant', 'fields': {'gesture': '1', 'pageY': '240'}}]
            store.write_text(json.dumps({'e2e-connection-diag': json.dumps(trace), 'prefs': 'synthetic preference', 'e2e-pair': 'synthetic pairing'}))
            script = '''set -eu
work=$1; shots=$2; app=$3; src=$3; run=$4; tag=sample; id=sample.app; SIM_UDID=sample
out="$work/steps"; placeholder=sample; hc=hc
L() { "$@"; }
autostart_guard_end() { return 0; }
''' + block + '\ntrap cleanup EXIT\nexit "$5"\n'
            env = dict(os.environ, PATH=str(self.bin)+':'+os.environ['PATH'], EVIDENCE_TEST_CONTAINER=str(container))
            result = subprocess.run(['sh','-c',script,'test',str(work),str(shots),str(app),str(root/'run'),str(status)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode,status,result.stderr)
            for name in ['sample-steps.txt','sample-test.log','sample-serve.log','sample-connection-diag.json']:
                self.assertTrue((shots/name).exists(),name)
            self.assertEqual(json.loads((shots/'sample-connection-diag.json').read_text()),trace)
            self.assertFalse(work.exists())
    def test_success(self): self.check_path(0)
    def test_failure(self): self.check_path(1)

if __name__ == '__main__': unittest.main()
