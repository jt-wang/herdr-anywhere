import Foundation
import TailPocketMobile

/// A pairing invite, decoded from a QR code, a link, or same-Wi-Fi delivery. Holding it lets a phone
/// pair until `expiresAt`, so do not log or share it.
public struct TailPocketInvite: Codable, Sendable, Equatable {
    public let v: Int
    public let address: String
    public let port: Int
    public let key: String
    public let proof: String
    public let expiresAt: Int64
    public let name: String?

    public init(v: Int, address: String, port: Int, key: String, proof: String, expiresAt: Int64, name: String?) {
        self.v = v; self.address = address; self.port = port; self.key = key; self.proof = proof
        self.expiresAt = expiresAt; self.name = name
    }

    /// The computer's name, as its app set it.
    public var hostName: String { name ?? "" }
}

/// One way to reach a computer (v0.3): `tailcat`, `relay` (your tailpocket-relay) or `lan`. Phones
/// skip types they do not know.
public struct TailPocketRoute: Codable, Sendable, Equatable {
    public let t: String
    public let a: [String]?
    public let p: Int?
    public let u: String?
    public let r: String?
    /// "opaque": the relay carries the app's own encryption, not TailPocket's.
    public let m: String?
    /// In an invite's tailcat route: the v1 pairing window.
    public let inv: TailPocketInvite?

    public init(t: String, a: [String]? = nil, p: Int? = nil, u: String? = nil, r: String? = nil, m: String? = nil, inv: TailPocketInvite? = nil) {
        self.t = t; self.a = a; self.p = p; self.u = u; self.r = r; self.m = m; self.inv = inv
    }
}

/// A v2 invite (`tailpocket2:`): every route to the computer and its end-to-end key. A v1 invite
/// decodes as one with a single tailcat route. Holding it lets a phone pair until `exp`.
public struct TailPocketInvite2: Codable, Sendable, Equatable {
    public let v: Int
    public let name: String?
    public let exp: Int64
    public let pk: String?
    public let psk: String?
    public let via: [TailPocketRoute]

    public init(v: Int, name: String?, exp: Int64, pk: String?, psk: String?, via: [TailPocketRoute]) {
        self.v = v; self.name = name; self.exp = exp; self.pk = pk; self.psk = psk; self.via = via
    }

    /// The computer's name, as its app set it.
    public var hostName: String { name ?? "" }
}

/// Everything a paired phone needs to reach its computer. Contains the phone's private key: store it
/// in the Keychain (see `TailPocketKeychainStore`).
///
/// `hostKey` and `via` (v0.3) are the computer's end-to-end key and its routes besides Tailcat. A link
/// paired before v0.3 has neither until `TailPocketConnection.sync()`; a link paired over a relay may
/// have an empty `address`. Store the link again whenever `sync()` or `currentLink()` returns a new one.
public struct TailPocketHostLink: Codable, Sendable, Equatable {
    public let address: String
    public let port: Int
    public let name: String
    /// What the computer's app sent along (a token, settings…), or nil.
    public let payload: String?
    public let key: String
    public let hostKey: String?
    public let via: [TailPocketRoute]?

    public init(address: String, port: Int, name: String, payload: String?, key: String,
                hostKey: String? = nil, via: [TailPocketRoute]? = nil) {
        self.address = address; self.port = port; self.name = name; self.payload = payload; self.key = key
        self.hostKey = hostKey; self.via = via
    }

    enum CodingKeys: String, CodingKey { case address, port, name, payload, key, hostKey = "pk", via }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        address = try c.decodeIfPresent(String.self, forKey: .address) ?? ""
        port = try c.decodeIfPresent(Int.self, forKey: .port) ?? 0
        name = try c.decode(String.self, forKey: .name)
        payload = try c.decodeIfPresent(String.self, forKey: .payload)
        key = try c.decode(String.self, forKey: .key)
        hostKey = try c.decodeIfPresent(String.self, forKey: .hostKey)
        via = try c.decodeIfPresent([TailPocketRoute].self, forKey: .via)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        if !address.isEmpty {
            try c.encode(address, forKey: .address)
            try c.encode(port, forKey: .port)
        }
        try c.encode(name, forKey: .name)
        try c.encodeIfPresent(payload, forKey: .payload)
        try c.encode(key, forKey: .key)
        try c.encodeIfPresent(hostKey, forKey: .hostKey)
        try c.encodeIfPresent(via, forKey: .via)
    }
}

/// How a connection reaches its computer now: `route` is "direct" or "relay" (Tailcat), "relay" (your
/// tailpocket-relay, `relay` its host name) or "lan".
public struct TailPocketPath: Codable, Sendable, Equatable {
    public let route: String
    public let relay: String
}

