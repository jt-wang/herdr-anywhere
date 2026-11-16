import XCTest
// Run by e2e/ios-ask.sh: in the space SPACEQ a stand-in for Claude Code 2.1.325 asks two questions, in SPACEP it asks
// to run a command (e2e/fake-ask, its screens in e2e/files/ask). CARD=yes (the tree has the decision card): the first
// question answered with a digit from the card, the second's boxes ticked, Next, Submit answers; then the permission's
// Yes. CARD=no: the same screens, shot only. Writes what it saw to $OUT and screenshots to $SHOTS-<step>.png.
final class Ask: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/ask.txt", shots = env["SHOTS"] ?? "/tmp/ask"
    let spaceQ = env["SPACEQ"] ?? "spask", spaceP = env["SPACEP"] ?? "spperm", card = env["CARD"] == "yes"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { sleep(1); try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let opener = SpaceOpener(app: app, out: out, note: note)
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    func must(_ e: XCUIElement, _ why: String, _ t: Double = 30) {
      if e.waitForExistence(timeout: t) { note("\(why.isEmpty ? "" : "ok: ")\(why)"); return }
      // What the screen held instead: a picture and its texts, for the failure folder.
      note("FAIL: \(why)"); shot("fail")
      note("on screen: " + app.staticTexts.allElementsBoundByIndex.prefix(40).map { $0.label }.joined(separator: " | "))
      XCTFail(why)
    }
    func tap(_ id: String) { must(el(id), "\(id) on screen", 30); el(id).tap() }
    func gone(_ e: XCUIElement, _ why: String) {
      for _ in 0..<30 where e.exists { sleep(1) }
      XCTAssertFalse(e.exists, why); note("ok: \(why)")
    }
    // The app opens a pane the way the last one was shown (the chat, after the light pass ended in it): the terminal
    // first. The chat can come up a few seconds after the terminal's row (once the pane's record is read), so a row seen
    // is watched a while before it counts.
    func open(_ space: String) -> Bool {
      let again = opener.open(space)
      let row = el("choice-1"), toTerminal = el("show-terminal")
      for _ in 0..<30 {
        if toTerminal.exists { break }
        if row.exists { sleep(5); if !toTerminal.exists { return again }; break }
        sleep(1)
      }
      if toTerminal.exists { note("opened in the chat: back to the terminal"); toTerminal.tap() }
      return again
    }
    func back() { tap("show-terminal"); must(el("back"), "back to the terminal"); el("back").tap() }

    must(el("space-" + spaceQ), "home lists the asking space", 40)
    let again = open(spaceQ)
    must(el("choice-1"), "the terminal view has its row of choices for the question")
    opener.finish(again: again, shown: el("choice-1").exists)
    shot("1-terminal-question")
    tap("show-chat")
    if !card {
      // Before the card: shot as it is, as the permission's below; what the chat shows is noted, not required (a record
      // behind the screen showed no question there).
      let q = saying("Which plan should the fake shop use?").waitForExistence(timeout: 15)
      note(q ? "before: the chat shows the question" : "before: the chat does not show the question")
      shot("2-chat-question")
      back()
      let againPermission = open(spaceP)
      must(el("choice-1"), "the terminal view has its row of choices for the permission")
      opener.finish(again: againPermission, shown: el("choice-1").exists)
      shot("6-terminal-permission")
      tap("show-chat"); sleep(3)
      shot("7-chat-permission")
      return
    }
    must(el("ask-card"), "a card for the question")
    must(saying("Question 1 of 2"), "the card says which question")
    for id in ["ask-row-1", "ask-row-2", "ask-row-3", "ask-own", "ask-skip"] { XCTAssertTrue(el(id).exists, "the card has no \(id)") }
    XCTAssertFalse(saying("Answer it in the terminal").exists, "the card says to answer in the terminal")
    shot("2-card-question")
    tap("ask-row-2")
    must(saying("Question 2 of 2"), "a digit answered the first question")
    must(el("ask-box-1-off"), "the second question's boxes")
    shot("3-card-multi")
    tap("ask-row-1"); must(el("ask-box-1-on"), "Home ticked")
    tap("ask-row-3"); must(el("ask-box-3-on"), "Help ticked")
    tap("ask-next")
    must(saying("Ready to submit your answers?"), "Next goes to the review")
    shot("4-card-review")
    tap("ask-row-1")
    gone(el("ask-card"), "the card goes after Submit answers")
    must(saying("Noted your answers."), "the reply after the answers")
    shot("5-answered")
    back()
    let againPermission = open(spaceP)
    must(el("choice-1"), "the terminal view has its row of choices for the permission")
    opener.finish(again: againPermission, shown: el("choice-1").exists)
    shot("6-terminal-permission")
    tap("show-chat")
    must(el("ask-card"), "a card for the permission")
    must(saying("wants to run a command"), "the permission's card says what it asks")
    shot("7-card-permission")
    tap("ask-row-1")
    gone(el("ask-card"), "the card goes after Yes")
  }
}
