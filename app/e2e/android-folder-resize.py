#!/usr/bin/env python3
"""New space's folder search when the text size changes while it is open, with the real IME (320x640 dp).

The Activity handles a font-size or rotation change itself, so the open picker is not rebuilt. Its title and search
stay at the top while the window has 400 dp of height per unit of text size and scroll with the folders below that
(ui/folder-picker.tsx). Both ways across: open at one text size with the search focused, change the size in place,
and the search keeps its focus, its text and the keyboard; the title then scrolls (2x) or stays (1x) as the list
moves; the rest of the query types, a whole 48 dp result takes the first tap, and Use picks it.

  ANDROID_SERIAL=emulator-5554 android-folder-resize.py ENTRY   ENTRY: a folder in ~/src; the list there must be
                                                                longer than the sheet at 1x (android.sh adds some)
Restores the emulator's display and font settings even on failure, and leaves Home with no sheet.
"""
import os
import re
import shutil
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

serial = os.environ.get('ANDROID_SERIAL', '')
assert serial.startswith('emulator-'), 'requires an explicitly selected isolated emulator'
entry = sys.argv[1] if len(sys.argv) > 1 else 'acme-api'
assert re.fullmatch(r'[a-zA-Z0-9_-]{3,}', entry), 'expected a sample folder name'
sdk = os.environ.get('ANDROID_HOME') or os.path.expanduser('~/Library/Android/sdk')
adb = os.path.join(sdk, 'platform-tools/adb')
if not os.path.isfile(adb):
    adb = shutil.which('adb')
assert adb, 'adb is required'
ROW = 120  # 48 dp at 400 dpi


def run(*args):
    return subprocess.check_output([adb, '-s', serial, *args], text=True).strip()


def tree():
    run('shell', 'uiautomator', 'dump', '/sdcard/hm-resize.xml')
    return ET.fromstring(run('shell', 'cat', '/sdcard/hm-resize.xml'))


def rect(node):
    return tuple(map(int, re.findall(r'-?\d+', node.get('bounds', ''))))


def find(root, key):
    return next((n for n in root.iter('node') if n.get('resource-id') == key), None)


def title(root):
    return next((n for n in root.iter('node') if n.get('text') == 'New space'), None)


def field(root):
    search = find(root, 'folder-search')
    return next((n for n in search.iter('node') if n.get('class') == 'android.widget.EditText'), None) if search is not None else None


def keyboard_top():
    assert 'isOnScreen=true' in run('shell', 'dumpsys', 'window', 'InputMethod'), 'the keyboard went down'
    line = next((l for l in run('shell', 'dumpsys', 'window').splitlines() if 'type=ime ' in l and 'visible=true' in l), '')
    frame = re.search(r'visibleFrame=\[\d+,(\d+)\]', line) or re.search(r'frame=\[\d+,(\d+)\]', line)
    assert frame and 0 < int(frame[1]) < 1600, 'cannot read the keyboard\'s top'
    return int(frame[1])


