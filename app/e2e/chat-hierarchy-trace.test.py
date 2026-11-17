"""Check the real chat view's mount/scroll commands, not only text presence.

Run from app/: python3 e2e/chat-hierarchy-trace.test.py
nsh's headless text selectors include clipped rows, so they cannot prove scroll position.
"""
import re
import subprocess
import unittest
from pathlib import Path

APP = Path(__file__).resolve().parents[1]


def trace(name):
    result = subprocess.run(['nsh', 'test', 'App.chathierarchy.test.tsx', '--filter', name, '--trace'],
                            cwd=APP, capture_output=True, text=True, timeout=60)
    if result.returncode:
        raise AssertionError('nsh interaction failed: ' + '\n'.join(result.stdout.splitlines()[-5:]))
    return result.stdout + result.stderr


class ChatHierarchy(unittest.TestCase):
    def test_expansion_keeps_reading_position(self):
        output = trace('opening work')
        expanded = output.split('── press(id:fold-review):', 1)[1]
        self.assertNotIn('command: ScrollTo', expanded, 'Opening work must not jump past its first rows')

    def test_streaming_preserves_the_pressed_rows(self):
        output = trace('expanded steps and details')
        # Row IDs are the native views receiving touches. Removing one mid-update loses that touch.
        ids = {}
        for name in ['fold-review', 'step-command-7']:
            ids[name] = re.findall(r'ViewProps \{ id: (\d+),[^\n]*test_id: Some\("' + name + r'"\)', output)
            self.assertTrue(ids[name], name)
        # Before the user deliberately folds, polling and completion must reuse the original native view.
        before_close = output.split('── press(id:fold-review):')[1]
        self.assertEqual(len(set(ids['fold-review'])), 1)
        original = ids['step-command-7'][0]
        self.assertNotRegex(before_close, r'Remove \{ id: ' + original + r'[, }]', 'stream replaced a row under a finger')

    def test_scroll_up_pauses_following_and_bottom_resumes_it(self):
        output = trace('reading older work')
        reading, bottom = output.split('── clock.runFor(3202):', 1)
        reading = reading.split('── clock.runFor(3101):', 1)[1]
        self.assertNotIn('command: ScrollTo', reading, 'Polling pulled the reader away')
        self.assertIn('command: ScrollTo', bottom, 'New replies did not follow at the bottom')


if __name__ == '__main__':
    unittest.main()
