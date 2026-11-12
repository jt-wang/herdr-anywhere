import XCTest
// Opening a space from home, for the tests that start there (TypeIn, Picture; Scroll has its own, with its self-test
// switches). Notes when it tapped the space, where, and how long after home listed it, with the screen's elements in
// OUT-before-space-tap.txt. If 3 s after the tap the app is still on home (the space listed, no space page), the
// elements then and a second tap; the caller fails all the same (finish). MISS_SPACE=1, the scripts' self-test, makes
// the first tap land at the top of the screen, beside the space.
struct SpaceOpener {
  let app: XCUIApplication, out: String, note: (String) -> Void

  func elements(_ name: String) { try? app.debugDescription.write(toFile: out + "-" + name + ".txt", atomically: true, encoding: .utf8) }

  /// Taps the space SPACE on home; true when it had to tap a second time. The caller then waits for the page as before.
  func open(_ space: String) -> Bool {
    let tile = app.descendants(matching: .any)["space-" + space].firstMatch
    let listed = tile.exists
    XCTAssertTrue(tile.waitForExistence(timeout: 40), "home does not list the space")
    note(listed ? "home listed the space when the test began" : "home listed the space")
    let seen = Date()
    elements("before-space-tap")
    if ProcessInfo.processInfo.environment["MISS_SPACE"] == "1" {
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.01)).tap()
      note("tapped beside the space (MISS_SPACE)")
    } else {
      tile.tap()
      note(String(format: "tapped the space at %@, %.1f s after the test saw it listed", NSCoder.string(for: tile.frame), Date().timeIntervalSince(seen)))
    }
    let page = app.descendants(matching: .any)["space-more"].firstMatch
    if page.waitForExistence(timeout: 3) { return false }
    if tile.exists {
      elements("3s-after-space-tap")
      note("3 s after the tap the app is still on home; tapped a second time")
      tile.tap()
      return true
    }
    note("3 s after the tap: no space page and no space on home; waiting")
    return false
  }

  /// After the caller waited for the space's page (SHOWN): fails when it took a second tap, with the elements then.
  func finish(again: Bool, shown: Bool) {
    guard again else { return }
    elements("after-second-space-tap")
    XCTFail("the first tap on the space opened nothing; the second \(shown ? "did" : "did not either")")
  }
}
