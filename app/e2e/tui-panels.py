# Stands in for OpenCode in e2e/android-tui-scroll.sh and e2e/ios-tui-scroll.sh: the alternate screen and mouse modes
# 1000, 1002, 1003 and 1006 as OpenCode turns on, a prompt box on its bottom 9 rows that ignores the wheel (OpenCode
# 1.19.0 does, measured), and two panels side by side that each move 3 rows per wheel report (OpenCode's
# default scroll_speed). Appends each report it gets to OUT as "up|down COL ROW left|right|ignored", and other input as "keys HEX"; writes its size
# to OUT.layout as "COLS ROWS HALF".
# STEP: rows a report moves the panels, 3 by default (OpenCode); 1 stands in for Pi 1.4.0.
# Usage: python3 -I tui-panels.py OUT [STEP]
import os, re, select, signal, sys, tty
out = sys.argv[1]
STEP = int(sys.argv[2]) if len(sys.argv) > 2 else 3
BOX = 9
top = [0, 0]
def draw(*_):
    cols, rows = os.get_terminal_size()
    half = cols // 2
    s = '\x1b[2J'
    for r in range(1, rows - BOX + 1):
        s += '\x1b[%d;1Hleft %d' % (r, top[0] + r) + '\x1b[%d;%dH|right %d' % (r, half + 1, top[1] + r)
    for r in range(rows - BOX + 1, rows + 1):
        s += '\x1b[%d;1H\x1b[7m%s\x1b[0m' % (r, ('prompt box' if r == rows - BOX + 2 else '').ljust(cols))
    os.write(1, s.encode())
    with open(out + '.layout', 'w') as f: f.write('%d %d %d\n' % (cols, rows, half))
os.write(1, b'\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h')
signal.signal(signal.SIGWINCH, draw)
tty.setraw(0)
draw()
buf = b''
while True:
    try: ready = select.select([0], [], [], 0.05)[0]
    except InterruptedError: continue
    if ready:
        buf += os.read(0, 4096)
        continue
    if not buf: continue
    cols, rows = os.get_terminal_size()
    lines = []
    for m in re.finditer(rb'\x1b\[<(6[45]);(\d+);(\d+)M', buf):
        x, y = int(m[2]), int(m[3])
        if y > rows - BOX: where = 'ignored'
        else:
            where = 'left' if x <= cols // 2 else 'right'
            top[0 if where == 'left' else 1] += -STEP if m[1] == b"64" else STEP
        lines.append('%s %d %d %s' % ('up' if m[1] == b'64' else 'down', x, y, where))
    rest = re.sub(rb'\x1b\[<\d+;\d+;\d+[Mm]', b'', buf)
    if rest: lines.append('keys ' + rest.hex())
    buf = b''
    if lines:
        draw()
        with open(out, 'a') as f: f.write('\n'.join(lines) + '\n')
