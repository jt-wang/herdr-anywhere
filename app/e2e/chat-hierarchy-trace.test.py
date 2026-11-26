"""Check where the real chat view leaves the reader, from its mount and scroll commands, not only text presence.

Run from app/: python3 e2e/chat-hierarchy-trace.test.py
nsh's headless text selectors include clipped rows, so they cannot prove scroll position. The trace can: it has every
frame, the scroll view's size and content, and each ScrollTo. A ScrollTo by itself proves nothing (the view restores
the turn being read with one, chat.tsx place()); what counts is where the rows the reader saw are in the viewport.
"""
import re
import subprocess
import unittest
from pathlib import Path

APP = Path(__file__).resolve().parents[1]
SCROLL = re.compile(r'^── scroll\(id:chat-scroll, (-?[\d.]+)pt\)')


def trace(name):
    result = subprocess.run(['nsh', 'test', 'App.chathierarchy.test.tsx', '--filter', name, '--trace'],
                            cwd=APP, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=60)
    if result.returncode:
        raise AssertionError('nsh interaction failed: ' + '\n'.join(result.stdout.splitlines()[-5:]))
    # One stream: the drags (perf events) in their place among the mount commands.
    return result.stdout


class View:
    """The chat's scroll view as the trace builds it, line by line. A user scroll (nsh's headless scroll()) sends
    ScrollDragStart, then moves frame by frame to where it started plus the distance, clamped to the content, whatever
    the app scrolls to meanwhile: so when that step ends, the view is there. A step's lines come after its header
    ("── scroll(…): n items"), maybe under several headers of the same name."""

    def __init__(self, output):
        self.lines = output.splitlines()
        self.scroll = next(int(m[1]) for m in (re.search(r'test_id: Some\("chat-scroll"\)', l) and
                                              re.search(r'ViewProps \{ id: (\d+),', l) for l in self.lines) if m)
        self.parent, self.frame, self.text, self.ids = {}, {}, {}, {}
        self.viewport = self.content = self.offset = 0
        self.width = self.content_width = 0
        self.i = 0
        self.drag_from = None
        self.step = None  # the user scroll in progress: (its header, where it ends)

    def run(self, until):
        """Apply lines until one that `until` matches (that line not applied), or the end."""
        while self.i < len(self.lines):
            line = self.lines[self.i]
            if until(line):
                self.boundary(line)
                return
            self.apply(line)
            self.i += 1

    def boundary(self, line):
        """A user scroll ends where it was going once the next step, or the next drag, begins."""
        if self.step and (line.startswith(f'perf event ScrollDragStart on view {self.scroll}') or
                          (line.startswith('── ') and line.rsplit(':', 1)[0] != self.step[0])):
            self.offset = self.step[1]
            self.step = None

    def apply(self, line):
        s = self.scroll
        self.boundary(line)
        if m := re.search(r'Insert \{ parent: (\d+), child: (\d+)', line):
            self.parent[int(m[2])] = int(m[1])
        elif m := re.search(r'(?:Remove|Delete) \{ id: (\d+) \}', line):
            self.parent.pop(int(m[1]), None)
        elif m := re.search(r'Frame \{ id: (\d+), x: (-?[\d.]+), y: (-?[\d.]+), w: ([\d.]+), h: ([\d.]+)', line):
            self.frame[int(m[1])] = tuple(float(v) for v in m.groups()[1:])
            if int(m[1]) == s:
                self.width, self.viewport = float(m[4]), float(m[5])
        elif m := re.search(r'ScrollContent \{ id: (\d+), size: Size \{ width: ([\d.]+)pt, height: ([\d.]+)pt', line):
            if int(m[1]) == s:
                self.content_width, self.content = float(m[2]), float(m[3])
        elif m := re.search(r'Text \{ id: (\d+), text: "((?:[^"\\]|\\.)*)"', line):
            self.text[int(m[1])] = m[2]
        elif m := re.search(r'ViewProps \{ id: (\d+),.*test_id: Some\("([^"]+)"\)', line):
            self.ids[m[2]] = int(m[1])
        elif m := re.search(rf'Command \{{ id: {s}, command: ScrollTo \{{ x: [\d.]+pt, y: ([\d.]+)pt', line):
            self.offset = min(float(m[1]), self.end())  # a scroll view stops at its content's end
        elif line.startswith(f'perf event ScrollDragStart on view {s}'):
            self.drag_from = self.offset
        elif (m := SCROLL.match(line)) and self.drag_from is not None:
            self.step = (line.rsplit(':', 1)[0], min(max(self.drag_from + float(m[1]), 0), self.end()))
            self.drag_from = None

    def end(self):
        return max(self.content - self.viewport, 0)

    def top(self, node):
        """Where the node's top is in the scroll view's content, or None when it is not in it."""
        y = 0.0
        while node != self.scroll:
            if node not in self.frame or node not in self.parent:
                return None
            y += self.frame[node][1]
            node = self.parent[node]
        return y

    def seen(self):
        """Text rows wholly in the viewport: id -> top in the viewport."""
        out = {}
        for node in self.text:
            top = self.top(node)
            if top is not None and top >= self.offset and top + self.frame[node][3] <= self.offset + self.viewport:
                out[node] = top - self.offset
        return out


