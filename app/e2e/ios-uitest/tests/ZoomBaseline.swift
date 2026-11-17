import XCTest

// The picture baseline must be taken without the system notification question over it.
func zoomBaselineReady(note: (String) -> Void) -> Bool {
  let question = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
  if question.waitForExistence(timeout: 3) {
    // Notifications.testDeny uses the first system-alert button too.
    question.buttons.element(boundBy: 0).tap()
    note("notification question dismissed before fitted baseline")
  }
  guard !question.waitForExistence(timeout: 1) else {
    XCTFail("system alert still covers the fitted baseline")
    return false
  }
  return true
}
