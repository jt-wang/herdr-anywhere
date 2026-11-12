import XCTest
// Run by e2e/ios-subkeep.sh, once a theme, for its sub-agent KID: the sub-agent's row on home opens a peek, Keep makes it
// a tab, and back on home the row is tapped again. Notes in $OUT what that second tap opened ("second tap: tab", "peek"
// or "neither"); before the fix it was a peek, and this then presses Keep once more, as a person would. Then the
// switcher. A screenshot at each step, $SHOTS-<n>-<step>.png. The script judges, with what herdr-core holds after.
final class SubKeep: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let kid = env["KID"] ?? "t2", out = env["OUT"] ?? "/tmp/subkeep.txt", shots = env["SHOTS"] ?? "/tmp/subkeep"
    var lines: [String] = []
    func note(_ s: String) {
      lines.append(s)
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    func elements(_ name: String) { try? app.debugDescription.write(toFile: out + "-" + name + ".txt", atomically: true, encoding: .utf8) }
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let row = el("sub-" + kid), keep = el("peek-keep"), tab = el("terminal-" + kid), back = el("back")
    // A run after another starts where that one left the app: a sheet over a space. Above the sheet, then back.
    for _ in 0..<4 where !row.exists {
      if back.exists && back.isHittable { back.tap() } else { app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.06)).tap() }
      sleep(2)
    }
    guard row.waitForExistence(timeout: 90) else { elements("home"); XCTFail("home does not list the sub-agent \(kid)"); return }
    sleep(1); shot("1-home")
    row.tap()
    guard keep.waitForExistence(timeout: 15) else { elements("peek"); shot("2-peek"); XCTFail("the sub-agent's row opened no peek"); return }
    sleep(1); shot("2-peek")
    keep.tap()
    note("after Keep its tab \(tab.waitForExistence(timeout: 15) ? "shown" : "not shown")")
    sleep(2); shot("3-kept")
    if back.waitForExistence(timeout: 5) { back.tap() }
    guard row.waitForExistence(timeout: 15) else { elements("home-again"); XCTFail("no way back to home"); return }
    row.tap()
    sleep(3)
    let second = keep.exists ? "peek" : tab.exists ? "tab" : "neither"
    note("second tap: \(second)")
    elements("second-tap")
    shot("4-second-tap")
    if second == "peek" { keep.tap(); sleep(3) }
    let switcher = el("switcher")
    if switcher.waitForExistence(timeout: 10) { switcher.tap(); sleep(2); shot("5-switcher") }
    note("done")
  }
}
