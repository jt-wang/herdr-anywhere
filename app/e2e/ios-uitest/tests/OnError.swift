import XCTest
// Run by e2e/ios-onerror.sh after the app's JS threw an error nothing caught (ios-e2e-error.tsx): the app is still
// running, and a tap on the space SPACE still opens its terminal.
final class OnError: XCTestCase {
  func test() {
    let space = ProcessInfo.processInfo.environment["SPACE"] ?? "sperror"
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    // Read before activate(), which would start a crashed app again.
    XCTAssertNotEqual(app.state, .notRunning, "the app quit after a JS error nothing caught")
    app.activate()
    let tile = app.descendants(matching: .any)["space-" + space].firstMatch
    XCTAssertTrue(tile.waitForExistence(timeout: 30), "home does not list the space")
    // Taps right after home lists a space have been lost on CI (an open question of its own); this test is about
    // the error, so it taps once home has settled.
    sleep(5)
    tile.tap()
    let term = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-'")).firstMatch
    XCTAssertTrue(term.waitForExistence(timeout: 20), "a tap on the space opened no terminal")
  }
}
