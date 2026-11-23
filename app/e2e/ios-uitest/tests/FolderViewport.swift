import XCTest

// A short iPad-landscape regression for a sheet that moves above the IME after UIKit has inset its scroll view.
// Run against the same isolated folder fixture as FolderSheet; no production accounts or agents.
final class FolderViewport: XCTestCase {
  func test() {
    continueAfterFailure = false
    let out = ProcessInfo.processInfo.environment["OUT"] ?? "/tmp/folders.txt"
    var lines: [String] = []
    func note(_ text: String) {
      lines.append(text)
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.terminate(); app.activate()
    XCUIDevice.shared.orientation = .landscapeLeft
    defer { XCUIDevice.shared.orientation = .portrait }
    sleep(2)
    let screen = app.windows.firstMatch.frame
    XCTAssertGreaterThan(screen.width, screen.height, "this regression requires a real landscape viewport")
    XCTAssertGreaterThanOrEqual(screen.width, 840, "this regression requires an iPad-wide viewport")
    func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
    XCTAssertTrue(el("space-spios").waitForExistence(timeout: 40), "isolated fixture is not paired")
    let new = el("space-new").exists ? el("space-new") : el("new")
    XCTAssertTrue(new.waitForExistence(timeout: 10), "no New space control")
    new.tap()
    let search = el("folder-search"), use = el("folder-use"), kb = app.keyboards.firstMatch
    let title = app.staticTexts.matching(NSPredicate(format: "label == %@", "New space")).firstMatch
    XCTAssertTrue(search.waitForExistence(timeout: 10))
    XCTAssertTrue(el("folder-sample-01").waitForExistence(timeout: 10), "not the isolated folder fixture")
    search.tap()
    for _ in 0..<20 where !(kb.exists && kb.frame.minY < screen.maxY) { usleep(500_000) }
    XCTAssertTrue(kb.exists && kb.frame.minY < screen.maxY, "real software keyboard did not rise")
    let bar = XCUIApplication(bundleIdentifier: "com.apple.springboard").statusBars.firstMatch
    XCTAssertTrue(bar.exists && bar.frame.height > 0, "no status bar to measure")
    func visible(_ element: XCUIElement, _ name: String, _ step: String) {
      XCTAssertTrue(element.waitForExistence(timeout: 5), "\(step): missing \(name)")
      let f = element.frame
      note("\(step): \(name) \(NSCoder.string(for: f)); keyboard \(NSCoder.string(for: kb.frame))")
      XCTAssertGreaterThanOrEqual(f.minY, bar.frame.maxY, "\(step): \(name) moved above the visible sheet")
      XCTAssertLessThanOrEqual(f.maxY, kb.frame.minY, "\(step): \(name) is behind the keyboard")
    }
    func sheet(_ step: String) {
      XCTAssertTrue(kb.exists && kb.frame.minY < screen.maxY, "\(step): keyboard is down")
      visible(title, "title", step); visible(search, "search", step); visible(use, "Use", step)
      XCTAssertTrue(search.isHittable && use.isHittable, "\(step): controls are clipped by their sheet")
    }
    sleep(1); sheet("keyboard up")
    for _ in 0..<4 {
      XCTAssertTrue(kb.keys["z"].isHittable)
      kb.keys["z"].tap()
    }
    let empty = app.staticTexts["No folders match"].firstMatch
    XCTAssertTrue(empty.waitForExistence(timeout: 5), "search did not filter to zero results")
    sleep(1); sheet("no matches"); visible(empty, "empty result", "no matches")
    el("folder-clear").tap()
    let restored = el("folder-sample-01")
    XCTAssertTrue(restored.waitForExistence(timeout: 5), "clearing did not restore folders")
    sleep(1); sheet("results restored")
    visible(restored, "restored folder", "results restored")
    XCTAssertTrue(restored.isHittable, "restored folder is clipped by its scroll viewport")
    note("PASS: wide folder search and results remain visible above the real keyboard")
  }
}
