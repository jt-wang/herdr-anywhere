import Foundation

@main struct DiagnosticLogTests {
  static func main() throws {
    let suite = "herdr-diagnostic-consent-\(UUID().uuidString)"
    let defaults = UserDefaults(suiteName: suite)!
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(suite)
    defer {
      defaults.removePersistentDomain(forName: suite)
      try? FileManager.default.removeItem(at: root)
    }
    let names = ["interaction", "performance", "network", "resources"]
    let events = ["touch.ended.after", "performance.main-loop-gap", "network.interface", "resources.snapshot"]
    func make() -> DiagnosticLog { DiagnosticLog(directory: root, defaults: defaults) }
    func noCollection(_ log: DiagnosticLog) throws {
      let before = try log.snapshot().count
      for event in events { log.record(event) }
      let after = try log.snapshot().count
      precondition(after == before, "Disabled categories collected events")
    }

    let fresh = make()
    precondition(!fresh.enabled && names.allSatisfy { fresh.categories[$0] == false }, "Fresh install must default every category off")
    try noCollection(fresh)
    precondition(!FileManager.default.fileExists(atPath: root.path), "Fresh install wrote diagnostic files")
    try fresh.setEnabled(true)
    precondition(names.allSatisfy { fresh.categories[$0] == false }, "Enabling the master implicitly selected categories")
    try noCollection(fresh)
    let masterOnlyRestart = make()
    precondition(masterOnlyRestart.enabled && names.allSatisfy { masterOnlyRestart.categories[$0] == false }, "Restart implicitly selected categories")
    try noCollection(masterOnlyRestart)
    try fresh.setEnabled(false)
    let upgradedOff = make()
    precondition(!upgradedOff.enabled && names.allSatisfy { upgradedOff.categories[$0] == false })
    try noCollection(upgradedOff)

    // A pre-upgrade installation that never opted in has no category dictionary.
    defaults.removePersistentDomain(forName: suite)
    defaults.set(false, forKey: "diagnostics.enabled")
    let legacyOff = make()
    precondition(!legacyOff.enabled && names.allSatisfy { legacyOff.categories[$0] == false })
    try noCollection(legacyOff)

    // Before category choices were persisted, an enabled master meant all categories.
    defaults.set(true, forKey: "diagnostics.enabled")
    let legacyOn = make()
    precondition(legacyOn.enabled && names.allSatisfy { legacyOn.categories[$0] == true }, "Upgrade erased existing consent")
    try legacyOn.setEnabled(false)
    let legacyRestart = make()
    precondition(!legacyRestart.enabled && names.allSatisfy { legacyRestart.categories[$0] == true }, "Disabling erased legacy category choices")
    try noCollection(legacyRestart)
    legacyOn.setCategories(["interaction": false, "performance": false, "network": true, "resources": false])
    try legacyOn.setEnabled(false)
    let savedOff = make()
    precondition(!savedOff.enabled && savedOff.categories["network"] == true)
    try noCollection(savedOff)
    try savedOff.setEnabled(true)
    let savedOn = make()
    precondition(savedOn.enabled && savedOn.categories["network"] == true && savedOn.categories["interaction"] == false)
    let before = try savedOn.snapshot().count
    for event in events { savedOn.record(event) }
    let added = Array(try savedOn.snapshot().dropFirst(before))
    precondition(added.count == 1 && added[0]["event"] as? String == "network.interface", "Only the selected category may collect")
    print("PASS: fresh install, never-enabled upgrade, master-only no collection, saved consent and selected-category collection")
  }
}
