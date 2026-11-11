# picture-timing.py VIDEO TX TY L T R B: ms from the finger's lift (show_touches' dot at TX,TY starting to fade) to the
# picture in the box L T R B, twice as START/SETTLED: START is the first frame from the lift whose box, shrunk to 8×8,
# differs from the first frame's by more than 4 per channel on average (the sheet starts to come up); SETTLED the first
# within 4 of the last frame's (the sheet stopped, the picture drawn). Prints them, or "-" with why on stderr.
# VIDEO: adb screenrecord, dark theme.
import subprocess, sys
video, tx, ty, l, t, r, b = sys.argv[1], *map(int, sys.argv[2:8])

def frames(x, y, w, h, n):
    # Each frame's time and its box shrunk to n×n (RGB bytes).
    # passthrough: one output frame per recorded frame (screenrecord writes one only when the screen changes).
    p = subprocess.run(["ffmpeg", "-v", "info", "-i", video, "-fps_mode", "passthrough", "-vf", f"crop={w}:{h}:{x}:{y},scale={n}:{n}:flags=area,showinfo",
                        "-f", "rawvideo", "-pix_fmt", "rgb24", "-"], capture_output=True)
    ts = [float(l.split("pts_time:")[1].split()[0]) for l in p.stderr.decode().splitlines() if "pts_time:" in l]
    size = n * n * 3
    return [(ts[i], p.stdout[i * size:(i + 1) * size]) for i in range(min(len(ts), len(p.stdout) // size))]

def diff(a, c): return sum(abs(a[i] - c[i]) for i in range(len(a))) / len(a)
dot = frames(tx - 8, ty - 8, 16, 16, 1)
if not dot: print("-"); print("no frames", file=sys.stderr); sys.exit()
base = dot[0][1]
down = next((i for i, (_, c) in enumerate(dot) if diff(c, base) > 30), None)
if down is None: print("-"); print("no touch dot seen", file=sys.stderr); sys.exit()
# The dot fades once the finger is up: the lift is the first frame the dot is fainter than while it was down.
pressed = dot[down][1]
lift = next((dot[i][0] for i in range(down + 1, len(dot)) if diff(dot[i][1], pressed) > 8), None)
if lift is None: print("-"); print("the dot never went", file=sys.stderr); sys.exit()
pic = frames(l, t, r - l, b - t, 8)
end = pic[-1][1]
start = next((ts for ts, c in pic if ts >= lift and diff(c, pic[0][1]) > 4), None)
shown = next((ts for ts, c in pic if ts >= lift and diff(c, end) < 4), None)
if start is None or shown is None: print("-"); print("the picture never came or never settled", file=sys.stderr); sys.exit()
print(f"{round((start - lift) * 1000)}/{round((shown - lift) * 1000)}")
