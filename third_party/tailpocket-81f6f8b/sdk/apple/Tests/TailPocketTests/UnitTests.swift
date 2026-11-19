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
            _ = try await TailPocket.pair(invite, phoneKey: TailPocket.newKey(), device: "test")
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? TailPocketError, TailPocketError(code: "expired"))
        }
    }

    func testScannedTextFilter() {
        XCTAssertEqual(TailPocket.inviteText(in: "  tailpocket1:abc \n", prefix: "tailpocket1:"), "tailpocket1:abc")
        XCTAssertEqual(TailPocket.inviteText(in: "myapp://pair?c=tailpocket1:abc", prefix: "tailpocket1:"), "tailpocket1:abc")
        XCTAssertNil(TailPocket.inviteText(in: "https://example.com", prefix: "tailpocket1:"))
    }
}
