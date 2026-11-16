"""Run the real post-XCTest preservation block before a failing shell assertion."""
from pathlib import Path
import subprocess
import tempfile
import unittest


class LogsTest(unittest.TestCase):
    def test_raw_logs_survive_later_failure(self):
        self.check_saved({'steps': 'start a b c d e\nup a b c d e\ndown b c d a e\nedge e b c d a\n',
                          'uitest/test.log': 'XCTest raw output\n', 'serve.log': 'connection record\n'})

    def test_startup_failure_keeps_logs_without_steps(self):
        self.check_saved({'uitest/test.log': 'XCTest did not start\n', 'serve.log': 'connection record\n'})

    def check_saved(self, samples):
        source = Path(__file__).with_name('ios-pin-order.sh').read_text()
        start = source.index('touch "$work/done"; wait "$watcher" 2>/dev/null || true')
        start = source.index('\n', start) + 1
        block = source[start:source.index('# On a failure:', start)]
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            work, shots = root / 'work', root / 'shots'
            (work / 'uitest').mkdir(parents=True)
            for name, text in samples.items():
                (work / name).write_text(text)
            script = 'set -eu\nwork=$1; shots=$2; tag=sample; out="$work/steps"\n' + block + '\nexit 1\n'
            result = subprocess.run(['sh', '-c', script, 'test', str(work), str(shots)])
            self.assertEqual(result.returncode, 1)
            for name, text in samples.items():
                target = {'steps': 'sample-steps.txt', 'uitest/test.log': 'sample-test.log',
                          'serve.log': 'sample-serve.log'}[name]
                self.assertTrue((shots / target).is_file(), target)
                self.assertEqual((shots / target).read_text(), text)


if __name__ == '__main__':
    unittest.main()
