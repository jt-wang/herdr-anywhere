import XCTest
// Run by e2e/ios-keyboard.sh. The menu's Type in the terminal brings the software keyboard up, on a fresh start and
// after the key bar's hide button put it away; then the input box row's switch, and typing on the keyboard's keys straight
// into the terminal, checked as the bytes its program got. Writes what it saw to $OUT (one line per step) as well as asserting.
final class TypeIn: XCTestCase {
  func test() {
    let out = ProcessInfo.processInfo.environment["OUT"] ?? "/tmp/typein.txt"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let kbd = { app.keyboards.firstMatch.waitForExistence(timeout: 5) }
    func typeIn() {
      XCTAssertTrue(el("space-more").waitForExistence(timeout: 10), "no space-more")
      el("space-more").tap()
      let item = app.descendants(matching: .any)["Type in the terminal"].firstMatch
      XCTAssertTrue(item.waitForExistence(timeout: 5), "no Type in the terminal")
      item.tap()
    }
    XCTAssertTrue(el("space-spios").waitForExistence(timeout: 40), "home does not list the space")
    el("space-spios").tap()
    // The terminal page loaded and attached: "Connecting…" (terminal-state) goes once the page says ready. On the iOS 26.3
    // simulator it stayed for 90 s over a blank page, and the keyboard check below still passed.
    XCTAssertTrue(el("space-more").waitForExistence(timeout: 10), "the space did not open")
    let state = el("terminal-state")
    let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: state)
    let live = XCTWaiter().wait(for: [gone], timeout: 20) == .completed
    note("terminal live: \(live)\(live ? "" : ", still says: \(state.label)")")
    XCTAssertTrue(live, "the terminal page did not load: still \(state.label) after 20 s")
    sleep(2)
    note("opened, keyboard up: \(app.keyboards.count > 0)")
    typeIn()
    let fresh = kbd(); note("type-in fresh, keyboard up: \(fresh)")
    if fresh {
      XCTAssertTrue(el("hide-keyboard").waitForExistence(timeout: 5), "no hide-keyboard")
      el("hide-keyboard").tap()
      sleep(2); note("after hide, keyboard up: \(app.keyboards.count > 0)")
      typeIn()
      let again = kbd(); note("type-in after hide, keyboard up: \(again)")
      XCTAssertTrue(again, "Type in the terminal after the hide button brings no keyboard")
    }
    XCTAssertTrue(fresh, "Type in the terminal brings no keyboard")

    // The input box row's switch, then the keyboard's own keys straight into the terminal. The program there logs each
    // read as a Python bytes literal, one per line ($RAW).
    let raw = ProcessInfo.processInfo.environment["RAW"] ?? ""
    func got() -> String {
      sleep(2)
      let s = (try? String(contentsOfFile: raw, encoding: .utf8)) ?? ""
      try? "".write(toFile: raw, atomically: false, encoding: .utf8)
      return s.split(separator: "\n").map { String($0.dropFirst(2).dropLast()) }.joined()
    }
    let kb = app.keyboards.firstMatch
    func key(_ name: String) {
      let k = kb.keys[name].exists ? kb.keys[name] : kb.buttons[name]
      XCTAssertTrue(k.exists, "no key \(name) on the keyboard")
      k.tap(); usleep(400_000)
    }
    if app.keyboards.count > 0 { el("hide-keyboard").tap(); sleep(1) }
    XCTAssertTrue(el("to-terminal").waitForExistence(timeout: 5), "no to-terminal in the input box row")
    el("to-terminal").tap()
    XCTAssertTrue(kb.waitForExistence(timeout: 5), "the switch to the terminal brings no keyboard")
    XCTAssertFalse(el("compose-input").exists, "the input box is still there after the switch")
    sleep(1); _ = got()
    for k in ["h", "i", "space"] { key(k) }
    let hi = got(); note("h i space: \(hi)")
    XCTAssertEqual(hi, "hi ")
    key("return")
    let ret = got(); note("return: \(ret)")
    XCTAssertEqual(ret, "\\r", "Return sends a carriage return alone")
    for k in ["t", "e", "h", "space", "a"] { key(k) }
    let teh = got(); note("t e h space a: \(teh)")
    XCTAssertEqual(teh, "teh a", "after \"teh \" the keyboard is still lowercase")
    // WebKit's form bar (˄ ˅ Done) sits between the key row and the keyboard, about 44 pt tall; without it the key row
    // rests on the keyboard. The screen at this moment is saved next to $OUT.
    let shot = XCUIScreen.main.screenshot().pngRepresentation
    try? shot.write(to: URL(fileURLWithPath: (out as NSString).deletingPathExtension + "-terminal.png"))
    let gap = kb.frame.minY - el("key-row").frame.maxY
    note("key row to keyboard: \(Int(gap)) pt")
    XCTAssertLessThan(gap, 20, "a form bar (˄ ˅ Done) between the key row and the keyboard")
    el("to-box").tap(); note("to-box tapped")
    let back = el("compose-input").waitForExistence(timeout: 5); note("input box back: \(back)")
    XCTAssertTrue(back, "the key row's pen brings the input box back")
  }
}
