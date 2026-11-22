import XCTest
// Run by e2e/ios-pin-order.sh: real touches on Pinned. AGENTS: the pinned agents' addresses in the order their places
// give. One line per step goes to OUT: "<step> <addresses top to bottom> spaces=<top of the Spaces header>", and
// "mark <name>" when the script is to take a screenshot. SHOTS=1: at rest and after the drop the test waits for it
// (OUT.shot-<name>, written once the screenshot is taken), and the drag is held long enough for the one taken mid-drag.
// BEFORE=1: only the first line, and the screenshot at rest.
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
    // A screenshot from the script takes seconds on a CI simulator: taken while the test went on, it showed the next step.
    let shots = env["SHOTS"] == "1"
    func shot(_ name: String) {
      write("mark \(name)")
      guard shots else { return }
      let done = Date().addingTimeInterval(60)
      while !FileManager.default.fileExists(atPath: out + ".shot-" + name) && Date() < done { usleep(200_000) }
      XCTAssertTrue(FileManager.default.fileExists(atPath: out + ".shot-" + name), "no screenshot \(name) in 60 s")
    }
    shot("rest")
    if env["BEFORE"] == "1" { return }
    func middle(_ e: XCUIElement) -> XCUICoordinate { e.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)) }
    let top = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
    func sp() -> Int { Int(el("space-new").frame.minY) }
    // Held still, then dragged; held at the end for the script's screenshot. The row lifts on the app's 400 ms timer
    // and the list stops scrolling then (home.tsx: `scrollable`, a change the app sends to the list). Until that change
    // is in, a finger that moves is the list's too: on a CI host at load 116-130 a row lifted, and the list's own pan
    // took the touch from it with the finger's first move, 0.8 s down (the app's log: pin-lift, then pin-terminate
    // with lifted=1 moves=0; the list stood still, no row moved). Held 1.5 s, the change has 1.1 s to get there.
    // A drag that still comes to nothing is made once more, saying why, when the app never dropped the row (its log,
    // SERVE_LOG: each touch on a Pinned row is a /hm-e2e/pin-… line): the hold came out as a scroll (the list moved,
    // put back first), the app got no touch, or the touch was taken from the lifted row. A drop in the wrong place
    // is not made again. HM_PIN_SCROLL_FIRST=1 (the suite's self-test) makes the first hold a quick swipe, which must
    // come out as a scroll and be made again.
    let serveLog = env["SERVE_LOG"] ?? ""
    func pinLog() -> String {
      ((try? String(contentsOfFile: serveLog, encoding: .utf8)) ?? "").components(separatedBy: "\n").filter { $0.contains("/hm-e2e/pin-") }.joined(separator: "\n")
    }
    var first = env["SCROLL_FIRST"] == "1"
    var upFrames: (source: CGRect, target: CGRect)?
    func drag(_ name: String, _ i: Int, by: () -> CGFloat, hold: TimeInterval = 0.3) {
      for attempt in 1...2 {
        let o = order(), s = sp(), before = pinLog().count, c = middle(row(i))
        let press: TimeInterval = first ? 0.05 : 1.5
        first = false
        let destination = c.withOffset(CGVector(dx: 0, dy: by()))
        let perform = { c.press(forDuration: press, thenDragTo: destination, withVelocity: 300, thenHoldForDuration: hold) }
        if name == "up", attempt == 1, let frames = upFrames {
          pinGestureTrace(name: name, attempt: attempt, source: frames.source, target: frames.target,
                          start: c.screenPoint, end: destination.screenPoint, press: press, hold: hold,
                          note: write, drag: perform)
        } else { perform() }
        write("let go \(name)")
        sleep(2)
        if attempt == 2 || order() != o { return }
        if sp() != s {
          write("retry \(name): the hold came out as a scroll (no row moved, the list went from \(s) to \(sp()))")
          top.press(forDuration: 0.05, thenDragTo: top.withOffset(CGVector(dx: 0, dy: 500)), withVelocity: 1500, thenHoldForDuration: 0)
          sleep(2)
          continue
        }
        // A busy app's lines can come late: 3 s more for a drop to show, in the list or in its log.
        sleep(3)
        let said = String(pinLog().dropFirst(before))
        if order() != o || serveLog.isEmpty || said.contains("/hm-e2e/pin-drop/") { return }
        let why = !said.contains("/hm-e2e/pin-grant/") ? "the app got no touch"
          : said.contains("/hm-e2e/pin-lift/") && said.contains("/hm-e2e/pin-terminate/") ? "the touch was taken from the lifted row before it moved"
          : "the app did not lift the row"
        write("retry \(name): \(why) (no row moved, the list stood still)")
      }
    }
    // 4th up to 2nd.
    write("mark drag")
    drag("up", 3, by: {
      let target = row(1).frame, source = row(3).frame
      upFrames = (source, target)
      return target.midY - source.midY - 10
    }, hold: shots ? 10 : 2.5)
    step("up")
    shot("dropped")
    // Then the first (A) down to 4th: its middle onto the middle of the row now 4th.
    drag("down", 0, by: { row(2).frame.midY - row(0).frame.midY })
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
    // The app's menu panel (ui/menu-sheet.tsx), or a system menu in a build before it.
    write("menu shown=\(el("menu-pin").exists || app.buttons["Unpin"].exists) opened=\(el("pane-header").exists)")
    if el("menu-cancel").exists { el("menu-cancel").tap() } else if app.buttons["Cancel"].exists { app.buttons["Cancel"].tap() }
    sleep(1)
    step("after-menu")
    // A quick swipe up over a row scrolls the list; then back.
    let s0 = Int(el("space-new").frame.minY)
    let m = middle(row(1))
    m.press(forDuration: 0.05, thenDragTo: m.withOffset(CGVector(dx: 0, dy: -260)), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(2)
    write("swipe from=\(s0) to=\(Int(el("space-new").frame.minY))")
    top.press(forDuration: 0.05, thenDragTo: top.withOffset(CGVector(dx: 0, dy: 500)), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(2)
    step("after-swipe")
    // The list scrolled so the top of Pinned is out of sight: E held and taken to the top edge scrolls the list itself.
    // Scrolled so the first two rows (the order is D B C A E here) are above the list's top, Needs you maybe above
    // Pinned: E lands first only if the list scrolls back while it is held. From the Spaces header (nothing there to
    // press), rested before letting go so it does not fling.
    let listTop = el("settings").frame.maxY
    let by = row(1).frame.midY - listTop + row(1).frame.height
    // Setup, not the step under test: a drag the CI simulator's busy app let pass without a scroll is written down
    // ("retry …", shown by the script) and made once more.
    for attempt in 1...2 {
      let header = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 100, dy: el("space-new").frame.midY))
      header.press(forDuration: 0.05, thenDragTo: header.withOffset(CGVector(dx: 0, dy: -by)), withVelocity: 300, thenHoldForDuration: 0.5)
      sleep(2)
      if attempt == 2 || row(1).frame.minY <= listTop { break }
      write("retry edge-setup: the drag from the Spaces header did not scroll (second row at \(Int(row(1).frame.minY)), list top \(Int(listTop)))")
    }
    let edge = listTop + 8
    write("edge-setup second=\(Int(row(1).frame.minY)) top=\(Int(listTop))")
    let e = middle(row(4))
    e.press(forDuration: 1.5, thenDragTo: e.withOffset(CGVector(dx: 0, dy: edge - row(4).frame.midY)), withVelocity: 300, thenHoldForDuration: 3)
    sleep(2)
    top.press(forDuration: 0.05, thenDragTo: top.withOffset(CGVector(dx: 0, dy: 500)), withVelocity: 1500, thenHoldForDuration: 0)
    sleep(2)
    step("edge")
  }
}
