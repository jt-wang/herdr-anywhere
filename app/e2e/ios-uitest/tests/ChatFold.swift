import XCTest
// Run by e2e/ios-chat-four.sh with UITEST=ChatFold (devices.yml group ios-chatfold): screenshots of the chat view for
// a change's before and after, on the app as built, over the Companion and a fake Claude Code (its turns slowed by
// the script), through companion/internal/testfake/chatproxy, which this test steers by files in CTL:
//   fold  while the agent works, the command's step opened; one answer that the computer can't tell (no-status); the
//         answers after it: is the step still open?
//   late  an answer from while the agent worked is held back until the chat shows the turn finished, then lands: does
//         the view go back to "Working…"?
// It fails only when it can't get to a screenshot; what the screenshots show is for comparing a before and an after
// (its notes say it too). Screenshots to $SHOTS-<name>.png, notes to $OUT.
final class ChatFold: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/chat-fold.txt", shots = env["SHOTS"] ?? "/tmp/chat-fold", ctl = env["CTL"] ?? ""
    XCTAssertFalse(ctl.isEmpty, "no CTL folder"); if ctl.isEmpty { return }
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let all = { app.descendants(matching: .any) }
    let el = { (id: String) in all()[id].firstMatch }
    let saying = { (text: String) in all().matching(NSPredicate(format: "label == %@", text)).firstMatch }
    let working = { saying("Working…").exists }
    let has = { (name: String) in FileManager.default.fileExists(atPath: "\(ctl)/\(name)") }
    let put = { (name: String) in _ = FileManager.default.createFile(atPath: "\(ctl)/\(name)", contents: nil) }
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
    guard el("space-spcclaudecode").waitForExistence(timeout: 40) else { XCTFail("home does not list the space"); return }
    el("space-spcclaudecode").tap()
    if !el("chat-view").waitForExistence(timeout: 5) {
      guard el("show-chat").waitForExistence(timeout: 20) else { XCTFail("no chat button"); return }
      el("show-chat").tap()
    }
    guard all().matching(NSPredicate(format: "label BEGINSWITH '040 '")).firstMatch.waitForExistence(timeout: 30) else {
      XCTFail("the conversation did not show"); return
    }

    // fold
    say("LIST the files here")
    let list = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_list'"))
    guard until(30, { list.count > 0 && working() }) else { XCTFail("fold: no command step while working"); return }
    let step = list.element(boundBy: list.count - 1).identifier
    el(step).tap()
    let more = saying("Show all 500 lines")
    guard more.waitForExistence(timeout: 15) else { XCTFail("fold: the step did not open"); return }
    sleep(1); shot("fold-1-opened")
    put("no-status")
    guard el("chat-unknown").waitForExistence(timeout: 15) else { XCTFail("fold: the answer that can't tell did not show"); return }
    sleep(1); shot("fold-2-doubt")
    guard el(step).waitForExistence(timeout: 15) else { XCTFail("fold: the turns did not come back"); return }
    sleep(2)
    note("fold: still working when back: \(working()); the step open after the doubt: \(more.exists)")
    shot("fold-3-back")
    guard until(90, { !working() }) else { XCTFail("fold: the turn did not finish"); return }

    // late
    put("hold")
    say("EDIT a file for me")
    guard until(30, { has("held") }) else { XCTFail("late: no answer of the turn working was held"); return }
    guard until(90, { saying("EDIT a file for me").exists && !working() }) else { XCTFail("late: the turn did not show finished"); return }
    sleep(1); shot("late-1-newer")
    put("release")
    guard until(15, { has("released") }) else { XCTFail("late: the held answer was not sent"); return }
    sleep(2)
    let back = working()
    shot("late-2-older-landed")
    _ = until(6, { false })
    note("late: 'Working…' after the older answer landed: \(back); 6 s later: \(working())")
    XCTAssertEqual(app.state, .runningForeground, "the app is not running in front")
  }
}
