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
    let cycles = Int(env["CHAT_AWAY_CYCLES"] ?? "0") ?? -1
    let awayOnly = env["CHAT_AWAY_ONLY"] == "1"
    let stopOnResume = env["CHAT_AWAY_SELFTEST"] == "stop-companion"
    guard (0...30).contains(cycles) else { XCTFail("CHAT_AWAY_CYCLES must be 0...30"); return }
    if cycles > 0 { continueAfterFailure = false }
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
    // Read replies and completion markers from one snapshot. Enumerating live elements while the chat updates
    // can fail between reading the list and reading an element's identifier (as in ChatFold's failure capture).
    func chatSnapshot() -> (ids: Set<String>, labels: Set<String>)? {
      guard let root = try? app.snapshot() else { return nil }
      var found: Set<String> = [], labels: Set<String> = [], pending = [root]
      while let element = pending.popLast() {
        found.insert(element.identifier)
        labels.insert(element.label)
        pending.append(contentsOf: element.children)
      }
      return (found, labels)
    }
    func completedReply(_ current: (ids: Set<String>, labels: Set<String>), _ oldReplies: Set<String>, _ prompt: String) -> Bool {
      let fresh = Set(current.ids.filter { $0.hasPrefix("reply-") }).subtracting(oldReplies)
      return current.labels.contains(prompt) && !current.ids.contains("chat-offline") && !current.labels.contains(where: { $0 == "Working…" || $0.hasPrefix("Working… · ") }) &&
        fresh.contains { current.ids.contains("fold-" + String($0.dropFirst("reply-".count))) }
    }
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
    // The fake's turns so far, which it numbers -p<n> in its ids (testfake/replay): ios-chat-four.sh's -preload 40,
    // then one for each prompt said.
    var plays = 0
    func say(_ text: String) {
      plays += 1
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
    // After a failed step: which of the pane's views is up, and what herdr-core says of the agent's terminal (the
    // script writes it to hc-get.txt next to the screenshots).
    func onScreen(_ step: String, _ name: String) {
      note("\(step): on screen: chat-view \(el("chat-view").exists), show-chat \(el("show-chat").exists), show-terminal \(el("show-terminal").exists)")
      guard !ctl.isEmpty else { return }
      FileManager.default.createFile(atPath: "\(ctl)/get", contents: name.data(using: .utf8))
      _ = until(10) { FileManager.default.fileExists(atPath: "\(ctl)/got") }
      try? FileManager.default.removeItem(atPath: "\(ctl)/got")
    }
    func ctlDo(_ what: String, _ done: String) -> Bool {
      guard !ctl.isEmpty else { return false }
      FileManager.default.createFile(atPath: "\(ctl)/\(what)", contents: nil)
      return until(60) { FileManager.default.fileExists(atPath: "\(ctl)/\(done)") }
    }
    for (n, agent) in agents.enumerated() {
      plays = 40
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
      // Pi's chat needs Herdr Anywhere's file in Pi's folder: the first open asks, as for a person, and the test says yes
      // there. A test that went on without it tapped the sheet's button by chance, after its 30 s.
      if agent == "pi" {
        let asked = el("chat-setup-yes").waitForExistence(timeout: 10)
        note("\(agent) open: asks to install its chat setup: \(asked)")
        XCTAssertTrue(asked, "\(agent): the chat did not ask to install Pi's setup")
        if asked { el("chat-setup-yes").tap() }
      }
      let long = starting("040 ").firstMatch.waitForExistence(timeout: 30)
      note("\(agent) open: turn 040 shown: \(long)")
      XCTAssertTrue(long, "\(agent): the long conversation did not show")
      shot("\(agent)-open"); alive("\(agent) open")
      if awayOnly {
        var oldReplies: Set<String> = []
        guard until(10, {
          guard let beforeReply = chatSnapshot() else { return false }
          oldReplies = Set(beforeReply.ids.filter { $0.hasPrefix("reply-") })
          return !oldReplies.isEmpty
        }) else {
          shot("\(agent)-preflight-failed")
          XCTFail("background preflight: cannot read existing replies")
          return
        }
        let prompt = "EDIT before background check"
        say(prompt)
        let completed = until(60) {
          guard let current = chatSnapshot() else { return false }
          return completedReply(current, oldReplies, prompt)
        }
        note("background preflight: new completed reply: \(completed)")
        guard completed else {
          shot("\(agent)-preflight-failed")
          XCTFail("background preflight: no new completed reply")
          return
        }
        shot("\(agent)-preflight")
      } else {
      // long
      sent("LIST the files here", "\(agent) long")
      XCTAssertTrue(openLast(), "\(agent) long: no fold")
      let lsSteps = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_list'"))
      if lsSteps.count > 0 { lsSteps.element(boundBy: lsSteps.count - 1).tap() }
      let more = saying("Show all 500 lines").waitForExistence(timeout: 20)
      note("\(agent) long: 'Show all 500 lines': \(more)")
      XCTAssertTrue(more, "\(agent) long: the command's 500 lines are not offered")
      if !more { onScreen("\(agent) long", a) }
      if more { saying("Show all 500 lines").tap(); sleep(2) }
      let end = starting("line 500 of the long listing").firstMatch.exists || all().matching(NSPredicate(format: "label CONTAINS 'line 500 of the long listing'")).count > 0
      note("\(agent) long: the last line shown: \(end)")
      shot("\(agent)-long"); alive("\(agent) long")
      // image
      // This turn's picture: the image- button (not image-source-) of the turn this prompt plays. Ids end in -p<turn>,
      // with .n or #n for the record's own suffix (image-<uuid>-p42.1, image-call-p42, image-call|item-p42#1); an older
      // picture's button, shown again after a redraw or preloaded, is another turn's.
      // Read from one snapshot: a live query's elements, resolved one by one while the chat redraws, fail the test
      // ("Failed to get matching snapshot") instead of being read again.
      func turn(_ id: String) -> Int? {
        guard let r = id.range(of: "-p[0-9]+([.#][0-9]+)?$", options: .regularExpression) else { return nil }
        return Int(id[r].dropFirst(2).prefix { $0.isNumber })
      }
      if agent == "claude-code" { sent("HELLO, what is in this picture?", "\(agent) image") } else {
        sent("LOOK at shot.png", "\(agent) image")
        XCTAssertTrue(openLast(), "\(agent) image: no fold")
        let look = all().matching(NSPredicate(format: "identifier BEGINSWITH 'step-' AND identifier CONTAINS 'fake_look'"))
        if look.count > 0 { look.element(boundBy: look.count - 1).tap() }
      }
      let want = plays
      var found: [String] = []
      let pics = until(20) {
        guard let s = chatSnapshot() else { return false }
        found = s.ids.filter { $0.hasPrefix("image-") && !$0.hasPrefix("image-source-") && turn($0) == want }.sorted()
        return !found.isEmpty
      }
      note("\(agent) image: pictures of turn \(want): \(found)")
      XCTAssertTrue(pics, "\(agent) image: no picture showed")
      shot("\(agent)-image"); alive("\(agent) image")
      // scroll: the chat holds the last 30 turns; the oldest is numbered 01x
      var top = false
      for _ in 0..<40 { if starting("01").firstMatch.exists && starting("01").firstMatch.isHittable { top = true; break }; el("chat-view").swipeDown() }
      note("\(agent) scroll: the oldest turns reached: \(top)")
      XCTAssertTrue(top, "\(agent) scroll: did not reach the oldest turns")
      shot("\(agent)-top"); alive("\(agent) scroll")
      // Switching views must restore the turn at the same viewport offset, not merely keep it in the tree.
      let anchor = starting("01").allElementsBoundByIndex.first { $0.isHittable }
      guard let anchor = anchor else { XCTFail("\(agent): no reading anchor"); return }
      let anchorLabel = anchor.label
      let anchorY = anchor.frame.minY - el("chat-scroll").frame.minY
      el("show-terminal").tap(); el("show-chat").tap()
      let restored = until(15) {
        let row = saying(anchorLabel)
        return row.exists && abs(row.frame.minY - el("chat-scroll").frame.minY - anchorY) < 3
      }
      note("\(agent) reading anchor restored: \(restored)")
      XCTAssertTrue(restored, "\(agent): switching to the terminal and back lost the reading position")
      shot("\(agent)-reading-restored")
      var bottom = false
      for _ in 0..<40 { if saying("LOOK at shot.png").isHittable || saying("HELLO, what is in this picture?").isHittable { bottom = true; break }; el("chat-view").swipeUp() }
      note("\(agent) scroll: back to the newest: \(bottom)")
      XCTAssertTrue(bottom, "\(agent) scroll: did not get back to the newest turn")
      }
      // away
      let stress = cycles > 0 && n == 0
      let waits = stress ? Array(repeating: UInt32(60), count: cycles) : (n == 0 ? away : Array(away.prefix(1)))
      for (index, secs) in waits.enumerated() {
        let cycle = index + 1
        if stress {
          // Leave while the fake agent is still writing, on both the before and after builds.
          say("EDIT before background cycle \(cycle)")
          note("away-cycle start \(cycle)/\(cycles) seconds=60")
        }
        // This investigation cuts the original socket inside the first 60-second background interval.
        let backgroundCut = stress && cycle == 1 && !stopOnResume
        if backgroundCut {
          guard ctlDo("arm-cut", "cut-armed") else { XCTFail("background cut: no live socket before Home"); return }
        }
        XCUIDevice.shared.press(.home)
        let resumeAt = Date().addingTimeInterval(Double(secs))
        if backgroundCut {
          note("away-cut Home; stopping at 45 seconds")
          sleep(45)
          guard ctlDo("stop", "stopped") else { XCTFail("background cut: Companion did not stop"); return }
          let closed = FileManager.default.fileExists(atPath: "\(ctl)/cut-disconnected")
          note("away-cut original socket disconnected in serve log: \(closed)")
          guard closed else { XCTFail("background cut: original socket has no disconnect record"); return }
          guard ctlDo("start", "started") else { XCTFail("background cut: Companion did not start"); return }
          guard Date() < resumeAt else { XCTFail("background cut: stop/start exceeded the 60-second interval"); return }
          note("away-cut Companion restarted before resume")
          for file in ["stopped", "started", "cut-armed", "cut-socket", "cut-disconnected"] {
            try? FileManager.default.removeItem(atPath: "\(ctl)/\(file)")
          }
          Thread.sleep(until: resumeAt)
        } else { sleep(secs) }
        if stress { note("away-cycle resume \(cycle)/\(cycles) seconds=60") }
        // The detector's negative control: the old conversation stays visible, but no new reply can arrive.
        if stress && stopOnResume && cycle == 1 {
          guard ctlDo("stop", "stopped") else { XCTFail("negative control could not stop Companion"); return }
          note("away-control companion stopped")
        }
        app.activate()
        // app.state can still say background (3) a moment after activate() on iOS 27 with the chat already shown.
        _ = app.wait(for: .runningForeground, timeout: 10)
        let back = el("chat-view").waitForExistence(timeout: 15)
        alive("\(agent) away \(secs) s")
        note("\(agent) away \(secs) s: chat there: \(back), says it can't reach the computer 15 s later: \(el("chat-offline").exists)")
        XCTAssertTrue(back, "\(agent) away \(secs) s: the chat is gone")
        if stress {
          var oldReplies: Set<String> = []
          guard until(10, {
            guard let beforeReply = chatSnapshot() else { return false }
            oldReplies = Set(beforeReply.ids.filter { $0.hasPrefix("reply-") })
            return !oldReplies.isEmpty
          }) else { XCTFail("away cycle \(cycle): cannot read existing replies before sending"); return }
          let prompt = "EDIT after 60 s away cycle \(cycle)"
          say(prompt)
          let completed = until(60) {
            guard let current = chatSnapshot() else { return false }
            return completedReply(current, oldReplies, prompt)
          }
          guard completed else {
            shot("\(agent)-away60-cycle\(cycle)-failed")
            onScreen("\(agent) away cycle \(cycle)", a)
            XCTFail("away cycle \(cycle): no new completed reply after resuming")
            return
          }
          note("away-cycle pass \(cycle)/\(cycles) seconds=60")
          if cycle % 10 == 0 || cycle == cycles { shot("\(agent)-away60-cycle\(cycle)") }
        } else {
          shot("\(agent)-away\(secs)")
          sent("EDIT after \(secs) s away", "\(agent) away \(secs) s", 60)
        }
      }
      if awayOnly { return }
      // cut
      if ctlDo("stop", "stopped") {
        let off = el("chat-offline").waitForExistence(timeout: 45)
        let kept = saying(stress ? "EDIT after 60 s away cycle \(cycles)" : "EDIT after \(away.first ?? 0) s away").exists
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
