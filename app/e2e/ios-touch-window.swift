// The iOS test app's window (e2e/ios-touch-app.sh appends this to the copy's Sources/App.swift and makes its window
// one; app/ itself is not changed). Each touch that begins, ends or is cancelled is written to the app's log as one
// "hm-touch …" line, which e2e/ios-touch-log.sh shows beside the test's tap: the view UIKit gave the touch and the chain
// of views above it; the gesture recognizers on that view, and every one the touch went to, each with its state; and
// the views a picture sheet puts on the window (an id starting "picture", or a scroll view that zooms) still on it,
// with whether the touched view is inside one. Written after UIKit has handled the event, so the states are its own.
// Each view and recognizer is named with its address, so a line at the lift tells the same object from a new one. From
// the touch's start to its end, every 50 ms (the run loop's common modes, so also while a list tracks the finger), a
// "phase=watch" line whenever what the touch's recognizers are or say has changed: when, and by whom, it was let go.
final class HMTouchWindow: UIWindow {
  /** A touch being watched. The view is held weakly: the watch must not keep a view the app let go of alive. */
  private final class Watch {
    let touch: UITouch, began = Date(), timer: Timer
    weak var view: UIView?
    var last: String
    init(_ touch: UITouch, _ timer: Timer) { self.touch = touch; self.timer = timer; view = touch.view; last = HMTouchWindow.watching(touch, view: touch.view) }
  }
  private var watched: [ObjectIdentifier: Watch] = [:]

  override func sendEvent(_ event: UIEvent) {
    super.sendEvent(event)
    guard event.type == .touches, let touches = event.allTouches else { return }
    for touch in touches where touch.phase == .began || touch.phase == .ended || touch.phase == .cancelled {
      let id = ObjectIdentifier(touch)
      var extra = ""
      if touch.phase == .began {
        watched[id]?.timer.invalidate()
        let timer = Timer(timeInterval: 0.05, target: self, selector: #selector(look(_:)), userInfo: id, repeats: true)
        RunLoop.main.add(timer, forMode: .common)
        watched[id] = Watch(touch, timer)
      } else if let w = watched.removeValue(forKey: id) {
        w.timer.invalidate()
        extra = " +\(Int(Date().timeIntervalSince(w.began) * 1000))ms began-view=\(HMTouchWindow.name(w.view)) began-view-in-window=\(HMTouchWindow.inWindow(w.view))"
      }
      NSLog("%@", HMTouchWindow.line(touch, in: self) + extra)
    }
  }

  @objc private func look(_ timer: Timer) {
    guard let id = timer.userInfo as? ObjectIdentifier, let w = watched[id] else { timer.invalidate(); return }
    let ms = Int(Date().timeIntervalSince(w.began) * 1000)
    if ms > 10_000 { timer.invalidate(); watched.removeValue(forKey: id); return }
    let now = HMTouchWindow.watching(w.touch, view: w.view)
    guard now != w.last else { return }
    w.last = now
    NSLog("%@", "hm-touch phase=watch +\(ms)ms \(now)")
  }

  /** What a look in between says: whether the view the touch began on is still on the window, and its recognizers. */
  static func watching(_ touch: UITouch, view: UIView?) -> String {
    "view-in-window=\(inWindow(view)) touch-view=\(name(touch.view)) touch-gestures=\(gestures(touch.gestureRecognizers))"
      + " view-gestures=\(gestures(view?.gestureRecognizers))"
  }

  static func inWindow(_ view: UIView?) -> String { view == nil ? "gone" : view?.window != nil ? "yes" : "no" }

  static func address(_ o: AnyObject) -> String { "@\(Unmanaged.passUnretained(o).toOpaque())" }

  static func name(_ view: UIView?) -> String {
    guard let view else { return "none" }
    var s = String(describing: type(of: view)) + address(view)
    if let id = view.accessibilityIdentifier, !id.isEmpty { s += "#" + id }
    if view.isHidden { s += "(hidden)" }
    if view.alpha < 0.01 { s += "(clear)" }
    if !view.isUserInteractionEnabled { s += "(no touch)" }
    return s
  }

  static func state(_ g: UIGestureRecognizer) -> String {
    switch g.state {
    case .possible: return "possible"
    case .began: return "began"
    case .changed: return "changed"
    case .ended: return "ended"
    case .cancelled: return "cancelled"
    case .failed: return "failed"
    @unknown default: return "unknown"
    }
  }

  static func gestures(_ list: [UIGestureRecognizer]?) -> String {
    "[" + (list ?? []).map { g in
      "\(type(of: g))\(address(g)) \(state(g))\(g.isEnabled ? "" : " off")\(g.cancelsTouchesInView ? " cancels" : "") on \(name(g.view))"
    }.joined(separator: ", ") + "]"
  }

  /** Views a picture sheet puts on the window: its ids start "picture", and its zoom is a scroll view that pinches. */
  static func pictureViews(in root: UIView) -> [UIView] {
    var found: [UIView] = [], queue: [UIView] = [root]
    while let v = queue.popLast() {
      let id = v.accessibilityIdentifier ?? ""
      if id.hasPrefix("picture") || ((v as? UIScrollView)?.pinchGestureRecognizer != nil) { found.append(v) }
      queue.append(contentsOf: v.subviews)
    }
    return found
  }

  static func line(_ touch: UITouch, in window: UIWindow) -> String {
    let phase = touch.phase == .began ? "began" : touch.phase == .ended ? "ended" : "cancelled"
    let at = touch.location(in: window)
    var chain: [UIView] = []
    var up = touch.view
    while let v = up, chain.count < 40 { chain.append(v); up = v.superview }
    let pictures = pictureViews(in: window)
    let inPicture = chain.contains { v in pictures.contains { $0 === v } }
    let pictureList = pictures.map { v -> String in
      let f = v.convert(v.bounds, to: window)
      return "\(name(v)) at (\(Int(f.minX)),\(Int(f.minY)) \(Int(f.width))x\(Int(f.height)))"
    }
    return "hm-touch phase=\(phase) at=(\(Int(at.x)),\(Int(at.y))) view=\(name(touch.view)) in-picture=\(inPicture ? "yes" : "no")"
      + " view-gestures=\(gestures(touch.view?.gestureRecognizers)) touch-gestures=\(gestures(touch.gestureRecognizers))"
      + " picture-views=[\(pictureList.joined(separator: ", "))] chain=\(chain.map { name($0) }.joined(separator: " < "))"
  }
}
