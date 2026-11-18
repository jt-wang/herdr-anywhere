import NativeShell
import Network
import UIKit

@MainActor
final class Diagnostics: NSObject {
  static let shared = Diagnostics()
  let log: DiagnosticLog
  private var observers: [NSObjectProtocol] = []
  private var gestures = NSHashTable<UIGestureRecognizer>.weakObjects()
  private var touches: [ObjectIdentifier: String] = [:]
  private var viewIDs: [String: String] = [:]
  private var share: UIActivityViewController?
  private var timer: Timer?
  private var pathMonitor: NWPathMonitor?
  private var lastTick = 0.0
  private var lastResource = 0.0
  private let defaults = UserDefaults.standard

  override init() {
    let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
    log = DiagnosticLog(directory: base.appendingPathComponent("HerdrDiagnostics"), defaults: .standard)
    super.init()
    log.record("process.start", ["previousRun": defaults.bool(forKey: "diagnostics.running") ? "interrupted-or-suspended" : "not-recorded"])
    if log.enabled { defaults.set(true, forKey: "diagnostics.running") }
    for (notification, event) in [(UIApplication.didBecomeActiveNotification, "lifecycle.active"),
                                   (UIApplication.willResignActiveNotification, "lifecycle.inactive"),
                                   (UIApplication.didEnterBackgroundNotification, "lifecycle.background"),
                                   (UIApplication.willEnterForegroundNotification, "lifecycle.foreground"),
                                   (UIApplication.didReceiveMemoryWarningNotification, "lifecycle.memory-warning"),
                                   (UIApplication.willTerminateNotification, "lifecycle.terminate")] {
      observers.append(NotificationCenter.default.addObserver(forName: notification, object: nil, queue: .main) { [weak self] _ in
        MainActor.assumeIsolated {
          self?.log.record(event)
          if event == "lifecycle.terminate" { self?.defaults.set(false, forKey: "diagnostics.running") }
          self?.configureMetrics()
        }
      })
    }
    for notification in [UIDevice.batteryLevelDidChangeNotification, UIDevice.batteryStateDidChangeNotification, ProcessInfo.thermalStateDidChangeNotification] {
      observers.append(NotificationCenter.default.addObserver(forName: notification, object: nil, queue: .main) { [weak self] _ in
        MainActor.assumeIsolated { self?.resources() }
      })
    }
  }

  func attach(_ app: NshAppView, presenter: UIViewController) {
    app.handle("page.diagnostics.status") { [weak self] _, reply in
      Task { @MainActor in reply.resolve(self?.log.status()) }
    }
    app.handle("page.diagnostics.set") { [weak self] data, reply in
      Task { @MainActor in
        guard let self, let data = data as? [String: Any] else { reply.reject("Invalid diagnostic switch"); return }
        defer {
          if !self.log.enabled { self.removeGestureObservers() }
          self.configureMetrics()
        }
        do {
          if let categories = data["categories"] as? [String: Bool] { self.log.setCategories(categories) }
          let on = data["enabled"] as? Bool ?? self.log.enabled
          try self.log.setEnabled(on)
          if on { self.defaults.set(true, forKey: "diagnostics.running"); self.log.record("device.metadata", self.metadata()) }
          reply.resolve(self.log.status())
        } catch { reply.resolve(self.log.status()) }
      }
    }
    app.handle("page.diagnostics.event") { [weak self] data, reply in
      Task { @MainActor in
        if let self, self.log.enabled, let a = data as? [String: Any], let event = a["event"] as? String,
           let fields = Self.fields(event, a["fields"] as? [String: String] ?? [:]) {
          self.log.record("js." + event, fields, at: a["at"] as? Double)
        }
        reply.resolve(nil)
      }
    }
    app.handle("page.diagnostics.clear") { [weak self] _, reply in
      Task { @MainActor in
        do { try self?.log.clear(); self?.viewIDs.removeAll(); self?.removeExports(); reply.resolve(nil) }
        catch { reply.reject("Could not clear diagnostic files") }
      }
    }
    app.handle("page.diagnostics.export") { [weak self, weak presenter] _, reply in
      Task { @MainActor in
        guard let self, let presenter else { reply.reject("No share presenter"); return }
        self.export(from: presenter) { shared in reply.resolve(["shared": shared]) }
      }
    }
    log.record("runtime.attached", metadata())
    configureMetrics()
  }

