import XCTest
// Run by e2e/ios-edge-back.sh on the app's Try it data. Swipes from the screen's left edge on each page that has one to go
// back to, at three heights (the page's header, its middle, low on it), two speeds and two starting distances from the
// edge, N times each, and writes a line per swipe to OUT: the page, where and how fast, whether the page went back and
// whether the terminal changed tab (the terminal's own sideways swipe, ui/terminal-page.ts touchend). It asserts
// nothing; the script counts.
final class EdgeBack: XCTestCase {
  let env = ProcessInfo.processInfo.environment
  lazy var app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")

  func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
  func shown(_ id: String) -> Bool {
    app.descendants(matching: .any).matching(identifier: id).allElementsBoundByIndex.contains { $0.exists && $0.isHittable }
  }
  func write(_ s: String) {
    let out = env["OUT"] ?? "/tmp/edge-back.txt"
    if !FileManager.default.fileExists(atPath: out) { FileManager.default.createFile(atPath: out, contents: nil) }
    guard let h = FileHandle(forWritingAtPath: out) else { return }
    h.seekToEndOfFile(); h.write((s + "\n").data(using: .utf8)!); h.closeFile()
  }
  /** The back button of the page on top. */
  func tapBack() {
    let all = app.descendants(matching: .any).matching(identifier: "back").allElementsBoundByIndex.filter { $0.isHittable }
    all.last?.tap()
  }
  func paneName() -> String { let n = el("pane-name"); return n.exists ? n.label : "" }
  var tabName: [String: String] = [:]
  /** Space sp1 on tab `tab`: tb2 (Codex, working), whose terminal's own swipe goes to tb1 and so shows; tb1 (Claude Code,
   *  asking), whose choices sit in a row that scrolls sideways (ui/kit.tsx ChoiceRow). */
  func onTab(_ tab: String) {
    if let n = tabName[tab], n == paneName() { return }
    el("switcher").tap()
    let row = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'switch-tab-' AND identifier ENDSWITH %@", tab)).firstMatch
    if row.waitForExistence(timeout: 5) { row.tap() }
    sleep(1)
    tabName[tab] = paneName()
  }
  func openSpace() {
    if !shown("pane-header") { el("space-sp1").tap(); _ = el("pane-header").waitForExistence(timeout: 10); sleep(2) }
  }

  /** The elements under where a swipe starts, smallest first: what the finger lands on. One snapshot of the tree, read
   *  here; querying every element through the app failed the whole test. */
  func logUnder(page: String, hn: String, y: CGFloat, x: CGFloat) {
    let p = CGPoint(x: app.frame.minX + x, y: app.frame.minY + app.frame.height * y)
    guard let root = try? app.snapshot() else { write("under page=\(page) y=\(hn) x=\(Int(x)) (no snapshot)"); return }
    var hits: [XCUIElementSnapshot] = [], pending = [root]
    while let e = pending.popLast() {
      if e.frame.contains(p) { hits.append(e) }
      pending.append(contentsOf: e.children)
    }
    let shown = hits.sorted { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }.prefix(4)
      .map { "\($0.elementType.rawValue):\($0.identifier.isEmpty ? "-" : $0.identifier)@\(Int($0.frame.minX)),\(Int($0.frame.minY)),\(Int($0.frame.width))x\(Int($0.frame.height))" }
    write("under page=\(page) y=\(hn) x=\(Int(x)) \(shown.joined(separator: " "))")
  }

