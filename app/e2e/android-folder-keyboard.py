#!/usr/bin/env python3
"""From the isolated emulator's paired Home, pick ~/src/<entry> at 320x640/200% with a real IME.

Called by android.sh; entry and a nonmatching sibling can be passed for an isolated UI-only run.
No agent is started. Restores the emulator's display/font overrides even on failure.
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
assert re.fullmatch(r'[a-zA-Z0-9_-]{2,}', entry), 'expected a sample folder name'
other = sys.argv[2] if len(sys.argv) > 2 else 'beta-web'
assert re.fullmatch(r'[a-zA-Z0-9_-]{2,}', other), 'expected a sample sibling name'
query = entry[:-1]
assert query.lower() not in other.lower(), 'the sibling must not match the filter'
sdk = os.environ.get('ANDROID_HOME') or os.path.expanduser('~/Library/Android/sdk')
adb = os.path.join(sdk, 'platform-tools/adb')
if not os.path.isfile(adb):
    adb = shutil.which('adb')
assert adb, 'adb is required'


def run(*args):
    return subprocess.check_output([adb, '-s', serial, *args], text=True).strip()


def tree():
    run('shell', 'uiautomator', 'dump', '/sdcard/hm-folder.xml')
    return ET.fromstring(run('shell', 'cat', '/sdcard/hm-folder.xml'))


def rect(node):
    return tuple(map(int, re.findall(r'-?\d+', node.get('bounds', ''))))


def find(root, key):
    return next((n for n in root.iter('node') if n.get('resource-id') == key), None)


def tap(key):
    for _ in range(10):
        node = find(tree(), key)
        if node is not None:
            x, y, right, bottom = rect(node)
            if right > x and bottom > y:
                run('shell', 'input', 'tap', str((x + right) // 2), str((y + bottom) // 2))
                return
        time.sleep(0.5)
    raise AssertionError(f'no reachable {key}')


def ime_top():
    window = run('shell', 'dumpsys', 'window', 'InputMethod')
    assert 'isOnScreen=true' in window, 'real IME must remain visible'
    # The input-method window can span the whole display; only its visible IME insets bound the keyboard.
    insets = run('shell', 'dumpsys', 'window')
    source = next((line for line in insets.splitlines() if 'type=ime ' in line and 'visible=true' in line), '')
    bounds = r'\[(\d+),(\d+)\]\[(\d+),(\d+)\]'
    frame = re.search('visibleFrame=' + bounds, source) or re.search('frame=' + bounds, source)
    assert frame, 'cannot read the visible IME insets frame'
    top = int(frame[2])
    assert 0 < top < 1600, 'IME frame is not a bounded visible keyboard'
    return top


def visible_row(root, keyboard, key=None):
    """Intersect the row with its scroll ancestors, not just its un-clipped layout box."""
    def visit(node, clip):
        box = rect(node)
        if node.get('scrollable') == 'true' and len(box) == 4:
            clip = (max(clip[0], box[0]), max(clip[1], box[1]),
                    min(clip[2], box[2]), min(clip[3], box[3]))
        if node.get('resource-id') == (key or f'folder-{entry}') and len(box) == 4:
            x, y, right, bottom = (max(box[0], clip[0]), max(box[1], clip[1]),
                                   min(box[2], clip[2]), min(box[3], clip[3]))
            if right > x and bottom - y >= 120:  # 48dp at 400dpi
                return ((x + right) // 2, (y + bottom) // 2)
        for child in node:
            result = visit(child, clip)
            if result:
                return result
        return None
    return visit(root, (0, 0, 800, keyboard))


def scroll_form(root, sheet_top=0, backward=False, slow=False):
    # Stay inside the actual native viewport, never drag Use or the backdrop.
    footer = find(root, 'folder-use')
    assert footer is not None, 'Use disappeared while browsing'
    regions = []
    for node in root.iter('node'):
        kind = node.get('class', '')
        box = rect(node)
        if len(box) != 4 or 'EditText' in kind or 'HorizontalScrollView' in kind:
            continue
        if node.get('scrollable') == 'true' or 'ScrollView' in kind:
            x, y, right, bottom = box
            if y >= sheet_top and bottom <= rect(footer)[1] and right - x >= 400 and bottom - y >= 120:
                regions.append(box)
    assert regions, 'no reachable 48dp folder scroll viewport with IME at 320x640/200%'
    x, y, right, bottom = max(regions, key=lambda box: box[3] - box[1])
    start, end = (y + 12, bottom - 12) if backward else (bottom - 12, y + 12)
    if slow:
        end = (start + end) // 2
    run('shell', 'input', 'swipe', str((x + right) // 2), str(start), str((x + right) // 2), str(end), '800' if slow else '400')
    time.sleep(0.5)
    return bottom - y


def observed_scroll(root, sheet_top):
    # Android's native ScrollView reports its offset and range. Silence is NOT an end signal.
    # A device-side timeout releases UiAutomation before the next hierarchy dump.
    package = find(root, 'folder-use').get('package')
    assert package, 'the form must belong to an app package'
    observer = subprocess.Popen([adb, '-s', serial, 'shell', 'timeout', '-k', '1', '6',
                                 'uiautomator', 'events'], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    try:
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            assert observer.poll() is None, 'native scroll observer exited before the gesture'
            if 'Ui Automation[eventTypes=' in run('shell', 'dumpsys', 'accessibility'):
                break
            time.sleep(0.1)
        else:
            raise AssertionError('native scroll observer did not connect')
        height = scroll_form(root, sheet_top, slow=True)
        output = observer.communicate(timeout=8)[0].decode('utf-8', errors='replace')
        assert observer.returncode in (124, 137), 'native scroll observer did not finish its bounded capture'
    finally:
        if observer.poll() is None:
            try:
                observer.communicate(timeout=8)
            except subprocess.TimeoutExpired:
                observer.kill()
                observer.wait()
    positions = []
    for line in output.splitlines():
        if ('TYPE_VIEW_SCROLLED;' not in line or f'PackageName: {package};' not in line
                or 'ClassName: android.widget.ScrollView;' not in line):
            continue
        fields = [re.search(r'(?:^|; )' + name + r': (-?\d+)(?=;|$)', line)
                  for name in ('ScrollX', 'MaxScrollX', 'ScrollY', 'MaxScrollY')]
        if all(fields):
            x, max_x, y, maximum = (int(field[1]) for field in fields)
            if x == max_x == 0 and 0 <= y <= maximum and maximum > 0:
                positions.append((y, maximum))
    assert positions, 'a forward gesture must emit a fresh native vertical scroll position'
    return positions[-1], height


def scroll_to_heading(height):
    for _ in range(8):
        root = tree()
        heading = next((n for n in root.iter('node') if n.get('text') == 'New space'), None)
        if heading is not None and rect(heading)[1] >= 0 and rect(heading)[3] - rect(heading)[1] >= height:
            return
        ime_top()
        scroll_form(root, backward=True)
    raise AssertionError('form must scroll back to its full heading')


def check_filter_exclusion(key, sheet_top):
    # UI Automator omits offscreen rows: inspect overlapping pages through a proven native end.
    root = tree()
    assert find(root, key) is None, 'the filter must remove its nonmatching sibling on every page'
    previous = 0
    for _ in range(12):
        ime_top()
        (position, maximum), height = observed_scroll(root, sheet_top)
        assert 0 < position - previous <= height, 'the gesture must advance without skipping a viewport'
        previous = position
        ime_top()
        root = tree()
        assert find(root, key) is None, 'the filter must remove its nonmatching sibling on every page'
        if position == maximum:
            return
    raise AssertionError('filtered listing did not reach its native scroll end')


size = re.search(r'Override size: (\S+)', run('shell', 'wm', 'size'))
density = re.search(r'Override density: (\d+)', run('shell', 'wm', 'density'))
scale = run('shell', 'settings', 'get', 'system', 'font_scale')
try:
    run('shell', 'wm', 'size', '800x1600')
    run('shell', 'wm', 'density', '400')
    run('shell', 'settings', 'put', 'system', 'font_scale', '2.0')
    time.sleep(2)
    tap('new')
    point = None
    for _ in range(8):
        root = tree()
        point = visible_row(root, 1600, 'folder-src')
        if point:
            break
        footer = find(root, 'folder-use')
        assert footer is not None, 'folder browser must be open before browsing src'
        crumbs = find(root, 'crumbs')
        start = rect(footer)[1] - 50
        end = max(rect(crumbs)[3] + 25 if crumbs is not None else 115, start - 500)
        assert end < start, 'no scrollable folder space before opening IME'
        run('shell', 'input', 'swipe', '400', str(start), '400', str(end), '400')
        time.sleep(0.5)
    assert point, 'src must be reachable by browsing before opening IME'
    run('shell', 'input', 'tap', *map(str, point))
    for _ in range(10):
        current = find(tree(), 'folder-use')
        if current is not None and current.get('content-desc', '').endswith('~/src'):
            break
        time.sleep(0.5)
    assert current is not None and current.get('content-desc', '').endswith('~/src'), 'must browse src before filtering'
    root = tree()
    assert find(root, f'folder-{other}') is not None, 'nonmatching sibling must exist before filtering'
    heading = next(n for n in root.iter('node') if n.get('text') == 'New space')
    # Measure before the IME can clip the title, not from a possibly clipped baseline.
    heading_height = rect(heading)[3] - rect(heading)[1]
    tap('folder-search')
    time.sleep(1)
    keyboard = ime_top()
    root = tree()
    heading = next(n for n in root.iter('node') if n.get('text') == 'New space')
    assert rect(heading)[1] >= 0, 'heading moved above the screen on initial keyboard raise'
    assert rect(heading)[3] - rect(heading)[1] >= heading_height, 'initial keyboard raise clipped the heading'
    footer = find(root, 'folder-use')
    assert footer is not None and rect(footer)[3] <= keyboard, 'Use must stay above IME'
    sheet_top = max(0, rect(heading)[1] - 60)
    # A positive empty-result state proves filtering actually ran. A sibling's
    # absence alone is not proof: UI Automator omits rows clipped by the IME.
    no_match = f'hm-no-match-{entry}-{other}'
    run('shell', 'input', 'text', no_match)
    empty = None
    for _ in range(8):
        ime_top()
        root = tree()
        empty = next((n for n in root.iter('node') if n.get('text') == 'No folders match'), None)
        if empty is not None:
            break
        scroll_form(root)
    assert empty is not None, 'a nonmatching query must render the empty-result message, not merely clip rows'
    # Keep the same real IME and focused field; then prove matching results return.
    run('shell', 'input', 'keyevent', 'KEYCODE_MOVE_END')
    for _ in no_match:
        run('shell', 'input', 'keyevent', 'KEYCODE_DEL')
    run('shell', 'input', 'text', query)
    time.sleep(1)
    keyboard = ime_top()
    root = tree()
    # The empty-result check scrolled the form down. Return to its heading before
    # browsing forward, or the first matching row could be above the viewport.
    scroll_to_heading(heading_height)
    keyboard = ime_top()
    root = tree()
    search = find(root, 'folder-search')
    field = next((n for n in search.iter('node') if n.get('class') == 'android.widget.EditText'), None) if search is not None else None
    assert field is not None and field.get('text') == query, 'search must contain the entered filter'
    footer = find(root, 'folder-use')
    assert footer is not None and rect(footer)[3] <= keyboard, 'Use must stay above IME'
    check_filter_exclusion(f'folder-{other}', sheet_top)
    # Stop reversing at the heading, then seek forward. A retry swipe past the top
    # is an unhandled background gesture and dismisses Android's real keyboard.
    scroll_to_heading(heading_height)
    point = None
    for _ in range(8):
        keyboard = ime_top()
        point = visible_row(tree(), keyboard)
        if point:
            break
        scroll_form(tree(), sheet_top)
    assert point, 'matching folder is clipped/unreachable with IME at 320x640/200%'
    run('shell', 'input', 'tap', *map(str, point))
    for _ in range(10):
        root = tree()
        selected = find(root, 'folder-use')
        if selected is not None and f'~/src/{entry}' in selected.get('content-desc', ''):
            break
        time.sleep(0.5)
    assert selected is not None and f'~/src/{entry}' in selected.get('content-desc', ''), 'first tap did not browse the matching folder with IME open'
    ime_top()
    tap('folder-use')
    for _ in range(10):
        chosen = find(tree(), 'folder-chosen')
        if chosen is not None:
            break
        time.sleep(0.5)
    assert chosen is not None and f'~/src/{entry}' in chosen.get('content-desc', ''), 'Use selected the wrong folder'
    print('PASS: compact large-text folder browse/filter/Use with real IME')
finally:
    run('shell', 'wm', 'size', size[1] if size else 'reset')
    run('shell', 'wm', 'density', density[1] if density else 'reset')
    if scale == 'null':
        run('shell', 'settings', 'delete', 'system', 'font_scale')
    else:
        run('shell', 'settings', 'put', 'system', 'font_scale', scale)
