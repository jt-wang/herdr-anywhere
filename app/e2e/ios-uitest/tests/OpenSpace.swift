import XCTest
// First-tap outcomes stay in the suite's notes. For 0.2.0 only, still being on home after 3 s is a known issue:
// save it, then tap once more so the rest of the suite can run. Menus, process exit, intentional misses and failure
// to open after that remain failures. ios-open-space-test.sh checks this policy without a simulator.
struct SpaceOpener {
  let app: XCUIApplication, out: String, note: (String) -> Void

  func elements(_ name: String) { try? app.debugDescription.write(toFile: out + "-" + name + ".txt", atomically: true, encoding: .utf8) }

  private func blocked(_ stage: String) -> Bool {
    let why: String
    if app.state == .notRunning { why = "process exited" }
    else if app.descendants(matching: .any)["menu"].firstMatch.exists { why = "long-press menu" }
    else { return false }
    elements("space-tap-failed")
    note("\(stage): \(why)")
    XCTFail("\(stage): \(why)")
    return true
  }

  /// Taps the space SPACE on home; true when it had to tap a second time. The caller then waits for the page as before.
  func open(_ space: String, missFirst: Bool = ProcessInfo.processInfo.environment["MISS_SPACE"] == "1") -> Bool {
    guard !blocked("before first space tap") else { return false }
    let tile = app.descendants(matching: .any)["space-" + space].firstMatch
    let listed = tile.exists
    XCTAssertTrue(tile.waitForExistence(timeout: 40), "home does not list the space")
    note(listed ? "home listed the space when the test began" : "home listed the space")
    let seen = Date()
    elements("before-space-tap")
    if missFirst {
      note("tapping beside the space (MISS_SPACE)")
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.01)).tap()
      note("tapped beside the space (MISS_SPACE)")
    } else {
      // Where and when, read before the tap: once the space opens the tile is gone, and reading it then failed the
      // test ("Failed to get matching snapshot").
      let row = tile.frame
      // Wait for visible content and record it. Pixels do not prove the row will handle a tap.
      let waitFrom = Date()
      var drawnRow = 0
      while Date().timeIntervalSince(waitFrom) < 10 {
        drawnRow = drawnPixels(in: row.insetBy(dx: 4, dy: 8), of: app)
        if drawnRow >= 30 { break }
        usleep(250_000)
      }
      note(String(format: "the space's row drawn: %d pixels, %.1f s after the wait began", drawnRow, Date().timeIntervalSince(waitFrom)))
      let at = NSCoder.string(for: row)
      note("tapping the space")
      tile.tap()
      note(String(format: "tapped the space at %@, %.1f s after the test saw it listed", at, Date().timeIntervalSince(seen)))
    }
    if missFirst {
      note("first space tap: intentional miss (self-test)")
      XCTFail("intentional first-space miss (self-test)")
    }
    let page = app.descendants(matching: .any)["space-more"].firstMatch
    let opened = page.waitForExistence(timeout: 3)
    guard !blocked("first space tap") else { return false }
    if opened { note("first space tap: opened"); return false }
    if tile.exists {
      elements("3s-after-space-tap")
      note("first space tap: still on home; KNOWN ISSUE in 0.2.0")
      note("3 s after the tap the app is still on home; tapping the space again")
      tile.tap()
      note("tapped the space again")
      return true
    }
    note("first space tap: neither home nor space")
    XCTFail("first space tap: neither home nor space")
    return false
  }

  /// The caller's page/terminal check still has to pass, even after the known first-tap issue.
  func finish(again: Bool, shown: Bool) {
    guard !blocked("after opening space") else { return }
    XCTAssertTrue(shown, "the space or terminal did not open")
    guard again else { return }
    elements("after-second-space-tap")
    note("after the known first-tap issue: space/terminal \(shown ? "opened" : "did not open")")
  }
}
