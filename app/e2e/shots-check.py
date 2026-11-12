"""What ci-shots-check.sh needs Python for: QR codes in pictures and videos, and the words on pictures.

    python shots-check.py mask <folder>         every QR code on a picture painted over, in place; a video with one fails
    python shots-check.py words <folder>        each picture's words, as "<file>\t<line>", read by tesseract or, on a
                                                Mac without it, by Apple's Vision (desktop-tool.swift scan)
    python shots-check.py qr <picture>          what each QR code on it holds, one per line (for the self-test)
    python shots-check.py sample <out> <text>   <out>/qr.png holding <text> as a QR code, <out>/words.png showing it

Never prints what a code holds, except with qr. Needs zxing-cpp and Pillow; videos need ffmpeg.
"""
import os
import shutil
import subprocess
import sys
import tempfile

import zxingcpp
from PIL import Image, ImageDraw, ImageFont

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
    for path in files(folder, PICTURES):
        if tesseract:
            out = subprocess.run([tesseract, path, "-", "--psm", "11"], capture_output=True, text=True, check=True).stdout
            lines = out.splitlines()
        else:
            out = subprocess.run(["swift", os.path.join(HERE, "desktop-tool.swift"), "scan", path],
                                 capture_output=True, text=True, check=True).stdout
            lines = [l[5:] for l in out.splitlines() if l.startswith("text ")]
        for line in lines:
            if line.strip():
                print(f"{path}\t{line}")


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
