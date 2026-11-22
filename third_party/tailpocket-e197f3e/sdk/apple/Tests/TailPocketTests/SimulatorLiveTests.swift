#if os(iOS)
import XCTest
import TailPocket

/// Runs inside the iOS Simulator against a host started by scripts/e2e-apple.sh, which passes the
/// invite as TEST_RUNNER_TAILPOCKET_INVITE.
final class SimulatorLiveTests: XCTestCase {
    func testPairAndFetchFromTheSimulator() async throws {
        let env = ProcessInfo.processInfo.environment
        let qr = try XCTUnwrap(env["TAILPOCKET_INVITE"], "run through scripts/e2e-apple.sh")
        let invite = try TailPocket.decodeInvite(qr)
        let link = try await TailPocket.pair(invite, phoneKey: TailPocket.newKey(), device: "iOS Simulator")
        XCTAssertEqual(link.payload, "welcome")
        let connection = try TailPocket.connect(link)
        defer { connection.close() }
        var body = ""
        for _ in 0..<6 {
            var request = URLRequest(url: connection.baseURL.appendingPathComponent("hello.txt"))
            request.timeoutInterval = 20
            if let (data, _) = try? await URLSession.shared.data(for: request) {
                body = String(decoding: data, as: UTF8.self)
                break
            }
        }
        XCTAssertEqual(body, "hello from the computer\n")
    }

    /// v0.3 inside the simulator: pair from the v2 QR over a local tailpocket-relay alone, then fetch
    /// with no Tailcat address in the link. scripts/e2e-apple.sh passes TEST_RUNNER_TAILPOCKET_RELAY_INVITE.
    func testPairAndFetchOverTheRelayFromTheSimulator() async throws {
        let qr = try XCTUnwrap(ProcessInfo.processInfo.environment["TAILPOCKET_RELAY_INVITE"], "run through scripts/e2e-apple.sh")
        let invite = try TailPocket.decodeInvite2(qr)
        let relayOnly = TailPocketInvite2(v: invite.v, name: invite.name, exp: invite.exp, pk: invite.pk, psk: invite.psk,
                                          via: invite.via.filter { $0.t == "relay" })
        XCTAssertEqual(relayOnly.via.count, 1)
        var shown: [String] = []
        let paired = try await TailPocket.pair(relayOnly, phoneKey: TailPocket.newKey(), device: "iOS relay") { shown = $0 }
        XCTAssertEqual(shown.count, 4)
        XCTAssertEqual(paired.payload, "welcome")
        let link = TailPocketHostLink(address: "", port: 0, name: paired.name, payload: paired.payload, key: paired.key,
                                      hostKey: paired.hostKey, via: paired.via)
        let connection = try TailPocket.connect(link)
        defer { connection.close() }
        var body = ""
        for _ in 0..<6 where body.isEmpty {
            var request = URLRequest(url: connection.baseURL.appendingPathComponent("hello.txt"))
            request.timeoutInterval = 20
            if let (data, _) = try? await URLSession.shared.data(for: request) { body = String(decoding: data, as: UTF8.self) }
        }
        XCTAssertEqual(body, "hello from the computer\n")
        let path = try await connection.path()
        XCTAssertEqual(path.route, "relay")
    }

    /// The function table NSH and React Native forward to, inside the simulator.
    func testBridgeInTheSimulator() async throws {
        let qr = try XCTUnwrap(ProcessInfo.processInfo.environment["TAILPOCKET_BRIDGE_INVITE"], "run through scripts/e2e-apple.sh")
        // A package test bundle has no keychain entitlement in the simulator (errSecMissingEntitlement,
        // -34018, measured 2026-10-29), so the store is in memory; TailPocketKeychainStore is tested on macOS.
        let bridge = TailPocketBridge(store: BridgeTests.MemoryStore(key: TailPocket.newKey()), emit: { _, _ in })
        _ = try await bridge.call("tailPocket.pair", ["invite": qr, "device": "iOS bridge", "save": true])
        let opened = try await bridge.call("tailPocket.connect", [:]) as? [String: Any]
        let base = try XCTUnwrap(opened?["baseUrl"] as? String)
        var body = ""
        for _ in 0..<6 where body.isEmpty {
            var request = URLRequest(url: URL(string: base + "/hello.txt")!)
            request.timeoutInterval = 20
            if let (data, _) = try? await URLSession.shared.data(for: request) { body = String(decoding: data, as: UTF8.self) }
        }
        XCTAssertEqual(body, "hello from the computer\n")
        _ = try await bridge.call("tailPocket.store.deleteLink", nil)
    }
}
#endif
