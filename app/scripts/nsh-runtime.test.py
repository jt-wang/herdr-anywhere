#!/usr/bin/env python3
"""Cache boundaries: mismatched, incomplete or corrupted native/web runtimes must fail closed."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('nsh-runtime.sh')
PATCHES = SCRIPT.parent.parent / 'ios/nsh-patches'


class RuntimeCacheTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='nsh runtime ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.key = subprocess.check_output([str(PATCHES / 'key.sh')])
        cli = self.root / 'cli'
        (cli / 'bin').mkdir(parents=True)
        (cli / 'bin/nsh').write_bytes(b'#!/bin/sh\nexit 0\n')
        (cli / 'bin/nsh').chmod(0o755)
        (cli / 'nsh-build.txt').write_bytes(self.key)
        self.sums(cli, ['bin/nsh', 'nsh-build.txt'])

    def sums(self, directory, names):
        (directory / 'SHA256SUMS').write_text(''.join(
            f'{hashlib.sha256((directory / name).read_bytes()).hexdigest()}  {name}\n' for name in names))

    def runtime(self, kind):
        dest = self.root / ('web' if kind == 'web' else 'embed/android')
        dest.mkdir(parents=True)
        files = ['nsh.wasm', 'nsh-dev.wasm', 'nsh.js'] if kind == 'web' else ['nsh.aar']
        for name in files:
            (dest / name).write_bytes(('sample ' + name).encode())
        if kind == 'web':
            (dest / 'nsh-web-build.txt').write_bytes(b'stack-size=8388608\n')
            files.append('nsh-web-build.txt')
        (dest / 'nsh-build.txt').write_bytes(self.key)
        self.sums(dest, files + ['nsh-build.txt'])
        if kind == 'android':
            standalone = self.root / 'android/release'
            names = ['classes.dex'] + [f'lib/{abi}/libnsh.so' for abi in ('arm64-v8a', 'armeabi-v7a', 'x86_64')]
            for name in names:
                path = standalone / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(('sample ' + name).encode())
            (standalone / 'nsh-build.txt').write_bytes(self.key)
            patches = SCRIPT.parent.parent / 'android/nsh-patches'
            recipe = b''.join(p.read_bytes() for p in sorted(patches.glob('0*.patch'))) + (patches / 'patch-aar.sh').read_bytes()
            (standalone / 'nsh-android-build.txt').write_text(hashlib.sha256(recipe).hexdigest() + '\n')
            self.sums(standalone, names + ['nsh-android-build.txt', 'nsh-build.txt'])
        return dest

    def verify(self, kind):
        return subprocess.run(['sh', str(SCRIPT), '--verify-' + kind, str(self.root)],
                              capture_output=True).returncode

    def test_matched_web_and_android_caches(self):
        for kind in ['web', 'android']:
            self.runtime(kind)
            self.assertEqual(self.verify(kind), 0)

    def test_corrupted_web_runtime_rejected(self):
        dest = self.runtime('web')
        (dest / 'nsh.wasm').write_bytes(b'changed')
        self.assertNotEqual(self.verify('web'), 0)

    def test_missing_development_wasm_rejected(self):
        dest = self.runtime('web')
        (dest / 'nsh-dev.wasm').unlink()
        self.assertNotEqual(self.verify('web'), 0)

    def test_old_web_stack_recipe_rejected(self):
        dest = self.runtime('web')
        (dest / 'nsh-web-build.txt').unlink()
        self.sums(dest, ['nsh.wasm', 'nsh-dev.wasm', 'nsh.js', 'nsh-build.txt'])
        self.assertNotEqual(self.verify('web'), 0)

    def test_incomplete_aar_checksum_manifest_rejected(self):
        dest = self.runtime('android')
        self.sums(dest, ['nsh-build.txt'])
        self.assertNotEqual(self.verify('android'), 0)

    def test_wrong_patch_stack_rejected_even_with_valid_checksums(self):
        dest = self.runtime('android')
        (dest / 'nsh-build.txt').write_bytes(b'old runtime\n')
        self.sums(dest, ['nsh.aar', 'nsh-build.txt'])
        self.assertNotEqual(self.verify('android'), 0)

    def test_corrupted_cli_rejected(self):
        self.runtime('android')
        (self.root / 'cli/bin/nsh').write_bytes(b'changed')
        self.assertNotEqual(self.verify('android'), 0)

    def test_standalone_android_dex_is_required(self):
        self.runtime('android')
        (self.root / 'android/release/classes.dex').unlink()
        self.assertNotEqual(self.verify('android'), 0)

    def test_standalone_android_abi_corruption_is_rejected(self):
        self.runtime('android')
        (self.root / 'android/release/lib/x86_64/libnsh.so').write_bytes(b'wrong native runtime')
        self.assertNotEqual(self.verify('android'), 0)

    def test_standalone_android_checksums_cannot_omit_the_dex(self):
        self.runtime('android')
        self.sums(self.root / 'android/release', ['nsh-build.txt'])
        self.assertNotEqual(self.verify('android'), 0)

    def android_cached_build(self, complete=False, corrupt_aar=False):
        dest = self.runtime('android')
        if not complete:
            (self.root / 'android/release/SHA256SUMS').unlink()
        if corrupt_aar:
            (dest / 'nsh.aar').write_bytes(b'corrupt embedding runtime')
        fake_bin = self.root / 'fake-bin'
        fake_bin.mkdir()
        curl = fake_bin / 'curl'
        curl.write_text('#!/bin/sh\necho "attempted Android Java rebuild" >&2\nexit 42\n')
        curl.chmod(0o755)
        # Stop at the external compiler dependency, without Rust, the SDK or any network access.
        return subprocess.run(['sh', str(SCRIPT), 'android', str(self.root / 'unused-source'), str(self.root)],
                              capture_output=True, env={**os.environ, 'PATH': str(fake_bin) + os.pathsep + os.environ['PATH']})

    def test_incomplete_android_cache_retries_preparing_the_java_runtime(self):
        result = self.android_cached_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'attempted Android Java rebuild', result.stderr)

    def test_partial_android_cache_never_rebuilds_from_a_corrupt_aar(self):
        result = self.android_cached_build(corrupt_aar=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(b'attempted Android Java rebuild', result.stderr)

    def test_complete_android_cache_build_reuses_verified_artifacts_without_compilers(self):
        result = self.android_cached_build(complete=True)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertIn(b'classes.dex: OK', result.stdout)
        self.assertNotIn(b'attempted Android Java rebuild', result.stderr)
        self.assertFalse((self.root / 'unused-source').exists())


if __name__ == '__main__':
    unittest.main()
