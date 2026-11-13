import XCTest
// Run by e2e/ios-chat.sh: in the space SPACE a fake Claude Code replays the image turn, then the files turn (2.1.322
// fixtures). Real taps: two prompts typed and sent from the composer, then the reply's cards. The video's card shows
// its length and opens the player, which plays it with the keyboard put away; the PDF opens in a preview. Writes what
// it saw to $OUT and screenshots to $SHOTS-<step>.png.
final class Chat: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/chat.txt", shots = env["SHOTS"] ?? "/tmp/chat", space = env["SPACE"] ?? "spchat"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    XCTAssertTrue(el("space-" + space).waitForExistence(timeout: 40), "home does not list the space")
    el("space-" + space).tap()
    let chat = el("show-chat").waitForExistence(timeout: 20); shot("space")
    XCTAssertTrue(chat, "no chat button"); el("show-chat").tap()
    func say(_ text: String) {
      let box = el("compose-input"); XCTAssertTrue(box.waitForExistence(timeout: 10), "no composer"); box.tap(); sleep(1)
      box.typeText(text); sleep(1)
      XCTAssertTrue(el("compose-send").waitForExistence(timeout: 5), "no send button"); el("compose-send").tap()
    }
    say("HELLO, what is in this picture?")
    XCTAssertTrue(saying("HELLO, what is in this picture?").waitForExistence(timeout: 30), "the first prompt did not show")
    sleep(5)
    say("RENDER the demo video")
    let pdf = el("file-docs/guide.pdf"), frame = el("video-frame-renders/demo.mp4")
    let cards = pdf.waitForExistence(timeout: 60)
    note("file cards: \(cards)")
    XCTAssertTrue(cards, "the reply's files got no cards")
    let framed = frame.waitForExistence(timeout: 30)
    let length = saying("0:03").exists
    note("video frame: \(framed), length 0:03 shown: \(length)")
    XCTAssertTrue(length, "the video's card does not say how long it is")
    // The frame is an image the accessibility tree may not list (iOS): the cards shot shows it.
    sleep(2); shot("cards")
    el("file-renders/demo.mp4").tap()
    let player = el("video-player").waitForExistence(timeout: 20)
    note("player open: \(player)")
    XCTAssertTrue(player, "the video did not open in the player")
    var keysUp = true
    for _ in 0..<6 { if app.keyboards.count == 0 { keysUp = false; break }; usleep(500_000) }
    note("keyboard up over the player: \(keysUp)")
    sleep(3)
    let cant = saying("The video can't be played.").exists
    note("can't-play text: \(cant)")
    XCTAssertFalse(cant, "the player could not play the video")
    shot("playing")
    el("video-close").tap()
    XCTAssertTrue(pdf.waitForExistence(timeout: 10), "the player did not close")
    sleep(1)
    pdf.tap()
    let look = app.navigationBars.firstMatch.waitForExistence(timeout: 20) || saying("guide").waitForExistence(timeout: 5)
    note("preview open: \(look)")
    sleep(4); shot("pdf")
    XCTAssertTrue(look, "the PDF did not open")
    note("labels in the preview: " + app.staticTexts.allElementsBoundByIndex.prefix(12).map { $0.label }.joined(separator: " | "))
    for name in ["Close", "Done"] where app.buttons[name].firstMatch.exists { app.buttons[name].firstMatch.tap(); break }
    XCTAssertFalse(keysUp, "the keyboard stayed up over the player")
  }
}
