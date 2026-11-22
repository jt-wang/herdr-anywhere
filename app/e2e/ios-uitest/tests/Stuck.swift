import XCTest
// Run by e2e/ios-stuck.sh once the Companion has stopped: the banner that says the app still can't reach the computer
// (kit.tsx ConnBanner) and the sheet its Other options opens (StuckSheet). Leaves $DIR/$THEME-1-banner.png and
// $DIR/$THEME-2-sheet.png, then closes the sheet.
final class Stuck: XCTestCase {
  func test() {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let dir = env["DIR"] ?? "/tmp/stuck", theme = env["THEME"] ?? "light"
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
    func shot(_ name: String) {
      try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(theme)-\(name).png"))
    }
    XCTAssertTrue(el("conn-options").waitForExistence(timeout: 90), "no Other options on the banner")
    XCTAssertTrue(el("conn-retry").exists, "no Try again on the banner")
    XCTAssertFalse(el("pair-again").exists, "Pair again is on the banner, not in the sheet")
    sleep(2); shot("1-banner")
    el("conn-options").tap()
    if !el("stuck-sheet").waitForExistence(timeout: 5), el("conn-options").exists { el("conn-options").tap() }
    XCTAssertTrue(el("stuck-sheet").waitForExistence(timeout: 10), "Other options opened no sheet")
    for id in ["conn-sign-in", "conn-command", "conn-copy", "conn-open-connection", "pair-again"] {
      XCTAssertTrue(el(id).exists, "the sheet has no \(id)")
    }
    let scan = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Only scan the QR code")).firstMatch
    XCTAssertTrue(scan.exists, "the sheet does not say to scan only the computer's own code")
    sleep(2); shot("2-sheet")
    el("stuck-close").tap()
    XCTAssertTrue(el("conn-options").waitForExistence(timeout: 10), "closing the sheet left no banner")
  }
}
