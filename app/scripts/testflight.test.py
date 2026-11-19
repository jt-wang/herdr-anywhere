#!/usr/bin/env python3
import importlib.util
import base64
import datetime
import json
import os
import signal
import sys
import plistlib
import subprocess
import zipfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('testflight', Path(__file__).with_name('testflight.py'))
tf = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tf)

class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.env = {
            'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REPOSITORY': 'jt-wang/herdr-anywhere',
            'GITHUB_REF': 'refs/heads/main', 'TF_DEFAULT_BRANCH': 'main',
            'GITHUB_SHA': 'a' * 40, 'GITHUB_WORKFLOW_SHA': 'a' * 40,
            'GITHUB_WORKFLOW_REF': 'jt-wang/herdr-anywhere/.github/workflows/testflight.yml@refs/heads/main',
        }

    def run_release(self, **kwargs):
        return tf.release(self.root, '0.2.0', '26', 'a' * 40, env=self.env, **kwargs)

    def test_missing_configuration_fails_before_any_external_command(self):
        self.env.pop('APPLE_TEAM_ID', None)
        with patch.object(tf, 'subprocess', create=True) as external:
            with self.assertRaisesRegex(tf.ReleaseError, 'APPLE_TEAM_ID'):
                self.run_release(config_only=True)
            external.run.assert_not_called()

    def test_only_default_branch_workflow_and_exact_sha_are_accepted(self):
        self.run_release(request_only=True)
        invalid = [
            ('GITHUB_EVENT_NAME', 'pull_request'), ('GITHUB_REF', 'refs/heads/feature'),
            ('GITHUB_WORKFLOW_SHA', 'b' * 40), ('GITHUB_SHA', 'b' * 40),
            ('GITHUB_WORKFLOW_REF', 'jt-wang/herdr-anywhere/.github/workflows/testflight.yml@refs/heads/feature'),
            ('GITHUB_REPOSITORY', 'someone/fork'),
        ]
        for key, value in invalid:
            with self.subTest(key=key), patch.dict(self.env, {key: value}):
                with self.assertRaisesRegex(tf.ReleaseError, 'Trusted'):
                    self.run_release(request_only=True)

