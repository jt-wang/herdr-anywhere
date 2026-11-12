"""How many rows a terminal's content moved between two screenshots, read from numbered rows ("acme-api line N", the
fake models' ROWS answer): python rows.py <before.png> <after.png> x y w h

x y w h: the terminal's box on both, in the shots' pixels. Reads the box's left part with tesseract and, for each
numbered row on the before shot, the row at the same height on the after shot. Prints "moved <rows> pitch <pixels>
pairs <n>": the median of the number before less the number after (positive: the finger pulled history into view),
the median height of a row, and how many rows were paired; "moved none" when fewer than 3 were. Reading the numbers
replaced matching pixels: a row is not a whole number of pixels high on the phone, so the same row drawn lower is not
the same pixels. Needs Pillow and tesseract.
"""
import re
import statistics
import subprocess
import sys
import tempfile

from PIL import Image

LINE = re.compile(r"^line$", re.I)


def numbered(path, box):
    x, y, w, h = box
    with tempfile.NamedTemporaryFile(suffix=".png") as f:
        Image.open(path).convert("L").crop((x, y, x + int(w * 0.6), y + h)).save(f.name)
        tsv = subprocess.run(["tesseract", f.name, "-", "--psm", "6", "tsv"], capture_output=True, text=True).stdout
    words = [r.split("\t") for r in tsv.splitlines()[1:]]
    words = [(int(r[7]), int(r[9]), r[11]) for r in words if len(r) == 12 and r[11].strip()]  # top, height, text
    rows = {}
    for (t0, h0, a), (t1, h1, b) in zip(words, words[1:]):
        if LINE.match(a) and re.fullmatch(r"\d+", b) and abs(t0 - t1) < h0:
            rows[t1 + h1 / 2] = int(b)
    return sorted(rows.items())


def main():
    a, b = sys.argv[1:3]
    box = tuple(int(v) for v in sys.argv[3:7])
    ra, rb = numbered(a, box), numbered(b, box)
    gaps = [(y1 - y0) / (n1 - n0) for (y0, n0), (y1, n1) in zip(ra, ra[1:]) if n1 > n0]
    pitch = statistics.median(gaps) if gaps else 0
    moves = []
    for y, n in ra:
        near = min(rb, key=lambda r: abs(r[0] - y), default=None)
        if near and pitch and abs(near[0] - y) < pitch / 2:
            moves.append(n - near[1])
    if len(moves) < 3:
        print(f"moved none pitch {pitch:.1f} pairs {len(moves)}")
    else:
        print(f"moved {statistics.median(moves):g} pitch {pitch:.1f} pairs {len(moves)}")


if __name__ == "__main__":
    main()
