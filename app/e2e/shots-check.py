"""What ci-shots-check.sh needs Python for: QR codes in pictures and videos, and the words on pictures.

    python shots-check.py mask <folder>         every QR code on a picture painted over, in place; a video with one fails
    python shots-check.py words <folder>        each picture's words, as "<file>\t<line>", read by tesseract or, on a
                                                Mac without it, by Apple's Vision (desktop-tool.swift scan)
    python shots-check.py qr <picture>          what each QR code on it holds, one per line (for the self-test)
    python shots-check.py sample <out> <text>   <out>/qr.png holding <text> as a QR code, <out>/words.png showing it,
                                                <out>/words-grey.png showing it in a field's grey on a phone screen

Never prints what a code holds, except with qr. Needs zxing-cpp and Pillow; videos need ffmpeg.
"""
import os
import shutil
import subprocess
import sys
import tempfile

import zxingcpp
from PIL import Image, ImageChops, ImageDraw, ImageFilter, ImageFont, ImageOps

PICTURES = (".png", ".jpg", ".jpeg")
VIDEOS = (".mp4", ".webm", ".mov")
HERE = os.path.dirname(os.path.abspath(__file__))


def files(folder, ends):
    for root, _, names in os.walk(folder):
        for name in sorted(names):
            if name.lower().endswith(ends):
                yield os.path.join(root, name)


def codes(image):
    return zxingcpp.read_barcodes(image, formats=zxingcpp.BarcodeFormat.QRCode)


def paint(path):
    image = Image.open(path).convert("RGB")
    found = codes(image)
    if not found:
        return 0
    draw = ImageDraw.Draw(image)
    for code in found:
        p = code.position
        xs = [p.top_left.x, p.top_right.x, p.bottom_right.x, p.bottom_left.x]
        ys = [p.top_left.y, p.top_right.y, p.bottom_right.y, p.bottom_left.y]
        # The quiet zone too, as frame.py does: a code painted to its finder patterns' edge still shows a module or two.
        pad = max(12, (max(xs) - min(xs)) // 8)
        draw.rectangle((min(xs) - pad, min(ys) - pad, max(xs) + pad, max(ys) + pad), fill=(128, 128, 128))
    image.save(path)
    if codes(Image.open(path).convert("RGB")):
        sys.exit(f"FAIL: {path} still has a QR code after painting")
    return len(found)


def video_codes(path):
    tmp = tempfile.mkdtemp()
    try:
        subprocess.run(["ffmpeg", "-loglevel", "error", "-i", path, "-vf", "fps=2", os.path.join(tmp, "%05d.png")], check=True)
        return sum(1 for f in files(tmp, PICTURES) if codes(Image.open(f).convert("RGB")))
    finally:
        shutil.rmtree(tmp)


def mask(folder):
    pictures = painted = 0
    for path in files(folder, PICTURES):
        pictures += 1
        painted += paint(path)
    for path in files(folder, VIDEOS):
        n = video_codes(path)
        if n:
            sys.exit(f"FAIL: {path} shows a QR code in {n} frames; a video can't be painted over, so it can't be uploaded")
    print(f"ok: {pictures} pictures, {painted} QR codes painted over")


def words(folder):
    tesseract = shutil.which("tesseract")
    if not tesseract and sys.platform != "darwin":
        sys.exit("FAIL: no tesseract to read the pictures' words")
    print(f"words read by {'tesseract' if tesseract else 'Vision'}", file=sys.stderr)
    tmp = tempfile.mkdtemp()
    for path in files(folder, PICTURES):
        if tesseract:
            lines = []
            for picture, psm in reads(path, tmp):
                out = subprocess.run([tesseract, picture, "-", "--psm", psm], capture_output=True, text=True, check=True).stdout
                lines += out.splitlines()
        else:
            out = subprocess.run(["swift", os.path.join(HERE, "desktop-tool.swift"), "scan", path],
                                 capture_output=True, text=True, check=True).stdout
            lines = [l[5:] for l in out.splitlines() if l.startswith("text ")]
        for line in lines:
            if line.strip():
                print(f"{path}\t{line}")
    shutil.rmtree(tmp)


def reads(path, tmp):
    """The picture read three ways. As it is, tesseract missed text in a light grey (a field's placeholder, 150 on 255)
    on a whole phone screen, and the white labels of dark buttons. So also: the parts darker than their surroundings,
    black on white, read as a page (--psm 11 missed the link there even on a crop of the field alone); and the parts
    lighter than a dark surrounding. Measured on a pairing screen and on the samples sample() makes."""
    g = ImageOps.grayscale(Image.open(path))
    ground = g.filter(ImageFilter.BoxBlur(25))
    darker = os.path.join(tmp, "darker.png")
    ImageChops.subtract(ground, g).point(lambda v: 0 if v > 25 else 255).save(darker)
    lighter = os.path.join(tmp, "lighter.png")
    on_dark = ground.point(lambda v: 255 if v < 110 else 0)
    Image.composite(ImageChops.subtract(g, ground), Image.new("L", g.size, 0), on_dark).point(lambda v: 0 if v > 40 else 255).save(lighter)
    return [(path, "11"), (darker, "3"), (lighter, "11")]


def sample(out, text):
    os.makedirs(out, exist_ok=True)
    qr = zxingcpp.create_barcode(text, zxingcpp.BarcodeFormat.QRCode)
    code = Image.fromarray(qr.to_image(scale=8))
    page = Image.new("RGB", (code.width + 200, code.height + 200), "white")
    page.paste(code, (100, 100))
    page.save(os.path.join(out, "qr.png"))
    font = ImageFont.load_default(size=36)
    width = int(ImageDraw.Draw(Image.new("RGB", (1, 1))).textlength(text, font=font)) + 80
    page = Image.new("RGB", (width, 200), "white")
    ImageDraw.Draw(page).text((40, 70), text, fill="black", font=font)
    page.save(os.path.join(out, "words.png"))
    # A phone screen with the text in a field, in the grey of its placeholder: tesseract read it only after reads().
    page = Image.new("RGB", (1206, 2622), (242, 242, 240))
    draw = ImageDraw.Draw(page)
    for i, line in enumerate(["Your agents, on your phone", "On the computer", "herdr-anywhere start", "herdr-anywhere pair"]):
        draw.text((50, 300 + i * 120), line, fill="black", font=ImageFont.load_default(size=44))
    draw.rounded_rectangle((24, 1100, 1182, 1220), 20, fill="white", outline=(210, 210, 210), width=3)
    small = ImageFont.load_default(size=36 if draw.textlength(text, font=font) < 1100 else 28)
    draw.text((50, 1140), text, fill=(150, 150, 150), font=small)
    draw.rounded_rectangle((24, 1260, 1182, 1380), 20, fill=(30, 30, 30))
    draw.text((520, 1300), "Pair", fill="white", font=ImageFont.load_default(size=40))
    page.save(os.path.join(out, "words-grey.png"))


def main():
    what = sys.argv[1]
    if what == "mask":
        mask(sys.argv[2])
    elif what == "words":
        words(sys.argv[2])
    elif what == "qr":
        for code in codes(Image.open(sys.argv[2]).convert("RGB")):
            print(code.text)
    elif what == "sample":
        sample(sys.argv[2], sys.argv[3])
    else:
        sys.exit(f"unknown: {what}")


if __name__ == "__main__":
    main()
