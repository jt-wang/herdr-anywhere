import XCTest
// Run by e2e/ios-chat-four.sh: the chat view of four fake agents (companion/internal/testfake/fakeagent: Claude Code,
// Codex, Pi, OpenCode 2, replaying their recordings, each with a conversation of 40 turns from the start), one space
// each (AGENTS: "claude-code codex pi opencode", space "spc" + the name without "-"). For each, with real taps:
//   open    the chat shows the long conversation (OpenCode only once it has worked, as OpenCode does)
//   long    LIST: its command printed 500 lines; the step opens, says so, and shows them all
//   image   LOOK at shot.png (Claude Code: the prompt with a picture): the picture shows
//   scroll  to the oldest of the 30 turns the chat holds and back to the newest
//   away    the app in the background AWAY seconds (each of them, the first agent; the first, the others), then back:
//           the chat is there, and a prompt sent then shows
//   cut     the Companion stopped (the script does, when CTL/stop appears): the chat keeps the turns and says the
//           computer can't be reached; started again: the line goes and a prompt sent shows
// Steps are found by the recordings' ids (fake_list, fake_look), which the fakes keep with a suffix of their own.
// The app must stay the same process all through (the script checks its pid). Writes what it saw to $OUT, screenshots
// to $SHOTS-<agent>-<step>.png.
final class ChatFour: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/chat-four.txt", shots = env["SHOTS"] ?? "/tmp/chat-four", ctl = env["CTL"] ?? ""
    let agents = (env["AGENTS"] ?? "claude-code codex pi opencode").split(separator: " ").map(String.init)
    let away = (env["AWAY"] ?? "10 60").split(separator: " ").compactMap { UInt32($0) }
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let all = { app.descendants(matching: .any) }
    let el = { (id: String) in all()[id].firstMatch }
    let saying = { (text: String) in all().matching(NSPredicate(format: "label == %@", text)).firstMatch }
    let starting = { (text: String) in all().matching(NSPredicate(format: "label BEGINSWITH %@", text)) }
    let ids = { (prefix: String) in all().matching(NSPredicate(format: "identifier BEGINSWITH %@", prefix)) }
    // The app is still up and in front; a crash or a hang shows here first.
    func alive(_ step: String) {
      XCTAssertEqual(app.state, .runningForeground, "\(step): the app is not running in front (\(app.state.rawValue))")
    }
    // Waits until ok, up to secs; false if it never was.
    func until(_ secs: Double, _ ok: () -> Bool) -> Bool {
      let end = Date().addingTimeInterval(secs)
      while Date() < end { if ok() { return true }; usleep(250_000) }
      return ok()
    }
    func say(_ text: String) {
      let box = el("compose-input"); XCTAssertTrue(box.waitForExistence(timeout: 10), "no composer"); box.tap(); sleep(1)
      box.typeText(text); sleep(1)
      XCTAssertTrue(el("compose-send").waitForExistence(timeout: 5), "no send button"); el("compose-send").tap()
    }
    // A prompt sent, and the chat shows it as the newest turn, finished.
    func sent(_ text: String, _ step: String, _ secs: Double = 40) {
      say(text)
      let shown = saying(text).waitForExistence(timeout: secs)
      note("\(step): '\(text)' shown: \(shown)")
      XCTAssertTrue(shown, "\(step): the chat did not show '\(text)'")
      // Its fold (a finished turn) is the last one.
      _ = until(20) { ids("fold-").count > 0 && !saying("Working…").exists }
    }
    // The newest turn's steps, opened.
    func openLast() -> Bool {
      let folds = ids("fold-")
      guard folds.count > 0 else { return false }
      let f = folds.element(boundBy: folds.count - 1)
      if !f.isHittable { app.swipeUp() }
      f.tap(); sleep(1)
      return true
    }
    func ctlDo(_ what: String, _ done: String) -> Bool {
      guard !ctl.isEmpty else { return false }
      FileManager.default.createFile(atPath: "\(ctl)/\(what)", contents: nil)
      return until(60) { FileManager.default.fileExists(atPath: "\(ctl)/\(done)") }
    }
    for (n, agent) in agents.enumerated() {
      let a = agent.replacingOccurrences(of: "-", with: "")
      guard el("space-spc" + a).waitForExistence(timeout: 40) else { XCTFail("\(agent): home does not list its space"); return }
      el("space-spc" + a).tap()
      // After the first agent a pane opens in its chat (the view picked last), with no chat button.
      if !el("chat-view").waitForExistence(timeout: 5) {
        guard el("show-chat").waitForExistence(timeout: 20) else { XCTFail("\(agent): no chat button"); return }
        el("show-chat").tap()
      }
      guard el("chat-view").waitForExistence(timeout: 10) else { XCTFail("\(agent): no chat view"); return }
      // open
      if agent == "opencode" {
        let not = el("chat-unknown").waitForExistence(timeout: 20)
        note("\(agent) open: says it can't tell yet: \(not)")
        sent("HELLO again", "\(agent) open")
      }
      let long = starting("040 ").firstMatch.waitForExistence(timeout: 30)
      note("\(agent) open: turn 040 shown: \(long)")
      XCTAssertTrue(long, "\(agent): the long conversation did not show")
      shot("\(agent)-open"); alive("\(agent) open")
      // long
      sent("LIST the files here", "\(agent) long")
      XCTAssertTrue(openLast(), "\(agent) long: no fold")
      let lsSteps = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_list'"))
      if lsSteps.count > 0 { lsSteps.element(boundBy: lsSteps.count - 1).tap() }
      let more = saying("Show all 500 lines").waitForExistence(timeout: 20)
      note("\(agent) long: 'Show all 500 lines': \(more)")
      XCTAssertTrue(more, "\(agent) long: the command's 500 lines are not offered")
      if more { saying("Show all 500 lines").tap(); sleep(2) }
      let end = starting("line 500 of the long listing").firstMatch.exists || all().matching(NSPredicate(format: "label CONTAINS 'line 500 of the long listing'")).count > 0
      note("\(agent) long: the last line shown: \(end)")
      shot("\(agent)-long"); alive("\(agent) long")
      // image
      // A new picture: one whose id wasn't there (the chat holds 30 turns, so the oldest one's may go as one comes).
      let names = { () -> Set<String> in Set(ids("image-").allElementsBoundByIndex.map { $0.identifier }.filter { !$0.hasPrefix("image-source-") }) }
      let before = names()
      if agent == "claude-code" { sent("HELLO, what is in this picture?", "\(agent) image") } else {
        sent("LOOK at shot.png", "\(agent) image")
        XCTAssertTrue(openLast(), "\(agent) image: no fold")
        let look = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_look'"))
        if look.count > 0 { look.element(boundBy: look.count - 1).tap() }
      }
      let pics = until(20) { !names().subtracting(before).isEmpty }
      note("\(agent) image: new pictures \(names().subtracting(before).sorted())")
      XCTAssertTrue(pics, "\(agent) image: no picture showed")
      shot("\(agent)-image"); alive("\(agent) image")
      // scroll: the chat holds the last 30 turns; the oldest is numbered 01x
      var top = false
      for _ in 0..<40 { if starting("01").firstMatch.exists && starting("01").firstMatch.isHittable { top = true; break }; el("chat-view").swipeDown() }
      note("\(agent) scroll: the oldest turns reached: \(top)")
      XCTAssertTrue(top, "\(agent) scroll: did not reach the oldest turns")
      shot("\(agent)-top"); alive("\(agent) scroll")
      var bottom = false
      for _ in 0..<40 { if saying("LOOK at shot.png").isHittable || saying("HELLO, what is in this picture?").isHittable { bottom = true; break }; el("chat-view").swipeUp() }
      note("\(agent) scroll: back to the newest: \(bottom)")
      XCTAssertTrue(bottom, "\(agent) scroll: did not get back to the newest turn")
      // away
      for secs in (n == 0 ? away : Array(away.prefix(1))) {
        XCUIDevice.shared.press(.home)
        sleep(secs)
        app.activate()
        // app.state can still say background (3) a moment after activate() on iOS 27 with the chat already shown.
        _ = app.wait(for: .runningForeground, timeout: 10)
        let back = el("chat-view").waitForExistence(timeout: 15)
        alive("\(agent) away \(secs) s")
        note("\(agent) away \(secs) s: chat there: \(back), says it can't reach the computer 15 s later: \(el("chat-offline").exists)")
        XCTAssertTrue(back, "\(agent) away \(secs) s: the chat is gone")
        shot("\(agent)-away\(secs)")
        sent("EDIT after \(secs) s away", "\(agent) away \(secs) s", 60)
      }
      // cut
      if ctlDo("stop", "stopped") {
        let off = el("chat-offline").waitForExistence(timeout: 45)
        let kept = saying("EDIT after \(away.first ?? 0) s away").exists
        note("\(agent) cut: says it can't reach the computer: \(off), the turns kept: \(kept)")
        XCTAssertTrue(off, "\(agent) cut: nothing says the computer can't be reached")
        XCTAssertTrue(kept, "\(agent) cut: the turns went")
        shot("\(agent)-cut"); alive("\(agent) cut")
        XCTAssertTrue(ctlDo("start", "started"), "\(agent) cut: the Companion did not start again")
        let on = until(60) { !el("chat-offline").exists }
        note("\(agent) cut: the line gone once it is back: \(on)")
        XCTAssertTrue(on, "\(agent) cut: still says it can't reach the computer")
        sent("CHANGE after the restart", "\(agent) cut", 60)
        shot("\(agent)-back"); alive("\(agent) back")
        try? FileManager.default.removeItem(atPath: "\(ctl)/stopped"); try? FileManager.default.removeItem(atPath: "\(ctl)/started")
      }
      el("back").tap(); sleep(1)
    }
  }
}
