# The program e2e/android-typing.sh runs in the terminal: it logs each read as a Python bytes literal (to hm-raw) and shows it.
import os, termios, tty
saved = termios.tcgetattr(0); tty.setraw(0)
os.write(1, b'raw: each read on its own line\r\n')
with open('hm-raw', 'ab', 0) as f:
    while True:
        d = os.read(0, 4096)
        f.write(repr(d).encode() + b'\n')
        os.write(1, b'got ' + repr(d).encode() + b'\r\n')
        if d == b'\x04': break
termios.tcsetattr(0, termios.TCSADRAIN, saved)