def tap_node(node):
    x, y, right, bottom = rect(node)
    run('shell', 'input', 'tap', str((x + right) // 2), str((y + bottom) // 2))


def wait(what, ok, seconds=10):
    end = time.time() + seconds
    while time.time() < end:
        root = tree()
        got = ok(root)
        if got is not None and got is not False:
            return root, got
        time.sleep(0.5)
    raise AssertionError(what)


def viewport(root):
    """The list's native scroll view: the tallest one above Use and the keyboard."""
    use = find(root, 'folder-use')
    assert use is not None, 'Use is gone'
    boxes = [rect(n) for n in root.iter('node') if n.get('class') == 'android.widget.ScrollView']  # scrollable=false when it fits
    boxes = [b for b in boxes if b[3] <= rect(use)[1] and b[2] - b[0] >= 400 and b[3] - b[1] >= ROW]
    assert boxes, 'no folder list with 48 dp of room above the keyboard'
    return max(boxes, key=lambda b: b[3] - b[1])


def drag(root, up):
    x, y, right, bottom = viewport(root)
    a, b = (bottom - 15, y + 15) if up else (y + 15, bottom - 15)
    run('shell', 'input', 'swipe', str((x + right) // 2), str(a), str((x + right) // 2), str(b), '700')
    time.sleep(1)


def rows(root):
    return {n.get('resource-id'): rect(n) for n in root.iter('node') if (n.get('resource-id') or '').startswith(('folder-', 'crumb'))}


def mode(want_pinned, what):
    """The list moved by one drag; the title stayed where it was (pinned) or went up with it. Then back to the top."""
    before = tree()
    head = title(before)
    assert head is not None and rect(head)[1] >= 0, f'{what}: the title is not on screen before the drag'
    drag(before, up=True)
    after = tree()
    moved = [k for k, b in rows(before).items() if k in rows(after) and rows(after)[k][1] < b[1] - 20]
    gone = [k for k in rows(before) if k not in rows(after)]
    assert moved or gone, f'{what}: one drag moved no row of the list (it must be longer than the sheet)'
    now = title(after)
    if want_pinned:
        assert now is not None and rect(now) == rect(head), f'{what}: the title moved with the list ({rect(head)} -> {rect(now) if now is not None else "gone"})'
        assert field(after) is not None, f'{what}: the search moved with the list'
    else:
        assert now is None or rect(now)[1] < rect(head)[1] - 20, f'{what}: the title stayed while the list moved ({rect(head)})'
    # Back to where it was, and no further: a drag past the top is an unhandled gesture that puts Android's keyboard
    # away (as in android-folder-keyboard.py).
    for _ in range(6):
        root = tree()
        if title(root) is not None and rect(title(root)) == rect(head):
            break
        drag(root, up=False)
    else:
        raise AssertionError(f'{what}: the list did not scroll back to its title')
    keyboard_top()
    print(f'ok: {what}: the title {"stays at the top" if want_pinned else "scrolls with the folders"} as the list moves')


def visible_row(root, key, bottom):
    box = rect(find(root, key)) if find(root, key) is not None else None
    if not box:
        return None
    x, y, right, b = box
    top, end = max(y, viewport(root)[1]), min(b, viewport(root)[3], bottom)
    return (x, top, right, end) if end - top >= ROW else None


def across(start, end, pinned_after):
    what = f'text {start}x to {end}x with the search focused'
    run('shell', 'settings', 'put', 'system', 'font_scale', start)
    time.sleep(2)
    wait('no New space button', lambda r: find(r, 'new'))
    tap_node(find(tree(), 'new'))
    wait('the folder browser did not open', lambda r: find(r, 'folder-search'))
    for _ in range(8):
        root = tree()
        src = visible_row(root, 'folder-src', 1600)
        if src:
            run('shell', 'input', 'tap', str((src[0] + src[2]) // 2), str((src[1] + src[3]) // 2))
            break
        drag(root, up=True)
    wait('src did not open', lambda r: (find(r, 'folder-use') is not None and find(r, 'folder-use').get('content-desc', '').endswith('~/src')) or None)
    for _ in range(4):
        drag(tree(), up=False)
    tap_node(field(tree()))
    wait('the keyboard did not come up for the search', lambda r: 'isOnScreen=true' in run('shell', 'dumpsys', 'window', 'InputMethod') or None)
    run('shell', 'input', 'text', entry[:1])
    wait('the search did not take the first letter', lambda r: field(r) is not None and field(r).get('text') == entry[:1] or None)
    assert field(tree()).get('focused') == 'true', f'{what}: the search is not focused before the change'
    # The change itself, in place: the picker is not rebuilt.
    run('shell', 'settings', 'put', 'system', 'font_scale', end)
    time.sleep(3)
    keyboard_top()
    root = tree()
    box = field(root)
    assert box is not None and box.get('focused') == 'true', f'{what}: the search lost its focus'
    assert box.get('text') == entry[:1], f'{what}: the search lost its text ({box.get("text")!r})'
    print(f'ok: {what}: the search keeps its focus, its text and the keyboard')
    mode(pinned_after, what)
    run('shell', 'input', 'text', entry[1:-1])
    try:
        wait('', lambda r: field(r) is not None and field(r).get('text') == entry[:-1] or None)
    except AssertionError:
        box = field(tree())
        raise AssertionError(f'{what}: the search did not take the rest of the query: it has '
                             f'{box.get("text") if box is not None else None!r}, focused {box.get("focused") if box is not None else None}, '
                             f'keyboard {"up" if "isOnScreen=true" in run("shell", "dumpsys", "window", "InputMethod") else "down"}')
    point = None
    for _ in range(8):
        root = tree()
        point = visible_row(root, f'folder-{entry}', keyboard_top())
        if point:
            break
        drag(root, up=True)
    assert point, f'{what}: no whole 48 dp {entry} row above the keyboard'
    run('shell', 'input', 'tap', str((point[0] + point[2]) // 2), str((point[1] + point[3]) // 2))
    wait(f'{what}: the first tap did not open {entry}', lambda r: (find(r, 'folder-use') is not None and find(r, 'folder-use').get('content-desc', '').endswith(f'~/src/{entry}')) or None)
    tap_node(find(tree(), 'folder-use'))
    wait(f'{what}: Use did not pick {entry}', lambda r: (find(r, 'folder-chosen') is not None and f'~/src/{entry}' in find(r, 'folder-chosen').get('content-desc', '')) or None)
    print(f'ok: {what}: typed on, a whole result took the first tap, Use picked ~/src/{entry}')
    for _ in range(3):
        if find(tree(), 'folder-chosen') is None:
            break
        run('shell', 'input', 'keyevent', 'KEYCODE_BACK')
        time.sleep(1)
    wait('the sheet did not close', lambda r: find(r, 'folder-chosen') is None and find(r, 'new') is not None or None)


size = re.search(r'Override size: (\S+)', run('shell', 'wm', 'size'))
density = re.search(r'Override density: (\d+)', run('shell', 'wm', 'density'))
scale = run('shell', 'settings', 'get', 'system', 'font_scale')
try:
    run('shell', 'wm', 'size', '800x1600')
    run('shell', 'wm', 'density', '400')
    across('1.0', '2.0', pinned_after=False)
    across('2.0', '1.0', pinned_after=True)
    print('PASS: folder search across the text-size threshold, both ways, with the real IME')
finally:
    run('shell', 'wm', 'size', size[1] if size else 'reset')
    run('shell', 'wm', 'density', density[1] if density else 'reset')
    if scale == 'null':
        run('shell', 'settings', 'delete', 'system', 'font_scale')
    else:
        run('shell', 'settings', 'put', 'system', 'font_scale', scale)
