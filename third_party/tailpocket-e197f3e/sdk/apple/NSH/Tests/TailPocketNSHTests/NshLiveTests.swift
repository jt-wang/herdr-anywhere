import NativeShell
import TailPocket
import TailPocketNSH
import UIKit
import XCTest

/// The example NSH app (examples/nsh-phone) running in an NshAppView with the bridge attached: it
/// pairs with a real computer over Tailcat and shows what the computer answered. Run through
/// scripts/e2e-nsh-ios.sh, which starts the computer side and builds the NSH bundle.
@MainActor
final class NshLiveTests: XCTestCase {
    /// The keychain needs an entitlement a package test bundle does not have (-34018 in the simulator).
    final class MemoryStore: TailPocketLinkStore, @unchecked Sendable {
        let key = TailPocket.newKey()
        var links: [String: TailPocketHostLink] = [:]
        func phoneKey() throws -> String { key }
        func saveLink(_ link: TailPocketHostLink, slot: String) throws { links[slot] = link }
        func link(slot: String) throws -> TailPocketHostLink? { links[slot] }
        func deleteLink(slot: String) throws { links[slot] = nil }
    }

    func testTheNshAppPairsAndTalksToTheComputer() async throws {
        let env = ProcessInfo.processInfo.environment
        let invite = try XCTUnwrap(env["TAILPOCKET_NSH_INVITE"], "run through scripts/e2e-nsh-ios.sh")
        let bundle = try XCTUnwrap(env["TAILPOCKET_NSH_BUNDLE"], "run through scripts/e2e-nsh-ios.sh")

        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 390, height: 844))
        let root = UIViewController()
        let app = NshAppView(frame: window.bounds)
        root.view = app
        window.rootViewController = root
        window.makeKeyAndVisible()
        var errors: [String] = []
        app.onError = { errors.append($0) }
        app.params = ["invite": invite]
        // Its own storage, so runs do not share state.
        let storage = FileManager.default.temporaryDirectory.appendingPathComponent("nsh-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: storage, withIntermediateDirectories: true)
        app.storageDirectory = storage
        let store = MemoryStore()
        let bridge = TailPocketNsh.attach(app, store: store)
        defer { bridge.close() }
        // bundle.js: NSH loads bytecode (.nshc) only from the app's own resources or a signed package.
        try app.loadBundle(at: URL(fileURLWithPath: bundle))

        var texts: [String] = []
        for _ in 0..<180 {
            texts = Self.texts(in: app)
            if texts.contains(where: { $0.contains("hello from the computer") }) { break }
            try await Task.sleep(nanoseconds: 500_000_000)
        }
        XCTAssertTrue(texts.contains(where: { $0.contains("hello from the computer") }), "on screen: \(texts), errors: \(errors)")
        XCTAssertNotNil(store.links["default"], "the app saved the computer")
    }

    static func texts(in view: UIView) -> [String] {
        var out: [String] = []
        if let label = view as? UILabel, let text = label.text { out.append(text) }
        if let text = view.accessibilityLabel, !text.isEmpty { out.append(text) }
        for sub in view.subviews { out += texts(in: sub) }
        return out
    }
}
