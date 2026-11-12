import XCTest
// Run by e2e/ios-chat-four.sh with UITEST=ChatFold (devices.yml group ios-chatfold): screenshots of the chat view for
// a change's before and after, on the app as built, over the Companion and a fake Claude Code (its turns slowed by
// the script), through companion/internal/testfake/chatproxy, which this test steers by files in CTL:
//   fold  while the agent works, the command's step opened; one answer that the computer can't tell (no-status); the
//         answers after it, to the turn's end: its fold opened, is the step still open?
//   late  an answer from before the agent's write step is held back until the chat shows that step, then lands: does
//         the step go away? (Held for some 20 s, well within the 60 s a request waits on iOS.)
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
    // What the screen had at a failure, so a red run says why: a screenshot and the ids it showed.
    func failed(_ what: String) {
      shot("failed")
      // From one snapshot: a query over elements that change as it runs throws.
      var ids: [String] = []
      func walk(_ e: XCUIElementSnapshot) {
        let id = e.identifier
        if ["step-", "fold-", "you-", "chat-"].contains(where: { id.hasPrefix($0) }) { ids.append(id) }
        e.children.forEach(walk)
      }
      if let root = try? app.snapshot() { walk(root) }
      note("\(what): working \(working()), 'LIST the files here' \(saying("LIST the files here").exists), chat view \(el("chat-view").exists), ids \(ids.suffix(30))")
      XCTFail(what)
    }
    guard el("space-spcclaudecode").waitForExistence(timeout: 40) else { XCTFail("home does not list the space"); return }
    el("space-spcclaudecode").tap()
    guard until(40, { el("chat-view").exists || el("show-chat").exists }) else { failed("no chat view and no chat button"); return }
    if !el("chat-view").exists { el("show-chat").tap() }
    guard all().matching(NSPredicate(format: "label BEGINSWITH '040 '")).firstMatch.waitForExistence(timeout: 30) else {
      XCTFail("the conversation did not show"); return
    }

    // fold
    say("LIST the files here")
    let list = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_list'"))
    // The fake writes a line every 5 s (e2e/ios-chat-four.sh): the LIST turn's command step is line 17 of 30, its
    // output line 20, so the step shows about 85 s after the prompt and its output about 15 s later; the turn ends at
    // about 150 s. A step's details are fetched when it opens, so it is opened once its output is in, and the doubt
    // and its answers after it all come before the turn ends and folds its steps away.
    guard until(180, { list.count > 0 && working() }) else { failed("fold: no command step while working"); return }
    sleep(16)
    let step = list.element(boundBy: list.count - 1).identifier
    guard el(step).exists else { failed("fold: the command step went away before it was opened"); return }
    el(step).tap()
    let more = saying("Show all 500 lines")
    guard more.waitForExistence(timeout: 15) else { failed("fold: the step did not open"); return }
    sleep(1); shot("fold-1-opened")
    put("no-status")
    guard el("chat-unknown").waitForExistence(timeout: 15) else { failed("fold: the answer that can't tell did not show"); return }
    sleep(1); shot("fold-2-doubt")
    // Back from the doubt, and then the turn's end, which folds its steps away (when is up to the CI's pace): the
    // turn's fold is opened, and its step shows open only if the view kept what was opened.
    let folds = all().matching(NSPredicate(format: "identifier BEGINSWITH 'fold-'"))
    guard until(150, { folds.count > 0 && !el(step).exists }) else { failed("fold: the turn did not come back folded"); return }
    folds.element(boundBy: folds.count - 1).tap()
    guard el(step).waitForExistence(timeout: 10) else { failed("fold: the turn's fold did not show its step"); return }
    sleep(3)
    note("fold: the step open after the doubt and the turn's end: \(more.exists)")
    shot("fold-3-back")
    guard until(150, { !working() }) else { failed("fold: the turn did not finish"); return }

    // late
    put("hold")
    say("EDIT a file for me")
    guard until(30, { has("held") }) else { failed("late: no answer of the turn working was held"); return }
    let edits = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_write'"))
    guard until(90, { edits.count > 0 }) else { failed("late: the newer answers did not show the write step"); return }
    sleep(1); shot("late-1-newer")
    put("release")
    guard until(15, { has("released") }) else { failed("late: the held answer was not sent"); return }
    // The older answer, from before the step, put on screen: the step goes until the next answer (some 3 s).
    let gone = until(4, { edits.count == 0 })
    shot("late-2-older-landed")
    _ = until(6, { false })
    note("late: the write step gone when the older answer landed: \(gone); there 6 s later: \(edits.count > 0)")
    XCTAssertEqual(app.state, .runningForeground, "the app is not running in front")
  }
}
