import Foundation
var failures: [String] = []
func XCTFail(_ message: String) { failures.append(message) }
final class Alert {
  var present: Bool, dismisses: Bool
  var taps = 0
  init(_ present: Bool, _ dismisses: Bool) { self.present = present; self.dismisses = dismisses }
  var firstMatch: Alert { self }
  var buttons: Alert { self }
  var exists: Bool { present }
  func waitForExistence(timeout: Double) -> Bool { present }
  func element(boundBy: Int) -> Alert { precondition(boundBy == 0); return self }
  func tap() { taps += 1; if dismisses { present = false } }
}
var current = Alert(false, true)
struct XCUIApplication {
  init(bundleIdentifier: String) { precondition(bundleIdentifier == "com.apple.springboard") }
  var alerts: Alert { current }
}
var bad = 0
for (name, present, dismisses) in [("no-alert", false, true), ("notification", true, true), ("stuck-alert", true, false)] {
  current = Alert(present, dismisses)
  failures = []
  var notes: [String] = []
  let ready = zoomBaselineReady(note: { notes.append($0) })
  let expected = !present || dismisses
  // A baseline taken while the alert remains would compare the dark overlay with the unobscured picture.
  let captured = ready && !current.present
  let ok = ready == expected && captured == expected && current.taps == (present ? 1 : 0)
    && failures.isEmpty == expected
  print("\(ok ? "PASS" : "FAIL"): \(name), ready=\(ready), baseline=\(captured), taps=\(current.taps)")
  if !ok { bad += 1 }
}
exit(bad == 0 ? 0 : 1)
