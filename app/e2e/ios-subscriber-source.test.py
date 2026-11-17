"""Exercise same-build source capture, bytecode binding and redacted publication."""
from pathlib import Path
import hashlib
import json
import os
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1]
HELPER = APP / 'e2e/ios-subscriber-source.py'


class SubscriberSourceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.nsh = self.root / 'ios/nsh'; self.nsh.mkdir(parents=True)
        self.private = self.root / 'private'
        self.out = self.root / 'shots/subscriber-source'
        self.bytecode = self.root / 'Herdr.app/nsh/bundle.nshc'
        self.bytecode.parent.mkdir(parents=True)
        self.bytecode.write_bytes(b'synthetic same-build bytecode')
        (self.nsh / 'bundle.nshc').write_bytes(self.bytecode.read_bytes())
        self.link = 'http://127.0.0.1:7762#pair=synthetic-pair'
        self.secret = self.root / 'pair.txt'; self.secret.write_text(self.link)
        self.bundle = 'const pair = "' + self.link + '";\nconst offConn = store.conn.sub(() => {});\nconst off = s.sub(() => {});\n'
        (self.nsh / 'bundle.js').write_text(self.bundle)
        self.mapping = {'version': 3, 'file': 'bundle.js', 'sourceRoot': str(APP),
                        'sources': ['src/ui/root.tsx', 'src/ui/kit.tsx'],
                        'sourcesContent': ['const offConn = store.conn.sub(() => {});\n' + self.link, 'const off = s.sub(() => {});\n'],
                        'names': [], 'mappings': ';AAAA;ACAA'}
        (self.nsh / 'bundle.js.map').write_text(json.dumps(self.mapping))

    def capture(self):
        # Run the actual post-build block, without invoking nsh or Xcode.
        source = (APP / 'ios/build.sh').read_text()
        start = source.index('(cd "$root" && nsh build ')
        start = source.index('\n', start) + 1
        end = source.index('rm -f "$ios/nsh/bundle.js"', start)
        return subprocess.run(['sh', '-c', 'set -eu\nroot=$1; ios=$2; entry=$3; sdk=simulator\n' + source[start:end],
                               'test', str(APP), str(self.root / 'ios'), self.entry],
                              env=dict(os.environ, HM_TEST_BUNDLE_EVIDENCE=str(self.private)),
                              text=True, capture_output=True)

    def publish(self):
        return subprocess.run(['python3', str(HELPER), 'publish', str(self.private),
                               str(self.bytecode), str(self.out), str(self.secret), str(APP)],
                              text=True, capture_output=True)

    def prepared(self):
        self.entry = 'ios-e2e.tsx'
        r = self.capture(); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue((self.private / 'bundle.js').exists(), 'same-build source was not captured')

    def test_same_build_capture_and_redaction_preserve_positions(self):
        self.prepared()
        r = self.publish(); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual((self.private / 'bundle.js').read_text(), self.bundle)
        safe = (self.out / 'bundle.js').read_text()
        self.assertEqual(safe, self.bundle.replace(self.link, 'x' * len(self.link)))
        self.assertEqual(safe.index('offConn'), self.bundle.index('offConn'))
        self.assertEqual(safe.index('const off ='), self.bundle.index('const off ='))
        mapping = json.loads((self.out / 'bundle.js.map').read_text())
        self.assertEqual(mapping['sourceRoot'], '')
        self.assertEqual(mapping['sources'], ['app/src/ui/root.tsx', 'app/src/ui/kit.tsx'])
        self.assertEqual(mapping['mappings'], self.mapping['mappings'])
        self.assertNotIn('sourcesContent', mapping)
        sites = json.loads((self.out / 'known-sites.json').read_text())
        self.assertEqual([s['site'] for s in sites], ['bundle.js:2:1', 'bundle.js:3:1'])
        self.assertEqual([s['source'] for s in sites], mapping['sources'])
        manifest = json.loads((self.out / 'manifest.json').read_text())
        self.assertEqual(manifest['bytecode_sha256'], hashlib.sha256(self.bytecode.read_bytes()).hexdigest())
        self.assertEqual(manifest['files']['bundle.js']['original_sha256'], hashlib.sha256(self.bundle.encode()).hexdigest())
        self.assertEqual(manifest['files']['bundle.js']['published_sha256'], hashlib.sha256(safe.encode()).hexdigest())
        self.assertEqual(sorted(p.name for p in self.out.iterdir()), ['bundle.js', 'bundle.js.map', 'known-sites.json', 'manifest.json'])
        for p in self.out.iterdir():
            self.assertNotIn(self.link, p.read_text())
            self.assertNotIn(str(APP), p.read_text())

    def test_other_build_bytecode_is_rejected_before_publication(self):
        self.prepared(); self.bytecode.write_bytes(b'different build')
        r = self.publish()
        self.assertNotEqual(r.returncode, 0)
        self.assertFalse(self.out.exists())

    def test_unrelated_private_map_source_is_rejected(self):
        self.mapping['sources'] = ['/private/synthetic/unrelated.ts']
        (self.nsh / 'bundle.js.map').write_text(json.dumps(self.mapping))
        self.prepared()
        r = self.publish()
        self.assertNotEqual(r.returncode, 0)
        self.assertFalse(self.out.exists())

    def append_source(self, text):
        self.bundle += text + '\n'
        (self.nsh / 'bundle.js').write_text(self.bundle)
        self.mapping['sourcesContent'][0] += '\n' + text
        (self.nsh / 'bundle.js.map').write_text(json.dumps(self.mapping))

    def test_existing_demo_home_is_publishable(self):
        line = next(line for line in (APP / 'src/testing/fakecore.ts').read_text().splitlines()
                    if line.strip().startswith('home = '))
        self.append_source(line)
        self.prepared()
        r = self.publish(); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn(line, (self.out / 'bundle.js').read_text())

    def test_existing_pair_protocol_source_is_publishable(self):
        lines = (APP / 'src/link.ts').read_text().splitlines()
        self.append_source('\n'.join(line for line in lines if '#pair=' in line))
        self.prepared()
        r = self.publish(); self.assertEqual(r.returncode, 0, r.stderr)

    def test_existing_public_pair_example_is_redacted(self):
        line = next(line for line in (APP / 'src/ui/pair.tsx').read_text().splitlines()
                    if "t('pasteLan', { example:" in line)
        example = line.split("example: '", 1)[1].split("'", 1)[0]
        self.append_source(line)
        self.prepared()
        r = self.publish(); self.assertEqual(r.returncode, 0, r.stderr)
        safe = (self.out / 'bundle.js').read_text()
        self.assertEqual(safe, self.bundle.replace(self.link, 'x' * len(self.link)).replace(example, 'x' * len(example)))
        self.assertNotIn(example, (self.out / 'bundle.js.map').read_text())

    def test_other_home_and_unknown_pair_link_are_rejected(self):
        original = self.bundle
        for text in ['const home = "/Users/samuel";',
                     'const link = "http://127.0.0.1:7762#pair=unknown-synthetic-secret";']:
            with self.subTest(text=text.split('=')[0]):
                self.bundle = original
                self.append_source(text)
                if not self.private.exists(): self.prepared()
                else:
                    (self.private / 'bundle.js').write_text(self.bundle)
                r = self.publish()
                self.assertNotEqual(r.returncode, 0)
                self.assertFalse(self.out.exists())

    def ci_shape_result(self, data):
        line = next(line for line in (APP / 'e2e/ci-shots-check.sh').read_text().splitlines()
                    if line.startswith("shapes='"))
        shapes = line.split("'", 2)[1]
        return subprocess.run(['grep', '-Eq', shapes], input=data, capture_output=True).returncode

    def test_timer_assignment_redaction_passes_original_ci_shapes(self):
        snippet = 'this._token=setTimeout(()=>{},1);'
        self.assertEqual(self.ci_shape_result(snippet.encode()), 0)
        self.append_source(snippet)
        self.prepared()
        r = self.publish(); self.assertEqual(r.returncode, 0, r.stderr)
        for file in self.out.iterdir():
            self.assertEqual(self.ci_shape_result(file.read_bytes()), 1, file.name)
        fragment = '_token=setTimeout'
        expected = self.bundle.replace(self.link, 'x' * len(self.link)).replace(fragment, 'x' * len(fragment))
        self.assertEqual((self.out / 'bundle.js').read_bytes(), expected.encode())
        self.assertEqual((self.private / 'bundle.js').read_text(), self.bundle)
        manifest = json.loads((self.out / 'manifest.json').read_text())
        self.assertEqual(manifest['timer_assignment_replacements'], 1)
        sites = json.loads((self.out / 'known-sites.json').read_text())
        self.assertEqual([s['site'] for s in sites], ['bundle.js:2:1', 'bundle.js:3:1'])

    def test_unknown_token_shape_is_rejected(self):
        text = 'const text = "token=unknown-synthetic-value";'
        self.assertEqual(self.ci_shape_result(text.encode()), 0)
        self.append_source(text)
        self.prepared()
        r = self.publish()
        self.assertNotEqual(r.returncode, 0)
        self.assertFalse(self.out.exists())

    def test_release_entry_does_not_capture(self):
        self.entry = 'App.tsx'
        r = self.capture(); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse(self.private.exists())


if __name__ == '__main__': unittest.main()
