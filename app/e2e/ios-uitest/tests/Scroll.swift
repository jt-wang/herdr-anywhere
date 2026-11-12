import XCTest
// Run by e2e/ios-tui-scroll.sh with e2e/tui-panels.py in the space SPACE. Real drags on the terminal: a drag in the
// left panel, one in the right panel, and a flick that ends over the program's prompt box. Before each it appends
// "mark NAME HEIGHT" to EVENTS (the file the program writes its wheel reports to), so the script can tell them apart.
// It notes in OUT when it tapped the space and when the terminal showed, with the screen's elements in OUT-<name>.txt
// before the tap. If 3 s after it the app is still on home (the space listed, no space page), the tap was lost: the
// elements then and after a second tap, which fails the test all the same. On the space page it only notes that and
// waits for the terminal as before. For the script's self-test: MISS_FIRST=1 makes the first tap land beside the
// space; SLOW_TERM=1 looks for the terminal only from 5 s after the tap, as on a phone that shows it late.
final class Scroll: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let events = env["EVENTS"] ?? "/tmp/events", space = env["SPACE"] ?? "spscroll", out = env["OUT"] ?? "/tmp/scroll.txt"
    var lines: [String] = []
    let start = Date(), clock = DateFormatter()
    clock.dateFormat = "HH:mm:ss.SSS"
    // The time of day too, to line up with the app's own log lines (e2e/ios-touch-app.sh).
    func note(_ s: String) {
      lines.append(clock.string(from: Date()) + String(format: " +%.1f s ", Date().timeIntervalSince(start)) + s)
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    func mark(_ s: String) {
      guard let h = FileHandle(forWritingAtPath: events) else { return }
      h.seekToEndOfFile(); h.write((s + "\n").data(using: .utf8)!); h.closeFile()
    }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let tile = app.descendants(matching: .any)["space-" + space].firstMatch
    let listed = tile.exists
    XCTAssertTrue(tile.waitForExistence(timeout: 40), "home does not list the space")
    note(listed ? "home listed the space when the test began" : "home listed the space")
    let seen = Date()
    func elements(_ name: String) { try? app.debugDescription.write(toFile: out + "-" + name + ".txt", atomically: true, encoding: .utf8) }
    elements("before-tap")
    if env["MISS_FIRST"] == "1" {
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.01)).tap()
      note("tapped beside the space (MISS_FIRST)")
    } else {
      // Where and when, read before the tap: once the space opens the tile is gone, and reading it then failed the
      // test ("Failed to get matching snapshot").
      let at = NSCoder.string(for: tile.frame), after = Date().timeIntervalSince(seen)
      tile.tap()
      note(String(format: "tapped the space at %@, %.1f s after the test saw it listed", at, after))
    }
    // The terminal itself, not its "Connecting…" line (terminal-state), which the same prefix also finds. On a CI run the
    // first drag, 4 s after the terminal showed, sent the program nothing and the later ones worked, most likely as the page was not
    // ready yet. So the drags wait until it is, as TypeIn does.
    let term = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-' AND identifier != 'terminal-state'")).firstMatch
    var again = false
    let page = app.descendants(matching: .any)["space-more"].firstMatch
    let slow = env["SLOW_TERM"] == "1"
    if slow || !term.waitForExistence(timeout: 3) {
      if slow { sleep(3) }
      if tile.exists && !page.exists {
        elements("3s-after-tap")
        note("3 s after the tap the app is still on home; tapped a second time")
        again = true
        tile.tap()
      } else {
        note("3 s after the tap: the space page is open, no terminal yet\(slow ? " (SLOW_TERM)" : ""); waiting")
      }
    }
    if slow { sleep(2) }
    let shown = term.waitForExistence(timeout: 17)
    if again { elements("after-second-tap") }
    note("terminal \(shown ? "showed" : "not there")")
    XCTAssertTrue(shown, "no terminal")
    XCTAssertFalse(again, "the first tap on the space opened nothing; the second \(shown ? "did" : "did not either")")
    // The page loaded and attached: terminal-state goes once the page says ready (as TypeIn waits).
    let state = app.descendants(matching: .any)["terminal-state"].firstMatch
    let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: state)
    XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 30), .completed, "the terminal page did not load: still \(state.label) after 30 s")
    sleep(4)
    let h = term.frame.height
    let at = { (x: CGFloat, y: CGFloat) in term.coordinate(withNormalizedOffset: CGVector(dx: x, dy: y)) }
    // The screen just before and after the first drag, kept for e2e/ios-tui-scroll.sh to show when it fails.
    func shot(_ name: String) {
      let a = XCTAttachment(screenshot: XCUIScreen.main.screenshot()); a.name = name; a.lifetime = .keepAlways; add(a)
    }
    shot("before-drag")
    // A finger held before it lifts: no glide.
    mark("mark drag \(h)")
    at(0.25, 0.10).press(forDuration: 0.1, thenDragTo: at(0.25, 0.45), withVelocity: 300, thenHoldForDuration: 0.6)
    sleep(2)
    shot("after-drag")
    mark("mark right \(h)")
    at(0.75, 0.10).press(forDuration: 0.1, thenDragTo: at(0.75, 0.40), withVelocity: 300, thenHoldForDuration: 0.6)
    sleep(2)
    // Lifts at speed over the prompt box.
    mark("mark flick \(h)")
    at(0.25, 0.12).press(forDuration: 0.05, thenDragTo: at(0.25, 0.99), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(4)
    mark("mark end \(h)")
  }
}
