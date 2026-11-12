import XCTest
// Run by e2e/ios-resume.sh: the app on the chat page of the space SPACE, then switched to Settings for AWAY seconds
// (each of a comma-separated list) and back. Every 0.25 s it reads the text on screen, the app's and any system
// alert's, in one snapshot (the chat's turns and the keyboard left out, but the chat page's own notices kept), and notes
// each line that was not on screen in the steady state before, with the time since the switch back. A screenshot is
// taken the first time each new line shows. When usable: right after the switch back it sends a prompt from the
// composer and notes when the prompt shows on the chat page, sending again while the text stays in the box; the same
// in the steady state gives the time that takes on a line that works. Two checks of the reading itself: STEADY seconds
// in the front with no switch, which must see nothing new, and, at the end, the companion stopped (the script does it
// when the file $OUT.stop appears), which must see the banner.
// Writes to $OUT: lines "<epoch s> <phase> +<s since the phase began> <seen|gone> <text>", and "usable +<s>".
final class Resume: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/resume.txt", shots = env["SHOTS"] ?? "/tmp/resume", space = env["SPACE"] ?? "spresume"
    let aways = (env["AWAY"] ?? "5,30,90").split(separator: ",").compactMap { Double($0) }
    let steady = Double(env["STEADY"] ?? "20") ?? 20
    var lines: [String] = []
    func note(_ s: String) {
      lines.append(String(format: "%.3f ", Date().timeIntervalSince1970) + s)
      try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8)
    }
    func shot(_ name: String) {
      try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png"))
    }
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
    let settings = XCUIApplication(bundleIdentifier: "com.apple.Preferences")
    app.activate()
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }

    let notices: Set<String> = ["chat-offline", "chat-behind"]
    func texts(_ root: XCUIElementSnapshot) -> [String] {
      var all: [String] = []
      func walk(_ s: XCUIElementSnapshot, inChat: Bool) {
        // The keyboard, the key row, the box and the pane's header (its status moves with the prompts sent) say nothing
        // about the line.
        if s.elementType == .keyboard || ["key-row", "compose-input", "pane-header"].contains(s.identifier) { return }
        let chat = inChat || s.identifier == "chat-view"
        let l = s.label.trimmingCharacters(in: .whitespacesAndNewlines)
        let kept = !chat || notices.contains(s.identifier)
        if kept && !l.isEmpty && (s.elementType == .staticText || s.elementType == .alert || s.elementType == .textView) { all.append(l) }
        s.children.forEach { walk($0, inChat: chat && !notices.contains(s.identifier)) }
      }
      walk(root, inChat: false)
      return all
    }
    func onScreen() -> Set<String> {
      var seen = Set<String>()
      if let s = try? app.snapshot() { seen.formUnion(texts(s)) }
      if springboard.alerts.count > 0, let s = try? springboard.snapshot() { seen.formUnion(texts(s).map { "[system] " + $0 }) }
      return seen
    }

    XCTAssertTrue(el("space-" + space).waitForExistence(timeout: 60), "home does not list the space")
    el("space-" + space).tap()
    XCTAssertTrue(el("show-chat").waitForExistence(timeout: 20), "no chat button")
    el("show-chat").tap()
    XCTAssertTrue(el("chat-view").waitForExistence(timeout: 20), "the chat view did not open")
    // One turn on the page, as on a phone in use: a prompt typed in the composer, which the fake Claude Code answers.
    let prompt = "HELLO, what is in this picture?"
    let box = el("compose-input"); XCTAssertTrue(box.waitForExistence(timeout: 10), "no composer"); box.tap(); sleep(1)
    box.typeText(prompt); sleep(1)
    XCTAssertTrue(el("compose-send").waitForExistence(timeout: 5), "no send button"); el("compose-send").tap()
    let said = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", prompt)).firstMatch
    XCTAssertTrue(said.waitForExistence(timeout: 30), "the prompt did not show on the chat page")
    app.swipeDown() // the keyboard away, as after reading a reply
    sleep(8)
    // The steady state: what the chat page says while connected. A line that changes on its own (an age) is not news.
    var base = onScreen()
    for _ in 0..<8 { usleep(250_000); base.formUnion(onScreen()) }
    note("base \(base.count) lines")
    shot("base")

    /// Sends `text` from the composer.
    func send(_ text: String) {
      let box = el("compose-input")
      guard box.waitForExistence(timeout: 5) else { note("no composer"); return }
      box.tap(); box.typeText(text)
      if el("compose-send").waitForExistence(timeout: 3) { el("compose-send").tap() }
    }
    // On the chat page, not in the box: a send that failed puts the text back there.
    let showing = { (text: String) in el("chat-view").descendants(matching: .staticText).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    /// Sends `text` and, while watching, notes when it shows on the chat page; while it does not, sends again every 8 s
    /// if the text came back to the box (a send that failed).
    func usable(_ phase: String, _ text: String) -> () -> Void {
      let began = Date()
      var done = false, last = Date()
      send(text)
      return {
        if done { return }
        if showing(text).exists {
          done = true
          note("\(phase) usable " + String(format: "+%.2f", Date().timeIntervalSince(began)))
        } else if Date().timeIntervalSince(last) > 8, (el("compose-input").value as? String ?? "").contains(text) {
          last = Date(); note("\(phase) send again"); el("compose-send").tap()
        }
      }
    }

    /// Reads the screen for `secs`, noting each line that comes and goes; returns the new lines it saw.
    func watch(_ phase: String, _ secs: Double, each: (() -> Void)? = nil, until stop: (() -> Bool)? = nil) -> Set<String> {
      let began = Date()
      var shown = Set<String>(), ever = Set<String>()
      while Date().timeIntervalSince(began) < secs {
        let now = onScreen().subtracting(base)
        let t = String(format: "+%.2f", Date().timeIntervalSince(began))
        for x in now.subtracting(shown).sorted() {
          note("\(phase) \(t) seen \(x)")
          if !ever.contains(x) { shot("\(phase)-\(ever.count)") }
          ever.insert(x)
        }
        for x in shown.subtracting(now).sorted() { note("\(phase) \(t) gone \(x)") }
        shown = now
        each?()
        if let stop, stop() { break }
        usleep(250_000)
      }
      return ever
    }

    // How long a prompt takes to show on a line that works, then the reading's check: nothing new with nothing changed.
    _ = watch("warm", 30, each: usable("warm", "PING warm"), until: { lines.last?.contains("warm usable") ?? false })
    sleep(5)
    base.formUnion(onScreen())
    let calm = watch("steady", steady)
    note("steady saw \(calm.count)")
    XCTAssertTrue(calm.isEmpty, "the reading saw new lines with nothing changed: \(calm)")

    for away in aways {
      let phase = "away\(Int(away))"
      settings.activate()
      XCTAssertTrue(settings.wait(for: .runningForeground, timeout: 10), "Settings did not come to the front")
      note("\(phase) left")
      Thread.sleep(forTimeInterval: away)
      app.activate()
      XCTAssertTrue(app.wait(for: .runningForeground, timeout: 10), "the app did not come back")
      note("\(phase) back")
      // Back on the chat page? Note where it landed, then watch.
      note("\(phase) on chat page: \(el("chat-view").exists)")
      _ = watch(phase, 60, each: usable(phase, "PING \(phase)"))
    }

    // The companion stops: the reading must see the banner say so.
    try? "".write(toFile: out + ".stop", atomically: true, encoding: .utf8)
    note("stop asked")
    let banner = el("conn-banner")
    let lost = watch("stopped", 40, until: { banner.exists })
    _ = watch("stopped-after", 3)
    note("stopped saw \(lost.count)")
    XCTAssertTrue(banner.exists, "the companion stopped, but no banner showed")
  }
}
