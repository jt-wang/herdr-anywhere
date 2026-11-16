import Foundation
import CoreGraphics

func pinGestureTrace(name: String, attempt: Int, source: CGRect, target: CGRect,
                     start: CGPoint, end: CGPoint, press: Double, hold: Double,
                     now: () -> Double = { Date().timeIntervalSince1970 * 1000 },
                     note: (String) -> Void, drag: () -> Void) {
  let firstUp = name == "up" && attempt == 1
  if firstUp {
    note("pin-up begin time=\(now()) attempt=\(attempt) source=\(source.origin.x),\(source.origin.y),\(source.width),\(source.height) target=\(target.origin.x),\(target.origin.y),\(target.width),\(target.height) start=\(start.x),\(start.y) end=\(end.x),\(end.y) press=\(press) hold=\(hold) velocity=300")
  }
  drag()
  if firstUp { note("pin-up returned time=\(now()) attempt=\(attempt)") }
}
