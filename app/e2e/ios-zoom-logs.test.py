"""Run the actual zoom XCTest/failure block and its real result-bundle exporter."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]


class ZoomLogsTest(unittest.TestCase):
    def test_failure_keeps_assertion_and_attachments(self):
        source = (APP / 'e2e/ios-zoom.sh').read_text()
        start = source.index('  rc=0\n')
        end = source.index('  grep -q "sheet open after the drags:', start)
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            tools, work, out = [root / n for n in ['tools', 'work', 'out']]
            for p in [tools, work, out]: p.mkdir()
            subprocess.run(['go', 'build', '-o', str(tools / 'xcodebuild'), './internal/testfake/zoomfailure'],
                           cwd=APP.parent / 'companion', check=True)
            shutil.copyfile(tools / 'xcodebuild', tools / 'xcrun')
            (tools / 'xcrun').chmod(0o755)
            script = '''set -eu
app=$1; work=$2; out=$3; SIM_UDID=sample; id=sample.app; rec=""; failed=0
say() { echo "$1"; case $1 in FAIL*) failed=1;; esac; }
for scheme in light; do
''' + source[start:end] + '\ndone\n[ "$failed" = 0 ]\n'
            env = dict(os.environ, PATH=str(tools) + ':' + os.environ['PATH'])
            run = subprocess.run(['sh', '-c', script, 'test', str(APP), str(work), str(out)],
                                 env=env, text=True, capture_output=True)
            self.assertEqual(run.returncode, 1, run.stderr)
            self.assertIn('FAIL: xcodebuild test exit 65', run.stdout)
            self.assertTrue((out / 'test-light.log').is_file())
            reason = out / 'test-light-failures.txt'
            self.assertTrue(reason.is_file(), 'specific XCTest failure must survive cleanup')
            self.assertIn('synthetic baseline failure', reason.read_text())
            self.assertEqual((out / 'failure-light/assertion.txt').read_text(), 'synthetic UI state\n')


if __name__ == '__main__': unittest.main()
