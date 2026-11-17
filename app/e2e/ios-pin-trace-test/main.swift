import Foundation
import CoreGraphics

var events: [String] = []
var clock = 1000.0
pinGestureTrace(name: "up", attempt: 1, source: CGRect(x: 10, y: 200, width: 300, height: 40),
                target: CGRect(x: 10, y: 100, width: 300, height: 60), start: CGPoint(x: 160, y: 220),
                end: CGPoint(x: 160, y: 120), press: 0.8, hold: 10,
                now: { clock += 1; return clock }, note: { events.append($0) }, drag: { events.append("gesture") })
let expected = [
  "pin-up begin time=1001.0 attempt=1 source=10.0,200.0,300.0,40.0 target=10.0,100.0,300.0,60.0 start=160.0,220.0 end=160.0,120.0 press=0.8 hold=10.0 velocity=300",
  "gesture", "pin-up returned time=1002.0 attempt=1"
]
if events != expected { fputs("FAIL: first up lacks geometry and before/after timestamps\n", stderr); exit(1) }
for (name, attempt) in [("down", 1), ("up", 2)] {
  events = []
  pinGestureTrace(name: name, attempt: attempt, source: .zero, target: .zero, start: .zero, end: .zero,
                  press: 0.8, hold: 0.3, note: { events.append($0) }, drag: { events.append("gesture") })
  if events != ["gesture"] { fputs("FAIL: traced beyond first up\n", stderr); exit(1) }
}
print("PASS: first up geometry/timing, unchanged single gesture, later drags untraced")