/// A failure with a fixed code: invalid-invite, invalid-key, invalid-link, expired, unauthorized
/// (after the pairing emoji: the computer's user said no or the code was spent; show a new code there),
/// taken, unreachable, revoked, mismatch, handshake-closed, denied, local-failed, cancelled, timeout,
/// unknown-handle, failed. Only "revoked" means the computer removed this phone: delete its link.
public struct TailPocketError: Error, Equatable, Sendable, CustomStringConvertible {
    public let code: String
    public init(code: String) { self.code = code }
    public var description: String { "TailPocketError(\(code))" }
}

/// An open route to the computer: point any HTTP or WebSocket client at `baseURL`.
///
/// Any program on the phone can reach a loopback port, so the forward lets through only requests that
/// carry this connection's `token`. Build every URL by appending to `baseURL`
/// (`baseURL.appendingPathComponent("api/info")` keeps the prefix; the computer sees `/api/info`). A URL
/// relative to "/" (`URL(string: "/api/info", relativeTo: baseURL)`) drops the prefix and gets 403.
public final class TailPocketConnection: @unchecked Sendable {
    public let port: Int
    /// The secret every connection to the forward presents. HTTP and WebSocket clients get it through
    /// `baseURL` (or the header "Tailpocket-Token"); any other protocol (your own TLS) starts the
    /// connection with the line "TP-FWD <token>\n". Keep it in memory only: do not store or log it.
    public let token: String
    private let base: String
    private let handle: Int64
    private let lock = NSLock()
    private var closed = false

    init(handle: Int64, port: Int, token: String, base: String) {
        self.handle = handle; self.port = port; self.token = token; self.base = base
    }

    /// http://127.0.0.1:<port>/_tailpocket/<token> — traffic continues to the computer inside the
    /// encrypted tunnel, with the prefix removed.
    public var baseURL: URL { URL(string: "http://127.0.0.1:\(port)\(base)")! }

    /// The link with the computer's latest routes; store it when it differs from the saved one.
    public func currentLink() throws -> TailPocketHostLink { try TailPocket.decode(ConnectmobileLink(handle)) }

    /// Registers this phone's end-to-end key with the computer over Tailcat and returns the updated link
    /// to store. A link paired before v0.3 needs this once to use the relay and lan routes; no new QR.
    public func sync(timeout: TimeInterval = 30) async throws -> TailPocketHostLink {
        let handle = self.handle
        return try await TailPocket.blocking(cancel: {}) { ConnectmobileSync(handle, Int(timeout * 1000)) }
    }

    /// How the connection reaches the computer now. Throws "revoked" once the computer removed this phone.
    public func path(timeout: TimeInterval = 5) async throws -> TailPocketPath {
        let handle = self.handle
        return try await TailPocket.blocking(cancel: {}) { ConnectmobilePath(handle, Int(timeout * 1000)) }
    }

    public func close() {
        lock.lock(); defer { lock.unlock() }
        guard !closed else { return }
        closed = true
        ConnectmobileStop(handle)
    }

    deinit { close() }
}

public enum TailPocket {
    public static let defaultPrefix = "tailpocket1:"
    /// The prefix of v2 invites (v0.3), which carry every route.
    public static let defaultPrefix2 = "tailpocket2:"

    /// A new phone identity. Create it once and keep it (`TailPocketKeychainStore.phoneKey()` does both).
    public static func newKey() -> String { ConnectmobileNewKey() }

    /// The "nodekey:…" the computer lists for this phone, or nil for an invalid key.
    public static func publicKey(for key: String) -> String? {
        let p = ConnectmobilePublicKey(key)
        return p.isEmpty ? nil : p
    }

    /// Finds invite text inside scanned or pasted text (a bare code, or a link that contains it).
    /// The first of `prefixes` found in `text` wins.
    public static func inviteText(in text: String, prefixes: [String]) -> String? {
        for prefix in prefixes { if let invite = inviteText(in: text, prefix: prefix) { return invite } }
        return nil
    }