class ArchiveTest(ReleaseTest):
    def setUp(self):
        super().setUp()
        self.env.update({
            'ASC_KEY_ID': 'SYNTHETIC', 'ASC_ISSUER_ID': '00000000-0000-4000-8000-000000000000',
            'ASC_PRIVATE_KEY_P8': 'synthetic-private-key', 'APPLE_TEAM_ID': 'AAAAAAAAAA',
            'IOS_DISTRIBUTION_P12_BASE64': base64.b64encode(b'synthetic-p12').decode(),
            'IOS_DISTRIBUTION_P12_PASSWORD': 'synthetic-password',
            'IOS_PROVISION_PROFILE_BASE64': base64.b64encode(b'synthetic-profile').decode(),
            'HOME': str(self.root / 'home'), 'RUNNER_TEMP': str(self.root / 'temp'),
        })
        (self.root / 'temp').mkdir()
        self.originals = {
            'app/ios/project.yml': '    MARKETING_VERSION: "0.1.0"\n    CURRENT_PROJECT_VERSION: "1"\n',
            'app/nsh.json': '{"id":"io.jingtao.herdranywhere","version":"0.1.0","build":1}',
            'app/src/channel.ts': "export const CHANNEL: 'source' | 'store' = 'source'\n",
            'app/assets/sounds/tone.mp3': 'synthetic-sound',
        }
        for name, content in self.originals.items():
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        self.profile = {
            'UUID': '00000000-0000-4000-8000-000000000000', 'TeamIdentifier': ['AAAAAAAAAA'],
            'ExpirationDate': datetime.datetime.now() + datetime.timedelta(days=30),
            'Entitlements': {'application-identifier': 'AAAAAAAAAA.io.jingtao.herdranywhere',
                             'get-task-allow': False, 'beta-reports-active': True},
        }
        self.source_sha = 'a' * 40
        self.source_dirty = False
        self.child_envs = []
        self.commands = []
        self.secrets_seen = []
        self.fail_at = None
        self.bad_ipa = False
        self.symbols = False
        self.export_options = None
        self.patcher = patch.object(tf.subprocess, 'run', side_effect=self.external)
        self.patcher.start()
        self.addCleanup(self.patcher.stop)
        self.run_release(prepare_only=True)
        self.commands.clear()
        self.child_envs.clear()

    def external(self, args, **kwargs):
        args = list(map(str, args))
        self.commands.append(args)
        self.child_envs.append((args, kwargs['env']))
        out = b''
        if self.fail_at and self.fail_at in args:
            return subprocess.CompletedProcess(args, 17, b'', b'private tool detail')
        if args[0] == 'git' and args[-2:] == ['rev-parse', 'HEAD']:
            out = (self.source_sha + '\n').encode()
        elif args[0] == 'git' and 'diff' in args:
            return subprocess.CompletedProcess(args, 1 if self.source_dirty else 0, b'', b'')
        elif args[:3] == ['security', 'cms', '-D']:
            out = plistlib.dumps(self.profile)
        elif args[:3] == ['security', 'list-keychains', '-d'] and '-s' not in args:
            out = b'"existing.keychain-db"\n'
        elif args[:2] == ['security', 'import']:
            self.secrets_seen.append(Path(args[2]))
            self.assertEqual(Path(args[2]).stat().st_mode & 0o777, 0o600)
        elif args[0].endswith('/build.sh'):
            self.assertIn("= 'store'", (self.root / 'app/src/channel.ts').read_text())
            self.assertEqual(json.loads((self.root / 'app/nsh.json').read_text())['version'], '0.2.0')
            inputs = {
                'app/ios/Herdr.xcodeproj/project.pbxproj': 'synthetic generated project',
                'app/ios/nsh/bundle.nshc': 'synthetic store bytecode',
                'app/ios/Info.plist': 'synthetic generated info',
                'third_party/tailpocket-342ebe5/build/apple/TailPocketMobile.xcframework/Info.plist': 'synthetic mobile library',
                'third_party/tailpocket-342ebe5/sdk/apple/NSH/NativeShell.xcframework/ios-arm64/NativeShell.framework/NativeShell': 'synthetic runtime',
            }
            for name, content in inputs.items():
                path = self.root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
        elif '-exportArchive' in args:
            self.export_options = plistlib.loads(Path(args[args.index('-exportOptionsPlist') + 1]).read_bytes())
            folder = Path(args[args.index('-exportPath') + 1]); folder.mkdir(parents=True)
            info = {'CFBundleIdentifier': 'io.jingtao.herdranywhere', 'CFBundleShortVersionString': '0.2.0',
                    'CFBundleVersion': 'wrong' if self.bad_ipa else '26', 'DTPlatformName': 'iphoneos',
                    'ITSAppUsesNonExemptEncryption': False, 'CFBundleExecutable': 'Herdr',
                    'CFBundleIcons': {'CFBundlePrimaryIcon': {'CFBundleIconName': 'AppIcon'}}}
            with zipfile.ZipFile(folder / 'Herdr.ipa', 'w') as z:
                for name, value in {'Info.plist': plistlib.dumps(info), 'Herdr': getattr(self, 'binary_data', b'synthetic-binary'),
                                    'PrivacyInfo.xcprivacy': plistlib.dumps({'NSPrivacyAccessedAPITypes': []}),
                                    'embedded.mobileprovision': b'synthetic-profile', 'nsh/bundle.nshc': b'bytecode',
                                    'nsh/assets/assets/sounds/tone.mp3': b'synthetic-sound'}.items():
                    z.writestr('Payload/Herdr.app/' + name, value)
                if self.symbols:
                    z.writestr('Symbols/synthetic.symbols', b'synthetic-symbols')
        elif args[:2] == ['codesign', '-dvv']:
            return subprocess.CompletedProcess(args, 0, b'', b'Authority=Apple Distribution: Synthetic\n')
        elif '--validate-app' in args or '--upload-app' in args:
            key = Path(kwargs['env']['API_PRIVATE_KEYS_DIR']) / 'AuthKey_SYNTHETIC.p8'
            self.assertEqual(key.read_text(), 'synthetic-private-key')
            self.assertEqual(key.stat().st_mode & 0o777, 0o600)
            self.secrets_seen.append(key)
        return subprocess.CompletedProcess(args, 0, out, b'')

    def assert_cleaned(self):
        for name, content in self.originals.items():
            self.assertEqual((self.root / name).read_text(), content)
        self.assertFalse((self.root / 'app/ios/Signing.local.xcconfig').exists())
        self.assertTrue(all(not p.exists() for p in self.secrets_seen))
        self.assertEqual(list((self.root / 'temp').iterdir()), [])
        self.assertTrue(any(c[:2] == ['security', 'delete-keychain'] for c in self.commands))
        self.assertIn(['security', 'list-keychains', '-d', 'user', '-s', 'existing.keychain-db'], self.commands)
        self.assertFalse(list((self.root / 'home').rglob('*.mobileprovision')))

    def test_binary_control_byte_terminates_synthetic_home(self):
        # QuickJS bytecode stores this approved synthetic home before byte 0x01.
        self.binary_data = b'/Users/sam\x01'
        self.run_release(upload=True)
        self.assertTrue(any('--upload-app' in c for c in self.commands))
        self.assert_cleaned()

    def test_private_home_still_blocks_upload_before_control_byte(self):
        self.binary_data = b'/Users/samuel\x01'
        with self.assertRaisesRegex(tf.ReleaseError, 'private build path'):
            self.run_release(upload=True)
        self.assertFalse(any('--validate-app' in c or '--upload-app' in c for c in self.commands))
        self.assert_cleaned()

    def test_validate_builds_checks_and_cleans_without_uploading(self):
        for key in ('ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8'):
            self.env.pop(key)
        self.run_release()
        self.assertEqual(sum('--validate-app' in c for c in self.commands), 0)
        self.assertFalse(any('--upload-app' in c for c in self.commands))
        receipt = json.loads((self.root / 'app/ios/build/testflight/receipt.json').read_text())
        self.assertEqual(receipt['uploaded'], False)
        self.assertEqual(receipt['local_checked'], True)
        self.assertEqual(receipt['validated'], False)
        self.assertEqual(receipt['sha'], 'a' * 40)
        self.assert_cleaned()

    def test_default_does_not_contact_asc_even_with_credentials_present(self):
        self.run_release()
        self.assertFalse(any(c[:2] == ['xcrun', 'altool'] for c in self.commands))
        self.assert_cleaned()

    def test_upload_explicitly_validates_then_uploads_once(self):
        self.run_release(upload=True)
        actions = [c[2] for c in self.commands if c[:2] == ['xcrun', 'altool']]
        self.assertEqual(actions, ['--validate-app', '--upload-app'])
        self.assert_cleaned()

    def test_upload_missing_api_key_fails_before_build(self):
        del self.env['ASC_PRIVATE_KEY_P8']
        with self.assertRaisesRegex(tf.ReleaseError, 'ASC_PRIVATE_KEY_P8'):
            self.run_release(upload=True)
        self.assertEqual(self.commands, [])

    def test_external_failures_remain_failures_and_clean_up(self):
        for phase in ('archive', '--validate-app', '--upload-app'):
            with self.subTest(phase=phase):
                self.fail_at = phase
                with self.assertRaisesRegex(tf.ReleaseError, 'exit 17'):
                    self.run_release(upload=True)
                self.assert_cleaned()
                self.assertFalse((self.root / 'app/ios/build/testflight/receipt.json').exists())
                import shutil
                shutil.rmtree(self.root / 'app/ios/build/testflight')
                self.commands.clear()

    def test_wrong_ipa_build_prevents_validation_and_upload(self):
        self.bad_ipa = True
        with self.assertRaisesRegex(tf.ReleaseError, 'metadata mismatch'):
            self.run_release(upload=True)
        self.assertFalse(any(c[:2] == ['xcrun', 'altool'] for c in self.commands))
        self.assert_cleaned()

    def test_wrong_profile_rejected_before_import(self):
        self.profile['Entitlements']['get-task-allow'] = True
        with self.assertRaisesRegex(tf.ReleaseError, 'App Store profile'):
            self.run_release()
        self.assertFalse(any(c[:2] == ['security', 'import'] for c in self.commands))
        self.assertEqual(list((self.root / 'temp').iterdir()), [])

    def test_apple_api_credentials_never_enter_build_or_security_environment(self):
        self.run_release(upload=True)
        for args, child_env in self.child_envs:
            with self.subTest(program=args[0]):
                self.assertFalse(any(key.startswith('ASC_') for key in child_env))
                if args[:2] != ['xcrun', 'altool']:
                    self.assertNotIn('API_PRIVATE_KEYS_DIR', child_env)

    def test_symbols_are_neither_requested_nor_shipped(self):
        self.run_release()
        self.assertIs(self.export_options['uploadSymbols'], False)

    def test_symbols_in_ipa_fail_before_contacting_apple(self):
        self.symbols = True
        with self.assertRaisesRegex(tf.ReleaseError, 'Symbols'):
            self.run_release(upload=True)
        self.assertFalse(any(c[:2] == ['xcrun', 'altool'] for c in self.commands))
        self.assert_cleaned()


    def test_build_and_signing_children_have_no_raw_secrets(self):
        self.run_release()
        secret_names = {'APPLE_TEAM_ID', 'IOS_DISTRIBUTION_P12_BASE64', 'IOS_DISTRIBUTION_P12_PASSWORD',
                        'IOS_PROVISION_PROFILE_BASE64', 'ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8'}
        for args, child_env in self.child_envs:
            self.assertFalse(secret_names.intersection(child_env), args[0])

    def test_signing_never_runs_dependency_prepare(self):
        self.run_release()
        self.assertFalse(any(c[0].endswith('/build.sh') for c in self.commands),
                         'signing invokes dependency compilation')

    def test_missing_prepared_inputs_fails_before_signing(self):
        (self.root / 'app/ios/build/testflight-prepared.json').unlink(missing_ok=True)
        with self.assertRaisesRegex(tf.ReleaseError, 'prepared'):
            self.run_release()
        self.assertFalse(any(c[0] == 'security' for c in self.commands))

    def test_prepare_only_failure_does_not_import_or_reuse_old_receipt(self):
        marker = self.root / 'app/ios/build/testflight-prepared.json'
        marker.parent.mkdir(parents=True, exist_ok=True)
        marker.write_text('{}')
        self.fail_at = 'prepare'
        with self.assertRaisesRegex(tf.ReleaseError, 'exit 17'):
            self.run_release(prepare_only=True)
        self.assertFalse(marker.exists())
        self.assertFalse(any(c[0] == 'security' for c in self.commands))


    def test_prepare_needs_no_signing_configuration_and_restores_sources(self):
        for key in list(self.env):
            if key == 'APPLE_TEAM_ID' or key.startswith(('ASC_', 'IOS_')):
                del self.env[key]
        self.run_release(prepare_only=True)
        self.assertTrue(any(c[0].endswith('/build.sh') for c in self.commands))
        self.assertFalse(any(c[0] == 'security' for c in self.commands))
        for name, content in self.originals.items():
            self.assertEqual((self.root / name).read_text(), content)
        for _, child_env in self.child_envs:
            self.assertFalse(any(k == 'APPLE_TEAM_ID' or k.startswith(('ASC_', 'IOS_')) for k in child_env))

    def test_prepare_filters_raw_credentials_even_if_caller_has_them(self):
        self.run_release(prepare_only=True)
        self.assertFalse(any(c[0] == 'security' for c in self.commands))
        for args, child_env in self.child_envs:
            self.assertFalse(any(k == 'APPLE_TEAM_ID' or k.startswith(('ASC_', 'IOS_DISTRIBUTION_', 'IOS_PROVISION_')) for k in child_env), args[0])

    def test_changed_prepared_artifact_is_rejected_before_signing(self):
        for name in ['app/ios/Herdr.xcodeproj/project.pbxproj', 'app/ios/nsh/bundle.nshc',
                     'third_party/tailpocket-342ebe5/build/apple/TailPocketMobile.xcframework/Info.plist',
                     'third_party/tailpocket-342ebe5/sdk/apple/NSH/NativeShell.xcframework/ios-arm64/NativeShell.framework/NativeShell']:
            with self.subTest(name=name):
                file = self.root / name
                original = file.read_bytes()
                file.write_bytes(original + b'changed')
                with self.assertRaisesRegex(tf.ReleaseError, 'hash mismatch'):
                    self.run_release()
                self.assertFalse(any(c[0] == 'security' for c in self.commands))
                file.write_bytes(original)

    def test_wrong_source_or_request_is_rejected_before_signing(self):
        self.source_sha = 'b' * 40
        with self.assertRaisesRegex(tf.ReleaseError, 'SHA mismatch'):
            self.run_release()
        self.source_sha = 'a' * 40
        self.source_dirty = True
        with self.assertRaisesRegex(tf.ReleaseError, 'git failed'):
            self.run_release()
        self.source_dirty = False
        marker = self.root / 'app/ios/build/testflight-prepared.json'
        record = json.loads(marker.read_text())
        record['build'] = '25'
        marker.write_text(json.dumps(record))
        with self.assertRaisesRegex(tf.ReleaseError, 'request mismatch'):
            self.run_release()
        self.assertFalse(any(c[0] == 'security' for c in self.commands))


