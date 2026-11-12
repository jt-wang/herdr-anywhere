"""Whether a terminal shows a selection, from screenshots: python selection.py learn <before.png> <after.png> x y w h
                                                         python selection.py count <shot.png> x y w h r g b

x y w h: the terminal's box, in the shots' pixels. "learn" takes a shot before and after a word was selected (a hold)
and prints the selection's colour and how many more pixels of it the after shot has: "color <r> <g> <b> grew <n>".
The colour is the commonest one among the pixels that changed (a word's background outnumbers its letters).
"count" prints how many pixels in the box are within 12 of a colour on each channel. Learning the colour from the
app's own hold, on the same simulator and theme, needs no theme's numbers here. Needs Pillow.
"""
import collections
import sys

from PIL import Image


def box(path, b):
    x, y, w, h = b
    return Image.open(path).convert("RGB").crop((x, y, x + w, y + h))


def pixels(img):
    raw = img.tobytes()
    return zip(raw[0::3], raw[1::3], raw[2::3])


def count(img, rgb):
    r0, g0, b0 = rgb
    return sum(1 for r, g, b in pixels(img) if abs(r - r0) <= 12 and abs(g - g0) <= 12 and abs(b - b0) <= 12)


def main():
    how = sys.argv[1]
    if how == "learn":
        a, b = sys.argv[2:4]
        bx = tuple(int(v) for v in sys.argv[4:8])
        ia, ib = box(a, bx), box(b, bx)
        changed = collections.Counter(
            (q[0] // 4 * 4, q[1] // 4 * 4, q[2] // 4 * 4)
            for p, q in zip(pixels(ia), pixels(ib)) if sum(abs(u - v) for u, v in zip(p, q)) > 40)
        if not changed:
            print("color none grew 0")
            return
        rgb = changed.most_common(1)[0][0]
        print(f"color {rgb[0]} {rgb[1]} {rgb[2]} grew {count(ib, rgb) - count(ia, rgb)}")
    elif how == "count":
        bx = tuple(int(v) for v in sys.argv[3:7])
        print(count(box(sys.argv[2], bx), tuple(int(v) for v in sys.argv[7:10])))
    else:
        sys.exit("learn or count")


if __name__ == "__main__":
    main()