def at(prefix):
    return lambda line: line.startswith(prefix)


class ChatHierarchy(unittest.TestCase):
    def assert_kept(self, before, after, what):
        kept = [n for n in before if n in after]
        self.assertTrue(kept, f'{what}: none of the rows the reader saw is still there')
        anchor = min(kept, key=lambda n: before[n])
        self.assertAlmostEqual(after[anchor], before[anchor], delta=1,
                               msg=f'{what}: {anchor} moved from {before[anchor]} to {after[anchor]} in the viewport')

    def test_expansion_keeps_reading_position(self):
        view = View(trace('opening work'))
        view.run(at('── press(id:fold-review):'))
        before = view.seen()
        fold = view.ids['fold-review']
        fold_top = view.top(fold) - view.offset
        view.run(lambda line: False)
        self.assert_kept(before, view.seen(), 'Opening work')
        self.assertAlmostEqual(view.top(fold) - view.offset, fold_top, delta=1,
                               msg='Opening work moved its own summary in the viewport')
        self.assertLess(view.offset, view.end(), 'Opening work jumped to the end')

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
        view = View(trace('reading older work'))
        view.run(at('── clock.runFor(3101):'))
        self.assertGreater(view.end() - view.offset, 100, 'the reader is not scrolled up before the poll')
        before = view.seen()
        view.run(at('── scroll(id:chat-scroll, 10000pt):'))
        self.assert_kept(before, view.seen(), 'Polling while reading older work')
        self.assertGreater(view.end() - view.offset, 100, 'Polling pulled the reader to the end')
        view.run(at('── clock.runFor(3202):'))
        self.assertAlmostEqual(view.offset, view.end(), delta=1, msg='scrolling to the end did not reach it')
        view.run(lambda line: False)
        self.assertIn('The report is ready.', view.text.values())
        self.assertAlmostEqual(view.offset, view.end(), delta=1, msg='New replies did not follow at the bottom')

    def test_a_pi_chat_ends_at_its_last_reply(self):
        # Pi's icon, 800 points of its own drawn at 15, stretched the content on NativeShell 0.3.0: a chat
        # that scrolled sideways and ended a screen of nothing below its last turn.
        view = View(trace('a long Pi chat'))
        view.run(lambda line: False)
        self.assertGreater(view.end(), 0, 'the chat is not longer than its viewport')
        self.assertLessEqual(view.content_width, view.width + 0.5, 'the chat is wider than its viewport')
        # At the scroll view's own end, where a finger takes it: below the last reply only the content's bottom
        # padding (pb-4). Measured against the content's size, not where the app scrolled to.
        reply = view.ids['reply-pi-11']
        below = view.content - (view.top(reply) + view.frame[reply][3])
        self.assertAlmostEqual(below, 16, delta=2, msg=f'{below:.0f} pt below the last reply at the end')


if __name__ == '__main__':
    unittest.main()
