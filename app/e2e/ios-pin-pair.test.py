"""Run the pin script's real pairing/build/install/launch block with a virtual clock."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]

class PinPairTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.fake = Path(cls.build.name) / 'pinpair'
        subprocess.run(['go', 'build', '-o', str(cls.fake), './internal/testfake/pinpair'], cwd=APP.parent / 'companion', check=True)
    @classmethod
    def tearDownClass(cls): cls.build.cleanup()

    def run_case(self, seconds, failure=''):
        source = (APP / 'e2e/ios-pin-order.sh').read_text()
        begin = source.index('link=$(L env HERDR_ANYWHERE_STATE=')
        end = source.index('\nshot() {', begin)
        block = source[begin:end]
        temp = tempfile.TemporaryDirectory(); self.addCleanup(temp.cleanup)
        root = Path(temp.name); src=root/'src'; work=root/'work'; binpath=root/'bin'
        (src/'e2e').mkdir(parents=True); (src/'ios').mkdir(); work.mkdir(); binpath.mkdir()
        for path in [work/'herdr-anywhere',src/'ios/build.sh',binpath/'xcrun']:
            shutil.copyfile(self.fake,path);path.chmod(0o755)
        (src/'e2e/pair.ts').write_text("export const PAIR: string = 'placeholder'\n")
        script='set -eu\nwork=$1; src=$2; port=7991; SIM_UDID=sample; id=sample.app\nplaceholder=$(cat "$src/e2e/pair.ts")\nL() { "$@"; }\n'+block+'\necho reached-launch\n'
        env=dict(os.environ,PATH=str(binpath)+':'+os.environ['PATH'],PIN_PAIR_TEST_ROOT=str(root),
                 PIN_PAIR_TEST_BUILD_SECONDS=str(seconds),PIN_PAIR_TEST_FAIL=failure)
        result=subprocess.run(['sh','-c',script,'test',str(work),str(src)],env=env,capture_output=True,text=True)
        self.assertNotIn('fixture-',result.stdout+result.stderr,'pairing value leaked to output')
        return root,result

    def test_fast_build_control(self):
        root,result=self.run_case(1)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertTrue((root/'accepted').exists())

    def test_slow_build_uses_new_pair_after_install(self):
        root,result=self.run_case(601)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertTrue((root/'accepted').exists())
        self.assertTrue((root/'pair-after-install').exists())
        self.assertTrue((root/'used-storage').exists())
        self.assertEqual((root/'issued-count').read_text(),'2')

    def test_pair_failure_stops_before_launch(self):
        root,result=self.run_case(601,'pair')
        self.assertNotEqual(result.returncode,0)
        self.assertTrue((root/'pair-failed').exists(),'post-install pairing was not attempted')
        self.assertFalse((root/'launched').exists())
        self.assertNotIn('reached-launch',result.stdout)

    def test_storage_write_failure_stops_before_launch(self):
        root,result=self.run_case(601,'storage')
        self.assertNotEqual(result.returncode,0)
        self.assertTrue((root/'pair-after-install').exists(),'post-install pairing was not attempted')
        self.assertFalse((root/'launched').exists())
        self.assertNotIn('reached-launch',result.stdout)

if __name__ == '__main__': unittest.main()
