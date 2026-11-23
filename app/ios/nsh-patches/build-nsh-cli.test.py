"""Exercise the real CLI builder with Go Git/Rust stand-ins, without compiling nsh."""
from pathlib import Path
import hashlib
import os
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
PATCHES = ROOT / 'app/ios/nsh-patches'


class CliBuildTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.compiled = tempfile.TemporaryDirectory()
        cls.fake = Path(cls.compiled.name) / 'fake'
        subprocess.run(['go', 'build', '-o', str(cls.fake), './internal/testfake/nshcli'], cwd=ROOT / 'companion', check=True)

    @classmethod
    def tearDownClass(cls):
        cls.compiled.cleanup()

    def setUp(self):
        tmp = tempfile.TemporaryDirectory(prefix='nsh cli '); self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name).resolve()
        self.patches = self.root / 'patches'
        shutil.copytree(PATCHES, self.patches)
        self.src = self.root / 'source'
        (self.src / '.git').mkdir(parents=True)
        self.out = self.root / 'out'
        tools = self.root / 'tools'; tools.mkdir()
        for name in ['git', 'rustup']:
            shutil.copyfile(self.fake, tools / name)
            (tools / name).chmod(0o755)
        self.env = dict(os.environ, PATH=str(tools) + ':' + os.environ['PATH'], CC_SHELL_ESCAPED_FLAGS='',
                        NSH_CLI_TEST_ROOT=str(self.root), CARGO_HOME=str(self.root / 'cargo'))
        self.key = subprocess.check_output(['sh', str(self.patches / 'key.sh')], text=True).strip()

    def run_builder(self, failure='', verify=False):
        args = ['--verify', str(self.out)] if verify else [str(self.src), str(self.out)]
        return subprocess.run(['sh', str(self.patches / 'build-nsh-cli.sh'), *args],
                              env=dict(self.env, NSH_CLI_TEST_FAIL=failure), capture_output=True, text=True)

    def calls(self):
        p = self.root / 'calls'
        return p.read_text() if p.exists() else ''

    def assert_not_published(self):
        self.assertFalse((self.out / 'nsh-build.txt').exists())
        self.assertFalse((self.out / 'SHA256SUMS').exists())

    def test_builds_eight_patches_with_key_and_checksum(self):
        r = self.run_builder()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue((self.out / 'bin/nsh').is_file(), 'builder did not publish CLI')
        self.assertEqual((self.out / 'nsh-build.txt').read_text().strip(), self.key)
        calls = self.calls().splitlines()
        applied = [line for line in calls if ' apply ' in line and '--check' not in line]
        self.assertEqual(len(applied), 8)
        self.assertIn('0008-', applied[-1])
        self.assertEqual(len([line for line in calls if line.startswith('rustup ')]), 1)
        subprocess.run(['shasum', '-a', '256', '-c', 'SHA256SUMS'], cwd=self.out, check=True, capture_output=True)
        self.assertEqual(self.run_builder(verify=True).returncode, 0)

    def test_missing_fifth_patch_rejected_before_build(self):
        next(self.patches.glob('0005-*.patch')).unlink()
        r = self.run_builder()
        self.assertNotEqual(r.returncode, 0, 'missing fifth patch accepted')
        self.assertNotIn('rustup ', self.calls())
        self.assert_not_published()

    def test_missing_shimmer_patch_rejected_before_build(self):
        next(self.patches.glob('0006-*.patch')).unlink()
        r = self.run_builder()
        self.assertNotEqual(r.returncode, 0, 'missing shimmer patch accepted')
        self.assertNotIn('rustup ', self.calls())
        self.assert_not_published()

    def test_missing_keyboard_viewport_patch_rejected_before_build(self):
        next(self.patches.glob('0007-*.patch')).unlink()
        r = self.run_builder()
        self.assertNotEqual(r.returncode, 0, 'missing keyboard viewport patch accepted')
        self.assertNotIn('rustup ', self.calls())
        self.assert_not_published()

    def test_missing_android_keyboard_viewport_patch_rejected_before_build(self):
        next(self.patches.glob('0008-*.patch')).unlink()
        r = self.run_builder()
        self.assertNotEqual(r.returncode, 0, 'missing Android keyboard viewport patch accepted')
        self.assertNotIn('rustup ', self.calls())
        self.assert_not_published()

    def test_wrong_base_rejected_before_build(self):
        r = self.run_builder('base')
        self.assertNotEqual(r.returncode, 0, 'wrong source revision accepted')
        self.assertNotIn('rustup ', self.calls())
        self.assert_not_published()

    def test_build_failure_has_no_success_marker(self):
        r = self.run_builder('build')
        self.assertNotEqual(r.returncode, 0, 'build failure reported success')
        self.assertIn('synthetic build failure', r.stderr)
        self.assert_not_published()

    def test_wrong_cache_key_rejected(self):
        (self.out / 'bin').mkdir(parents=True)
        (self.out / 'bin/nsh').write_text('synthetic old CLI\n')
        (self.out / 'bin/nsh').chmod(0o755)
        (self.out / 'nsh-build.txt').write_text('wrong key\n')
        digest = hashlib.sha256((self.out / 'bin/nsh').read_bytes()).hexdigest()
        (self.out / 'SHA256SUMS').write_text(digest + '  bin/nsh\n')
        self.assertNotEqual(self.run_builder(verify=True).returncode, 0, 'wrong cached key accepted')

    def test_corrupt_cached_binary_rejected(self):
        (self.out / 'bin').mkdir(parents=True)
        (self.out / 'bin/nsh').write_text('synthetic modified CLI\n')
        (self.out / 'bin/nsh').chmod(0o755)
        (self.out / 'nsh-build.txt').write_text(self.key + '\n')
        digest = hashlib.sha256(b'synthetic original CLI\n').hexdigest()
        marker_digest = hashlib.sha256((self.out / 'nsh-build.txt').read_bytes()).hexdigest()
        (self.out / 'SHA256SUMS').write_text(digest + '  bin/nsh\n' + marker_digest + '  nsh-build.txt\n')
        self.assertNotEqual(self.run_builder(verify=True).returncode, 0, 'corrupt cached binary accepted')

    def test_unchecked_cached_binary_rejected(self):
        r = self.run_builder()
        self.assertEqual(r.returncode, 0, r.stderr)
        sums = self.out / 'SHA256SUMS'
        sums.write_text(''.join(line for line in sums.read_text().splitlines(keepends=True) if 'bin/nsh' not in line))
        (self.out / 'bin/nsh').write_text('synthetic unchecked replacement\n')
        self.assertNotEqual(self.run_builder(verify=True).returncode, 0, 'unchecked cached binary accepted')

    def test_fake_rejects_split_path_flags_and_accepts_encoded_flags(self):
        paths = [str(self.src), self.env['CARGO_HOME'], str(self.root / 'target')]
        flags = ['--remap-path-prefix=' + path + '=' + alias for path, alias in zip(paths, ['/nsh', '/cargo', '/target'])]
        env = dict(self.env, CARGO_TARGET_DIR=paths[2], RUSTFLAGS=' '.join(flags), CARGO_ENCODED_RUSTFLAGS='', CC_SHELL_ESCAPED_FLAGS='1',
                   CFLAGS=' '.join('"-ffile-prefix-map=' + path + '=' + alias + '"' for path, alias in zip(paths, ['/nsh', '/cargo', '/target'])))
        command = [str(self.root / 'tools/rustup'), 'run', '1.99.1', 'cargo', 'build', '--release', '-p', 'nsh-cli', '--locked']
        bad = subprocess.run(command, cwd=self.src, env=env, capture_output=True, text=True)
        self.assertEqual(bad.returncode, 5)
        self.assertIn('missing build path remap', bad.stderr)
        no_cc_mode = subprocess.run(command, cwd=self.src, env=dict(env, CARGO_ENCODED_RUSTFLAGS='\x1f'.join(flags), CC_SHELL_ESCAPED_FLAGS=''), capture_output=True, text=True)
        self.assertEqual(no_cc_mode.returncode, 6)
        self.assertIn('missing cc shell-escaped flags mode', no_cc_mode.stderr)
        good = subprocess.run(command, cwd=self.src, env=dict(env, CARGO_ENCODED_RUSTFLAGS='\x1f'.join(flags)), capture_output=True, text=True)
        self.assertEqual(good.returncode, 0, good.stderr)


