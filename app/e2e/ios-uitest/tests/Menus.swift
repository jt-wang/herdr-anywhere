import XCTest
// Run by e2e/ios-menus.sh, the app (BUNDLE) built from e2e/menus-app.tsx: each menu opened by a real touch from
// a fresh launch (an agent's row and a space's held, the pane's "…", Attach), then a toast in another space. A
// screenshot goes to $SHOTS-<name>.png only once the menu's own words are on screen; what it saw goes to $OUT, and the
// agent menu's "Pin to the top" row's frame (points) to $ROW as idb-style JSON for the icon check.
final class Menus: XCTestCase {
  func test() {
    let env = ProcessInfo.processInfo.environment
    let out = env["OUT"] ?? "/tmp/menus.txt", shots = env["SHOTS"] ?? "/tmp/menus", rowOut = env["ROW"] ?? "/tmp/menus-row.json"
    var lines: [String] = []
    func note(_ s: String) { lines.append(s); try? (lines.joined(separator: "\n") + "\n").write(toFile: out, atomically: true, encoding: .utf8) }
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(shots)-\(name).png")) }
    let app = XCUIApplication(bundleIdentifier: env["BUNDLE"] ?? "io.jingtao.herdranywhere")
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    let saying = { (text: String) in app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
    func fresh() -> Bool { app.terminate(); app.activate(); return saying("Needs you").waitForExistence(timeout: 40) }
    func take(_ name: String, _ words: String) {
      guard saying(words).waitForExistence(timeout: 8) else { note("FAIL \(name): \"\(words)\" not on screen"); return }
      sleep(1); shot(name); note("ok \(name)")
    }
    let steps: [(String, String, () -> Bool)] = [
      ("menu-agent", "Pin to the top", { let r = el("space-agent-t2"); guard r.waitForExistence(timeout: 10) else { return false }; r.press(forDuration: 1.0); return true }),
      ("menu-space", "Close space", { let r = el("space-sp1"); guard r.waitForExistence(timeout: 10) else { return false }; r.press(forDuration: 1.0); return true }),
      ("menu-more", "Split right", {
        el("space-sp1").tap(); let m = el("space-more"); guard m.waitForExistence(timeout: 15) else { return false }; sleep(2); m.tap(); return true }),
      ("menu-attach", "Photo", {
        el("space-sp2").tap(); let a = el("attach"); guard a.waitForExistence(timeout: 15) else { return false }; sleep(2); a.tap(); return true }),
    ]
    // ONLY: a comma list of "toast", "close-pane", "toast-gestures" takes those alone; empty takes all. The gestures
    // run in light only.
    let only = env["ONLY"] ?? "", onlys = Set(only.split(separator: ",").map(String.init))
    for (name, words, open) in steps where only.isEmpty {
      guard fresh() else { note("FAIL \(name): home never showed"); continue }
      guard open() else { note("FAIL \(name): nothing to press"); continue }
      take(name, words)
      if name == "menu-agent" {
        // Only the row itself (the mockup's id menu-pin), not a view around it; the system sheet has none, so no row.
        let row = el("menu-pin"); guard row.exists else { note("no row menu-pin"); continue }
        let f = row.frame, w = app.frame.width
        let json = "[{\"AXLabel\":null,\"frame\":{\"x\":0,\"y\":0,\"width\":\(w),\"height\":\(app.frame.height)}},{\"AXLabel\":\"Pin to the top\",\"frame\":{\"x\":\(f.minX),\"y\":\(f.minY),\"width\":\(f.width),\"height\":\(f.height)}}]"
        try? json.write(toFile: rowOut, atomically: true, encoding: .utf8)
      }
    }
    // Close pane from the … menu: what the next screen is (a build that asks first shows its question).
    if only.isEmpty || onlys.contains("close-pane"), fresh() {
      el("space-sp1").tap(); let m = el("space-more")
      if m.waitForExistence(timeout: 15) {
        sleep(2); m.tap()
        let row = el("menu-close-pane").waitForExistence(timeout: 5) ? el("menu-close-pane") : saying("Close pane")
        if row.waitForExistence(timeout: 5) { row.tap(); sleep(2); shot("close-pane"); note("ok close-pane") } else { note("FAIL close-pane: no row") }
      } else { note("FAIL close-pane: no … button") }
    }
    if only.isEmpty || onlys.contains("toast"), fresh() {
      el("space-sp2").tap()
      if saying("Codex needs you").waitForExistence(timeout: 50) {
        shot("toast")
        let card = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH %@", "toast-")).firstMatch
        note("ok toast; toast top \(card.exists ? Int(card.frame.minY) : -1) pt")
      } else { note("FAIL toast: never showed") }
    } else if only.isEmpty || onlys.contains("toast") { note("FAIL toast: home never showed") }
    // On the toast, a swipe up, a tap, and a drag in from the screen's left edge at its height.
    // What each leaves on screen goes to $OUT and a picture, the same steps on the build before and after.
    guard only.isEmpty || onlys.contains("toast-gestures"), env["SCHEME"] ?? "light" == "light" else { return }
    let card = { app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH %@", "toast-")).firstMatch }
    func withToast(_ step: String) -> XCUIElement? {
      guard fresh() else { note("FAIL \(step): home never showed"); return nil }
      el("space-sp2").tap()
      guard saying("Codex needs you").waitForExistence(timeout: 50), card().exists else { note("FAIL \(step): no toast"); return nil }
      return card()
    }
    if let c = withToast("swipe") {
      c.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).press(forDuration: 0.05, thenDragTo: c.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: -1.5)))
      sleep(1); shot("swipe"); note("swipe up: toast \(saying("Codex needs you").exists ? "still there" : "gone"); in the space: \(el("space-more").exists)")
    }
    if let c = withToast("tap") {
      c.tap(); sleep(2); shot("tap"); note("tap: toast \(saying("Codex needs you").exists ? "still there" : "gone"); Codex's pane open: \(saying("Codex").exists && el("space-more").exists)")
    }
    if let c = withToast("edge") {
      let y = c.frame.midY, from = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 2, dy: y))
      from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 260, dy: 0)))
      sleep(2); shot("edge"); note("edge drag at the toast's height: home \(el("space-sp1").exists && !el("space-more").exists ? "shows" : "not shown"); toast \(saying("Codex needs you").exists ? "still there" : "gone")")
    }
  }
}
