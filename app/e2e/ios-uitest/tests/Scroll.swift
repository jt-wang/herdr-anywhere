import XCTest
// Run by e2e/ios-tui-scroll.sh with e2e/tui-panels.py in the space SPACE. Real drags on the terminal: a drag in the
// left panel, one in the right panel, and a flick that ends over the program's prompt box. Before each it appends
// "mark NAME HEIGHT" to EVENTS (the file the program writes its wheel reports to), so the script can tell them apart.
final class Scroll: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let events = env["EVENTS"] ?? "/tmp/events", space = env["SPACE"] ?? "spscroll"
    func mark(_ s: String) {
      guard let h = FileHandle(forWritingAtPath: events) else { return }
      h.seekToEndOfFile(); h.write((s + "\n").data(using: .utf8)!); h.closeFile()
    }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let tile = app.descendants(matching: .any)["space-" + space].firstMatch
    XCTAssertTrue(tile.waitForExistence(timeout: 40), "home does not list the space")
    tile.tap()
    let term = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-'")).firstMatch
    XCTAssertTrue(term.waitForExistence(timeout: 20), "no terminal")
    sleep(4)
    let h = term.frame.height
    let at = { (x: CGFloat, y: CGFloat) in term.coordinate(withNormalizedOffset: CGVector(dx: x, dy: y)) }
    // A finger held before it lifts: no glide.
    mark("mark drag \(h)")
    at(0.25, 0.10).press(forDuration: 0.1, thenDragTo: at(0.25, 0.45), withVelocity: 300, thenHoldForDuration: 0.6)
    sleep(2)
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
