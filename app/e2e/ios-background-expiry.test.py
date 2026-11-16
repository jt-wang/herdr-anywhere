"""Synthetic process identities; no CI log or device identity in fixtures."""
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from unittest.mock import MagicMock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('expiry', HERE / 'ios-background-expiry.py')
expiry = importlib.util.module_from_spec(spec)
spec.loader.exec_module(expiry)
BUNDLE = 'org.example.capture'
PID = 4242


def background(pid=PID, bundle=BUNDLE):
    return (f'2030-01-02 03:04:05.000 Df SpringBoard[90:ab] Application process state changed for {bundle}: '
            f'<State; pid: {pid}; taskState: Running; visibility: Background>')


def warning(pid=PID, bundle=BUNDLE):
    return f'Assertions for process will expire soon: [app<{bundle}((null))>:{pid}]'


def stack(pid=PID):
    return (f'Process: Capture [{pid}]\nCall graph:\n'
            '    10 Thread_1 DispatchQueue_1: com.apple.main-thread (serial)\n'
            '    + 10 UIApplicationMain (in UIKitCore)\n')


class ExpiryTest(unittest.TestCase):
    def test_background_25_seconds_and_first_expiry_only(self):
        t = expiry.Triggers(PID, BUNDLE)
        t.feed(background(), 100)
        self.assertEqual(t.due(124.9), [])
        self.assertEqual(t.due(125), ['background-25s'])
        t.feed(warning(), 132)
        self.assertEqual(t.due(132), ['expiry'])
        t.feed(background(), 140)
        t.feed(warning(), 141)
        self.assertEqual(t.due(200), [])

    def test_other_pid_app_and_foreground_never_trigger(self):
        t = expiry.Triggers(PID, BUNDLE)
        for line in [background(4343), warning(4343), background(bundle='org.example.other'),
                     warning(bundle='org.example.other'), background().replace('Background', 'Foreground')]:
            t.feed(line, 100)
        self.assertEqual(t.due(200), [])

    def test_expiry_before_background_is_ignored(self):
        t = expiry.Triggers(PID, BUNDLE)
        t.feed(warning(), 50)
        self.assertEqual(t.due(50), [])

    def test_sample_needs_matching_pid_main_thread_and_frame(self):
        self.assertTrue(expiry.valid_sample(stack(), PID))
        for text in ['', stack(4343), 'Process: Capture [4242]\nCall graph:\n',
                     stack().replace('com.apple.main-thread', 'worker'),
                     stack().split('    +')[0]]:
            self.assertFalse(expiry.valid_sample(text, PID), text)

    def test_sampler_has_exact_command_and_timeout(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder) / 'sample.txt'
            def run(command, **kwargs):
                self.assertEqual(command, ['/usr/bin/sample', '4242', '1', '10', '-mayDie', '-file', str(out)])
                self.assertEqual(kwargs['timeout'], 10)
                out.write_text(stack())
                return subprocess.CompletedProcess(command, 0, stdout='', stderr='')
            with patch.object(subprocess, 'run', side_effect=run) as runner:
                result = expiry.take_sample(PID, out)
            self.assertEqual(runner.call_count, 1)
            self.assertTrue(result.get('accepted'), result)

    def test_timeout_and_empty_success_are_not_accepted(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder) / 'sample.txt'
            with patch.object(subprocess, 'run', side_effect=subprocess.TimeoutExpired('sample', 10)):
                result = expiry.take_sample(PID, out)
            self.assertTrue(result.get('timeout'), result)
            self.assertFalse(result.get('accepted'), result)
            with patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, '', '')):
                result = expiry.take_sample(PID, out)
            self.assertFalse(result.get('accepted'), result)

    def test_public_copy_redacts_only_known_values_and_records_hashes(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); raw = root / 'raw'; raw.mkdir()
            link = 'http://127.0.0.1:9/#pair=aaaaaaaaaaaaaaaa'
            secret = root / 'secret'; secret.write_text(link)
            original = link + '\n#pair=aaaaaaaaaaaaaaaa\ntoken=aaaaaaaaaaaaaaaa\nUIKit task 12\n'
            (raw / 'app-debug.log').write_text(original)
            expiry.publish(raw, root / 'public', secret)
            out = root / 'public/app-debug.log'
            self.assertTrue(out.is_file(), 'debug log must survive cleanup')
            clean = out.read_text()
            self.assertEqual(len(clean), len(original))
            self.assertEqual(clean.count('\n'), original.count('\n'))
            self.assertIn('UIKit task 12', clean)
            shapes = re.search(r"^shapes='([^']+)'", (HERE / 'ci-shots-check.sh').read_text(), re.M)[1]
            self.assertIsNone(re.search(shapes, clean))
            manifest = json.loads((root / 'public/manifest.json').read_text())
            entry = manifest['files']['app-debug.log']
            self.assertNotEqual(entry['raw_sha256'], entry['public_sha256'])
            self.assertEqual(sum(entry['replacements'].values()), 3)

    def test_unknown_secret_stays_visible_to_original_upload_check(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); raw = root / 'raw'; raw.mkdir()
            secret = root / 'secret'; secret.write_text('http://127.0.0.1:9/#pair=aaaaaaaaaaaaaaaa')
            text = 'token=bbbbbbbbbbbbbbbb\n'
            (raw / 'app-debug.log').write_text(text)
            expiry.publish(raw, root / 'public', secret)
            out = root / 'public/app-debug.log'
            self.assertTrue(out.is_file())
            self.assertEqual(out.read_text(), text)
            shapes = re.search(r"^shapes='([^']+)'", (HERE / 'ci-shots-check.sh').read_text(), re.M)[1]
            self.assertIsNotNone(re.search(shapes, out.read_text()))

    def test_calibration_requires_live_stack_and_rejected_reaped_child(self):
        with tempfile.TemporaryDirectory() as folder:
            child = MagicMock(pid=4545)
            child.wait.return_value = 0
            with patch.object(subprocess, 'Popen', return_value=child), \
                 patch('os.kill', side_effect=ProcessLookupError), \
                 patch.object(expiry, 'take_sample', side_effect=[
                     {'accepted': True, 'exit': 0, 'timeout': False},
                     {'accepted': False, 'exit': 1, 'timeout': False}]) as samples:
                self.assertTrue(expiry.calibrate(PID, Path(folder)))
                self.assertEqual([call.args[0] for call in samples.call_args_list], [PID, 4545])
                child.wait.assert_called_once()

    def test_failed_positive_calibration_stops_before_negative(self):
        with tempfile.TemporaryDirectory() as folder:
            with patch.object(expiry, 'take_sample', return_value={'accepted': False}) as samples, \
                 patch.object(subprocess, 'Popen') as child:
                self.assertFalse(expiry.calibrate(PID, Path(folder)))
                samples.assert_called_once()
                child.assert_not_called()

    def test_workflow_cache_miss_does_not_build(self):
        action = (HERE.parents[1] / '.github/actions/nsh-ios/action.yml').read_text()
        miss = action.split('- name: Build it (none in the cache)', 1)[1].split('- uses: actions/cache/save', 1)[0]
        self.assertNotIn('rustup toolchain install', miss)
        self.assertNotIn('build-nsh-ios.sh', miss)
        self.assertIn('exit 1', miss)

    def test_calibration_precedes_xctest_and_debug_is_unfiltered(self):
        source = (HERE / 'ios-chat-four.sh').read_text()
        marker = 'ios-background-expiry.py" calibrate'
        self.assertIn(marker, source)
        self.assertLess(source.index(marker), source.index('xcodebuild test -quiet'))
        self.assertIn('--level debug --predicate \'process == "Herdr"\'', source)


if __name__ == '__main__':
    unittest.main()
