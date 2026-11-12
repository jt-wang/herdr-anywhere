import XCTest
// Run by e2e/ios-picture.sh, its terminal TERMINAL (t1) showing three picture paths in the space SPACE, the picture at
// FULL; the script writes the
// terminal's size as the phone set it ("cols rows") to $SIZE once the space is open. Real taps: the
// path opens the sheet with the picture under its full path, holding the picture puts it on the pasteboard, and a text
// file named .png and a path only mentioned open nothing. Writes what it saw to $OUT and screenshots to $SHOTS-<step>.png.
final class Picture: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/picture.txt", shots = env["SHOTS"] ?? "/tmp/picture"
    let space = env["SPACE"] ?? "sppicture"
    let sizeFile = env["SIZE"] ?? "/tmp/picture-size"
    let terminal = env["TERMINAL"] ?? "t1", full = env["FULL"] ?? "/home/src/acme-app/art/sunset.png"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    // When the page got the last taps and what it made of them, from the label e2e/touch-times.ts keeps in the page (the
    // test app is built with it). Read with no sheet over the page, which may hide it.
    func touches(_ step: String) {
      let t = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'hm-touch'")).firstMatch
      note("\(step), the page's touch times: \(t.exists ? String(t.label.dropFirst(9)) : "none")")
    }
    // A second run (dark) starts where the first left the app: on the space.
    if !el("terminal-" + terminal).waitForExistence(timeout: 3) {
      XCTAssertTrue(el("space-" + space).waitForExistence(timeout: 40), "home does not list the space")
      el("space-" + space).tap()
    }
    let term = el("terminal-" + terminal)
    XCTAssertTrue(term.waitForExistence(timeout: 20), "no terminal")
    var size: [CGFloat] = []
    for _ in 0..<30 {
      size = ((try? String(contentsOfFile: sizeFile, encoding: .utf8)) ?? "").split(separator: " ").compactMap { Double($0.trimmingCharacters(in: .whitespacesAndNewlines)) }.map { CGFloat($0) }
      if size.count == 2 { break }
      sleep(1)
    }
    XCTAssertEqual(size.count, 2, "the script never wrote the terminal's size")
    let cols = size[0], rows = size[1]
    note("terminal \(Int(cols)) × \(Int(rows))")
    // The page has no side padding and 12 pt above the first row and below the last.
    func cell(_ r: CGFloat, _ c: CGFloat) -> XCUICoordinate {
      let f = term.frame
      return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: f.minX + (c + 0.5) * f.width / cols, dy: f.minY + 12 + (r + 0.5) * (f.height - 24) / rows))
    }
    shot("terminal")
    cell(0, 12).tap()
    let opened = el("picture-image").waitForExistence(timeout: 20)
    note("tap the path, picture shown: \(opened)")
    if !opened { sleep(1); touches("tap the path") }
    XCTAssertTrue(opened, "the tap opened no picture")
    sleep(1); shot("sheet")
    let shown = saying(full).exists
    note("full path shown: \(shown)")
    XCTAssertTrue(shown, "no full path over the picture")
    // Writing and has… don't ask to paste; reading would.
    UIPasteboard.general.string = "before hold"
    note("before the hold, pasteboard has an image: \(UIPasteboard.general.hasImages)")
    el("picture").press(forDuration: 1.0)
    let told = saying("Picture copied").waitForExistence(timeout: 5)
    let image = UIPasteboard.general.hasImages
    note("held, says copied: \(told), pasteboard has an image: \(image)")
    shot("held")
    XCTAssertTrue(told && image, "holding the picture put no image on the pasteboard")
    el("picture-close").tap()
    sleep(1)
    touches("sheet closed")
    // Not pictures there: a text file named .png, a path only mentioned. The Companion says so and nothing opens.
    for (r, c, name) in [(CGFloat(3), CGFloat(9), "notes.png"), (CGFloat(6), CGFloat(12), "art/sunset-wide.png")] {
      cell(r, c).tap()
      let opened = el("picture-sheet").waitForExistence(timeout: 4)
      note("\(name) opens nothing: \(!opened)")
      XCTAssertFalse(opened, "\(name) opened the sheet")
      if opened { el("picture-close").tap(); sleep(1) }
    }
    shot("not-pictures")
    sleep(1); touches("end")
  }
}