    public static func inviteText(in text: String, prefix: String = defaultPrefix) -> String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let range = trimmed.range(of: prefix) else { return nil }
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")
        let tail = trimmed[range.upperBound...].unicodeScalars.prefix { allowed.contains($0) }
        return tail.isEmpty ? nil : prefix + String(String.UnicodeScalarView(tail))
    }

    public static func decodeInvite(_ text: String, prefix: String = defaultPrefix) throws -> TailPocketInvite {
        try decode(ConnectmobileDecodeInvite(text, prefix))
    }

    /// Decodes a v2 invite with `prefix`, or a v1 one (with `prefix` or tailpocket1:) as a v2 invite with one tailcat route.
    public static func decodeInvite2(_ text: String, prefix: String = defaultPrefix2) throws -> TailPocketInvite2 {
        try decode(ConnectmobileDecodeInvite2(text, prefix))
    }

    /// Pairs over whichever of the invite's routes works first and returns the link to store. Once the
    /// computer waits for its user to compare them, `onEmoji` gets four emoji: show them with "Check that
    /// your computer shows the same, then confirm there" and return; pairing then waits for the computer's
    /// answer (its user has up to a minute more than `timeout`). A refusal there throws "unauthorized": ask
    /// for a new code on the computer. A phone pairing again after a lost reply gets the link without
    /// `onEmoji`. Cancelling the task cancels it.
    public static func pair(_ invite: TailPocketInvite2, phoneKey: String, device: String, derpMapURL: String = "",
                            timeout: TimeInterval = 90, onEmoji: ([String]) async -> Void) async throws -> TailPocketHostLink {
        let json = String(decoding: try JSONEncoder().encode(invite), as: UTF8.self)
        let op = Operation.next()
        let first = await blockingText(cancel: { ConnectmobileCancel(op) }) {
            ConnectmobilePair2(op, json, phoneKey, device, derpMapURL, Int(timeout * 1000))
        }
        struct Emoji: Decodable { let emoji: [String] }
        guard let shown = try? JSONDecoder().decode(Emoji.self, from: Data(first.utf8)) else { return try decode(first) }
        await onEmoji(shown.emoji)
        if Task.isCancelled {
            ConnectmobileCancel(op)
            throw CancellationError()
        }
        // The native side's own deadline (timeout and a minute, from the start) ends the wait first.
        return try await blocking(cancel: { ConnectmobileCancel(op) }) {
            ConnectmobilePair2Finish(op, Int((timeout + 60) * 1000))
        }
    }

    /// The phone's preference to reach computers over Tailcat only: `pair`, `connect` and `sync` then never
    /// use the relay or local network routes a computer lists. Set it before `connect`; open connections keep theirs.
    public static func setTailcatOnly(_ on: Bool) { ConnectmobileSetTailcatOnly(on) }

    /// Joins the computer's pairing window and returns the TailPocketHostLink to store. Cancelling the task cancels it.
    public static func pair(_ invite: TailPocketInvite, phoneKey: String, device: String, derpMapURL: String = "",
                            timeout: TimeInterval = 90) async throws -> TailPocketHostLink {
        let json = String(decoding: try JSONEncoder().encode(invite), as: UTF8.self)
        let op = Operation.next()
        return try await blocking(cancel: { ConnectmobileCancel(op) }) {
            ConnectmobilePair(op, json, phoneKey, device, derpMapURL, Int(timeout * 1000))
        }
    }

    /// Opens a local route to the computer. Nothing goes over the network until a client connects.
    public static func connect(_ link: TailPocketHostLink, derpMapURL: String = "") throws -> TailPocketConnection {
        let json = String(decoding: try JSONEncoder().encode(link), as: UTF8.self)
        struct Opened: Decodable { let handle: Int64; let port: Int; let token: String; let base: String }
        let opened: Opened = try decode(ConnectmobileConnect(json, derpMapURL))
        return TailPocketConnection(handle: opened.handle, port: opened.port, token: opened.token, base: opened.base)
    }

    // MARK: - plumbing

    struct Failure: Decodable { let error: String }

    static func decode<T: Decodable>(_ text: String) throws -> T {
        let data = Data(text.utf8)
        if let failure = try? JSONDecoder().decode(Failure.self, from: data) {
            throw TailPocketError(code: failure.error)
        }
        do { return try JSONDecoder().decode(T.self, from: data) } catch { throw TailPocketError(code: "failed") }
    }

    /// Runs a blocking Go call off the cooperative pool, cancelling it when the task is cancelled.
    static func blocking<T: Decodable>(cancel: @escaping @Sendable () -> Void, _ call: @escaping @Sendable () -> String) async throws -> T {
        let text = await blockingText(cancel: cancel, call)
        return try decode(text)
    }

    /// `blocking` without decoding the answer.
    static func blockingText(cancel: @escaping @Sendable () -> Void, _ call: @escaping @Sendable () -> String) async -> String {
        await withTaskCancellationHandler {
            await withCheckedContinuation { (continuation: CheckedContinuation<String, Never>) in
                DispatchQueue.global(qos: .userInitiated).async { continuation.resume(returning: call()) }
            }
        } onCancel: {
            cancel()
        }
    }

    enum Operation {
        private static let lock = NSLock()
        private static var counter: Int64 = 0
        static func next() -> Int64 {
            lock.lock(); defer { lock.unlock() }
            counter += 1
            return counter
        }
    }
}
