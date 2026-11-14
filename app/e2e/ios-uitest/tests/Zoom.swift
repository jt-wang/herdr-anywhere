import XCTest
// Run by e2e/ios-zoom.sh, the app (BUNDLE) opened on the picture sheet (e2e/picture-app.tsx). Real touches: a pinch,
// drags to the bottom right corner and once more past it, a double tap out, one in, one out again; then a hold copies
// the picture, Copy the path, and Close closes. Each step's screenshot goes to $SHOTS-<step>.png and the picture's box
// in the screenshots' pixels ("box l t r b") to $OUT, where the script compares them (e2e/zoom-look.py).
final class Zoom: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/zoom.txt", shots = env["SHOTS"] ?? "/tmp/zoom"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: env["BUNDLE"] ?? "io.jingtao.herdranywhere.zoomcheck")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    XCTAssertTrue(el("picture-image").waitForExistence(timeout: 40), "the sheet shows no picture")
    sleep(1)
    // The zooming scroll view stays put; a build from before zoom has only the picture.
    let zoom = el("picture-zoom").exists ? el("picture-zoom") : el("picture-image")
    let f = zoom.frame
    let scale = XCUIScreen.main.screenshot().image.size.width > 0 ? XCUIScreen.main.screenshot().image.scale : 1
    note(String(format: "box %d %d %d %d", Int(f.minX * scale), Int(f.minY * scale), Int(f.maxX * scale), Int(f.maxY * scale)))
    let at = { (x: CGFloat, y: CGFloat) in zoom.coordinate(withNormalizedOffset: CGVector(dx: x, dy: y)) }
    shot("fitted")
    zoom.pinch(withScale: 3, velocity: 3)
    sleep(1); shot("pinched")
    for _ in 0..<6 { at(0.8, 0.8).press(forDuration: 0.05, thenDragTo: at(0.2, 0.2), withVelocity: 600, thenHoldForDuration: 0.2) }
    sleep(1); shot("corner")
    at(0.8, 0.8).press(forDuration: 0.05, thenDragTo: at(0.2, 0.2), withVelocity: 600, thenHoldForDuration: 0.2)
    sleep(1); shot("corner-again")
    let open = el("picture-image").exists
    note("sheet open after the drags: \(open)")
    XCTAssertTrue(open, "the drags closed the sheet")
    at(0.5, 0.5).doubleTap()
    sleep(1); shot("out")
    at(0.25, 0.25).doubleTap()
    sleep(1); shot("doubletap")
    at(0.5, 0.5).doubleTap()
    sleep(1); shot("out2")
    UIPasteboard.general.string = "before hold"
    el("picture").press(forDuration: 1.0)
    // This package has no Herdr host to copy with: the hold says it couldn't. That it says anything shows the hold
    // still reaches the picture; ios-picture.sh checks the copy on the real build.
    let told = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'Picture copied' OR label CONTAINS %@", "Couldn't copy the picture")).firstMatch.waitForExistence(timeout: 5)
    shot("held")
    note("held, asks to copy: \(told)")
    sleep(3)
    el("picture-copy-path").tap()
    note("Copy says copied: \(saying("Copied").waitForExistence(timeout: 3))")
    sleep(2)
    el("picture-close").tap()
    sleep(1)
    note("closed: \(!el("picture-sheet").exists)")
  }
}
