"""Execute chat's real XCTest failure block with a synthetic simulator tool."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]


class ChatFailureTest(unittest.TestCase):
    def test_failure_keeps_screen_and_both_process_ids(self):
        source = (APP / 'e2e/ios-chat-four.sh').read_text()
        start = source.index('if [ "$rc" != 0 ] || [ "$now" != "$before" ]; then')
        end = source.index('if [ "$now" != "$before" ]; then', start)
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            tools, work, shots = [root / n for n in ['tools', 'work', 'shots']]
            for p in [tools, work / 'uitest/result.xcresult', shots]: p.mkdir(parents=True)
            for name in ['serve.log', 'uitest/test.log', 'proxy.log']:
                (work / name).write_text('synthetic log\n')
            subprocess.run(['go', 'build', '-o', str(tools / 'xcrun'), './internal/testfake/zoomfailure'],
                           cwd=APP.parent / 'companion', check=True)
            script = '''set -eu
app=$1; work=$2; shots=$3; SIM_UDID=sample; rc=65; before=101; now=101
''' + source[start:end] + '\nexit 1\n'
            env = dict(os.environ, PATH=str(tools) + ':' + os.environ['PATH'])
            run = subprocess.run(['sh', '-c', script, 'test', str(APP), str(work), str(shots)],
                                 env=env, text=True, capture_output=True)
            self.assertEqual(run.returncode, 1, run.stderr)
            self.assertEqual((shots / 'serve.log').read_text(), 'synthetic log\n')
            ids = shots / 'process-ids.txt'
            self.assertTrue(ids.is_file(), 'the observed PID pair must survive cleanup')
            self.assertEqual(ids.read_text(), 'at-launch=101\nafter-test=101\n')
            self.assertEqual((shots / 'after-failure.png').read_text(), 'synthetic screenshot\n')


if __name__ == '__main__': unittest.main()
