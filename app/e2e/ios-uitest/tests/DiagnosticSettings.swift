import XCTest

// Run on an installation whose diagnostic preferences are absent (fresh), off (upgrade-off), or
// enabled with only network selected (upgrade-on). The driver preserves the existing pairing.
final class DiagnosticSettings: XCTestCase {
  func testConsentAndNavigation() throws {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let mode = env["DIAGNOSTIC_MODE"] ?? "fresh"
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    let all = app.descendants(matching: .any)
    func el(_ id: String) -> XCUIElement { all[id].firstMatch }
    func open() {
      for _ in 0..<4 {
        if el("settings").exists { break }
        if el("diagnostic-back").exists { el("diagnostic-back").tap() }
        else if el("back").exists { el("back").tap() }
      }
      XCTAssertTrue(el("settings").waitForExistence(timeout: 15))
      el("settings").tap()
      for _ in 0..<8 {
        if el("settings-diagnostics").isHittable { break }
        app.swipeUp()
      }
      XCTAssertTrue(el("settings-diagnostics").isHittable)
      XCTAssertEqual(el("settings-diagnostics-state").label, mode == "upgrade-on" ? "On" : "Off")
      XCTAssertFalse(el("diagnostic-toggle").exists)
      el("settings-diagnostics").tap()
      XCTAssertTrue(el("diagnostic-toggle").waitForExistence(timeout: 10))
    }
    func shot(_ name: String) throws {
      let image = XCUIScreen.main.screenshot()
      let attachment = XCTAttachment(screenshot: image)
      attachment.name = "diagnostics-" + name; attachment.lifetime = .keepAlways; add(attachment)
      if let prefix = env["SHOTS"] { try image.pngRepresentation.write(to: URL(fileURLWithPath: prefix + "-" + name + ".png")) }
    }
    let categories = ["interaction", "performance", "network", "resources"]
    func off() {
      XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == 'Off'")).count, 4)
      for name in categories { XCTAssertFalse(el("diagnostic-" + name).isEnabled) }
      XCTAssertTrue(el("diagnostic-recording").label.contains("Recording off"))
    }
    app.activate(); open()
    if mode == "upgrade-on" {
      XCTAssertTrue(el("diagnostic-recording").label.contains("Recording selected"))
      XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == 'On'")).count, 1)
      try shot("upgrade-preserved")
      el("diagnostic-toggle").tap(); off()
      try shot("disabled-saved-choice")
      el("diagnostic-toggle").tap()
      XCTAssertTrue(el("diagnostic-recording").label.contains("Recording selected"))
      el("diagnostic-toggle").tap(); off()
      return
    }
    off(); try shot(mode + "-off")
    if mode == "upgrade-off" { return }
    // Coordinates exercise a real touch even though the category is disabled.
    el("diagnostic-network").coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
    off()
    el("diagnostic-toggle").tap()
    XCTAssertTrue(el("diagnostic-recording").label.contains("No categories selected"))
    for name in categories { XCTAssertTrue(el("diagnostic-" + name).isEnabled) }
    XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == 'Off'")).count, 4)
    try shot("master-only")
    el("diagnostic-network").tap()
    XCTAssertTrue(el("diagnostic-recording").label.contains("Recording selected"))
    try shot("network-selected")
    el("diagnostic-toggle").tap(); off()
    el("diagnostic-back").tap()
    XCTAssertTrue(el("settings-diagnostics").waitForExistence(timeout: 5))
    XCTAssertEqual(el("settings-diagnostics-state").label, "Off")
    try shot("settings-entry")
  }
}
