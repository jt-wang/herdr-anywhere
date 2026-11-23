import XCTest
// Run by e2e/ios-keyboard.sh after TypeIn. New space's folder sheet with the software keyboard up, over a home with more
// folders than fit (the script makes them): its title, search, list and Use this folder stay between the status bar and
// the keyboard, while the list scrolls, the search filters with the keyboard's keys and a folder is browsed and used;
// then Rename space's field and Save, also with the keyboard up. Notes each frame to $OUT as well as asserting.
final class FolderSheet: XCTestCase {
  // Each failure into $OUT too, as it happens: the script prints that file whatever xcodebuild says.
  override func record(_ issue: XCTIssue) {
    let out = ProcessInfo.processInfo.environment["OUT"] ?? "/tmp/folders.txt"
    let line = "FAILED: \(issue.compactDescription) (FolderSheet.swift:\(issue.sourceCodeContext.location?.lineNumber ?? 0))\n"
    if let h = FileHandle(forWritingAtPath: out) { h.seekToEndOfFile(); h.write(Data(line.utf8)); h.closeFile() }
    else { try? line.write(toFile: out, atomically: true, encoding: .utf8) }
    super.record(issue)
  }
  func test() {
    continueAfterFailure = false // a failed step stops here, not in a later wait on what is not there
    let out = ProcessInfo.processInfo.environment["OUT"] ?? "/tmp/folders.txt"
    var lines: [String] = []
    let start = Date()
    func note(_ s: String) {
      lines.append(String(format: "+%.1f s ", Date().timeIntervalSince(start)) + s)
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    // Home again, whatever page TypeIn left. Not launch(): XCTest then hangs ending the app after the test.
    app.terminate(); app.activate()
    func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
    func shot(_ name: String) {
      let a = XCTAttachment(screenshot: XCUIScreen.main.screenshot()); a.name = "folders-\(name)"; a.lifetime = .keepAlways; add(a)
    }
    let kb = app.keyboards.firstMatch, screen = app.windows.firstMatch.frame
    func key(_ name: String) {
      let k = kb.keys[name].exists ? kb.keys[name] : kb.buttons[name]
      XCTAssertTrue(k.exists && k.frame.maxY <= screen.maxY, "no key \(name) on the screen's keyboard")
      k.tap(); usleep(400_000)
    }
    // The status bar's bottom: SpringBoard draws it over the app.
    let bar = XCUIApplication(bundleIdentifier: "com.apple.springboard").statusBars.firstMatch
    // The software keyboard on the screen: with a hardware keyboard it stays in the tree below the screen.
    func kbUp() -> Bool { kb.exists && kb.frame.minY < screen.maxY }
    func waitKb() -> Bool { for _ in 0..<10 where !kbUp() { usleep(500_000) }; return kbUp() }
    // Each element whole on the screen, below the status bar and above the keyboard.
    func clear(_ e: XCUIElement, _ name: String, _ step: String) {
      XCTAssertTrue(e.waitForExistence(timeout: 5), "\(step): no \(name)")
      XCTAssertTrue(bar.exists && bar.frame.height > 0, "\(step): no status bar to measure the top against")
      let f = e.frame, top = bar.frame.maxY
      let bottom = kbUp() ? kb.frame.minY : screen.maxY
      note("\(step): \(name) \(NSCoder.string(for: f)); status bar ends at \(top), keyboard \(kbUp() ? NSCoder.string(for: kb.frame) : "down")")
      XCTAssertGreaterThanOrEqual(f.minY, top, "\(step): \(name) \(NSCoder.string(for: f)) is under the status bar (ends at \(top))")
      XCTAssertLessThanOrEqual(f.maxY, bottom, "\(step): \(name) \(NSCoder.string(for: f)) is behind the keyboard (top \(bottom))")
      XCTAssertTrue(f.minX >= screen.minX && f.maxX <= screen.maxX, "\(step): \(name) \(NSCoder.string(for: f)) is off the screen's sides")
    }
    let search = el("folder-search"), use = el("folder-use")
    // The sheet's title, not the home screen's New space button under the sheet.
    let title = app.staticTexts.matching(NSPredicate(format: "label == %@", "New space")).firstMatch
    func sheet(_ step: String) {
      XCTAssertTrue(kbUp(), "\(step): the keyboard is not on the screen")
      clear(title, "title", step); clear(search, "search", step); clear(use, "Use this folder", step)
    }

    XCTAssertTrue(el("space-spios").waitForExistence(timeout: 40), "home does not list the space")
    let new = el("space-new").exists ? el("space-new") : el("new")
    XCTAssertTrue(new.waitForExistence(timeout: 10), "no New space button on home")
    new.tap()
    XCTAssertTrue(search.waitForExistence(timeout: 10), "New space opens no folder search")
    XCTAssertTrue(el("folder-sample-01").waitForExistence(timeout: 10), "the sheet does not list the home's folders")
    sleep(1)
    XCTAssertTrue(bar.exists && bar.frame.height > 0, "no status bar on the screen to measure the sheet's top against")
    note("status bar \(NSCoder.string(for: bar.frame)), window \(NSCoder.string(for: screen))")
    clear(title, "title", "keyboard down"); clear(use, "Use this folder", "keyboard down")
    search.tap()
    XCTAssertTrue(waitKb(), "the search brings no keyboard onto the screen")
    sleep(2); shot("keyboard-up")
    sheet("keyboard up")

    // Browse: drags between the search and Use this folder scroll the sheet to its last folder with the keyboard
    // still up; the heading may scroll away with the folders, Use this folder stays. Then drags back to the heading.
    let last = el("folder-sample-24"), top = bar.frame.maxY
    let low = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: screen.midX, dy: use.frame.minY - 12))
    let high = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: screen.midX, dy: search.frame.maxY + 12))
    func lastShown() -> Bool { last.exists && last.frame.minY >= top && last.frame.maxY <= use.frame.minY }
    for i in 0..<8 where !lastShown() { note("drag up \(i + 1)"); low.press(forDuration: 0.05, thenDragTo: high); usleep(500_000) }
    XCTAssertTrue(lastShown(), "the list does not scroll to its last folder")
    XCTAssertTrue(kbUp(), "scrolled: the keyboard is not on the screen")
    clear(last, "the last folder", "scrolled"); clear(use, "Use this folder", "scrolled")
    func headShown() -> Bool { title.exists && title.frame.minY >= top && search.frame.maxY <= use.frame.minY }
    for i in 0..<8 where !headShown() { note("drag down \(i + 1)"); high.press(forDuration: 0.05, thenDragTo: low); usleep(500_000) }
    sheet("scrolled back")
    note("scrolled; typing src")
    // Filter with the keyboard's own keys.
    for k in ["s", "r", "c"] { key(k) }
    sleep(1); shot("filtered")
    // The field's accessibility value stays empty under XCTest; what was typed shows in what the list keeps.
    XCTAssertTrue(el("folder-src").waitForExistence(timeout: 5), "the search does not find src")
    let filtered = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: el("folder-sample-01"))
    XCTAssertEqual(XCTWaiter().wait(for: [filtered], timeout: 5), .completed, "the search does not filter the folders")
    clear(el("folder-src"), "src", "filtered"); sheet("filtered")

    note("filtered; opening src")
    // Into src, then Use this folder: the next step names it.
    el("folder-src").tap()
    XCTAssertTrue(el("folder-acme-api").waitForExistence(timeout: 5), "src does not open")
    sheet("in src"); clear(el("folder-up"), "the way up", "in src")
    use.tap()
    let chosen = el("folder-chosen")
    XCTAssertTrue(chosen.waitForExistence(timeout: 5), "Use this folder does not go on to the program")
    XCTAssertTrue(chosen.label.contains("~/src"), "the next step's folder is \(chosen.label), not ~/src")
    sleep(1); shot("program")
    clear(chosen, "the folder chosen", "program"); clear(el("new-terminal"), "Terminal", "program")
    app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.08)).tap(); sleep(2) // above the sheet: closes it
    XCTAssertFalse(chosen.exists, "the sheet did not close")

    note("sheet closed; opening the space")
    // Rename space, with the keyboard up for its field.
    el("space-spios").tap()
    if !el("space-more").waitForExistence(timeout: 5) { el("space-spios").tap() }
    XCTAssertTrue(el("space-more").waitForExistence(timeout: 10), "the space did not open")
    el("space-more").tap()
    let item = app.descendants(matching: .any)["Rename space"].firstMatch
    XCTAssertTrue(item.waitForExistence(timeout: 5), "no Rename space")
    item.tap()
    XCTAssertTrue(el("rename").waitForExistence(timeout: 5), "Rename space opens no field")
    XCTAssertTrue(waitKb(), "Rename space brings no keyboard onto the screen")
    sleep(2); shot("rename")
    clear(el("rename"), "the name", "rename"); clear(el("rename-save"), "Save", "rename")
    note("saving")
    el("rename-save").tap()
    let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: el("rename"))
    XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 5), .completed, "Save does not close Rename space")
    note("saved")
  }
}
