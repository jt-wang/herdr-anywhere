import XCTest
// Run by e2e/ios-edge-scroll.sh with an agent in each space of SPACES. Once a space is open it writes "ready <space>"
// to OUT/drags.txt and waits for OUT/sent-<space>: the script then asks the agent for a long answer, so the answer
// reaches the phone as it is written, history included. On each, slow
// vertical drags, held before they lift (no glide): from the screen's left edge (inside the strip left to iOS's swipe
// back, ui/terminal-page.ts edgeBack) and from the terminal's middle, back into history and then forward again. Before
// and after each drag it saves a screenshot to OUT (a folder), and it writes a line per drag to OUT/drags.txt: the
// space, where, which way, the terminal's box in the shots' pixels and whether the page went back. It asserts
// nothing; the script measures the shots.
final class EdgeScroll: XCTestCase {
  let env = ProcessInfo.processInfo.environment
  lazy var app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")

  func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
  func shown(_ id: String) -> Bool {
    app.descendants(matching: .any).matching(identifier: id).allElementsBoundByIndex.contains { $0.exists && $0.isHittable }
  }

  func test() {
    let out = env["OUT"] ?? "/tmp/edge-scroll"
    let spaces = (env["SPACES"] ?? "").split(separator: ",").map(String.init)
    try? FileManager.default.createDirectory(atPath: out, withIntermediateDirectories: true)
    func write(_ s: String) {
      let p = out + "/drags.txt"
      if !FileManager.default.fileExists(atPath: p) { FileManager.default.createFile(atPath: p, contents: nil) }
      guard let h = FileHandle(forWritingAtPath: p) else { return }
      h.seekToEndOfFile(); h.write((s + "\n").data(using: .utf8)!); h.closeFile()
    }
    func shot(_ name: String) {
      try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: out + "/" + name + ".png"))
    }
    app.activate()
    // Points to the shots' pixels: a screenshot's width in pixels over the app's in points.
    let first = XCUIScreen.main.screenshot().image
    let scale = first.size.width * first.scale / app.frame.width
    for sp in spaces {
      let tile = el("space-" + sp)
      XCTAssertTrue(tile.waitForExistence(timeout: 40), "home does not list \(sp)")
      tile.tap()
      let term = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-'")).firstMatch
      XCTAssertTrue(term.waitForExistence(timeout: 20), "no terminal in \(sp)")
      sleep(3)
      write("ready \(sp)")
      let sent = out + "/sent-" + sp
      for _ in 0..<90 where !FileManager.default.fileExists(atPath: sent) { sleep(1) }
      XCTAssertTrue(FileManager.default.fileExists(atPath: sent), "the script never said the answer was in \(sp)")
      sleep(3)
      let f = term.frame
      let box = "\(Int(f.minX * scale)) \(Int(f.minY * scale)) \(Int(f.width * scale)) \(Int(f.height * scale))"
      // One drag back and one forward first, measured but not compared: right after a space opens, the first drag
      // in some programs did not scroll, in the build before this change too (Claude Code and OpenCode).
      // Then from the bottom each time, back and forward: from 10 pt and 20 pt off the screen's left edge (both in the
      // 24 pt strip), and from the middle.
      let plan: [(String, String, CGFloat, CGFloat, CGFloat)] = [
        ("warm", "back", f.width / 2, 0.25, 0.55), ("warm", "forward", f.width / 2, 0.55, 0.25),
        ("edge", "back", 10 - f.minX, 0.25, 0.55), ("edge", "forward", 10 - f.minX, 0.55, 0.25),
        ("edge20", "back", 20 - f.minX, 0.25, 0.55), ("edge20", "forward", 20 - f.minX, 0.55, 0.25),
        ("middle", "back", f.width / 2, 0.25, 0.55), ("middle", "forward", f.width / 2, 0.55, 0.25),
      ]
      for (at, dir, x, from, to) in plan {
        let name = "\(sp)-\(at)-\(dir)"
        shot(name + "-0")
        let start = term.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: x, dy: f.height * from))
        let end = term.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: x, dy: f.height * to))
        start.press(forDuration: 0.1, thenDragTo: end, withVelocity: 300, thenHoldForDuration: 0.6)
        sleep(2)
        shot(name + "-1")
        let back = !shown("pane-header")
        write("drag \(sp) \(at) \(dir) box \(box) scale \(Int(scale)) travel \(Int(f.height * abs(to - from) * scale)) back=\(back ? 1 : 0)")
        if back { _ = tile.waitForExistence(timeout: 5); tile.tap(); _ = term.waitForExistence(timeout: 20); sleep(3) }
      }
      if shown("back") { app.descendants(matching: .any).matching(identifier: "back").allElementsBoundByIndex.last(where: { $0.isHittable })?.tap(); sleep(1) }
    }
  }
}
