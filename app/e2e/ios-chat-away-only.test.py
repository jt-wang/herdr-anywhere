"""Check the explicit background entry and execute its Swift completion predicate."""
from pathlib import Path
import json
import re
import shlex
import os
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SWIFT = HERE / 'ios-uitest/tests/ChatFour.swift'


def block(source, start):
    begin = source.index('{', start)
    depth = 1
    for i in range(begin + 1, len(source)):
        if source[i] == '{': depth += 1
        elif source[i] == '}': depth -= 1
        if depth == 0: return source[begin + 1:i]
    raise AssertionError('unclosed Swift block')


class AwayOnlyTest(unittest.TestCase):
    def test_workflow_routes_regular_and_pressure_runs(self):
        workflow = (HERE.parents[1] / '.github/workflows/devices.yml').read_text()
        branch = workflow.split('            chat27)\n', 1)[1].split(';;', 1)[0]
        with tempfile.TemporaryDirectory() as folder:
            suite = Path(folder) / 'suite'
            subprocess.run(['go', 'build', '-o', str(suite), './internal/testfake/chatevidence'],
                           cwd=HERE.parents[1] / 'companion', check=True)
            env = {k: v for k, v in os.environ.items() if k not in
                   ['CHAT_AWAY_ONLY', 'AGENTS', 'CHAT_AWAY_CYCLES', 'CHAT_AWAY_SELFTEST']}
            for cycles in ['0', '30']:
                with self.subTest(cycles=cycles):
                    rendered = branch.replace("${{ inputs.chat_away_cycles || '0' }}", cycles)
                    rendered = rendered.replace('${{ inputs.chat_away_cycles }}', cycles)
                    rendered = rendered.replace("${{ inputs.chat_away_selftest || 'none' }}", 'none')
                    self.assertNotIn('${{', rendered)
                    command = 'set -eu\ns=' + shlex.quote(str(suite)) + '\nOUT=' + shlex.quote(folder) + '\n' + rendered
                    result = subprocess.run(['sh', '-c', command], env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    got = json.loads(result.stdout)
                    self.assertEqual(got['args'], ['ios-chat-four', 'e2e/ios-chat-four.sh', folder + '/chat27'])
                    expected = {'mode': '1', 'agents': 'claude-code', 'cycles': '30', 'control': 'none'} if cycles == '30' else {
                        'mode': '', 'agents': '', 'cycles': '', 'control': ''}
                    self.assertEqual({k: got[k] for k in expected}, expected)
        shell = (HERE / 'ios-chat-four.sh').read_text()
        self.assertIn('agents=${AGENTS:-claude-code codex pi opencode}', shell)
        self.assertIn('TEST_RUNNER_CHAT_AWAY_ONLY="${CHAT_AWAY_ONLY:-0}"', shell)

    def test_explicit_mode_reaches_swift_and_only_trial_chat27_enables_it(self):
        source = SWIFT.read_text()
        shell = (HERE / 'ios-chat-four.sh').read_text()
        workflow = (HERE.parents[1] / '.github/workflows/devices.yml').read_text()
        self.assertIn('let awayOnly = env["CHAT_AWAY_ONLY"] == "1"', source)
        self.assertIn('TEST_RUNNER_CHAT_AWAY_ONLY="${CHAT_AWAY_ONLY:-0}"', shell)
        self.assertIn('-preload 40', shell)
        chat27 = workflow.split('            chat27)\n', 1)[1].split(';;', 1)[0]
        self.assertIn('CHAT_AWAY_ONLY=1', chat27)
        self.assertEqual(workflow.count('CHAT_AWAY_ONLY=1'), 1)

    def test_preflight_requires_completed_reply_and_saves_failure_image(self):
        source = SWIFT.read_text()
        opened = source.index('shot("\\(agent)-open")')
        self.assertIn('if awayOnly {', source[opened:])
        start = source.index('if awayOnly {', opened)
        arm = block(source, start)
        for required in ['chatSnapshot()', 'oldReplies', 'say(prompt)', 'until(60)',
                         'completedReply(', 'guard completed else', 'XCTFail(', 'return']:
            self.assertIn(required, arm)
        until_start = arm.index('guard until(10, {')
        until_end = arm.index('}) else {', until_start)
        for failure_start in [until_end, arm.index('guard completed else')]:
            failure = block(arm, failure_start)
            self.assertLess(failure.index('shot('), failure.index('XCTFail('))
            self.assertLess(failure.index('XCTFail('), failure.index('return'))
        self.assertIn('let prompt = "EDIT before background check"', arm)
        self.assertLess(source.index('XCTAssertTrue(long'), start)
        self.assertIn('if awayOnly { return }\n      // cut', source)

    # The default run (CHAT_AWAY_ONLY unset) still takes the long conversation, its picture, scrolling and reading
    # anchor, and then the cut, each with its hard assertion, in this order. Away-only skips only these.
    LONG = ['sent("LIST the files here"', 'XCTAssertTrue(openLast(), "\\(agent) long: no fold")',
            'XCTAssertTrue(more, ', 'XCTAssertTrue(pics, ', 'XCTAssertTrue(top, ',
            'let anchorY = ', 'el("show-terminal").tap()', 'el("show-chat").tap()', 'let restored = until(15)',
            'abs(row.frame.minY - el("chat-scroll").frame.minY - anchorY) < 3\n', 'XCTAssertTrue(restored, ',
            'XCTAssertTrue(bottom, ']
    CUT = ['if ctlDo("stop", "stopped") {', 'XCTAssertTrue(off, ', 'XCTAssertTrue(kept, ',
           'XCTAssertTrue(ctlDo("start", "started")', 'XCTAssertTrue(on, ', 'sent("CHANGE after the restart"']

    def in_order(self, text, markers, what):
        at = 0
        for m in markers:
            i = text.find(m, at)
            self.assertGreaterEqual(i, 0, f'{what}: no {m} (after the steps before it)')
            at = i + len(m)

    def no_early_return(self, text, what):
        # A bare return ends the test: only a failure branch may take it, after its XCTFail. (A return with a value
        # belongs to a closure, such as until's.)
        for m in re.finditer(r'\breturn(?=\s*[}\n;])', text):
            depth, start = 0, None
            for i in range(m.start() - 1, -1, -1):
                if text[i] == '}': depth += 1
                elif text[i] == '{':
                    if depth == 0: start = i; break
                    depth -= 1
            branch = text[start:m.start()] if start is not None else ''
            self.assertIn('XCTFail(', branch, f'{what}: a return the default run can take: ...{text[max(0, m.start() - 80):m.end()]!r}')

    def test_default_long_and_cut_steps_are_kept(self):
        source = SWIFT.read_text()
        start = source.index('if awayOnly {', source.index('shot("\\(agent)-open")'))
        close = source.index('{', start) + 1 + len(block(source, start))
        rest = source[close + 1:]
        self.assertTrue(rest.lstrip().startswith('else {'), 'the long conversation is no longer the default branch')
        default_at = close + 1 + rest.index('else {')
        default = block(source, default_at)
        self.in_order(default, self.LONG, 'default long')
        self.no_early_return(default, 'default long')
        after = close + 1 + rest.index('else {') + len('else {') + len(default) + 1
        cut_at = source.index('      // cut\n', after)
        self.assertEqual(source[after:cut_at].count('if awayOnly'), 1, 'away-only must skip only the cut')
        away = source[after:cut_at].rstrip()
        self.assertTrue(away.endswith('if awayOnly { return }'), 'the cut is not the default path')
        self.no_early_return(away[:-len('if awayOnly { return }')], 'default away')
        cut = source[cut_at:source.index('el("back").tap()', cut_at)]
        self.in_order(cut, self.CUT, 'default cut')
        self.no_early_return(cut, 'default cut')

    def test_background_cut_and_resume_contract_still_present(self):
        source = SWIFT.read_text()
        for item in ['sleep(45)', 'Date().addingTimeInterval(Double(secs))', 'Thread.sleep(until: resumeAt)',
                     'cut-disconnected', 'guard closed else', 'say("EDIT before background cycle',
                     'note("away-cycle pass', 'XCTAssertTrue(back']:
            self.assertIn(item, source)
        shell = (HERE / 'ios-chat-four.sh').read_text()
        self.assertIn('if [ "$now" != "$before" ]; then', shell)
        self.assertIn('chat-away-check.py" "$cycles"', shell)

    def test_actual_completion_predicate_rejects_stale_and_unfinished_replies(self):
        source = SWIFT.read_text()
        marker = '    func completedReply('
        self.assertIn(marker, source, 'shared audited snapshot predicate is missing')
        start = source.index(marker)
        body = block(source, start)
        header = source[start:source.index('{', start)]
        program = header + '{' + body + '}\n' + r'''
let old: Set<String> = ["reply-old"]
let prompt = "EDIT before background check"
func check(_ ids: Set<String>, _ labels: Set<String>, _ want: Bool, _ reason: String) {
  precondition(completedReply((ids: ids, labels: labels), old, prompt) == want, reason)
}
check(["reply-old", "reply-new", "fold-new"], [prompt], true, "new completed reply")
check(["reply-old", "fold-old"], [prompt], false, "old completed reply")
check(["reply-new"], [prompt], false, "new reply not folded")
check(["reply-new", "fold-other"], [prompt], false, "wrong fold")
check(["reply-new", "fold-new"], [], false, "missing prompt")
check(["reply-new", "fold-new", "chat-offline"], [prompt], false, "offline")
check(["reply-new", "fold-new"], [prompt, "Working…"], false, "still working")
check(["reply-new", "fold-new"], [prompt, "Working… · 15 steps"], false, "open folded turn")
print("completion predicate: 1 positive, 7 negative passed")
'''
        with tempfile.TemporaryDirectory() as folder:
            test = Path(folder) / 'main.swift'; test.write_text(program)
            result = subprocess.run(['swift', str(test)], capture_output=True, text=True,
                                    env=os.environ, timeout=45)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('1 positive, 7 negative passed', result.stdout)
        self.assertEqual(source.count('return completedReply(current, oldReplies, prompt)'), 2,
                         'preflight and original resume must share the same condition')


if __name__ == '__main__': unittest.main()
