import XCTest
@testable import TailPocket

final class BridgeTests: XCTestCase {
    final class MemoryStore: TailPocketLinkStore, @unchecked Sendable {
        var links: [String: TailPocketHostLink] = [:]
        let key: String
        init(key: String = "privkey:stored") { self.key = key }
        func phoneKey() throws -> String { key }
        func saveLink(_ link: TailPocketHostLink, slot: String) throws { links[slot] = link }
        func link(slot: String) throws -> TailPocketHostLink? { links[slot] }
        func deleteLink(slot: String) throws { links[slot] = nil }
    }

    final class FakeNearby: TailPocketNearbySource, @unchecked Sendable {
        var onChange: (@Sendable ([String]) -> Void)?
        func start(onChange: @escaping @Sendable ([String]) -> Void) { self.onChange = onChange }
        func stop() { onChange = nil }
        func address(of name: String) async throws -> String { throw TailPocketError(code: "local-failed") }
    }

    final class Scans: @unchecked Sendable {
        let lock = NSLock()
        var asked: [[String]] = []
        func add(_ p: [String]) { lock.lock(); asked.append(p); lock.unlock() }
    }

    final class Events: @unchecked Sendable {
        let lock = NSLock()
        var items: [(String, Any?)] = []
        func add(_ name: String, _ data: Any?) { lock.lock(); items.append((name, data)); lock.unlock() }
    }

    func makeBridge(store: MemoryStore = MemoryStore(), nearby: FakeNearby = FakeNearby(), events: Events = Events(),
                    scans: Scans = Scans()) -> TailPocketBridge {
        TailPocketBridge(store: store, nearby: { _ in nearby }, scanner: { scans.add($0); return "tailpocket1:scanned" }, emit: { events.add($0, $1) })
    }

    func testNamesMatchTheContract() {
        XCTAssertEqual(Set(makeBridge().functions.keys), Set(TailPocketBridge.names))
        XCTAssertEqual(TailPocketBridge.names.count, 22)
    }

    func testKeysInviteTextAndErrors() async throws {
        let bridge = makeBridge()
        let key = try await bridge.call("tailPocket.newKey", nil) as? String
        XCTAssertEqual(key?.hasPrefix("privkey:"), true)
        let pub = try await bridge.call("tailPocket.publicKey", ["key": key!]) as? String
        XCTAssertEqual(pub?.hasPrefix("nodekey:"), true)
        let text = try await bridge.call("tailPocket.inviteText", ["text": "x tailpocket1:abc y"]) as? String
        XCTAssertEqual(text, "tailpocket1:abc")
        do { _ = try await bridge.call("tailPocket.decodeInvite", ["text": "junk"]); XCTFail() } catch { XCTAssertEqual((error as? TailPocketError)?.code, "invalid-invite") }
        do { _ = try await bridge.call("tailPocket.nope", nil); XCTFail() } catch { XCTAssertEqual((error as? TailPocketError)?.code, "unknown-function") }
        do { _ = try await bridge.call("tailPocket.pair", [:]); XCTFail() } catch { XCTAssertEqual((error as? TailPocketError)?.code, "invalid-arguments") }
    }

    // A v0.3 phone scans v1 and v2 invites: both defaults, or the app's two prefixes; one prefix alone stays one.
    func testScanLooksForBothPrefixes() async throws {
        let scans = Scans()
        let bridge = makeBridge(scans: scans)
        _ = try await bridge.call("tailPocket.scan", nil)
        _ = try await bridge.call("tailPocket.scan", ["prefix": "herdr1:", "prefix2": "herdr2:"])
        _ = try await bridge.call("tailPocket.scan", ["prefix": "herdr1:"])
        XCTAssertEqual(scans.asked, [["tailpocket1:", "tailpocket2:"], ["herdr1:", "herdr2:"], ["herdr1:"]])
        XCTAssertEqual(TailPocket.inviteText(in: "see herdr2:abc", prefixes: ["herdr1:", "herdr2:"]), "herdr2:abc")
        XCTAssertNil(TailPocket.inviteText(in: "tailpocket2:abc", prefixes: ["herdr1:", "herdr2:"]))
    }

