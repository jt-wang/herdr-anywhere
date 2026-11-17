// Executes the real SpaceOpener against scripted UI outcomes, without a simulator.
// This checks the test's failure policy, not UIKit's gesture recognition.
import Foundation
import CoreGraphics

var failures: [String] = []
func XCTFail(_ message: String) { failures.append(message) }
func XCTAssertTrue(_ value: @autoclosure () -> Bool, _ message: String) { if !value() { XCTFail(message) } }
func XCTAssertFalse(_ value: @autoclosure () -> Bool, _ message: String) { if value() { XCTFail(message) } }
extension NSCoder { static func string(for rect: CGRect) -> String { "row" } }

enum Scene { case home, space, menu, exited, gone, coveredSpace }
final class XCUIApplication {
  enum State { case runningForeground, notRunning }
  enum Kind { case any }
  var scene = Scene.home
  let first: Scene, second: Scene
  var taps = 0
  init(_ first: Scene, _ second: Scene) { self.first = first; self.second = second }
  var state: State { scene == .exited ? .notRunning : .runningForeground }
  var debugDescription: String { "scene=\(scene) taps=\(taps)" }
  func descendants(matching: Kind) -> Query { Query(app: self) }
  func coordinate(withNormalizedOffset: CGVector) -> XCUIElement { XCUIElement(app: self, id: "beside") }
  func tap() { taps += 1; scene = taps == 1 ? first : second }
}
struct Query {
  let app: XCUIApplication
  subscript(_ id: String) -> XCUIElement { XCUIElement(app: app, id: id) }
}
struct XCUIElement {
  let app: XCUIApplication, id: String
  var firstMatch: Self { self }
  var frame: CGRect { CGRect(x: 0, y: 100, width: 300, height: 44) }
  var exists: Bool {
    switch id {
    case "space-more": return app.scene == .space || app.scene == .coveredSpace
    case "menu": return app.scene == .menu || app.scene == .coveredSpace
    default: return app.scene == .home || app.scene == .menu
    }
  }
  func waitForExistence(timeout: Double) -> Bool { exists }
  func tap() { app.tap() }
}
func drawnPixels(in rect: CGRect, of app: XCUIApplication) -> Int { 100 }

let cases: [(String, Scene, Scene, Bool, Bool, Int, String)] = [
  // name, first outcome, recovery outcome, intentional miss, terminal shown, taps, required note
  ("opened", .space, .space, false, true, 1, "first space tap: opened"),
  ("known-home", .home, .space, false, true, 2, "KNOWN ISSUE"),
  ("menu", .menu, .space, false, true, 1, "long-press menu"),
  ("covered-space", .coveredSpace, .space, false, true, 1, "long-press menu"),
  ("exited", .exited, .space, false, false, 1, "process exited"),
  ("gone", .gone, .space, false, false, 1, "neither home nor space"),
  ("never-opens", .home, .home, false, false, 2, "KNOWN ISSUE"),
  ("recovery-menu", .home, .menu, false, false, 2, "long-press menu"),
  ("recovery-exit", .home, .exited, false, false, 2, "process exited"),
  ("missing-terminal", .space, .space, false, false, 1, "first space tap: opened"),
  ("intentional-miss", .home, .space, true, true, 2, "self-test"),
  ("scroll-intentional-miss", .home, .space, false, true, 2, "self-test"),
  ("exit-after-open", .space, .space, false, true, 1, "process exited"),
]
let root = CommandLine.arguments[1]
var broken = 0
for (name, first, second, miss, shown, taps, marker) in cases {
  if miss { setenv("MISS_SPACE", "1", 1) } else { unsetenv("MISS_SPACE") }
  failures = []
  var notes: [String] = []
  let app = XCUIApplication(first, second)
  let out = root + "/" + name
  let opener = SpaceOpener(app: app, out: out, note: { notes.append($0) })
  let again = name == "scroll-intentional-miss" ? opener.open("demo", missFirst: true) : opener.open("demo")
  if name == "exit-after-open" { app.scene = .exited }
  opener.finish(again: again, shown: shown)
  let shouldFail = name != "opened" && name != "known-home"
  var errors: [String] = []
  if failures.isEmpty == shouldFail { errors.append("failure policy: \(failures)") }
  if app.taps != taps { errors.append("taps \(app.taps), expected \(taps)") }
  if !notes.contains(where: { $0.contains(marker) }) { errors.append("missing note: \(marker)") }
  if name == "known-home" {
    let saved = (try? String(contentsOfFile: out + "-3s-after-space-tap.txt", encoding: .utf8)) ?? ""
    if !saved.contains("scene=home taps=1") { errors.append("first failure snapshot was lost") }
  }
  if errors.isEmpty { print("PASS: \(name)") }
  else { broken += 1; print("FAIL: \(name): \(errors.joined(separator: "; "))") }
}
exit(broken == 0 ? 0 : 1)
