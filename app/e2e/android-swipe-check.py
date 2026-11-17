#!/usr/bin/env python3
"""Check the last one-finger gesture recorded by Android's getevent -lt."""
import math
import re
import sys
import unittest


def check(points, start, end):
    length = math.dist(start, end)
    tolerance = max(4, length * 0.01)
    if length == 0 or len(points) < 4:
        raise ValueError('too few movement samples')
    if math.dist(points[0], start) > tolerance or math.dist(points[-1], end) > tolerance:
        raise ValueError('the MOVE path did not cover the requested swipe')
    dx, dy = end[0] - start[0], end[1] - start[1]
    for a, b in zip(points, points[1:]):
        if math.dist(a, b) > length / 3 + tolerance:
            raise ValueError('the MOVE path jumped over part of the swipe')
        if (b[0] - a[0]) * dx + (b[1] - a[1]) * dy < -tolerance * length:
            raise ValueError('the MOVE path reversed direction')
    if any(abs((x-start[0])*dy - (y-start[1])*dx) > tolerance*length for x,y in points):
        raise ValueError('the MOVE path left the requested line')


def read_events(text):
    slot, positions, active = 0, {}, set()
    current, last, multi = None, [], False
    for line in text.splitlines():
        m = re.search(r'(EV_ABS|EV_KEY|EV_SYN)\s+(\w+)\s+(\w+)', line)
        if not m:
            continue
        kind, code, raw = m.groups()
        if kind == 'EV_ABS':
            value = int(raw, 16)
            if code == 'ABS_MT_SLOT':
                slot = value
            elif code == 'ABS_MT_TRACKING_ID':
                if value == 0xffffffff:
                    active.discard(slot)
                else:
                    if not active:
                        current, multi = [], False
                    active.add(slot)
                    multi |= len(active) > 1
            elif code in ('ABS_MT_POSITION_X', 'ABS_MT_POSITION_Y'):
                positions.setdefault(slot, {})[code[-1]] = value
        elif code == 'SYN_REPORT' and current is not None:
            if not active:
                last = [] if multi else current
                current = None
            elif not multi:
                pos = positions.get(next(iter(active)), {})
                if 'X' in pos and 'Y' in pos:
                    point = (pos['X'], pos['Y'])
                    if not current or point != current[-1]:
                        current.append(point)
    if current is not None:
        raise ValueError('the finger has not lifted')
    return last


class Checks(unittest.TestCase):
    def test_complete_path(self):
        check([(100, y) for y in (200, 300, 400, 500, 600)], (100, 200), (100, 600))

    def test_incomplete_path(self):
        with self.assertRaises(ValueError):
            check([(100, y) for y in (200, 220, 250, 280)], (100, 200), (100, 600))

    def test_no_move(self):
        with self.assertRaises(ValueError):
            check([(100, 200)], (100, 200), (100, 600))

    def test_jump_to_end(self):
        with self.assertRaises(ValueError):
            check([(100, 200), (100, 600)], (100, 200), (100, 600))

    def test_wrong_start(self):
        with self.assertRaises(ValueError):
            check([(100, y) for y in (300, 400, 500, 600)], (100, 200), (100, 600))

    def test_wrong_direction(self):
        with self.assertRaises(ValueError):
            check([(100, y) for y in (200, 300, 250, 350, 450, 550, 600)], (100, 200), (100, 600))


class Events(unittest.TestCase):
    @staticmethod
    def trace(ys, lift=True):
        rows = ['EV_ABS ABS_MT_TRACKING_ID 00000001', 'EV_ABS ABS_MT_POSITION_X 00000064']
        for y in ys:
            rows += [f'EV_ABS ABS_MT_POSITION_Y {y:08x}', 'EV_SYN SYN_REPORT 00000000']
        if lift:
            rows += ['EV_ABS ABS_MT_TRACKING_ID ffffffff', 'EV_SYN SYN_REPORT 00000000']
        return '\n'.join(rows)

    def test_recorded_moves(self):
        points = read_events(self.trace([200, 300, 400, 500, 600]))
        self.assertEqual(points, [(100, y) for y in (200, 300, 400, 500, 600)])
        check(points, (100, 200), (100, 600))

    def test_recorded_down_up_is_not_a_swipe(self):
        with self.assertRaises(ValueError):
            check(read_events(self.trace([200])), (100, 200), (100, 600))

    def test_unreleased_finger_is_not_complete(self):
        with self.assertRaises(ValueError):
            read_events(self.trace([200, 300, 400, 500, 600], lift=False))

    def test_empty_recording_is_not_a_swipe(self):
        with self.assertRaises(ValueError):
            check(read_events(''), (100, 200), (100, 600))


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        unittest.main(argv=[sys.argv[0]])
    else:
        try:
            if len(sys.argv) != 6:
                raise ValueError('usage: android-swipe-check.py EVENTS X1 Y1 X2 Y2')
            with open(sys.argv[1]) as source:
                points = read_events(source.read())
            x1, y1, x2, y2 = map(int, sys.argv[2:])
            check(points, (x1, y1), (x2, y2))
            print(f'ok: the injected swipe covered its path ({len(points)} positions)')
        except (ValueError, OSError) as error:
            print(f'FAIL: {error}', file=sys.stderr)
            sys.exit(1)