    func testStoreScanAndNearbyEvents() async throws {
        let store = MemoryStore(), nearby = FakeNearby(), events = Events()
        let bridge = makeBridge(store: store, nearby: nearby, events: events)
        let link: [String: Any] = ["address": "tcM", "port": 48931, "name": "Mac", "key": "privkey:1"]
        _ = try await bridge.call("tailPocket.store.saveLink", ["link": link])
        let back = try await bridge.call("tailPocket.store.link", nil) as? [String: Any]
        XCTAssertEqual(back?["address"] as? String, "tcM")
        let stored = try await bridge.call("tailPocket.store.phoneKey", nil) as? String
        XCTAssertEqual(stored, "privkey:stored")
        let scanned = try await bridge.call("tailPocket.scan", nil) as? String
        XCTAssertEqual(scanned, "tailpocket1:scanned")
        _ = try await bridge.call("tailPocket.nearby.start", ["service": "_myapp._tcp"])
        nearby.onChange?(["Mac"])
        XCTAssertEqual(events.items.last?.0, "tailPocket.nearby")
        let hosts = (events.items.last?.1 as? [String: Any])?["hosts"] as? [[String: Any]]
        XCTAssertEqual(hosts?.first?["name"] as? String, "Mac")
        _ = try await bridge.call("tailPocket.nearby.stop", nil)
        XCTAssertNil(nearby.onChange)
        _ = try await bridge.call("tailPocket.store.deleteLink", nil)
        let gone = try await bridge.call("tailPocket.store.link", nil)
        XCTAssertNil(gone)
    }

    // tailPocket.setTailcatOnly reaches the Go side: a lan-only v2 invite then has no route, and no emoji event goes out.
    func testSetTailcatOnly() async throws {
        let events = Events()
        let bridge = makeBridge(events: events)
        _ = try await bridge.call("tailPocket.setTailcatOnly", ["on": true])
        defer { TailPocket.setTailcatOnly(false) }
        let invite: [String: Any] = ["v": 2, "name": "Mac", "exp": 1 << 40, "pk": String(repeating: "A", count: 43), "psk": String(repeating: "A", count: 22),
                                     "via": [["t": "lan", "a": ["192.168.1.20:7681"]]]]
        let key = try await bridge.call("tailPocket.newKey", nil) as? String
        do {
            _ = try await bridge.call("tailPocket.pair", ["invite": invite, "phoneKey": key ?? "", "device": "test", "timeoutMs": 5000])
            XCTFail("paired")
        } catch {
            XCTAssertEqual((error as? TailPocketError)?.code, "unreachable")
        }
        XCTAssertFalse(events.items.contains { $0.0 == "tailPocket.pair.emoji" })
        _ = try await bridge.call("tailPocket.setTailcatOnly", ["on": false])
    }

    // The bridge hands JavaScript the forward's token and a baseUrl under its prefix.
    func testConnectReturnsTheTokenAndItsBaseUrl() async throws {
        let bridge = makeBridge()
        let link: [String: Any] = ["name": "Mac", "key": TailPocket.newKey(), "pk": String(repeating: "A", count: 43),
                                   "via": [["t": "relay", "u": "wss://relay.example.com", "r": "AAAAAAAAAAAAAAAAAAAAAA"]]]
        let opened = try await bridge.call("tailPocket.connect", ["link": link]) as? [String: Any]
        let token = try XCTUnwrap(opened?["token"] as? String)
        let port = try XCTUnwrap(opened?["port"] as? Int)
        XCTAssertFalse(token.isEmpty)
        XCTAssertEqual(opened?["baseUrl"] as? String, "http://127.0.0.1:\(port)/_tailpocket/\(token)")
        _ = try await bridge.call("tailPocket.disconnect", ["handle": opened?["handle"] as Any])
    }

    func testConnectReturnsALocalURL() async throws {
        let bridge = makeBridge()
        let key = TailPocket.newKey()
        // A syntactically valid link to a host that does not exist: connect opens a local port without dialing.
        let addr = "tc" + String(repeating: "A", count: 4)
        do {
            _ = try await bridge.call("tailPocket.connect", ["link": ["address": addr, "port": 48931, "name": "Mac", "key": key]])
            XCTFail("invalid address accepted")
        } catch {
            XCTAssertEqual((error as? TailPocketError)?.code, "invalid-link")
        }
    }
}
