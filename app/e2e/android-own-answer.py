#!/usr/bin/env python3
"""The decision card's own answer on Android, with the real keyboard: each time it opens, without a finger on the
chat, the whole card shows above the keyboard (the box, Cancel, Send, the card's bottom edge); at the chat's end no
keyboard-sized blank is left under it. android-ask.sh runs it on the first question's card; it leaves the card as it
found it (the own answer closed, the keyboard down).

  ANDROID_SERIAL=emulator-5554 android-own-answer.py          the whole check, on the chat that shows the card
                                                              (HM_OWN_SHOTS=DIR: the first opening and after Cancel)
  android-own-answer.py check open|end DUMP.xml DENSITY       one uiautomator dump only (DENSITY: dpi, as wm density)
"""
import os
import re
import shutil
import subprocess
import sys
import time
import xml.etree.ElementTree as ET


def rect(root, key):
    node = next((n for n in root.iter('node') if n.get('resource-id') == key), None)
    if node is None:
        return None
    return tuple(map(int, re.findall(r'-?\d+', node.get('bounds', ''))))


def problem(root, mode, dpi):
    """What is wrong with the screen (None: nothing). Bounds are px; dp = px / (dpi / 160)."""
    dp = dpi / 160
    scroll, card = rect(root, 'chat-scroll'), rect(root, 'ask-card')
    if not scroll or not card:
        return 'no chat or no card on screen'
    # Under the card: the content's bottom padding (16 dp) when it is all in view, nothing when the chat cuts it.
    below = (scroll[3] - card[3]) / dp
    if mode == 'end':
        return None if below <= 40 else f'{below:.0f} dp of blank under the card at the chat\'s end'
    send, cancel, box = rect(root, 'ask-own-send'), rect(root, 'ask-own-cancel'), rect(root, 'ask-own-input')
    if not send or not cancel or not box:
        return 'the own answer has no box, Cancel or Send on screen'
    if below < 8:
        return f'the chat cuts the card ({below:.0f} dp under its bottom edge)'
    if box[1] < scroll[1]:
        return 'the box is above the chat\'s top'
    if (send[3] - send[1]) / dp < 40:
        return 'Send is cut'
    return None


if len(sys.argv) == 5 and sys.argv[1] == 'check':
    why = problem(ET.parse(sys.argv[3]).getroot(), sys.argv[2], float(sys.argv[4]))
    print(why or 'ok')
    sys.exit(1 if why else 0)

serial = os.environ.get('ANDROID_SERIAL', '')
assert serial, 'set ANDROID_SERIAL'
sdk = os.environ.get('ANDROID_HOME') or os.path.expanduser('~/Library/Android/sdk')
adb = os.path.join(sdk, 'platform-tools/adb')
if not os.path.isfile(adb):
    adb = shutil.which('adb')
assert adb, 'adb is required'


def run(*args):
    return subprocess.check_output([adb, '-s', serial, *args], text=True).strip()


def tree():
    run('shell', 'uiautomator', 'dump', '/sdcard/hm-own.xml')
    return ET.fromstring(run('shell', 'cat', '/sdcard/hm-own.xml'))


def keyboard_up():
    # The keyboard's window on screen (input_method's mInputShown stays true after the keyboard is gone, e2e/android.sh),
    # and its insets shown: the window can stay on screen a while after it went down, and a Back then leaves the chat.
    if 'isOnScreen=true' not in run('shell', 'dumpsys', 'window', 'InputMethod'):
        return False
    return any('type=ime ' in line and 'visible=true' in line for line in run('shell', 'dumpsys', 'window').splitlines())


def until(what, ok, seconds=20):
    end = time.time() + seconds
    while time.time() < end:
        root = tree()
        if ok(root):
            return root
        time.sleep(0.5)
    raise AssertionError(what)


def tap(key):
    r = rect(until(f'no {key} on screen', lambda root: rect(root, key)), key)
    run('shell', 'input', 'tap', str((r[0] + r[2]) // 2), str((r[1] + r[3]) // 2))


def open_own(i):
    """Opens the own answer and waits for its box. The card closes it when what it shows changes (ui/ask-card.tsx), as
    the agent's screen can just after the card appears: then the row is tapped again, and the log says so."""
    for attempt in range(1, 4):
        tap('ask-own')
        end = time.time() + 5
        while time.time() < end:
            if rect(tree(), 'ask-own-input'):
                if attempt > 1:
                    print(f'note: own answer, opening {i}: open after {attempt} taps (the card changed under the first)')
                return
            time.sleep(0.5)
    raise AssertionError(f'own answer, opening {i}: three taps on Write my own answer opened no box')


def snap(name):
    if os.environ.get('HM_OWN_SHOTS'):
        with open(os.path.join(os.environ['HM_OWN_SHOTS'], name), 'wb') as f:
            f.write(subprocess.check_output([adb, '-s', serial, 'exec-out', 'screencap', '-p']))


def settled():
    """The keyboard up and still: the screen reads the same twice, 0.6 s apart."""
    end = time.time() + 15
    while not keyboard_up():
        assert time.time() < end, 'the keyboard did not come up for the own answer'
        time.sleep(0.5)
    last = None
    while time.time() < end:
        root = tree()
        now = [rect(root, k) for k in ('chat-scroll', 'ask-card', 'ask-own-send')]
        if now == last:
            return root
        last = now
        time.sleep(0.6)
    return root


dpi = float(re.findall(r'\d+', run('shell', 'wm', 'density'))[-1])
for i in range(1, 4):
    open_own(i)
    why = problem(settled(), 'open', dpi)
    assert not why, f'own answer, opening {i}: {why}'
    print(f'ok: own answer, opening {i}: the whole card above the keyboard')
    if i == 1:
        snap('2b-own-answer-open.png')
    if i < 3:
        tap('ask-own-cancel')
        until('Cancel did not close the own answer', lambda root: not rect(root, 'ask-own-input'))
scroll = rect(tree(), 'chat-scroll')
x = (scroll[0] + scroll[2]) // 2
for _ in range(3):
    run('shell', 'input', 'swipe', str(x), str(scroll[3] - 20), str(x), str(scroll[1] + 20), '200')
    time.sleep(0.8)
why = problem(settled(), 'end', dpi)
assert not why, f'own answer, the chat at its end: {why}'
print('ok: own answer, the chat at its end: no blank under the card')
tap('ask-own-cancel')
until('Cancel did not close the own answer', lambda root: not rect(root, 'ask-own-input'))
# The keyboard goes down with the box (ui/ask-card.tsx): left up with nothing to type in, a Back leaves the chat.
end = time.time() + 5
while keyboard_up():
    assert time.time() < end, 'the keyboard stayed up after Cancel closed the own answer'
    time.sleep(0.5)
until('the chat is no longer on screen after the own answer', lambda root: rect(root, 'ask-card'), 10)
print('ok: own answer, Cancel: the box closed, the keyboard down, still on the chat')
snap('2c-own-answer-cancelled.png')
