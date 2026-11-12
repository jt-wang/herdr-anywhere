import XCTest
// Run by e2e/ios-keyboard.sh when HM_HOLD_SWEEP=1, in place of TypeIn: whether how long a finger stays on a space's row
// on home decides if the row opens. Presses the space for each hold in HOLDS (seconds, by turns, ROUNDS times over),
// and after each says whether its page came up within 3 s, then goes back home with the page's back button. A note
// "tapping …" just before each press and one just after: e2e/ios-touch-log.sh says how long UIKit itself held each one
// down, which under a busy runner is not the hold asked for. Fails when any press opened nothing, after all of them.
final class Hold: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/hold.txt"
    let holds = (env["HOLDS"] ?? "0.05,0.10,0.14,0.16,0.18,0.20,0.25").split(separator: ",").compactMap { Double($0) }
    let rounds = Int(env["ROUNDS"] ?? "") ?? 5
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
      for hold in holds {
        let h = String(format: "%.2f s", hold)
        guard tile.waitForExistence(timeout: 10) else { XCTFail("home does not list the space before the hold of \(h)"); return }
        note("tapping the space (round \(round), hold \(h))")
        tile.press(forDuration: hold)
        note("tapped the space (hold \(h))")
        made[h, default: 0] += 1
        if el("space-more").waitForExistence(timeout: 3) {
          note("hold \(h): opened")
          el("back").tap()
          let home = tile.waitForExistence(timeout: 10)
          note("back home: \(home)")
          if !home { XCTFail("the page's back button did not bring home back"); return }
        } else {
          missed[h, default: 0] += 1
          note("hold \(h): opened nothing in 3 s")
          // A page that came late still has to be left before the next press.
          if el("space-more").waitForExistence(timeout: 5) { note("hold \(h): its page came after 3 s"); el("back").tap() }
        }
      }
    }
    for hold in holds {
      let h = String(format: "%.2f s", hold)
      note("hold \(h): \(missed[h] ?? 0) of \(made[h] ?? 0) opened nothing")
    }
    let total = missed.values.reduce(0, +)
    XCTAssertEqual(total, 0, "\(total) presses on the space opened nothing")
  }
}
