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
TID = 1234
LAUNCH = "11111111-2222-3333-4444-555555555555"


def background(pid=PID, bundle=BUNDLE):
    return (f'2030-01-02 03:04:05.000 Df SpringBoard[90:ab] Application process state changed for {bundle}: '
            f'<State; pid: {pid}; taskState: Running; visibility: Background>')


def warning(pid=PID, bundle=BUNDLE):
    return f'Assertions for process will expire soon: [app<{bundle}((null))>:{pid}]'


def stack(pid=PID, tid=TID):
    return (f'Process: Capture [{pid}]\nUUID: AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE\n'
            '  Thread 0x4567    DispatchQueue 12    10 samples\n'
            '  10 ??? (worker + 12) [0x100001000]\n'
            f'  Thread 0x{tid:x}    DispatchQueue 34    10 samples\n'
            '  10 ??? (UIKitCore + 1234) [0x100002000]\n'
            '  Binary Images:\n')


def marker(pid=PID, tid=TID, launch=LAUNCH):
    return f'log prefix HM_EXPIRY_MAIN pid={pid} tid={tid} launch={launch}\n'


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

    def test_spindump_matches_recorded_tid_not_first_thread(self):
        self.assertTrue(expiry.valid_sample(stack(), PID, TID))
        for value in ['', stack(4343), stack(tid=5678),
                      stack().replace('  10 ??? (UIKitCore + 1234) [0x100002000]\n', ''),
                      stack() + stack(4343)]:
            self.assertFalse(expiry.valid_sample(value, PID, TID), value)

    def test_marker_matches_original_pid_and_distinguishes_launch(self):
        found = expiry.main_identity(marker(4343) + marker(), PID)
        self.assertEqual(found, {'pid': PID, 'tid': TID, 'launch': LAUNCH})
        for value in ['', marker(4343), marker(tid=0),
                      marker() + marker(launch='AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE')]:
            with self.assertRaises(ValueError):
                expiry.main_identity(value, PID)

    def test_sampler_has_exact_command_and_timeout(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder) / 'sample.spindump'
            child = MagicMock(returncode=0)
            def communicate(**kwargs):
                self.assertEqual(kwargs, {'timeout': 10})
                out.write_bytes(stack().encode() + b'\n\nSpindump binary format\nPRIVATE_BINARY')
                return b'', b''
            child.communicate.side_effect = communicate
            with patch.object(subprocess, 'Popen', return_value=child) as runner:
                result = expiry.take_sample(PID, out, TID)
            self.assertEqual(runner.call_args.args[0], ['sudo', '-n', '/usr/sbin/spindump', '4242',
                '1', '10', '-onlyTarget', '-noSymbolicate', '-noProcessingWhileSampling',
                '-timelimit', '8', '-o', str(out)])
            self.assertTrue(result['accepted'], result)
            self.assertEqual(result['tid'], TID)
            self.assertEqual(out.with_suffix('.raw.txt').read_text(), stack())

    def test_timeout_relays_term_and_empty_success_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder) / 'sample.spindump'
            child = MagicMock(returncode=-15)
            child.communicate.side_effect = [subprocess.TimeoutExpired('spindump', 10), (b'', b'')]
            with patch.object(subprocess, 'Popen', return_value=child):
                result = expiry.take_sample(PID, out, TID)
            child.terminate.assert_called_once()
            self.assertEqual(child.communicate.call_args.kwargs, {'timeout': 2})
            self.assertTrue(result['timeout'])
            self.assertFalse(result['accepted'])
            child = MagicMock(returncode=0)
            child.communicate.return_value = (b'', b'')
            with patch.object(subprocess, 'Popen', return_value=child):
                self.assertFalse(expiry.take_sample(PID, out, TID)['accepted'])

    def test_diagnostic_source_copy_and_spec_leave_production_unchanged(self):
        ios = HERE.parent / 'ios'
        original = (ios / 'Sources/App.swift').read_bytes()
        project = (ios / 'project.yml').read_bytes()
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder)
            expiry.prepare(ios, out)
            copy = (out / 'Sources/App.swift').read_text()
            launch = copy.split('didFinishLaunchingWithOptions', 1)[1].split('hideWebKeyboardBar()', 1)[0]
            for item in ['Thread.isMainThread', 'pthread_threadid_np(nil, &', 'getpid()',
                         'UUID().uuidString', 'HM_EXPIRY_MAIN pid=']:
                self.assertIn(item, launch)
            self.assertIn(str(out / 'Sources'), (out / 'project.yml').read_text())
            self.assertEqual((out / 'Sources/Notify.swift').read_bytes(), (ios / 'Sources/Notify.swift').read_bytes())
        self.assertEqual((ios / 'Sources/App.swift').read_bytes(), original)
        self.assertEqual((ios / 'project.yml').read_bytes(), project)

    def test_build_copy_is_opt_in_e2e_simulator_and_cleanup_decodes_after_watch(self):
        build = (HERE.parent / 'ios/build.sh').read_text()
        gate = 'if [ -n "${HM_TEST_MAIN_THREAD_MARKER:-}" ] && [ "$entry" = ios-e2e.tsx ] && [ "$sdk" = simulator ]; then'
        self.assertIn(gate, build)
        block = build.split(gate, 1)[1].split('fi', 1)[0]
        self.assertIn('ios-background-expiry.py" prepare', block)
        self.assertIn('--project-root "$ios"', block)
        self.assertIn('else\n  (cd "$ios" && xcodegen generate --quiet)', build)
        source = (HERE / 'ios-chat-four.sh').read_text()
        self.assertIn('HM_TEST_MAIN_THREAD_MARKER="$expiry_raw"', source)
        cleanup = source.split('cleanup() {', 1)[1].split('trap cleanup EXIT', 1)[0]
        self.assertLess(cleanup.index('wait "$expiry_pid"'), cleanup.index('ios-background-expiry.py" decode'))
        self.assertLess(cleanup.index('ios-background-expiry.py" decode'), cleanup.index('ios-background-expiry.py" publish'))

    def test_offline_decode_uses_saved_file_and_public_omits_binary_footer(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); raw = root / 'raw'; raw.mkdir()
            saved = raw / 'expiry.spindump'; data = stack().encode() + b'\n\nSpindump binary format\nPRIVATE_BINARY'
            saved.write_bytes(data)
            secret = root / 'secret'; secret.write_text('http://127.0.0.1:9/#pair=aaaaaaaaaaaaaaaa')
            def run(command, output):
                self.assertEqual(command, ['/usr/sbin/spindump', '-i', str(saved), '-symbolicate',
                                          '-timelimit', '8', '-o', str(raw / 'expiry.decoded')])
                Path(output).write_bytes(data)
                return {'exit': 0, 'timeout': False}
            with patch.object(expiry, 'command', side_effect=run):
                self.assertTrue(expiry.decode(raw))
            self.assertEqual(saved.read_bytes(), data)
            expiry.publish(raw, root / 'public', secret)
            self.assertEqual((root / 'public/expiry.txt').read_text(), stack())
            self.assertFalse((root / 'public/expiry.spindump').exists())
            self.assertFalse((root / 'public/expiry.decoded').exists())
            self.assertNotIn(b'PRIVATE_BINARY', b''.join(p.read_bytes() for p in (root / 'public').iterdir()))

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
            (raw / 'other.log').write_text(text)
            expiry.publish(raw, root / 'public', secret)
            out = root / 'public/other.log'
            self.assertTrue(out.is_file())
            self.assertEqual(out.read_text(), text)
            shapes = re.search(r"^shapes='([^']+)'", (HERE / 'ci-shots-check.sh').read_text(), re.M)[1]
            self.assertIsNotNone(re.search(shapes, out.read_text()))

    def test_debug_copy_masks_original_shapes_and_keeps_ordinary_events(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); raw = root / 'raw'; raw.mkdir()
            secret = root / 'secret'; secret.write_text('http://127.0.0.1:9/#pair=aaaaaaaaaaaaaaaa')
            normal = marker() + 'UIKit task 12 began\nUIKit task 12 ended\n'
            original = (normal + 'token=bbbbbbbbbbbbbbbb\n#pair=bbbbbbbbbbbbbbbb\n'
                        'herdr1:bbbbbbbbbbbbbbbb\n')
            source = raw / 'app-debug.log'; source.write_text(original)
            shapes = re.search(r"^shapes='([^']+)'", (HERE / 'ci-shots-check.sh').read_text(), re.M)[1]
            # The CI byte scanner's exact expression must reject the unmasked copy first.
            self.assertEqual(subprocess.run(['grep', '-qE', shapes, str(source)]).returncode, 0)
            expiry.publish(raw, root / 'public', secret)
            clean = root / 'public/app-debug.log'
            self.assertEqual(subprocess.run(['grep', '-qE', shapes, str(clean)]).returncode, 1)
            self.assertEqual(source.read_text(), original)
            self.assertTrue(clean.read_text().startswith(normal))
            self.assertEqual(len(clean.read_bytes()), len(source.read_bytes()))
            self.assertEqual(clean.read_text().count('\n'), original.count('\n'))
            manifest = json.loads((root / 'public/manifest.json').read_text())
            self.assertEqual(manifest['files']['app-debug.log']['replacements']['shapes'], 3)

    def test_targeted_ci_passes_existing_agent_parameter(self):
        workflow = (HERE.parents[1] / '.github/workflows/devices.yml').read_text()
        chat = workflow.split('            chat27)\n', 1)[1].split(';;', 1)[0]
        self.assertIn('AGENTS=claude-code', chat)
        shell = (HERE / 'ios-chat-four.sh').read_text()
        swift = (HERE / 'ios-uitest/tests/ChatFour.swift').read_text()
        self.assertIn('agents=${AGENTS:-claude-code codex pi opencode}', shell)
        self.assertIn('TEST_RUNNER_AGENTS="$agents"', shell)
        self.assertIn('env["AGENTS"]', swift)

    def test_calibration_requires_actual_herdr_marker_and_stack(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder); (out / 'app-debug.log').write_text(marker())
            with patch.object(expiry, 'take_sample', return_value={'accepted': True, 'exit': 0, 'timeout': False}) as samples:
                self.assertTrue(expiry.calibrate(PID, out))
            samples.assert_called_once_with(PID, out / 'calibration-live.spindump', TID)
            self.assertEqual(json.loads((out / 'calibration.json').read_text())['identity'],
                             {'pid': PID, 'tid': TID, 'launch': LAUNCH})

    def test_missing_marker_stops_before_capture(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder); (out / 'app-debug.log').write_text(marker(4343))
            with patch.object(expiry, 'take_sample') as samples:
                self.assertFalse(expiry.calibrate(PID, out))
            samples.assert_not_called()

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
