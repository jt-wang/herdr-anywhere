import XCTest
// Run by e2e/ios-kitty.sh: opens the space SPACE from home (OpenSpace.swift; on the space already, nothing to tap),
// waits for its terminal TERMINAL, notes "terminal showed" in $OUT, then keeps the app on that page until the script,
// which takes the screenshots meanwhile, writes the file $DONE.
final class Kitty: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/kitty.txt", done = env["DONE"] ?? "/tmp/kitty-done"
    let space = env["SPACE"] ?? "spacme", terminal = env["TERMINAL"] ?? "t1"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let term = app.descendants(matching: .any)["terminal-" + terminal].firstMatch
    let opener = SpaceOpener(app: app, out: out, note: note)
    var again: Bool? = nil
    if !term.waitForExistence(timeout: 3) { again = opener.open(space) }
    let showed = term.waitForExistence(timeout: 20)
    note("terminal \(showed ? "showed" : "not there")")
    XCTAssertTrue(showed, "no terminal")
    if let again { opener.finish(again: again, shown: showed) }
    guard showed else { return }
    for _ in 0..<480 {
      if FileManager.default.fileExists(atPath: done) { note("done"); return }
      sleep(1)
    }
    XCTFail("the script never wrote \(done)")
  }
}