class SignalTest(unittest.TestCase):
    def test_sigterm_and_sigint_clean_up_within_bound(self):
        harness = r"""
import importlib.util, os, signal, sys
from pathlib import Path
from unittest.mock import patch
spec = importlib.util.spec_from_file_location('cases', sys.argv[1])
cases = importlib.util.module_from_spec(spec); spec.loader.exec_module(cases)
c = cases.ArchiveTest('test_validate_builds_checks_and_cleans_without_uploading'); c.setUp()
original = c.external

def interrupt(args, **kwargs):
    if 'archive' in args:
        os.kill(os.getpid(), int(sys.argv[2]))
    if args[:2] in (['security', 'delete-keychain'],) or (args[:4] == ['security','list-keychains','-d','user'] and '-s' in args and 'existing.keychain-db' in args and len(args)==6):
        assert kwargs.get('timeout') == 3, 'cleanup command must be bounded'
    return original(args, **kwargs)

try:
    c.patcher.stop()
    with patch.object(cases.tf.subprocess, 'run', side_effect=interrupt), patch.dict(os.environ, c.env, clear=True), patch.object(sys, 'argv', ['testflight.py','--version','0.2.0','--build','26','--expected-sha','a'*40]):
        cases.tf.__file__ = str(c.root/'app/scripts/testflight.py')
        result = cases.tf.main()
    assert result == 1, result
    c.assert_cleaned()
    assert not (c.root/'app/ios/build/testflight/receipt.json').exists()
finally:
    c.doCleanups()
"""
        for sig in (signal.SIGTERM, signal.SIGINT):
            with self.subTest(signal=sig):
                result = subprocess.run([sys.executable, '-c', harness, str(Path(__file__).resolve()), str(int(sig))], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

if __name__ == '__main__':
    unittest.main()
