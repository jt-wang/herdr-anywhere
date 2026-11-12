#!/usr/bin/env python3
# Is the made-up bar chart (e2e/fake-model -png: four bars, blue, orange, green, purple, 60, 110, 85 and 135 px high on
# a 240 by 160 ground) drawn in this screenshot? Finds the chart's light ground (rows and columns mostly of its colour),
# then each bar's colour on it, and checks the bars stand left to right in that order, as wide as each other, and as
# high as each other as in the chart, and that right of it is only the terminal's background. Prints what it found;
# exits 1 if the chart is not there or something is drawn beside it.
# Usage: e2e/kitty-chart.py SCREENSHOT.png
import struct
import sys
import zlib

GROUND = (0xF4, 0xF1, 0xEA)
BARS = [((0x2F, 0x6F, 0xB3), 60), ((0xD9, 0x6C, 0x2B), 110), ((0x3C, 0x9A, 0x5F), 85), ((0x8A, 0x4F, 0xB8), 135)]


def pixels(path):
    data = open(path, 'rb').read()
    at, idat = 8, b''
    while at < len(data):
        n, kind = struct.unpack('>I4s', data[at:at + 8])
        if kind == b'IHDR':
            w, h, depth, ctype = struct.unpack('>IIBB', data[at + 8:at + 18])
            if depth != 8 or ctype not in (2, 6):
                sys.exit(f'{path}: an 8-bit RGB or RGBA PNG is needed')
            k = 4 if ctype == 6 else 3
        elif kind == b'IDAT':
            idat += data[at + 8:at + 8 + n]
        at += 12 + n
    raw, row = zlib.decompress(idat), w * k
    out, prev = bytearray(row * h), bytearray(row)
    for y in range(h):
        f, line = raw[y * (row + 1)], bytearray(raw[y * (row + 1) + 1:(y + 1) * (row + 1)])
        for i in range(row):
            a = line[i - k] if i >= k else 0
            b, c = prev[i], prev[i - k] if i >= k else 0
            if f == 1:
                line[i] = (line[i] + a) & 255
            elif f == 2:
                line[i] = (line[i] + b) & 255
            elif f == 3:
                line[i] = (line[i] + ((a + b) >> 1)) & 255
            elif f == 4:
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                line[i] = (line[i] + (a if pa <= pb and pa <= pc else b if pb <= pc else c)) & 255
        out[y * row:(y + 1) * row], prev = line, line
    return w, h, k, out


def main(path):
    w, h, k, px = pixels(path)
    near = lambda i, c, t: abs(px[i] - c[0]) + abs(px[i + 1] - c[1]) + abs(px[i + 2] - c[2]) <= t
    # The ground: the longest run of rows where a quarter of the points are its colour (the light theme's bars above and
    # below the terminal are near that colour too).
    def longest(ok):
        best, start = (0, -1, -1), None
        for i, v in enumerate(ok + [False]):
            if v and start is None:
                start = i
            elif not v and start is not None:
                best, start = max(best, (i - start, start, i - 1)), None
        return best
    ys = list(range(0, h, 2))
    n, a, b = longest([sum(near((y * w + x) * k, GROUND, 15) for x in range(0, w, 2)) * 2 >= w / 4 for y in ys])
    if not n:
        print('FAIL: no chart ground on screen')
        return 1
    top, bottom = ys[a], ys[b]
    # Its width: the longest run of ground in its top rows, above the bars.
    xs_ = list(range(0, w, 2))
    _, a, b = max(longest([near((y * w + x) * k, GROUND, 15) for x in xs_]) for y in range(top, min(top + 12, bottom + 1), 2))
    left, right = xs_[a], xs_[b]
    print(f'ground: x {left}..{right}, y {top}..{bottom}')
    boxes = []
    for colour, _ in BARS:
        x0 = y0 = 1 << 30
        x1 = y1 = -1
        n = 0
        for y in range(top, bottom + 1, 2):
            for x in range(left, right + 1, 2):
                if near((y * w + x) * k, colour, 40):
                    n += 1
                    x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
        boxes.append((n, x0, y0, x1 - x0 + 1, y1 - y0 + 1) if n else (0, 0, 0, 0, 0))
    print('bars (pixels seen, x, y, width, height):', boxes)
    if any(n < 50 for n, *_ in boxes):
        print('FAIL: not every bar is there')
        return 1
    xs = [x for _, x, *_ in boxes]
    widths = [bw for *_, bw, _ in boxes]
    heights = [bh for *_, bh in boxes]
    unit = heights[3] / BARS[3][1]
    bad = []
    if xs != sorted(xs):
        bad.append('bars out of order')
    if max(widths) > min(widths) * 1.2:
        bad.append(f'widths differ: {widths}')
    for (_, want), got in zip(BARS, heights):
        if abs(got - want * unit) > want * unit * 0.12 + 4:
            bad.append(f'a bar {got} px high, {round(want * unit)} expected')
    # Each bar is a block of its colour: most of its box is filled.
    for (n, _, _, bw, bh) in boxes:
        if n < bw * bh / 4 * 0.8:
            bad.append(f'a bar fills {n} of {bw * bh // 4} points in its box')
    # Beside the picture, out to the screen's edge, only the terminal's background: no letters drawn too large behind
    # it, no marks of its last cells. The edge itself is left out (a scroll bar or ruler may sit there).
    strip = [(y * w + x) * k for y in range(top, bottom + 1, 2) for x in range(right + 6, w - 12, 2)]
    if strip:
        counts = {}
        for i in strip:
            c = (px[i] >> 3, px[i + 1] >> 3, px[i + 2] >> 3)
            counts[c] = counts.get(c, 0) + 1
        bg = max(counts, key=counts.get)
        bg = (bg[0] * 8 + 4, bg[1] * 8 + 4, bg[2] * 8 + 4)
        off = sum(not near(i, bg, 24) for i in strip)
        print(f'right of the chart: {off} of {len(strip)} points not its background')
        if off > len(strip) // 500:
            bad.append(f'{off} points right of the chart are not the background')
    if bad:
        print('FAIL: ' + '; '.join(bad))
        return 1
    print('ok: the chart is drawn')
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1]))
