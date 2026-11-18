import Foundation

// Only the app's diagnostic recorder writes here. Three 256 KiB generations survive process termination.
// Never pass payloads, URLs, error descriptions, view labels or text to this recorder.
final class DiagnosticLog {
  static let generationBytes = 256 * 1024
  static let generations = 3
  let session = UUID().uuidString
  private let directory: URL
  private let defaults: UserDefaults
  private let lock = NSRecursiveLock()
  private var sequence = 0
  private var interaction = ""
  private(set) var enabled: Bool
  private(set) var categories: [String: Bool]
  private(set) var failure: String?

  init(directory: URL, defaults: UserDefaults) {
    self.directory = directory
    self.defaults = defaults
    enabled = defaults.bool(forKey: "diagnostics.enabled")
    let saved = defaults.dictionary(forKey: "diagnostics.categories") ?? [:]
    categories = Dictionary(uniqueKeysWithValues: ["interaction", "performance", "network", "resources"].map { ($0, saved[$0] as? Bool ?? true) })
  }

  func setCategories(_ changes: [String: Bool]) {
    lock.lock(); defer { lock.unlock() }
    for (key, value) in changes where categories[key] != nil { categories[key] = value }
    defaults.set(categories, forKey: "diagnostics.categories")
  }

  func collects(_ category: String) -> Bool { enabled && categories[category] == true }

  static func category(_ event: String) -> String {
    if ["js.http-", "js.rpc-", "js.socket-", "js.tunnel-", "js.connection-", "network."].contains(where: event.hasPrefix) { return "network" }
    if event.hasPrefix("performance.") { return "performance" }
    if event.hasPrefix("resources.") { return "resources" }
    return "interaction"
  }

  func setEnabled(_ on: Bool) throws {
    lock.lock(); defer { lock.unlock() }
    if !on {
      if enabled { record("diagnostics.disabled") }
      enabled = false
      defaults.set(false, forKey: "diagnostics.enabled")
      return
    }
    if on {
      try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
      var folder = directory
      var values = URLResourceValues(); values.isExcludedFromBackup = true
      try folder.setResourceValues(values)
    }
    enabled = on
    defaults.set(on, forKey: "diagnostics.enabled")
    if on { failure = nil; record("diagnostics.enabled") }
    if let failure {
      enabled = false; defaults.set(false, forKey: "diagnostics.enabled")
      throw NSError(domain: "Diagnostics", code: 1, userInfo: [NSLocalizedDescriptionKey: failure])
    }
  }

  @discardableResult func beginInteraction() -> String {
    lock.lock(); defer { lock.unlock() }
    interaction = UUID().uuidString
    return interaction
  }

  func status() -> [String: Any] {
    lock.lock(); defer { lock.unlock() }
    return ["supported": true, "enabled": enabled, "categories": categories, "session": session, "interaction": interaction,
            "error": failure as Any? ?? NSNull()]
  }

  func record(_ event: String, _ fields: [String: String] = [:], at: Double? = nil, touch: String? = nil) {
    lock.lock(); defer { lock.unlock() }
    let category = Self.category(event)
    guard collects(category) else { return }
    sequence += 1
    var row: [String: Any] = ["seq": sequence, "event": event, "category": category, "sessionId": session,
                            "interactionId": touch ?? interaction, "at": Date().timeIntervalSince1970 * 1000,
                            "monoMs": ProcessInfo.processInfo.systemUptime * 1000, "fields": fields]
    if let at, at.isFinite { row["jsWallMs"] = at }
    do {
      try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
      var bytes = try JSONSerialization.data(withJSONObject: row, options: [.sortedKeys])
      bytes.append(10)
      guard bytes.count <= Self.generationBytes else { throw NSError(domain: "Diagnostics", code: 2) }
      let current = file(0)
      let size = (try? current.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0
      if size + bytes.count > Self.generationBytes {
        if FileManager.default.fileExists(atPath: file(Self.generations - 1).path) { try FileManager.default.removeItem(at: file(Self.generations - 1)) }
        for n in stride(from: Self.generations - 2, through: 0, by: -1) {
          if FileManager.default.fileExists(atPath: file(n).path) { try FileManager.default.moveItem(at: file(n), to: file(n + 1)) }
        }
      }
      if !FileManager.default.fileExists(atPath: current.path) {
        guard FileManager.default.createFile(atPath: current.path, contents: nil) else { throw NSError(domain: "Diagnostics", code: 3) }
      }
      let handle = try FileHandle(forWritingTo: current)
      defer { try? handle.close() }
      try handle.seekToEnd()
      try handle.write(contentsOf: bytes)
    } catch {
      failure = "Diagnostic storage failed (\((error as NSError).code))"
    }
  }

  func snapshot() throws -> [[String: Any]] {
    lock.lock(); defer { lock.unlock() }
    var events: [[String: Any]] = []
    for n in (0..<Self.generations).reversed() where FileManager.default.fileExists(atPath: file(n).path) {
      let bytes = try Data(contentsOf: file(n))
      for line in bytes.split(separator: 10) {
        // A process can die during the final write. Keep every complete earlier breadcrumb.
        if let row = try? JSONSerialization.jsonObject(with: Data(line)) as? [String: Any] { events.append(row) }
      }
    }
    if let failure { throw NSError(domain: "Diagnostics", code: 1, userInfo: [NSLocalizedDescriptionKey: failure]) }
    return events
  }

  func clear() throws {
    lock.lock(); defer { lock.unlock() }
    if FileManager.default.fileExists(atPath: directory.path) {
      for file in try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil) {
        try FileManager.default.removeItem(at: file)
      }
    }
    sequence = 0; interaction = ""; failure = nil
  }

  private func file(_ n: Int) -> URL { directory.appendingPathComponent("events-\(n).jsonl") }
}
