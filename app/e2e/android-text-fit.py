"""Every text on an Android screen shows its last word: reads each text's own box in a screenshot with tesseract and
fails on one whose last word isn't drawn there. NativeShell on Android 15 once broke a text it had measured as one
line into two after the terminal's input box had been used, and the box, one line high, cut off the second ("Your
computer asks GitHub once a"); the view tree still had the whole text.
Only texts in the scrolling area (the largest), wholly on screen, count: the header, what is scrolled half out and
the page under this one (still in the view tree) are left. So is a text reaching below NAV_TOP, the top of the
navigation bar: the area scrolls on under it and the bar's handle is drawn over such a text (on CI it read "Check
for new versions daily" as "check for new vets o taste fe"); the next page shows it higher up. Such texts are
counted in a line "under the navigation bar: N", for the suite to fail on at the end of Settings, where nothing
scrolls them out.
Usage: python3 e2e/android-text-fit.py SCREENSHOT.png UI.xml [NAV_TOP]   (uiautomator's dump of the same screen)
       python3 e2e/android-text-fit.py --self-test               (needs Pillow and tesseract, as the suites do)"""
import difflib
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

from PIL import Image, ImageDraw, ImageFont, ImageOps, ImageStat


def box(node):
    a, b, c, d = map(int, re.findall(r'\d+', node.get('bounds', '')))
    return a, b, c, d


def words(text):
    return [w for w in (re.sub(r'[^0-9a-z]', '', t.lower()) for t in text.split()) if w]


def read(image):
    """The words tesseract reads in a crop, dark on light (dark mode inverted), twice the size."""
    g = ImageOps.grayscale(image)
    if ImageStat.Stat(g).mean[0] < 128:
        g = ImageOps.invert(g)
    g = g.resize((g.width * 2, g.height * 2), Image.LANCZOS)
    out = subprocess.run(['tesseract', '-', '-', '--psm', '6'], input=_png(g), capture_output=True, check=True)
    return words(out.stdout.decode('utf-8', 'replace'))


def _png(image):
    import io
    b = io.BytesIO()
    image.save(b, 'PNG')
    return b.getvalue()


def shows_last_word(text, seen):
    """Whether `seen` (the words read in the text's box) ends with the text's last word. A word of one or two letters
    alone is too little to go by: the last two count then."""
    want = words(text)
    if not want:
        return True
    target = ''.join(want[-2:]) if len(want[-1]) < 3 and len(want) > 1 else want[-1]
    tail = seen[-3:]
    candidates = tail + [a + b for a, b in zip(tail, tail[1:])]
    return any(difflib.SequenceMatcher(None, target, c).ratio() >= 0.75 for c in candidates)


def check(png, ui, nav_top=None):
    shot = Image.open(png).convert('RGB')
    root = ET.parse(ui).getroot()
    areas = [n for n in root.iter('node') if n.get('scrollable') == 'true']
    if not areas:
        return ['no scrolling area on the screen'], 0, 0
    area = max(areas, key=lambda n: (box(n)[2] - box(n)[0]) * (box(n)[3] - box(n)[1]))
    ax, ay, aw, ah = box(area)
    bad, checked, under = [], 0, 0
    for n in area.iter('node'):
        text = n.get('text', '').strip()
        if not text or n.get('package', '').startswith('com.android') or not words(text):
            continue
        x0, y0, x1, y1 = box(n)
        # uiautomator gives a text scrolled half out the part that shows: one at an edge may be cut by it.
        if x0 < ax or y0 <= ay + 2 or x1 > aw or y1 >= ah - 2 or x1 - x0 < 8 or y1 - y0 < 8:
            continue
        if nav_top is not None and y1 > nav_top:
            under += 1
            continue
        checked += 1
        seen = read(shot.crop((max(0, x0 - 4), max(0, y0 - 2), min(shot.width, x1 + 4), min(shot.height, y1 + 2))))
        if not shows_last_word(text, seen):
            bad.append('"%s" at [%d,%d][%d,%d] shows only: %s' % (text, x0, y0, x1, y1, ' '.join(seen) or '(nothing)'))
    return bad, checked, under


def self_test():
    """A drawn text that ends early fails; the whole text, light or dark, passes, also with the navigation bar's top
    below it or at its bottom edge. One reaching into the bar (its handle drawn over it) is not read but counted."""
    import os
    import tempfile
    font = None
    for f in ('/System/Library/Fonts/Supplemental/Arial.ttf', '/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'):
        if os.path.exists(f):
            font = ImageFont.truetype(f, 34)
    if font is None:
        print('FAIL: android-text-fit self-test: no font to draw with')
        return 1
    full = 'Your computer asks GitHub once a day.'
    cut = 'Your computer asks GitHub once a'
    failed = 0
    with tempfile.TemporaryDirectory() as t:
        # name, text drawn, dark, NAV_TOP, then what check gives: texts read, whole, texts under the bar
        for name, drawn, dark, nav, want in (('whole', full, False, None, (1, True, 0)), ('cut', cut, False, None, (1, False, 0)),
                                             ('dark', full, True, None, (1, True, 0)), ('cut, bar below', cut, False, 300, (1, False, 0)),
                                             ('whole, bar at its edge', full, False, 245, (1, True, 0)),
                                             ('under the bar', full, True, 230, (0, True, 1))):
            img = Image.new('RGB', (1080, 400), (20, 20, 20) if dark else (255, 255, 255))
            d = ImageDraw.Draw(img)
            d.text((42, 200), drawn, fill=(200, 200, 200) if dark else (90, 90, 90), font=font)
            if nav is not None and nav < 245:
                d.rounded_rectangle((400, 228, 680, 240), radius=6, fill=(240, 240, 240))  # the gesture handle
            png, ui = os.path.join(t, 'a.png'), os.path.join(t, 'a.xml')
            img.save(png)
            with open(ui, 'w') as f:
                f.write('<hierarchy><node scrollable="true" bounds="[0,100][1080,400]">'
                        '<node text="%s" package="io.example" bounds="[42,195][900,245]"/></node></hierarchy>' % full)
            bad, checked, under = check(png, ui, nav)
            if (checked, not bad, under) != want:
                print('FAIL: android-text-fit self-test: %s text: read %d, under the bar %d, problems %s' % (name, checked, under, bad))
                failed = 1
    if not failed:
        print('ok: android-text-fit self-test')
    return failed


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        sys.exit(self_test())
    problems, n, under = check(sys.argv[1], sys.argv[2], int(sys.argv[3]) if len(sys.argv) > 3 else None)
    for p in problems:
        print('FAIL: cut off: ' + p)
    if not problems:
        print('ok: %d texts show their last word' % n)
    if under:
        print('under the navigation bar: %d' % under)
    sys.exit(1 if problems else 0)
