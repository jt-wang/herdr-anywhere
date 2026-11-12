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
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    func must(_ e: XCUIElement, _ why: String, _ t: Double = 30) { XCTAssertTrue(e.waitForExistence(timeout: t), why); note("\(why.isEmpty ? "" : "ok: ")\(why)") }
    func tap(_ id: String) { must(el(id), "\(id) on screen", 30); el(id).tap() }
    func gone(_ e: XCUIElement, _ why: String) {
      for _ in 0..<30 where e.exists { sleep(1) }
      XCTAssertFalse(e.exists, why); note("ok: \(why)")
    }
    func back() { tap("show-terminal"); must(el("back"), "back to the terminal"); el("back").tap() }

    must(el("space-" + spaceQ), "home lists the asking space", 40)
    el("space-" + spaceQ).tap()
    must(el("choice-1"), "the terminal view has its row of choices for the question")
    shot("1-terminal-question")
    tap("show-chat")
    if !card {
      must(saying("Which plan should the fake shop use?"), "the chat shows the question")
      shot("2-chat-question")
      back()
      tap("space-" + spaceP)
      must(el("choice-1"), "the terminal view has its row of choices for the permission")
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
    tap("space-" + spaceP)
    must(el("choice-1"), "the terminal view has its row of choices for the permission")
    shot("6-terminal-permission")
    tap("show-chat")
    must(el("ask-card"), "a card for the permission")
    must(saying("wants to run a command"), "the permission's card says what it asks")
    shot("7-card-permission")
    tap("ask-row-1")
    gone(el("ask-card"), "the card goes after Yes")
  }
}