class WorkflowTest(unittest.TestCase):
    def job(self, name):
        text = (ROOT / '.github/workflows/tests.yml').read_text()
        match = re.search(r'^  ' + name + r':\n(.*?)(?=^  [\w-]+:|\Z)', text, re.M | re.S)
        self.assertIsNotNone(match, 'missing workflow job: ' + name)
        return match.group(1)

    def test_app_installs_companion_go_before_builder_tests(self):
        app = self.job('app')
        setup = re.search(r'^      - uses: actions/setup-go@([0-9a-f]{40})[^\n]*\n.*?(?=^      - |\Z)', app, re.M | re.S)
        self.assertIsNotNone(setup, 'app must install pinned Go before the builder tests')
        companion = re.search(r'actions/setup-go@([0-9a-f]{40})', self.job('companion'))
        self.assertIsNotNone(companion, 'companion must use pinned setup-go')
        self.assertEqual(setup.group(1), companion.group(1))
        self.assertIn('          go-version-file: companion/go.mod', setup.group(0).splitlines())
        self.assertIn('          cache-dependency-path: companion/go.sum', setup.group(0).splitlines())
        tests = '      - run: python3 ios/nsh-patches/build-nsh-cli.test.py'
        self.assertIn(tests, app.splitlines(), 'CI must keep the full builder test suite')
        self.assertLess(setup.start(), app.index(tests))

    def test_only_ios27_skips_cli_and_app_without_blocking_ios(self):
        for name in ['nsh-cli', 'app']:
            self.assertIn('if: ${{ !inputs.only_ios27 }}', self.job(name))
        self.assertIn('needs: nsh-cli', self.job('app'))
        self.assertNotRegex(self.job('ios27'), r'needs:.*nsh-cli')
        self.assertNotRegex(self.job('ios26'), r'needs:.*nsh-cli')

    def test_runner_temp_is_only_used_in_build_step_env(self):
        def child_block(text, key):
            lines = text.splitlines()
            at = next(i for i, line in enumerate(lines) if line.strip() == key + ':')
            indent = len(lines[at]) - len(lines[at].lstrip())
            end = at + 1
            while end < len(lines) and (not lines[end].strip() or len(lines[end]) - len(lines[end].lstrip()) > indent):
                end += 1
            return '\n'.join(lines[at + 1:end])

        build = self.job('nsh-cli')
        job_env = child_block(build.split('    steps:', 1)[0], 'env')
        self.assertNotRegex(job_env, r'\$\{\{[^}]*\brunner(?:\.|\[)', 'runner context is unavailable in job env')
        self.assertRegex(job_env, r'CARGO_BUILD_JOBS:\s*[\'"]?1[\'"]?(?:\s|$)')
        step = re.search(r'^([ ]*)- name: [\'"]?Build the CLI[\'"]?\s*\n', build, re.M)
        self.assertIsNotNone(step, 'missing CLI build step')
        rest = build[step.end():]
        next_step = re.search(r'^' + step.group(1) + r'- ', rest, re.M)
        step_env = child_block(rest[:next_step.start()] if next_step else rest, 'env')
        for name in ['CARGO_HOME', 'RUSTUP_HOME']:
            self.assertRegex(step_env, name + r':\s*[\'"]?\$\{\{\s*runner\.temp\s*\}\}/\S+', name + ' must stay in runner temp')

    def test_cli_artifact_verified_before_use(self):
        build = self.job('nsh-cli'); app = self.job('app')
        self.assertIn('runner.os', build)
        self.assertIn('runner.arch', build)
        self.assertIn('key.sh', build)
        self.assertIn('actions/upload-artifact@', build)
        self.assertIn('actions/download-artifact@', app)
        self.assertIn('build-nsh-cli.sh --verify', build)
        self.assertIn('build-nsh-cli.sh --verify', app)
        self.assertLess(app.index('build-nsh-cli.sh --verify'), app.index('GITHUB_PATH'))
        self.assertIn('nsh --version && nsh check && scripts/test.sh', app)
        for check in ['check-pipefail.sh', 'check-workflow-quotes.sh']:
            result = subprocess.run(['sh', str(ROOT / 'app/scripts' / check)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
