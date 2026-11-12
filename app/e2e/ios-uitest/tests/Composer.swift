import XCTest
// Run by e2e/ios-composer.sh. Opens Try it (sample data, no computer), its second space's terminal, and the input box
// under it, with the keyboard down and the box empty, so the hint is what shows. Writes the box's frame in points
// ("x y width height") and the screen's scale to $OUT, and the screen to $SHOT.
final class Composer: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/composer.txt", shotPath = env["SHOT"] ?? "/tmp/composer.png"
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    func shot(_ path: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: path)) }
    // At the largest text sizes Try it sits below the screen: the page is moved up until it shows.
    let tryIt = el("try-it")
    XCTAssertTrue(tryIt.waitForExistence(timeout: 30), "no Try it")
    for _ in 0..<6 where !tryIt.isHittable { app.swipeUp(); sleep(1) }
    if !tryIt.isHittable { shot(shotPath); XCTFail("Try it is not on screen") ; return }
    tryIt.tap()
    let space = el("space-sp2")
    XCTAssertTrue(space.waitForExistence(timeout: 20), "Try it lists no space-sp2")
    for _ in 0..<6 where !space.isHittable { app.swipeUp(); sleep(1) }
    space.tap()
    let terminal = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-'")).firstMatch
    XCTAssertTrue(terminal.waitForExistence(timeout: 20), "the space shows no terminal")
    sleep(2)
    let box = el("compose-input")
    if !box.exists, el("to-box").exists { el("to-box").tap() }
    XCTAssertTrue(box.waitForExistence(timeout: 10), "no input box under the terminal")
    if app.keyboards.count > 0, el("hide-keyboard").exists { el("hide-keyboard").tap() }
    sleep(2)
    let f = box.frame
    let scale = XCUIScreen.main.screenshot().image.scale
    try? "\(f.minX) \(f.minY) \(f.width) \(f.height) \(scale)\n".write(toFile: out, atomically: true, encoding: .utf8)
    shot(shotPath)
  }
}
