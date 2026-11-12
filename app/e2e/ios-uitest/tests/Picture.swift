import XCTest
// Run by e2e/ios-picture.sh, its terminal TERMINAL (t1) showing three picture paths in the space SPACE, the picture at
// FULL; the script writes the
// terminal's size as the phone set it ("cols rows") to $SIZE once the space is open. Real taps: the
// path opens the sheet with the picture under its full path, holding the picture puts it on the pasteboard, and a text
// file named .png and a path only mentioned open nothing. Writes what it saw to $OUT and screenshots to $SHOTS-<step>.png.
// Around the tap on the path it notes the terminal's frame, how long after the script switched the appearance
// (SWITCHED, seconds since 1970) it came, and the screen's elements in $OUT-<name>.txt. If the tap opens no picture: the
// frame and elements again, then a second tap only to see whether that one opens it; the test fails all the same. For
// the script's self-test, MISS_FIRST=1 makes the first tap land at the top of the screen, beside the path, and
// NO_TAP_FIRST=1 makes no first tap at all (only its notes), for the touch log to tell a tap never made from one made.
// SPACE_MISS_NOTED=1 (the script's loop): a first tap on the space that opened nothing is noted, and the test goes on
// to the path, which the loop is there to count.
final class Picture: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/picture.txt", shots = env["SHOTS"] ?? "/tmp/picture"
    let space = env["SPACE"] ?? "sppicture"
    let sizeFile = env["SIZE"] ?? "/tmp/picture-size"
    let terminal = env["TERMINAL"] ?? "t1", full = env["FULL"] ?? "/home/src/acme-app/art/sunset.png"
    var lines: [String] = []
    let start = Date(), clock = DateFormatter()
    clock.dateFormat = "HH:mm:ss.SSS"
    // The time of day too, to line up with the app's and the script's lines.
    func note(_ s: String) {
      let since = String(format: "+%.1f s", Date().timeIntervalSince(start))
      lines.append([clock.string(from: Date()), since, runnerLoad(), s].joined(separator: " "))
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    func elements(_ name: String) { try? app.debugDescription.write(toFile: out + "-" + name + ".txt", atomically: true, encoding: .utf8) }
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    // When the page got the last taps and what it made of them, from the label e2e/touch-times.ts keeps in the page (the
    // test app is built with it). Read with no sheet over the page, which may hide it.
    func touches(_ step: String) {
      let t = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'hm-touch'")).firstMatch
      note("\(step), the page's touch times: \(t.exists ? String(t.label.dropFirst(9)) : "none")")
    }
    // A second run (dark) starts where the first left the app: on the space. The first opens it from home, the tap
    // noted (OpenSpace.swift).
    let opener = SpaceOpener(app: app, out: out, note: note)
    var again: Bool? = nil
    if !el("terminal-" + terminal).waitForExistence(timeout: 3) { again = opener.open(space) }
    let term = el("terminal-" + terminal)
    let showed = term.waitForExistence(timeout: 20)
    note("terminal \(showed ? "showed" : "not there")")
    XCTAssertTrue(showed, "no terminal")
    if let again {
      if env["SPACE_MISS_NOTED"] == "1" {
        if again { note("the first tap on the space opened nothing; the second \(showed ? "did" : "did not either") (noted, not failed: SPACE_MISS_NOTED)") }
      } else { opener.finish(again: again, shown: showed) }
    }
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
    let switched = Double(env["SWITCHED"] ?? "").map { String(format: "%.1f s after the appearance switch", Date().timeIntervalSince1970 - $0) } ?? "no appearance switch time given"
    let frame = term.frame
    elements("before-tap")
    // "tapping" just before each tap: e2e/ios-touch-log.sh counts the touches UIKit sent the app from there to the next note.
    if env["MISS_FIRST"] == "1" {
      note("tapping beside the path (MISS_FIRST)")
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.01)).tap()
      note("tapped beside the path (MISS_FIRST), \(switched); terminal at \(NSCoder.string(for: frame))")
    } else if env["NO_TAP_FIRST"] == "1" {
      note("tapping the path (NO_TAP_FIRST: no tap is made)")
      note("did not tap the path (NO_TAP_FIRST), \(switched); terminal at \(NSCoder.string(for: frame))")
    } else {
      note("tapping the path")
      cell(0, 12).tap()
      note("tapped the path, \(switched); terminal at \(NSCoder.string(for: frame))")
    }
    let first = el("picture-image").waitForExistence(timeout: 20)
    note("tap the path, picture shown: \(first)")
    var opened = first
    if !first {
      let now = term.frame
      note("no picture: terminal now at \(NSCoder.string(for: now))\(now == frame ? ", where it was" : ", moved since the tap")")
      elements("no-picture")
      sleep(1); touches("tap the path")
      // Only to see whether the page takes a tap now; the test fails all the same.
      note("tapping the path again")
      cell(0, 12).tap()
      note("tapped the path again")
      opened = el("picture-image").waitForExistence(timeout: 20)
      note("tapped a second time, picture shown: \(opened)")
      if !opened { elements("after-second-tap"); sleep(1); touches("second tap") }
    }
    XCTAssertTrue(opened, "the tap opened no picture")
    XCTAssertTrue(first, "the first tap on the path opened no picture; the second \(opened ? "did" : "did not either")")
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
