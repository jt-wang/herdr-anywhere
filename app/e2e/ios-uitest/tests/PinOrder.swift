import XCTest
// Run by e2e/ios-pin-order.sh: real touches on Pinned. AGENTS: the pinned agents' addresses in the order their places
// give. One line per step goes to OUT: "<step> <addresses top to bottom> spaces=<top of the Spaces header>", and
// "mark <name>" just before a gesture the script takes a screenshot during. BEFORE=1: only the first line.
final class PinOrder: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/pinorder-ios.txt"
    let ids = (env["AGENTS"] ?? "").split(separator: " ").map(String.init)
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
    func row(_ i: Int) -> XCUIElement { el("pinned-agent-" + ids[i]) }
    func write(_ s: String) {
      if let h = FileHandle(forWritingAtPath: out) { h.seekToEndOfFile(); h.write((s + "\n").data(using: .utf8)!); h.closeFile() }
      else { try? (s + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    }
    func order() -> String {
      ids.filter { el("pinned-agent-" + $0).exists }.sorted { el("pinned-agent-" + $0).frame.minY < el("pinned-agent-" + $1).frame.minY }.joined(separator: " ")
    }
    func step(_ name: String) { write("\(name) \(order()) spaces=\(Int(el("space-new").frame.minY))") }
    XCTAssertTrue(row(ids.count - 1).waitForExistence(timeout: 60), "Pinned does not show every agent")
    sleep(3)
    step("start")
    if env["BEFORE"] == "1" { return }
    func middle(_ e: XCUIElement) -> XCUICoordinate { e.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)) }
    // Held still 0.8 s, then dragged; held at the end for the script's screenshot.
    func drag(_ e: XCUIElement, by dy: CGFloat, hold: TimeInterval = 0.3) {
      let c = middle(e)
      c.press(forDuration: 0.8, thenDragTo: c.withOffset(CGVector(dx: 0, dy: dy)), withVelocity: 300, thenHoldForDuration: hold)
      sleep(2)
    }
    // 4th up to 2nd.
    let d = row(3), b = row(1)
    write("mark drag")
    drag(d, by: b.frame.midY - d.frame.midY - 10, hold: 2.5)
    step("up")
    // Then the first (A) down to 4th: its middle onto the middle of the row now 4th.
    let a = row(0), c = row(2)
    drag(a, by: c.frame.midY - a.frame.midY)
    step("down")
    // A tap opens the agent.
    row(2).tap()
    write("tap opened=\(el("pane-header").waitForExistence(timeout: 10))")
    el("back").tap()
    XCTAssertTrue(row(2).waitForExistence(timeout: 10))
    sleep(1)
    // Held still and let go: the menu, nothing else.
    row(2).press(forDuration: 1.2)
    sleep(2)
    write("menu shown=\(app.buttons["Unpin"].exists) opened=\(el("pane-header").exists)")
    if app.buttons["Cancel"].exists { app.buttons["Cancel"].tap() }
    sleep(1)
    step("after-menu")
    // A quick swipe up over a row scrolls the list; then back.
    let s0 = Int(el("space-new").frame.minY)
    let m = middle(row(1))
    m.press(forDuration: 0.05, thenDragTo: m.withOffset(CGVector(dx: 0, dy: -260)), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(2)
    write("swipe from=\(s0) to=\(Int(el("space-new").frame.minY))")
    let top = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
    top.press(forDuration: 0.05, thenDragTo: top.withOffset(CGVector(dx: 0, dy: 500)), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(2)
    step("after-swipe")
    // The list scrolled so the top of Pinned is out of sight: E held and taken to the top edge scrolls the list itself.
    // Scrolled so the first two rows (the order is D B C A E here) are above the list's top, Needs you maybe above
    // Pinned: E lands first only if the list scrolls back while it is held. From the Spaces header (nothing there to
    // press), rested before letting go so it does not fling.
    let listTop = el("settings").frame.maxY
    let by = row(1).frame.midY - listTop + row(1).frame.height
    let header = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 100, dy: el("space-new").frame.midY))
    header.press(forDuration: 0.05, thenDragTo: header.withOffset(CGVector(dx: 0, dy: -by)), withVelocity: 300, thenHoldForDuration: 0.5)
    sleep(2)
    let edge = listTop + 8
    write("edge-setup second=\(Int(row(1).frame.minY)) top=\(Int(listTop))")
    let e = middle(row(4))
    e.press(forDuration: 0.8, thenDragTo: e.withOffset(CGVector(dx: 0, dy: edge - row(4).frame.midY)), withVelocity: 300, thenHoldForDuration: 3)
    sleep(2)
    top.press(forDuration: 0.05, thenDragTo: top.withOffset(CGVector(dx: 0, dy: 500)), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(2)
    step("edge")
  }
}
