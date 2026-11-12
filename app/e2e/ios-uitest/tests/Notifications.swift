import Foundation
import XCTest
// Run by e2e/ios-update-notify.sh: the app's own notifications on iOS, which it can post only while iOS lets it run.
// MODE=allow: the question about notifications, answered Allow; an agent's question while the app is away; then, for
// each wait in WAITS (seconds) and ROUNDS times, the app sent away, the computer told of a new version after that wait
// (written into the Companion's update.json, UPDATE_JSON, as its daily check would), and Notification Center read for it.
// The agent asks once the file ASK exists (the suite's terminal waits for it). A simulator's test runs on the Mac, so
// it writes those files where the suite reads them. The first one found is tapped: the app opens home
// with the notice. Writes "round wait version posted" lines to $OUT, and screenshots to $SHOTS.
// MODE=deny: the question answered Don't Allow; the app goes on, and asks nothing when it opens again.
final class Notifications: XCTestCase {
  let env = ProcessInfo.processInfo.environment
  lazy var app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
  let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
  lazy var shots = env["SHOTS"] ?? NSTemporaryDirectory()
  lazy var out = env["OUT"] ?? NSTemporaryDirectory() + "/notifications.txt"
  var lines: [String] = []

  func shot(_ name: String) {
    try? FileManager.default.createDirectory(atPath: shots, withIntermediateDirectories: true)
    try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)/\(name).png"))
  }
  func say(_ s: String) {
    lines.append(s)
    try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
  }
  func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
  /** The system's alert asking about notifications, over the app. */
  var question: XCUIElement { springboard.alerts.firstMatch }
  /** The computer's daily check found this version. */
  func latest(_ version: String) {
    let json = "{\"checks\":\"on\",\"latest\":\"\(version)\"}"
    XCTAssertNoThrow(try json.write(toFile: env["UPDATE_JSON"]!, atomically: true, encoding: .utf8))
  }
  func stamp() -> Int { Int(Date().timeIntervalSince1970) }
  /** Notification Center, pulled down over the home screen: whether it lists a notification with these words. */
  func listed(_ words: String) -> XCUIElement? {
    let top = springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.005))
    top.press(forDuration: 0.1, thenDragTo: springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.7)))
    sleep(2)
    let found = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", words)).firstMatch
    return found.waitForExistence(timeout: 3) ? found : nil
  }
  func closeCenter() { XCUIDevice.shared.press(.home); sleep(1) }

  func testAllow() {
    app.activate()
    XCTAssertTrue(question.waitForExistence(timeout: 60), "no question about notifications")
    shot("ask")
    say("asked: \(question.label)")
    let allow = question.buttons.element(boundBy: 1)
    say("allow button: \(allow.label)")
    allow.tap()
    XCTAssertTrue(el("space-spnotify").waitForExistence(timeout: 30), "home shows no space after Allow")
    shot("home")

    // An agent asks while the app is away: about 2 s after it leaves (the agent works 2 s first).
    FileManager.default.createFile(atPath: env["ASK"]!, contents: Data())
    XCUIDevice.shared.press(.home)
    sleep(8)
    if let n = listed("Which port") { shot("agent-alert"); say("agent alert: \(n.label)") } else { shot("agent-alert-missing"); say("agent alert: none") }
    closeCenter()

    let waits = (env["WAITS"] ?? "0 5 10 20 30 60").split(separator: " ").compactMap { Int($0) }
    let rounds = Int(env["ROUNDS"] ?? "3") ?? 3
    var tapped = false
    for r in 1...rounds {
      for w in waits {
        let version = "50.\(r).\(w)"
        app.activate()
        XCTAssertTrue(el("space-spnotify").waitForExistence(timeout: 60), "home shows no space before round \(r) wait \(w)")
        sleep(5) // connected again, and the app's last read done
        XCUIDevice.shared.press(.home)
        let away = stamp()
        sleep(UInt32(w))
        latest(version)
        let told = stamp()
        sleep(15) // the app reads every second in this build
        let n = listed("Herdr Anywhere \(version)")
        say("round=\(r) wait=\(w) version=\(version) away=\(away) told=\(told) \(n != nil ? "posted" : "none")")
        if let n, !tapped {
          shot("update-notification")
          n.tap()
          XCTAssertTrue(el("update-banner").waitForExistence(timeout: 30), "the tap did not open home with the notice")
          shot("update-home")
          tapped = true
        } else {
          closeCenter()
        }
      }
    }
  }

  /** An app from before notifications: it asks nothing, and nothing is posted while it is away. The same screens as
   *  testAllow's, as the picture of before. */
  func testBefore() {
    app.activate()
    XCTAssertTrue(el("space-spnotify").waitForExistence(timeout: 60), "home shows no space")
    sleep(5)
    XCTAssertFalse(question.exists, "an app from before asked about notifications")
    shot("home")
    FileManager.default.createFile(atPath: env["ASK"]!, contents: Data())
    XCUIDevice.shared.press(.home)
    sleep(8)
    let agent = listed("Which port")
    shot("agent-alert")
    say("agent alert: \(agent == nil ? "none" : "posted")")
    closeCenter()
    XCUIDevice.shared.press(.home)
    latest("50.1.0")
    sleep(15)
    let update = listed("50.1.0")
    shot("update-notification")
    say("update: \(update == nil ? "none" : "posted")")
    closeCenter()
    XCTAssertNil(agent, "an app from before posted an agent's question")
    XCTAssertNil(update, "an app from before posted a new version")
  }

  func testDeny() {
    app.activate()
    XCTAssertTrue(question.waitForExistence(timeout: 60), "no question about notifications")
    let deny = question.buttons.element(boundBy: 0)
    say("deny button: \(deny.label)")
    deny.tap()
    XCTAssertTrue(el("space-spnotify").waitForExistence(timeout: 30), "home shows no space after Don't Allow")
    el("space-spnotify").tap()
    let terminal = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'terminal-'")).firstMatch
    XCTAssertTrue(terminal.waitForExistence(timeout: 30), "the space opens no terminal after Don't Allow")
    shot("deny-terminal")
    app.terminate()
    app.activate()
    XCTAssertTrue(el("space-spnotify").waitForExistence(timeout: 60), "home shows no space when opened again")
    sleep(10)
    XCTAssertFalse(question.exists, "asked again after Don't Allow")
    shot("deny-again")
    say("deny: works, not asked again")
  }
}
