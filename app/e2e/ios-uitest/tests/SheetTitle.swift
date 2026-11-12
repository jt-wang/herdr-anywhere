import XCTest
// Run by e2e/ios-sheet-title.sh: the new-tab sheet opened as e2e/android-sheet-title.sh opens it (the space on box four
// times: light, again, dark, light; then the one on dock; then after a terminal was started from it, in light and in
// dark). Each opening leaves $DIR/<name>.png, the screen, and a line "<name> left top right bottom" in $DIR/frames.txt,
// the title's view in the picture's pixels; the script reads the title off the picture. A theme is asked for by writing
// it to $DIR/want; the script sets it and writes it to $DIR/now (a test cannot set the simulator's appearance).
final class SheetTitle: XCTestCase {
  func test() {
    let dir = ProcessInfo.processInfo.environment["DIR"] ?? "/tmp/sheettitle"
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
    func first(_ prefix: String) -> XCUIElement { app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH %@", prefix)).firstMatch }
    var frames = ""
    func theme(_ t: String) {
      try? t.write(toFile: "\(dir)/want", atomically: true, encoding: .utf8)
      for _ in 0..<60 { if (try? String(contentsOfFile: "\(dir)/now", encoding: .utf8)) == t { break }; sleep(1) }
      XCTAssertEqual(try? String(contentsOfFile: "\(dir)/now", encoding: .utf8), t, "the script did not set \(t)"); sleep(3)
    }
    func open(_ name: String, _ space: String = "box/spweb") {
      let add = el("add-agent-" + space); XCTAssertTrue(add.waitForExistence(timeout: 40), "\(name): no add-agent-\(space)"); add.tap(); sleep(3)
      let title = app.staticTexts.matching(NSPredicate(format: "label == %@", "Add an agent")).firstMatch
      XCTAssertTrue(title.waitForExistence(timeout: 10), "\(name): the sheet has no title Add an agent")
      let shot = XCUIScreen.main.screenshot()
      try? shot.pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
      // Points to the picture's pixels: its width in pixels over the app's in points.
      let f = title.frame, k = CGFloat(shot.image.cgImage?.width ?? 0) / app.frame.width
      XCTAssertGreaterThan(k, 0, "\(name): the screenshot has no width")
      frames += "\(name) \(Int(f.minX * k)) \(Int(f.minY * k)) \(Int(f.maxX * k)) \(Int(f.maxY * k))\n"
      try? frames.write(toFile: "\(dir)/frames.txt", atomically: true, encoding: .utf8)
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.08)).tap(); sleep(2)   // above the sheet: closes it
    }
    open("1-first"); open("2-second")
    theme("dark"); open("3-dark")
    theme("light"); open("4-light-again")
    open("5-other-machine", "dock/spdocs")
    let add = el("add-agent-box/spweb"); XCTAssertTrue(add.waitForExistence(timeout: 20), "no add-agent-box/spweb"); add.tap()
    let terminal = el("new-terminal"); XCTAssertTrue(terminal.waitForExistence(timeout: 10), "the sheet has no Terminal"); sleep(1); terminal.tap()
    XCTAssertTrue(first("terminal-box/").waitForExistence(timeout: 30), "the terminal page did not open"); sleep(3)
    let back = el("back"); XCTAssertTrue(back.waitForExistence(timeout: 10), "no back button"); back.tap(); sleep(2)
    open("6-after-start")
    theme("dark"); open("7-after-start-dark")
    theme("light")
  }
}
