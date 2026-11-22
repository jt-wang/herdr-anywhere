import XCTest
@testable import TailPocket

final class UnitTests: XCTestCase {
    func testKeys() {
        let key = TailPocket.newKey()
        XCTAssertTrue(key.hasPrefix("privkey:"))
        XCTAssertEqual(TailPocket.publicKey(for: key)?.hasPrefix("nodekey:"), true)
        XCTAssertNil(TailPocket.publicKey(for: "junk"))
    }

    func testDecodeInviteRejectsJunk() {
        XCTAssertThrowsError(try TailPocket.decodeInvite("tailpocket1:junk")) { error in
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "invalid-invite"))
        }
    }

    func testPairRejectsExpiredInviteWithoutNetwork() async {
        let invite = TailPocketInvite(v: 1, address: "tcAAAA", port: 48932, key: TailPocket.newKey(),
                            proof: String(repeating: "A", count: 43), expiresAt: 1, name: nil)
        do {
            _ = try await TailPocket.pair(invite, phoneKey: TailPocket.newKey(), device: "test")
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "invalid-invite"))
        }
    }

    // v0.3.4: lastFailure decodes the Go library's answer; "{}" (cleared after a working connection) is nil.
    func testDialFailureParses() throws {
        XCTAssertNil(try TailPocketDialFailure.parse("{}"))
        let f = try XCTUnwrap(try TailPocketDialFailure.parse(
            #"{"code":"handshake-closed","reason":"handshake-closed","route":"lan","routes":[{"t":"lan","r":"handshake-closed"},{"t":"relay","r":"no-answer"}],"at":1795300000000}"#))
        XCTAssertEqual(f, TailPocketDialFailure(code: "handshake-closed", reason: "handshake-closed", route: "lan",
                                                routes: [.init(t: "lan", r: "handshake-closed"), .init(t: "relay", r: "no-answer")], at: 1795300000000))
        XCTAssertEqual(f.date.timeIntervalSince1970, 1795300000, accuracy: 0.001)
        XCTAssertThrowsError(try TailPocketDialFailure.parse(#"{"error":"unknown-handle"}"#)) {
            XCTAssertEqual($0 as? TailPocketError, TailPocketError(code: "unknown-handle"))
        }
    }

    // v0.3.5: the Go library's own answer (connectmobile/testdata/lastfailure.json) decodes with every field.
    func testDialFailureDecodesTheGoAnswer() throws {
        let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .appendingPathComponent("../../../../connectmobile/testdata/lastfailure.json").standardized
        let text = try String(contentsOf: file, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
        XCTAssertEqual(try TailPocketDialFailure.parse(text),
                       TailPocketDialFailure(code: "unreachable", reason: "no-answer", route: "relay",
                                             routes: [.init(t: "lan", r: "refused"), .init(t: "relay", r: "no-answer")], at: 1795300000001))
    }

    // v0.3.4, through the Go library: a relay nothing listens on gives no-answer after the request fails.
    func testLastFailureAfterAFailedRequest() async throws {
        let link = TailPocketHostLink(address: "", port: 0, name: "Mac", payload: nil, key: TailPocket.newKey(),
                                      hostKey: String(repeating: "A", count: 43),
                                      via: [TailPocketRoute(t: "relay", u: "ws://127.0.0.1:1", r: "AAAAAAAAAAAAAAAAAAAAAA")])
        let c = try TailPocket.connect(link)
        XCTAssertNil(try c.lastFailure())
        var req = URLRequest(url: c.baseURL.appendingPathComponent("x")); req.timeoutInterval = 20
        _ = try? await URLSession(configuration: .ephemeral).data(for: req)
        let f = try XCTUnwrap(try c.lastFailure())
        XCTAssertEqual(f.code, "unreachable")
        XCTAssertEqual(f.reason, "no-answer")
        XCTAssertEqual(f.routes, [.init(t: "relay", r: "no-answer")])
        c.close()
        XCTAssertThrowsError(try c.lastFailure()) { XCTAssertEqual($0 as? TailPocketError, TailPocketError(code: "unknown-handle")) }
    }

    func testConnectRejectsBadLink() {
        let link = TailPocketHostLink(address: "nope", port: 1, name: "x", payload: nil, key: "k")
        XCTAssertThrowsError(try TailPocket.connect(link))
    }

    func testLinkRoundTripsThroughJSON() throws {
        let link = TailPocketHostLink(address: "tcX", port: 48931, name: "Mac", payload: "p", key: "privkey:x")
        let data = try JSONEncoder().encode(link)
        XCTAssertEqual(try JSONDecoder().decode(TailPocketHostLink.self, from: data), link)
    }

    // A link paired over a relay has no Tailcat address; a v0.2 link has no host key or routes. Both
    // must survive the Keychain round trip and be accepted by the Go side (connect validates).
    func testV3LinkShapes() throws {
        let relayOnly = #"{"name":"Mac","key":"\#(TailPocket.newKey())","pk":"\#(String(repeating: "A", count: 43))","via":[{"t":"relay","u":"wss://relay.example.com","r":"AAAAAAAAAAAAAAAAAAAAAA"}]}"#
        let link = try JSONDecoder().decode(TailPocketHostLink.self, from: Data(relayOnly.utf8))
        XCTAssertEqual(link.address, "")
        XCTAssertEqual(link.via?.first?.t, "relay")
        let encoded = String(decoding: try JSONEncoder().encode(link), as: UTF8.self)
        XCTAssertFalse(encoded.contains("\"address\""), encoded)
        XCTAssertTrue(encoded.contains("\"pk\""), encoded)
        XCTAssertEqual(try JSONDecoder().decode(TailPocketHostLink.self, from: Data(encoded.utf8)), link)
        let connection = try TailPocket.connect(link) // opens a local port, dials nothing yet
        XCTAssertEqual(try connection.currentLink(), link)
        // The forward admits only requests under its token's prefix; appending keeps it.
        XCTAssertFalse(connection.token.isEmpty)
        XCTAssertEqual(connection.baseURL.absoluteString, "http://127.0.0.1:\(connection.port)/_tailpocket/\(connection.token)")
        XCTAssertEqual(connection.baseURL.appendingPathComponent("api/info").path, "/_tailpocket/\(connection.token)/api/info")
        connection.close()
        let old = #"{"address":"tcX","port":48931,"name":"Mac","payload":"p","key":"privkey:x"}"#
        let v2 = try JSONDecoder().decode(TailPocketHostLink.self, from: Data(old.utf8))
        XCTAssertNil(v2.hostKey)
        XCTAssertNil(v2.via)
    }

    func testDecodeInvite2RejectsJunkAndPair2CodesWithoutNetwork() async {
        XCTAssertThrowsError(try TailPocket.decodeInvite2("tailpocket2:junk")) { error in
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "invalid-invite"))
        }
        let invite = TailPocketInvite2(v: 2, name: "Mac", exp: 1, pk: nil, psk: nil, via: [TailPocketRoute(t: "relay", u: "wss://relay.example.com", r: "AAAAAAAAAAAAAAAAAAAAAA", m: "opaque")])
        do {
            _ = try await TailPocket.pair(invite, phoneKey: TailPocket.newKey(), device: "test") { _ in XCTFail("emoji for an expired invite") }
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "expired"))
        }
    }

    // A phone set to Tailcat only finds nothing it may use in an invite with only a lan route: no dialing, no emoji.
    func testTailcatOnlyPairSkipsTheLanRoute() async {
        TailPocket.setTailcatOnly(true)
        defer { TailPocket.setTailcatOnly(false) }
        let invite = TailPocketInvite2(v: 2, name: "Mac", exp: 1 << 40, pk: String(repeating: "A", count: 43), psk: String(repeating: "A", count: 22),
                                       via: [TailPocketRoute(t: "lan", a: ["192.168.1.20:7681"])])
        do {
            _ = try await TailPocket.pair(invite, phoneKey: TailPocket.newKey(), device: "test", timeout: 5) { _ in XCTFail("emoji without a route") }
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "unreachable"))
        }
    }

    // A relay-only phone finds nothing it may use in an invite with only a lan route; bad lists throw.
    func testSetRoutes() async throws {
        try TailPocket.setRoutes(["relay"])
        defer { try? TailPocket.setRoutes(nil) }
        let invite = TailPocketInvite2(v: 2, name: "Mac", exp: 1 << 40, pk: String(repeating: "A", count: 43), psk: String(repeating: "A", count: 22),
                                       via: [TailPocketRoute(t: "lan", a: ["192.168.1.20:7681"])])
        do {
            _ = try await TailPocket.pair(invite, phoneKey: TailPocket.newKey(), device: "test", timeout: 5) { _ in XCTFail("emoji without a route") }
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "unreachable"))
        }
        for bad in [[], [""], ["lan,relay"], ["wifi"], ["lan", "lan"]] as [[String]] {
            XCTAssertThrowsError(try TailPocket.setRoutes(bad)) { XCTAssertEqual($0 as? TailPocketError, TailPocketError(code: "invalid-routes")) }
        }
    }

    func testScannedTextFilter() {
        XCTAssertEqual(TailPocket.inviteText(in: "  tailpocket1:abc \n", prefix: "tailpocket1:"), "tailpocket1:abc")
        XCTAssertEqual(TailPocket.inviteText(in: "myapp://pair?c=tailpocket1:abc", prefix: "tailpocket1:"), "tailpocket1:abc")
        XCTAssertNil(TailPocket.inviteText(in: "https://example.com", prefix: "tailpocket1:"))
    }
}