  /** A swipe that did not go back, as it looked: the screen and the element tree a second after the finger lifted, next
   *  to OUT, then whether it went back after all within 3 s more (late=1: the pop was only slow to finish, the counted
   *  miss is the test's 1 s wait; late=0: it never began or was undone). The count does not change: late or not, the
   *  script counts back=0. */
  func miss(_ tag: String, on: () -> Bool, under: () -> Bool) {
    let base = ((env["OUT"] ?? "/tmp/edge-back.txt") as NSString).deletingPathExtension + "-miss-" + tag
    try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: base + ".png"))
    try? app.debugDescription.write(toFile: base + ".tree.txt", atomically: true, encoding: .utf8)
    var late = false
    for _ in 0..<6 where !late { usleep(500_000); late = !on() && under() }
    write("miss \(tag) late=\(late ? 1 : 0) shot=\((base as NSString).lastPathComponent).png")
  }

  func swipe(y: CGFloat, x: CGFloat, v: CGFloat) {
    let w = app.frame.width, h = app.frame.height
    let start = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: x, dy: h * y))
    // Slow: to 90% of the width and held there, so a pop that began always finishes: a slow swipe that doesn't go
    // back never began. Fast: a flick to half the width, which UIKit finishes on its speed.
    let slow = v < 1000
    let end = start.withOffset(CGVector(dx: w * (slow ? 0.9 : 0.5) - x, dy: 0))
    start.press(forDuration: 0.05, thenDragTo: end, withVelocity: XCUIGestureVelocity(v), thenHoldForDuration: slow ? 0.3 : 0)
    sleep(1)
  }

  func test() {
    let n = Int(env["N"] ?? "8") ?? 8
    let pages = (env["PAGES"] ?? "space,space-asking,settings,space-settings").split(separator: ",").map(String.init)
    // HEIGHTS, SPEEDS and STARTS narrow the grid, e.g. HEIGHTS=middle,low STARTS=2.
    func only<T>(_ all: [(String, T)], _ key: String) -> [(String, T)] {
      guard let v = env[key], !v.isEmpty else { return all }
      let want = v.split(separator: ",").map(String.init)
      return all.filter { want.contains($0.0) }
    }
    let heights = only([("header", CGFloat(0.09)), ("middle", 0.45), ("low", 0.80)], "HEIGHTS")
    let speeds = only([("slow", CGFloat(400)), ("fast", 2000)], "SPEEDS")
    let starts = only([("2", CGFloat(2)), ("12", 12)], "STARTS").map { $0.1 }
    app.launch()
    let tryIt = el("try-it")
    if tryIt.waitForExistence(timeout: 30) { tryIt.tap() }
    XCTAssertTrue(el("space-sp1").waitForExistence(timeout: 30), "home does not list sp1")
    write("os \(UIDevice.current.systemVersion) screen \(Int(app.frame.width))x\(Int(app.frame.height))")
    for page in pages {
      // Opens the page; `on` says the page is up, `under` that the one below it is.
      let open: () -> Void, on: () -> Bool, under: () -> Bool
      switch page {
      case "space", "space-asking":
        open = { self.openSpace(); self.onTab(page == "space" ? "tb2" : "tb1") }
        on = { self.shown("pane-header") }
        under = { self.shown("space-sp1") }
      case "settings":
        open = { if !self.shown("progress-first") { self.el("settings").tap(); _ = self.el("progress-first").waitForExistence(timeout: 10); sleep(1) } }
        on = { self.shown("progress-first") }
        under = { self.shown("space-sp1") }
      default: // settings over a space, from the space's menu
        open = {
          if self.shown("progress-first") { return }
          self.openSpace()
          self.el("space-more").tap()
          let s = self.app.buttons.matching(identifier: "Settings")
          _ = s.firstMatch.waitForExistence(timeout: 5)
          s.allElementsBoundByIndex.last(where: { $0.isHittable })?.tap()
          _ = self.el("progress-first").waitForExistence(timeout: 10); sleep(1)
        }
        on = { self.shown("progress-first") }
        under = { self.shown("pane-header") }
      }
      for (hn, y) in heights { for (sn, v) in speeds { for x in starts { for i in 1...n {
        open()
        guard on() else { write("page=\(page) y=\(hn) x=\(Int(x)) v=\(sn) i=\(i) back=skip (page did not open)"); continue }
        if i == 1 && sn == "slow" { logUnder(page: page, hn: hn, y: y, x: x) }
        let before = page.hasPrefix("space") ? paneName() : ""
        // When the finger goes down, in ms since 1970 (the simulator shares the computer's clock): with RECORD's start
        // time, where this swipe is in the recording.
        write("touch page=\(page) y=\(hn) x=\(Int(x)) v=\(sn) i=\(i) at=\(Int(Date().timeIntervalSince1970 * 1000))")
        swipe(y: y, x: x, v: v)
        let back = !on() && under()
        let after = page.hasPrefix("space") && !back ? paneName() : before
        write("page=\(page) y=\(hn) x=\(Int(x)) v=\(sn) i=\(i) back=\(back ? 1 : 0) tab=\(after == before ? "same" : "changed")")
        if !back { miss("\(page)-\(hn)-\(Int(x))-\(sn)-\(i)", on: on, under: under) }
        if !back && on() { tapBack(); sleep(1) }
        if page == "space-settings", back { tapBack(); sleep(1) } // to home, so the next round opens both again
      } } } }
      while shown("back") { tapBack(); sleep(1) }
    }
  }
}
