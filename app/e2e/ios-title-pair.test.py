"""Exercise title's real build/install/launch and EXIT cleanup with pinpair's virtual clock."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]


class TitlePairTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.fake = Path(cls.build.name) / 'pinpair'
        subprocess.run(['go', 'build', '-o', str(cls.fake), './internal/testfake/pinpair'],
                       cwd=APP.parent / 'companion', check=True)

    @classmethod
    def tearDownClass(cls): cls.build.cleanup()

    def run_case(self, seconds, failure=''):
        source = Path(os.environ.get('IOS_TITLE_TEST_SCRIPT', APP / 'e2e/ios-sheet-title.sh')).read_text()
        begin = source.index('link=$(L "$bin" pair ')
        end = source.index('\ni=0; until grep -q "device ', begin)
        block = source[begin:end]
        cleanup = source[source.index('cleanup() {'):source.index('\nfor m in box dock;')]
        temp = tempfile.TemporaryDirectory(); self.addCleanup(temp.cleanup)
        root = Path(temp.name); app = root/'src'; work = root/'work'; tools = root/'bin'; shots = root/'shots'
        for p in [app/'e2e', app/'ios', work, tools, shots, root/'reports']:
            p.mkdir(parents=True)
        for p in [work/'herdr-anywhere', app/'ios/build.sh', tools/'xcrun', tools/'hc']:
            shutil.copyfile(self.fake, p); p.chmod(0o755)
        for name in ['ios-failure-logs.sh', 'ios-xcresult.sh']:
            shutil.copyfile(APP/'e2e'/name, app/'e2e'/name)
            (app/'e2e'/name).chmod(0o755)
        (app/'e2e/pair.ts').write_text("export const PAIR: string = 'placeholder'\n")
        (work/'serve.log').write_text('synthetic companion connection log\n')
        (work/'load.txt').write_text('synthetic load\n')
        (work/'started').touch()
        script = '''set -eu
work=$1; app=$2; shots=$3
bin="$work/herdr-anywhere"; hc=$(command -v hc)
port=7991; SIM_UDID=sample; id=sample.app
run="$work/run"; boxrun="$work/boxrun"; dockrun="$work/dockrun"; state="$work/state"
serve_pid=""; test_pid=""; sampler=""
placeholder=$(cat "$app/e2e/pair.ts")
L() { "$@"; }
autostart_guard_end() { :; }
''' + cleanup + '\n' + block + '\n' + ('exit 23\n' if failure == 'connection' else 'echo reached-launch\n')
        env = dict(os.environ, PATH=str(tools)+':'+os.environ['PATH'], PIN_PAIR_TEST_ROOT=str(root),
                   PIN_PAIR_TEST_BUILD_SECONDS=str(seconds), PIN_PAIR_TEST_FAIL=failure,
                   HM_DIAGNOSTIC_REPORTS=str(root/'reports'))
        result = subprocess.run(['sh', '-c', script, 'test', str(work), str(app), str(shots)],
                                env=env, capture_output=True, text=True)
        self.assertNotIn('fixture-', result.stdout+result.stderr, 'pairing value leaked to output')
        for artifact in shots.rglob('*'):
            if artifact.is_file():
                self.assertNotIn('fixture-', artifact.read_text(), 'pairing value leaked to an artifact')
        self.assertFalse(work.exists(), 'real cleanup did not remove the temporary work folder')
        self.assertIn('placeholder', (app/'e2e/pair.ts').read_text())
        return root, result

    def assert_logs(self, root, build=True):
        failure = root/'shots/failure'
        self.assertTrue((failure/'serve.log.txt').is_file(), 'cleanup lost the Companion log')
        self.assertEqual((failure/'serve.log.txt').read_text(), 'synthetic companion connection log\n')
        if build:
            self.assertEqual((failure/'build.log.txt').read_text(), 'synthetic build output\n')

    def test_fast_build_control(self):
        root, result = self.run_case(1)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((root/'accepted').exists())

    def test_expired_build_pair_is_replaced_after_install(self):
        root, result = self.run_case(601)
        self.assertEqual(result.returncode, 0, result.stderr)
        for name in ['accepted', 'pair-after-install', 'used-storage']:
            self.assertTrue((root/name).exists(), name)
        self.assertEqual((root/'issued-count').read_text(), '2')

    def test_pair_failure_prevents_launch_and_keeps_logs(self):
        root, result = self.run_case(1, 'pair')
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((root/'pair-failed').exists())
        self.assertFalse((root/'launched').exists())
        self.assert_logs(root)

    def test_storage_failure_prevents_launch_and_keeps_logs(self):
        root, result = self.run_case(1, 'storage')
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((root/'pair-after-install').exists())
        self.assertFalse((root/'launched').exists())
        self.assert_logs(root)

    def test_build_failure_keeps_early_logs(self):
        root, result = self.run_case(1, 'build')
        self.assertEqual(result.returncode, 1)
        self.assertFalse((root/'installed').exists())
        self.assert_logs(root)

    def test_launch_failure_keeps_early_logs(self):
        root, result = self.run_case(1, 'launch')
        self.assertEqual(result.returncode, 21)
        self.assert_logs(root)
        self.assertEqual((root/'shots/failure/launch.log.txt').read_text(), 'synthetic launch failure\n')

    def test_connection_failure_keeps_early_logs(self):
        root, result = self.run_case(1, 'connection')
        self.assertEqual(result.returncode, 23)
        self.assertTrue((root/'accepted').exists())
        self.assert_logs(root)


if __name__ == '__main__': unittest.main()
