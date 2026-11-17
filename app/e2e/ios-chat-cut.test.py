"""Run the real arm/stop block with delayed Companion disconnect output."""
from pathlib import Path
import os
import subprocess
import tempfile
import time
import unittest

APP = Path(__file__).resolve().parents[1]


class BackgroundCutTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.bin = Path(cls.build.name)
        # This existing Go fake is a no-op when invoked as pkill; no real process is stopped.
        subprocess.run(['go', 'build', '-o', str(cls.bin / 'pkill'),
                        './internal/testfake/chatevidence'], cwd=APP.parent / 'companion', check=True)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def arm(self, lines):
        source = (APP / 'e2e/ios-chat-four.sh').read_text()
        start = source.index('    if [ -e "$ctl/arm-cut" ]; then')
        end = source.index('    if [ -e "$ctl/stop" ]; then', start)
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            ctl = root / 'ctl'; ctl.mkdir()
            (root / 'serve.log').write_text(''.join(lines))
            (ctl / 'arm-cut').touch()
            result = subprocess.run(['sh', '-c', 'set -eu\nwork=$1; ctl=$2\n' + source[start:end],
                                     'test', str(root), str(ctl)], capture_output=True, text=True, timeout=5)
            self.assertIn(result.returncode, [0, 1], result.stderr)
            return (ctl / 'cut-armed').exists()

    def test_late_old_disconnect_does_not_disarm_new_instance(self):
        self.assertTrue(self.arm([
            '2006/01/02 00:00:00 device sample connected (binary, hc pid 101)\n',
            '2006/01/02 00:00:01 device sample connected (binary, hc pid 202)\n',
            '2006/01/02 00:00:02 device sample disconnected (hc: signal: killed) hc pid 101\n',
        ]), 'old instance close incorrectly disarmed the new instance')

    def test_target_disconnect_rejects_arm(self):
        self.assertFalse(self.arm([
            '2006/01/02 00:00:00 device sample connected (binary, hc pid 202)\n',
            '2006/01/02 00:00:01 device sample disconnected (hc: signal: killed) hc pid 202\n',
        ]))

    def test_missing_connect_id_rejects_arm(self):
        self.assertFalse(self.arm(['2006/01/02 00:00:00 device sample connected (binary)\n']),
                         'unidentified connection was armed')

    def test_missing_disconnect_id_rejects_arm(self):
        self.assertFalse(self.arm([
            '2006/01/02 00:00:00 device sample connected (binary, hc pid 202)\n',
            '2006/01/02 00:00:01 device sample disconnected (hc: signal: killed)\n',
        ]), 'ambiguous same-device close was accepted as a live connection')

    def check_stop(self, delayed):
        source = (APP / 'e2e/ios-chat-four.sh').read_text()
        start = source.index('    if [ -e "$ctl/arm-cut" ]; then')
        end = source.index('    # A failed step asks', start)
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            ctl = root / 'ctl'; ctl.mkdir()
            log = root / 'serve.log'
            log.write_text('2006/01/02 00:00:00 device sample connected (binary, hc pid 101)\n'
                           '2006/01/02 00:00:01 device sample disconnected (hc: signal: killed) hc pid 101\n'
                           '2006/01/02 00:00:02 device sample connected (binary, hc pid 202)\n')
            (ctl / 'arm-cut').touch()
            arm_end = source.index('    if [ -e "$ctl/stop" ]; then', start)
            env = dict(os.environ, PATH=str(self.bin) + ':' + os.environ['PATH'])
            setup = 'set -eu\nwork=$1; ctl=$2; state=synthetic\n'
            # Arm before the background interval, as ChatFour does before pressing Home.
            arm = subprocess.run(['sh', '-c', setup + source[start:arm_end], 'test', str(root), str(ctl)],
                                 env=env, text=True, capture_output=True, timeout=5)
            self.assertEqual(arm.returncode, 0, arm.stderr)
            self.assertTrue((ctl / 'cut-armed').exists())
            self.assertEqual((ctl / 'cut-socket').read_text().split()[:2], ['3', 'sample'])
            (ctl / 'stop').touch()
            if not delayed:
                with log.open('a') as f: f.write('2006/01/02 00:00:03 device sample disconnected (hc: signal: killed) hc pid 202\n')
            proc = subprocess.Popen(['sh', '-c', setup + source[arm_end:end], 'test', str(root), str(ctl)],
                                    env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                if delayed:
                    # The real run's disconnect arrived after the fixed one-second check.
                    # Neither another device, an older instance, nor an unidentified close may acknowledge it.
                    with log.open('a') as f:
                        f.write('2006/01/02 00:00:03 device other disconnected (hc: signal: killed) hc pid 202\n')
                        f.write('2006/01/02 00:00:03 device sample disconnected (hc: signal: killed) hc pid 101\n')
                        f.write('2006/01/02 00:00:03 device sample disconnected (hc: signal: killed)\n')
                    time.sleep(2)
                    early_ack = (ctl / 'stopped').exists()
                    with log.open('a') as f: f.write('2006/01/02 00:00:04 device sample disconnected (hc: signal: killed) hc pid 202\n')
                stdout, stderr = proc.communicate(timeout=12)
                self.assertEqual(proc.returncode, 0, stdout + stderr)
                if delayed:
                    self.assertFalse(early_ack, 'stopped acknowledged before the original socket disconnect arrived')
                self.assertTrue((ctl / 'cut-disconnected').exists(), 'original socket close must be recorded before stopped')
                self.assertTrue((ctl / 'stopped').exists())
            finally:
                if proc.poll() is None:
                    proc.kill(); proc.communicate()

    def test_immediate_original_disconnect(self): self.check_stop(False)
    def test_delayed_original_disconnect_before_ack(self): self.check_stop(True)


if __name__ == '__main__': unittest.main()
