import XCTest
// Run by e2e/ios-onerror.sh after the app's JS threw an error nothing caught (ios-e2e-error.tsx): the app is still
// running, and a tap on the space SPACE still opens its terminal. Notes in OUT how the tap went, so a failure tells a
// terminal that came late from a tap that was lost (a second tap opens it) from one that never opens.
final class OnError: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let space = env["SPACE"] ?? "sperror", out = env["OUT"] ?? "/tmp/onerror.txt"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    // Read before activate(), which would start a crashed app again.
    let running = app.state != .notRunning
    note("running before the tap: \(running)")
    XCTAssertTrue(running, "the app quit after a JS error nothing caught")
    app.activate()
    let tile = app.descendants(matching: .any)["space-" + space].firstMatch
    let listed = tile.waitForExistence(timeout: 30)
    note("home lists the space: \(listed)")
    XCTAssertTrue(listed, "home does not list the space")
    guard listed else { return }
    // Taps right after home lists a space have been lost on CI (an open question of its own); this test is about
    // the error, so it taps once home has settled.
    sleep(5)
    let term = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-'")).firstMatch
    let t0 = Date()
    tile.tap()
    if term.waitForExistence(timeout: 20) { note("tap: terminal after \(Int(Date().timeIntervalSince(t0) * 1000)) ms"); return }
    // No terminal in 20 s: one that comes in the next 20 s came late; else a second tap tells a lost tap from none.
    let late = term.waitForExistence(timeout: 20)
    let second = !late && tile.exists && { tile.tap(); return term.waitForExistence(timeout: 20) }()
    let what = late ? "came late (\(Int(Date().timeIntervalSince(t0) * 1000)) ms)"
      : second ? "lost: a second tap opened it" : "none: a second tap opened nothing either"
    note("tap: no terminal in 20 s; \(what); app running: \(app.state != .notRunning)")
    XCTFail("a tap on the space opened no terminal in 20 s: \(what)")
  }
}