  func touch(_ event: UIEvent, stage: String) {
    guard log.collects("interaction"), event.type == .touches else { return }
    for touch in event.allTouches ?? [] where [.began, .ended, .cancelled].contains(touch.phase) {
      let key = ObjectIdentifier(touch)
      if stage == "before", touch.phase == .began { touches[key] = log.beginInteraction() }
      let phase = touch.phase == .began ? "began" : touch.phase == .ended ? "ended" : "cancelled"
      var chain: [String] = []
      var view = touch.view
      while let current = view {
        let states = (current.gestureRecognizers ?? []).map { recognizer -> String in
          if !gestures.contains(recognizer) {
            gestures.add(recognizer)
            // Observation only: never change delegate, state, delays, cancellation or recognition rules.
            recognizer.addTarget(self, action: #selector(recognized(_:)))
          }
          return "\(String(describing: type(of: recognizer))):\(recognizer.state.rawValue)"
        }.joined(separator: ",")
        chain.append("\(String(describing: type(of: current)))#\(viewID(current.accessibilityIdentifier))@\(current.tag)[\(states)]")
        view = current.superview
      }
      log.record("touch.\(phase).\(stage)", ["hit": chain.prefix(24).joined(separator: ">"), "touchType": String(touch.type.rawValue)], touch: touches[key])
      if stage == "after", touch.phase != .began { touches.removeValue(forKey: key) }
    }
  }

  @objc private func recognized(_ recognizer: UIGestureRecognizer) {
    guard log.enabled else { return }
    log.record("gesture.state", ["recognizer": String(describing: type(of: recognizer)),
                                 "state": String(recognizer.state.rawValue), "view": viewID(recognizer.view?.accessibilityIdentifier)])
  }

  private func removeGestureObservers() {
    for gesture in gestures.allObjects { gesture.removeTarget(self, action: #selector(recognized(_:))) }
    gestures.removeAllObjects(); touches.removeAll()
  }

  private func metadata() -> [String: String] {
    ["version": Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown",
     "build": Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "unknown",
     "os": UIDevice.current.systemVersion, "device": UIDevice.current.model,
     "runtime": "NativeShell-0.3.0", "sdk": Bundle.main.object(forInfoDictionaryKey: "DTSDKName") as? String ?? "unknown",
     "environment": ProcessInfo.processInfo.environment["SIMULATOR_UDID"] == nil ? "device" : "simulator"]
  }

  private func viewID(_ id: String?) -> String {
    guard let id else { return "-" }
    let fixed = ["back", "settings", "new", "switcher", "pane-header", "compose-input", "compose-send", "status-line", "show-terminal", "attach"]
    if fixed.contains(id) { return id }
    // IDs are random, not hashes of project/session identifiers. Raw keys never leave this bounded RAM map.
    if viewIDs.count > 512 { viewIDs.removeAll() }
    let random = viewIDs[id] ?? UUID().uuidString
    viewIDs[id] = random
    for prefix in ["space-agent-", "space-", "terminal-", "chat-", "diagnostic-"] where id.hasPrefix(prefix) { return prefix + random }
    return "view-" + random
  }

  // An allowlist at the native boundary: even accidental JS diagnostics cannot export body/headers/URLs.
  static func fields(_ event: String, _ input: [String: String]) -> [String: String]? {
    let request = ["request", "elapsed", "status", "timeout", "requestBytes", "responseBytes", "result"]
    let rules: [String: [String]] = [
      "app-start": [], "ui-handler": ["action"], "ui-open-space": ["step"], "ui-nav": ["route", "depth"], "ui-page": ["route"],
      "http-start": request + ["endpoint", "method"], "http-end": request + ["endpoint", "method"],
      "rpc-start": request + ["method", "rpcId"], "rpc-end": request + ["method", "rpcId"],
      "socket-start": request + ["enc"], "socket-open": request, "socket-end": request + ["reason"],
      "tunnel-start": request, "tunnel-end": request,
      "connection-connect": ["attempt", "step"], "connection-connect-catch": ["attempt", "name"],
      "connection-lost": ["wanted", "retrying", "attempt"], "connection-step": ["attempt", "step"],
      "connection-return": ["attempt", "reason"], "connection-retry-fired": ["attempt", "wanted", "hasClient"],
      "connection-retry-scheduled": ["attempt", "delay"], "connection-error": ["attempt", "step", "name"]]
    guard let keys = rules[event] else { return nil }
    var result: [String: String] = [:]
    let numbers = ["attempt", "delay", "elapsed", "status", "timeout", "depth", "rpcId", "requestBytes", "responseBytes"]
    let booleans = ["wanted", "retrying", "hasClient", "agent", "chat", "front", "cached"]
    let words = Set(["start", "hello-before", "hello-after", "subscribe-before", "subscribe-after", "ready", "idle", "connecting", "reconnecting", "unpaired", "closed", "open", "home", "space", "settings", "chat", "terminal", "progress", "task", "pair", "background", "foreground", "true", "false", "ok", "error", "timeout", "cancelled", "network", "unknown", "abort", "Error", "TypeError", "RangeError", "SyntaxError", "ReferenceError", "URIError", "EvalError", "AbortError", "TimeoutError", "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "enter", "entry", "selected", "popped", "pushed", "already-open", "open-space", "open-terminal", "disconnected", "same-attempt", "not-wanted", "retry-pending", "stop-loops-before", "stop-loops-after", "clear-client-before", "clear-client-after", "conn-before", "conn-after", "lost-why-before", "lost-why-after", "error-before", "error-after", "bin", "b64", "no-core", "binary-unsupported"])
    let methods = Set(["hello", "terminal.run", "terminal.list", "terminal.kill", "terminal.label", "terminal.attach", "terminal.keep", "terminal.get", "terminal.send", "terminal.keys", "terminal.read", "doc.get", "doc.set", "events.subscribe", "machine.list", "machine.exec", "server.status"])
    let endpoints = Set(["/api/pair", "/api/info", "/api/folders", "/api/chat/:terminal", "/api/chat/:terminal/steps/:id", "/api/chat/:terminal/images/:id", "/api/chat-setups", "/api/chat-setups/:agent", "/api/file", "/api/file/check", "/api/file/link", "/api/files", "/api/image", "/api/image/check", "/api/update", "/api/update/check", "/api/update/checks", "/api/update/auto"])
    for key in keys {
      guard let value = input[key] else { continue }
      if numbers.contains(key), value.range(of: "^(0|[1-9][0-9]{0,14})$", options: .regularExpression) != nil { result[key] = value }
      else if booleans.contains(key), ["true", "false"].contains(value) { result[key] = value }
      else if key == "request", value.range(of: "^d[1-9][0-9]{0,14}$", options: .regularExpression) != nil { result[key] = value }
      else if ["state", "from", "to", "step", "reason", "name", "page", "route", "result", "method", "action", "enc"].contains(key) { result[key] = words.contains(value) || (key == "method" && methods.contains(value)) ? value : "other" }
      else if key == "endpoint" { result[key] = endpoints.contains(value) ? value : "other" }
    }
    return result
  }

  private func configureMetrics() {
    timer?.invalidate(); timer = nil
    pathMonitor?.cancel(); pathMonitor = nil
    let active = UIApplication.shared.applicationState == .active
    UIDevice.current.isBatteryMonitoringEnabled = active && log.collects("resources")
    guard active, log.enabled else { return }
    if log.collects("performance") || log.collects("resources") {
      lastTick = ProcessInfo.processInfo.systemUptime; lastResource = 0
      timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
        MainActor.assumeIsolated {
          guard let self else { return }
          let now = ProcessInfo.processInfo.systemUptime
          if self.log.collects("performance"), now - self.lastTick > 1.5 {
            self.log.record("performance.main-loop-gap", ["durationMs": String(Int((now - self.lastTick) * 1000)), "sampleIntervalMs": "1000"])
          }
          self.lastTick = now
          if now - self.lastResource >= 60 { self.resources(); self.lastResource = now }
        }
      }
      timer?.tolerance = 0.25
    }
    if log.collects("network") {
      let monitor = NWPathMonitor()
      monitor.pathUpdateHandler = { [weak self] path in
        let kind = path.status != .satisfied ? "offline" : path.usesInterfaceType(.wifi) ? "wifi" : path.usesInterfaceType(.cellular) ? "cellular" : path.usesInterfaceType(.wiredEthernet) ? "wired" : "other"
        Task { @MainActor in self?.log.record("network.interface", ["connectionType": kind]) }
      }
      pathMonitor = monitor
      monitor.start(queue: DispatchQueue(label: "herdr.diagnostics.path"))
    }
  }

  private func resources() {
    guard log.collects("resources"), UIApplication.shared.applicationState == .active else { return }
    var info = mach_task_basic_info()
    var count = mach_msg_type_number_t(MemoryLayout<mach_task_basic_info>.size / MemoryLayout<natural_t>.size)
    let result = withUnsafeMutablePointer(to: &info) { pointer in
      pointer.withMemoryRebound(to: integer_t.self, capacity: Int(count)) { task_info(mach_task_self_, task_flavor_t(MACH_TASK_BASIC_INFO), $0, &count) }
    }
    var usage = rusage(); getrusage(RUSAGE_SELF, &usage)
    var values = ["batteryLevel": String(UIDevice.current.batteryLevel), "chargingState": String(UIDevice.current.batteryState.rawValue),
                  "thermalState": String(ProcessInfo.processInfo.thermalState.rawValue), "cpuUserMs": String(usage.ru_utime.tv_sec * 1000 + Int(usage.ru_utime.tv_usec) / 1000),
                  "cpuSystemMs": String(usage.ru_stime.tv_sec * 1000 + Int(usage.ru_stime.tv_usec) / 1000), "appActive": "true"]
    if result == KERN_SUCCESS { values["residentBytes"] = String(info.resident_size) }
    log.record("resources.snapshot", values)
  }

  private var exports: URL { FileManager.default.temporaryDirectory.appendingPathComponent("HerdrDiagnosticExports") }
  private func removeExports() { try? FileManager.default.removeItem(at: exports) }

  private func export(from presenter: UIViewController, completion: @escaping (Bool) -> Void) {
    guard presenter.presentedViewController == nil, share == nil else { completion(false); return }
    let alert = UIAlertController(title: "选择时间范围 / Time range", message: "开启 → 复现一次 → 导出。仅分享本 App 诊断，无聊天/终端正文，无 backboardd 系统日志。", preferredStyle: .alert)
    alert.addAction(UIAlertAction(title: "取消 / Cancel", style: .cancel) { _ in completion(false) })
    for (title, seconds) in [("最近15分钟 / Last 15 min", 900.0), ("最近1小时 / Last hour", 3600.0), ("全部保留记录 / All retained", 0.0)] {
      alert.addAction(UIAlertAction(title: title, style: .default) { [weak self, weak presenter] _ in
        guard let self, let presenter else { completion(false); return }
        let notes = UIAlertController(title: "可选说明 / Optional note", message: "仅使用以下固定选项，不收集输入文字。/ No free text is collected.", preferredStyle: .alert)
        notes.addAction(UIAlertAction(title: "取消 / Cancel", style: .cancel) { _ in completion(false) })
        for (label, note) in [("不添加 / None", "none"), ("首击无响应 / First tap", "first-tap"), ("内容慢 / Slow content", "slow-content"), ("连接异常 / Connection", "connection")] {
          notes.addAction(UIAlertAction(title: label, style: .default) { [weak self, weak presenter] _ in
            guard let self, let presenter else { completion(false); return }
            self.preview(from: presenter, seconds: seconds, note: note, completion: completion)
          })
        }
        presenter.present(notes, animated: true)
      })
    }
    presenter.present(alert, animated: true)
  }

  private func preview(from presenter: UIViewController, seconds: Double, note: String, completion: @escaping (Bool) -> Void) {
    do {
      let until = Date().timeIntervalSince1970 * 1000
      let categories = log.categories.filter { $0.value }.map(\.key).sorted()
      let events = try log.snapshot().filter { row in
        categories.contains(row["category"] as? String ?? "") && (seconds == 0 || (row["at"] as? Double ?? 0) >= until - seconds * 1000)
      }
      let first = events.first?["at"] as? Double
      let last = events.last?["at"] as? Double
      let from = seconds == 0 ? first ?? until : until - seconds * 1000
      let formatter = ISO8601DateFormatter()
      let iso: (Double) -> String = { formatter.string(from: Date(timeIntervalSince1970: $0 / 1000)) }
      let preview = UIAlertController(title: "分享前预览 / Review before sharing", message: "\(categories.joined(separator: ", "))\n\(iso(from)) → \(iso(until))\n\(events.count) events · \(note)\n无正文、输入、凭据、URL或路径。资源数据不能精确归因 App 耗电。/ No content, input, credentials, URLs or paths. Battery readings cannot attribute app energy usage.", preferredStyle: .alert)
      preview.addAction(UIAlertAction(title: "取消 / Cancel", style: .cancel) { _ in completion(false) })
      preview.addAction(UIAlertAction(title: "分享 / Share", style: .default) { [weak self, weak presenter] _ in
      guard let self, let presenter else { completion(false); return }
      do {
        var ids: [String: String] = [:]
        func localID(_ id: String) -> String {
          if id.isEmpty { return "" }
          if let existing = ids[id] { return existing }
          let value = UUID().uuidString; ids[id] = value; return value
        }
        let exported = events.map { original -> [String: Any] in
          var row = original
          row["sessionId"] = localID(original["sessionId"] as? String ?? "")
          row["interactionId"] = localID(original["interactionId"] as? String ?? "")
          var fields = original["fields"] as? [String: String] ?? [:]
          if let request = fields["request"] { fields["request"] = localID("\(original["sessionId"] ?? "")/\(request)") }
          // Native hit chains only contain classes, enums and recorder-generated random UUIDs.
          for key in ["hit", "view"] {
            if var value = fields[key] {
              let regex = try! NSRegularExpression(pattern: "[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}")
              for match in regex.matches(in: value, range: NSRange(value.startIndex..., in: value)).reversed() {
                if let range = Range(match.range, in: value) { value.replaceSubrange(range, with: localID(String(value[range]))) }
              }
              fields[key] = value
            }
          }
          row["fields"] = fields; return row
        }
        var app = self.metadata(); app["platform"] = "ios"; app["deviceClass"] = app.removeValue(forKey: "device")
        let report: [String: Any] = ["schemaVersion": 1, "exportId": UUID().uuidString, "app": app,
                                   "range": ["from": from, "to": until], "actualRange": ["from": first as Any? ?? NSNull(), "to": last as Any? ?? NSNull()],
                                   "categories": categories, "reproduction": ["time": until, "note": note], "events": exported,
                                   "limits": "App-only breadcrumbs; no backboardd/system log access. An interrupted run is not proof of a crash. Battery readings cannot attribute app energy usage. No automatic upload."]
        self.removeExports()
        try FileManager.default.createDirectory(at: self.exports, withIntermediateDirectories: true)
        let url = self.exports.appendingPathComponent("Herdr-diagnostics.json")
        try JSONSerialization.data(withJSONObject: report, options: [.prettyPrinted, .sortedKeys]).write(to: url, options: .atomic)
        let sheet = UIActivityViewController(activityItems: [url], applicationActivities: nil)
        sheet.popoverPresentationController?.sourceView = presenter.view
        sheet.popoverPresentationController?.sourceRect = CGRect(x: presenter.view.bounds.midX, y: presenter.view.bounds.midY, width: 1, height: 1)
        sheet.completionWithItemsHandler = { [weak self] _, shared, _, _ in self?.share = nil; completion(shared) }
        self.share = sheet
        presenter.present(sheet, animated: true)
      } catch {
        let error = UIAlertController(title: "无法导出 / Export failed", message: "请检查设备剩余空间后重试。/ Check available device storage.", preferredStyle: .alert)
        error.addAction(UIAlertAction(title: "OK", style: .default))
        presenter.present(error, animated: true); completion(false)
      }
      })
      presenter.present(preview, animated: true)
    } catch {
      let alert = UIAlertController(title: "无法导出 / Export failed", message: "诊断存储不可用，请清空后重试。/ Diagnostic storage unavailable. Clear saved diagnostics and retry.", preferredStyle: .alert)
      alert.addAction(UIAlertAction(title: "OK", style: .default))
      presenter.present(alert, animated: true); completion(false)
    }
  }
}
