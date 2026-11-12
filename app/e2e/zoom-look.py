#!/usr/bin/env python3
# What a zoom script (android-zoom.sh, ios-zoom.sh) sees in the picture's box on a screenshot. The box is
# "left top right bottom" in the screenshot's pixels; a few pixels inside it are looked at, off its border and corners.
#   zoom-look.py diff A.png B.png BOX   how much the box differs between two shots: mean |Δ| per channel, 0..255
#   zoom-look.py edge A.png BOX COLOR   the share of the box's outer ring that is the sheet's own colour (#rrggbb), in
#                                       %: above a few, the picture was dragged off its edge and the sheet shows
import sys
from PIL import Image, ImageChops, ImageStat

INSET = 6


def crop(path, box):
    l, t, r, b = (int(v) for v in box.split())
    return Image.open(path).convert('RGB').crop((l + INSET, t + INSET, r - INSET, b - INSET))


def diff(a, b, box):
    x, y = crop(a, box), crop(b, box)
    return sum(ImageStat.Stat(ImageChops.difference(x, y)).mean) / 3


def edge(a, box, color):
    im = crop(a, box)
    w, h = im.size
    want = tuple(int(color.lstrip('#')[i:i + 2], 16) for i in (0, 2, 4))
    px = im.load()
    ring = [(x, y) for x in range(w) for y in (0, 1, 2, h - 3, h - 2, h - 1)]
    ring += [(x, y) for y in range(h) for x in (0, 1, 2, w - 3, w - 2, w - 1)]
    near = sum(1 for (x, y) in ring if all(abs(px[x, y][i] - want[i]) <= 6 for i in range(3)))
    return 100 * near / len(ring)


if __name__ == '__main__':
    cmd = sys.argv[1]
    if cmd == 'diff':
        print(f'{diff(sys.argv[2], sys.argv[3], sys.argv[4]):.1f}')
    elif cmd == 'edge':
        print(f'{edge(sys.argv[2], sys.argv[3], sys.argv[4]):.1f}')
    else:
        sys.exit(f'unknown command {cmd}')
