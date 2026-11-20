import XCTest

// A real app already paired to a real Companion through internal/testfake/attachproxy.
// TEST_RUNNER_PROXY/SPACE choose that isolated run; no mock store or shortened production RPC timeout.
final class AttachRetry: XCTestCase {
  func testTimeoutAndDisconnectRecoverInPlace() throws {
    continueAfterFailure = false
    let env = ProcessInfo.processInfo.environment
    let proxy = env["PROXY"] ?? "http://127.0.0.1:7782"
    let space = env["SPACE"] ?? "spattach"
    let app = XCUIApplication(bundleIdentifier: "io.jingtao.herdranywhere")
    let el = { (id: String) in app.descendants(matching: .any)[id].firstMatch }
    func control(_ mode: String) {
      let done = expectation(description: "fault " + mode)
      URLSession.shared.dataTask(with: URL(string: proxy + "/fault?mode=" + mode)!) { _, response, error in
        XCTAssertNil(error); XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 204); done.fulfill()
      }.resume()
      wait(for: [done], timeout: 10)
    }
    func until(_ ok: () -> Bool) -> Bool {
      let end = Date().addingTimeInterval(45)
      while Date() < end { if ok() { return true }; usleep(200_000) }
      return ok()
    }
    func requests() -> Int {
      let done = expectation(description: "proxy stats")
      var count = -1
      URLSession.shared.dataTask(with: URL(string: proxy + "/stats")!) { data, _, error in
        XCTAssertNil(error)
        if let data, let stats = try? JSONSerialization.jsonObject(with: data) as? [String: Int] { count = stats["attach_requests"] ?? -1 }
        done.fulfill()
      }.resume()
      wait(for: [done], timeout: 10)
      XCTAssertGreaterThanOrEqual(count, 0); return count
    }
    func shot(_ name: String) throws {
      let image = XCUIScreen.main.screenshot()
      let attachment = XCTAttachment(screenshot: image)
      attachment.name = "attach-" + name; attachment.lifetime = .keepAlways; add(attachment)
      if let prefix = env["SHOTS"] { try image.pngRepresentation.write(to: URL(fileURLWithPath: prefix + "-" + name + ".png")) }
    }
    func live() -> Bool { !el("terminal-state").exists && app.webViews.firstMatch.exists }
    control("pass")
    defer { control("pass") }
    app.activate()
    // Start from home, then stay inside this pane through every fault and recovery below.
    if el("back").exists { el("back").tap() }
    XCTAssertTrue(el("space-" + space).waitForExistence(timeout: 30)); el("space-" + space).tap()
    if el("show-terminal").waitForExistence(timeout: 3) { el("show-terminal").tap() }
    XCTAssertTrue(until(live)); try shot("live")
    control("hold"); control("drop")
    // The socket reconnects, but only the attach reply stays held beyond its real 10-second deadline.
    sleep(17)
    XCTAssertTrue(el("terminal-retry").waitForExistence(timeout: 35))
    XCTAssertTrue(el("terminal-state").label.contains("Reconnecting"))
    try shot("timeout")
    // No retry tap or navigation. Heartbeats and unrelated RPCs stayed live throughout the hold.
    control("pass")
    XCTAssertTrue(until(live), "attach did not automatically recover in the existing pane")
    try shot("recovered")
    let before = requests()
    control("drop")
    XCTAssertTrue(until { requests() > before })
    XCTAssertTrue(until(live), "socket reconnect did not reattach the existing pane")
    try shot("reconnected")
    // Returning from the background must not leave a failed viewer stuck either.
    control("hold"); control("drop")
    sleep(12)
    XCTAssertTrue(el("terminal-retry").waitForExistence(timeout: 35))
    XCUIDevice.shared.press(.home)
    control("pass"); app.activate()
    XCTAssertTrue(until(live), "foreground did not recover the existing pane")
    try shot("foreground")
  }
}
