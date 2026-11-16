"""A stopped or repeated background cycle must never count as a completed run."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("away", Path(__file__).with_name("chat-away-check.py"))
away = importlib.util.module_from_spec(spec)
spec.loader.exec_module(away)


def cycle(n, total):
    return [f"away-cycle {stage} {n}/{total} seconds=60" for stage in ("start", "resume", "pass")]


class CompletionTest(unittest.TestCase):
    def test_every_cycle_finishes(self):
        self.assertEqual(away.check(cycle(1, 2) + cycle(2, 2), 2), (2, 2, 2))

    def test_crash_while_away(self):
        with self.assertRaises(ValueError):
            away.check(cycle(1, 2) + cycle(2, 2)[:1], 2)

    def test_frozen_after_resuming(self):
        with self.assertRaises(ValueError):
            away.check(cycle(1, 2) + cycle(2, 2)[:2], 2)

    def test_repeated_old_success_does_not_count(self):
        with self.assertRaises(ValueError):
            away.check(cycle(1, 2) + cycle(1, 2), 2)

    def test_wrong_duration_or_count(self):
        for lines in [cycle(1, 1), [s.replace("seconds=60", "seconds=10") for s in cycle(1, 2)]]:
            with self.assertRaises(ValueError):
                away.check(lines, 2)

    def test_unrelated_log_lines_are_ignored(self):
        self.assertEqual(away.check(["chat view shown"] + cycle(1, 1) + ["PASS"], 1), (1, 1, 1))

    def test_no_cycles_cannot_pass(self):
        for count in (0, -1, 31):
            with self.assertRaises(ValueError):
                away.check([], count)


if __name__ == "__main__":
    unittest.main()
