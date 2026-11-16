"""Check the explicit background entry and execute its Swift completion predicate."""
from pathlib import Path
import hashlib
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

    def test_default_regression_blocks_are_preserved(self):
        source = SWIFT.read_text()
        for begin, end, digest in [
            ('      // long\n', '      // away\n', 'c8591c976790013acefe410b811c7307decf873d64cd881afa7895da776f8f3d'),
            ('      // cut\n', '      el("back").tap()', 'edba94e3ce4375d51de4f8fac43fa194d8e0a84dc81bede5b9c29330aacbb710')]:
            value = source[source.index(begin):source.index(end)]
            # Only the new wrapper ends after the original long/image/scroll body.
            if begin.endswith('long\n') and value.rstrip().endswith('}'):
                value = value[:value.rfind('}')]
            normalized = '\n'.join(line.strip() for line in value.splitlines()).rstrip()
            self.assertEqual(hashlib.sha256(normalized.encode()).hexdigest(), digest)

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
print("completion predicate: 1 positive, 6 negative passed")
'''
        with tempfile.TemporaryDirectory() as folder:
            test = Path(folder) / 'main.swift'; test.write_text(program)
            result = subprocess.run(['swift', str(test)], capture_output=True, text=True,
                                    env=os.environ, timeout=45)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('1 positive, 6 negative passed', result.stdout)
        self.assertEqual(source.count('return completedReply(current, oldReplies, prompt)'), 2,
                         'preflight and original resume must share the same condition')


if __name__ == '__main__': unittest.main()
