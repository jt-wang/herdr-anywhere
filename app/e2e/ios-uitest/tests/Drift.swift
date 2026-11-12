import XCTest
// Run by e2e/ios-keyboard.sh when HM_SPACE_PRESS is drift or busy, in place of TypeIn: whether a finger that moves a
// little while it is down, as a real one does, keeps a space's row on home from opening. Presses the space for HOLD
// seconds and then moves DRIFT points down at a slow finger's speed, for each drift in DRIFTS by turns, ROUNDS times
// over (0: a press with no move). After each, whether the space's page came up within 3 s; then back home with the
// page's back button. With busy, the test app also keeps its main thread busy after each press begins (the window's
// busy=, e2e/ios-touch-window.swift). A note "tapping …" just before each press and one just after, for
// e2e/ios-touch-log.sh and e2e/ios-hold-say.sh. Fails when any press opened nothing, after all of them.
final class Drift: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/drift.txt"
    let drifts = (env["DRIFTS"] ?? "0,2,4,8").split(separator: ",").compactMap { Double($0) }
    let rounds = Int(env["ROUNDS"] ?? "") ?? 6
    let hold = Double(env["HOLD"] ?? "") ?? 0.1
    var lines: [String] = []
    let start = Date(), clock = DateFormatter()
    clock.dateFormat = "HH:mm:ss.SSS"
    func note(_ s: String) {
      let since = String(format: "+%.1f s", Date().timeIntervalSince(start))
      lines.append([clock.string(from: Date()), since, runnerLoad(), s].joined(separator: " "))
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let tile = el("space-spios")
    XCTAssertTrue(tile.waitForExistence(timeout: 40), "home does not list the space")
    var missed: [String: Int] = [:], made: [String: Int] = [:]
    for round in 1...rounds {
      for drift in drifts {
        let d = String(format: "drift %.0f pt", drift)
        guard tile.waitForExistence(timeout: 10) else { XCTFail("home does not list the space before the \(d)"); return }
        let from = tile.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        note("tapping the space (round \(round), \(d))")
        if drift == 0 {
          from.press(forDuration: hold)
        } else {
          from.press(forDuration: hold, thenDragTo: from.withOffset(CGVector(dx: 0, dy: drift)), withVelocity: .slow, thenHoldForDuration: 0)
        }
        note("tapped the space (\(d))")
        made[d, default: 0] += 1
        if el("space-more").waitForExistence(timeout: 3) {
          note("\(d): opened")
          el("back").tap()
          let home = tile.waitForExistence(timeout: 10)
          note("back home: \(home)")
          if !home { XCTFail("the page's back button did not bring home back"); return }
        } else {
          missed[d, default: 0] += 1
          note("\(d): opened nothing in 3 s")
          if el("space-more").waitForExistence(timeout: 5) { note("\(d): its page came after 3 s"); el("back").tap() }
        }
      }
    }
    for drift in drifts {
      let d = String(format: "drift %.0f pt", drift)
      note("\(d): \(missed[d] ?? 0) of \(made[d] ?? 0) opened nothing")
    }
    let total = missed.values.reduce(0, +)
    XCTAssertEqual(total, 0, "\(total) presses on the space opened nothing")
  }
}
