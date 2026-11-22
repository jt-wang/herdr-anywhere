import Foundation

/// Where a bridge keeps the phone key and links; `TailPocketKeychainStore` is the default.
public protocol TailPocketLinkStore: Sendable {
    func phoneKey() throws -> String
    func saveLink(_ link: TailPocketHostLink, slot: String) throws
    func link(slot: String) throws -> TailPocketHostLink?
    func deleteLink(slot: String) throws
}

extension TailPocketKeychainStore: TailPocketLinkStore {
    public func saveLink(_ link: TailPocketHostLink, slot: String) throws { try saveLink(link, name: slot) }
    public func link(slot: String) throws -> TailPocketHostLink? { try link(name: slot) }
    public func deleteLink(slot: String) throws { try deleteLink(name: slot) }
}

/// Computers on this Wi-Fi by name; `TailPocketNearbyHosts` is the default.
public protocol TailPocketNearbySource: Sendable {
    func start(onChange: @escaping @Sendable ([String]) -> Void)
    func stop()
    func address(of name: String) async throws -> String
}

/// The SDK as a table of named async functions over JSON-like values (dictionaries, arrays, strings,
/// numbers, booleans, nil) plus events through `emit`. Any JavaScript bridge (NSH, React Native,
/// Capacitor, a WKWebView) registers each name and forwards calls; see docs/BRIDGE.md. Failures throw
/// `TailPocketError`; bridges reject with its `code`.
public final class TailPocketBridge: @unchecked Sendable {
    public typealias Function = @Sendable (Any?) async throws -> Any?

    public static let names = [
        "tailPocket.newKey", "tailPocket.publicKey", "tailPocket.inviteText", "tailPocket.decodeInvite", "tailPocket.decodeInvite2",
        "tailPocket.pair", "tailPocket.connect", "tailPocket.disconnect", "tailPocket.sync", "tailPocket.path", "tailPocket.link", "tailPocket.lastFailure",
        "tailPocket.nearby.start", "tailPocket.nearby.stop", "tailPocket.local.begin", "tailPocket.local.finish", "tailPocket.local.cancel", "tailPocket.scan",
        "tailPocket.store.phoneKey", "tailPocket.store.saveLink", "tailPocket.store.link", "tailPocket.store.deleteLink",
        "tailPocket.setTailcatOnly", "tailPocket.setRoutes",
    ]

    private let store: any TailPocketLinkStore
    private let nearbyFor: @Sendable (String) -> any TailPocketNearbySource
    private let scanner: (@Sendable ([String]) async throws -> String)?
    private let emit: @Sendable (String, Any?) -> Void
    private let lock = NSLock()
    private var nearby: (any TailPocketNearbySource)?
    private var connections: [Int64: (connection: TailPocketConnection, slot: String?)] = [:]
    private var pending: [Int64: TailPocketPendingLocalPairing] = [:]
    private var serial: Int64 = 0

    /// - Parameters:
    ///   - scanner: shows a QR scanner and returns the first invite text with one of the prefixes; nil makes
    ///     "tailPocket.scan" fail with "unsupported".
    public init(store: any TailPocketLinkStore = TailPocketKeychainStore(),
                nearby: @escaping @Sendable (String) -> any TailPocketNearbySource = { TailPocketNearbyHostsSource(service: $0) },
                scanner: (@Sendable ([String]) async throws -> String)? = nil,
                emit: @escaping @Sendable (String, Any?) -> Void) {
        self.store = store
        self.nearbyFor = nearby
        self.scanner = scanner
        self.emit = emit
    }

    public lazy var functions: [String: Function] = [
        "tailPocket.newKey": { _ in TailPocket.newKey() },
        "tailPocket.publicKey": { a in TailPocket.publicKey(for: try Self.string(a, "key")) },
        "tailPocket.inviteText": { a in TailPocket.inviteText(in: try Self.string(a, "text"), prefix: Self.prefix(a)) },
        "tailPocket.decodeInvite": { a in try Self.value(try TailPocket.decodeInvite(try Self.string(a, "text"), prefix: Self.prefix(a))) },
        "tailPocket.decodeInvite2": { a in
            try Self.value(try TailPocket.decodeInvite2(try Self.string(a, "text"), prefix: Self.dict(a)["prefix"] as? String ?? TailPocket.defaultPrefix2))
        },
        "tailPocket.pair": { [unowned self] a in
            let args = Self.dict(a)
            // Text and v2 objects pair over every route (a v1 text as its one tailcat route); a v1 object over Tailcat.
            let invite: TailPocketInvite2
            switch args["invite"] {
            case let given as String:
                let prefix2 = args["prefix2"] as? String ?? TailPocket.defaultPrefix2
                let text = TailPocket.inviteText(in: given, prefixes: [prefix2, Self.prefix(a)]) ?? given
                invite = try TailPocket.decodeInvite2(text, prefix: text.hasPrefix(prefix2) ? prefix2 : Self.prefix(a))
            case let object as [String: Any] where (object["v"] as? NSNumber)?.intValue == 2:
                invite = try Self.decode(TailPocketInvite2.self, object)
            case let object as [String: Any]:
                invite = TailPocketInvite2(v: 2, name: object["name"] as? String, exp: (object["expiresAt"] as? NSNumber)?.int64Value ?? 0,
                                           pk: nil, psk: nil, via: [TailPocketRoute(t: "tailcat", inv: try Self.decode(TailPocketInvite.self, object))])
            default:
                throw TailPocketError(code: "invalid-arguments")
            }
            let key = try (args["phoneKey"] as? String) ?? store.phoneKey()
            let timeout = (args["timeoutMs"] as? NSNumber).map { $0.doubleValue / 1000 } ?? 90
            let emit = self.emit
            let link = try await TailPocket.pair(invite, phoneKey: key, device: args["device"] as? String ?? "Phone",
                                                     derpMapURL: args["derpMapUrl"] as? String ?? "", timeout: timeout) { emoji in
                emit("tailPocket.pair.emoji", ["emoji": emoji])
            }
            if args["save"] as? Bool == true { try store.saveLink(link, slot: args["slot"] as? String ?? "default") }
            return try Self.value(link)
        },
        "tailPocket.setTailcatOnly": { a in
            TailPocket.setTailcatOnly(Self.dict(a)["on"] as? Bool == true)
            return nil
        },
        "tailPocket.setRoutes": { a in
            // Only an explicit null allows every route. A call without `routes` is a mistake (JavaScript's
            // undefined is dropped on the way here), not a choice to widen the phone's routes.
            guard let routes = Self.dict(a)["routes"] else { throw TailPocketError(code: "invalid-arguments") }
            if routes is NSNull {
                try TailPocket.setRoutes(nil)
            } else if let list = routes as? [String] {
                try TailPocket.setRoutes(list)
            } else {
                throw TailPocketError(code: "invalid-arguments")
            }
            return nil
        },
        "tailPocket.connect": { [unowned self] a in
            let args = Self.dict(a)
            let link: TailPocketHostLink
            var slot: String?
            if let object = args["link"] as? [String: Any] {
                link = try Self.decode(TailPocketHostLink.self, object)
            } else if let saved = try store.link(slot: args["slot"] as? String ?? "default") {
                link = saved
                slot = args["slot"] as? String ?? "default"
            } else {
                throw TailPocketError(code: "not-paired")
            }
            let connection = try TailPocket.connect(link, derpMapURL: args["derpMapUrl"] as? String ?? "")
            let handle = next()
            lock.withLock { connections[handle] = (connection, slot) }
            return ["handle": handle, "port": connection.port, "baseUrl": connection.baseURL.absoluteString, "token": connection.token] as [String: Any]
        },
        "tailPocket.disconnect": { [unowned self] a in
            let handle = (Self.dict(a)["handle"] as? NSNumber)?.int64Value ?? -1
            guard let open = lock.withLock({ connections.removeValue(forKey: handle) }) else { return nil }
            // Keep the routes the computer sent meanwhile, for a link that came from the store.
            if let slot = open.slot, let latest = try? open.connection.currentLink(), latest != (try? store.link(slot: slot)) {
                try? store.saveLink(latest, slot: slot)
            }
            open.connection.close()
            return nil
        },
        "tailPocket.sync": { [unowned self] a in
            let args = Self.dict(a)
            let open = try connection(args)
            let timeout = (args["timeoutMs"] as? NSNumber).map { $0.doubleValue / 1000 } ?? 30
            let link = try await open.connection.sync(timeout: timeout)
            if let slot = open.slot { try store.saveLink(link, slot: slot) }
            return try Self.value(link)
        },
        "tailPocket.path": { [unowned self] a in
            let args = Self.dict(a)
            let timeout = (args["timeoutMs"] as? NSNumber).map { $0.doubleValue / 1000 } ?? 5
            return try Self.value(try await connection(args).connection.path(timeout: timeout))
        },
        "tailPocket.link": { [unowned self] a in try Self.value(try connection(Self.dict(a)).connection.currentLink()) },
        "tailPocket.lastFailure": { [unowned self] a in
            try connection(Self.dict(a)).connection.lastFailure().map { try Self.value($0) }
        },
        "tailPocket.nearby.start": { [unowned self] a in
            let service = try Self.string(a, "service")
            let source = nearbyFor(service)
            let previous = lock.withLock { () -> (any TailPocketNearbySource)? in
                let p = nearby
                nearby = source
                return p
            }
            previous?.stop()
            let emit = self.emit
            source.start { names in emit("tailPocket.nearby", ["hosts": names.map { ["name": $0] }]) }
            return nil
        },
        "tailPocket.nearby.stop": { [unowned self] _ in
            lock.withLock { () -> (any TailPocketNearbySource)? in let p = nearby; nearby = nil; return p }?.stop()
            return nil
        },
        "tailPocket.local.begin": { [unowned self] a in
            let args = Self.dict(a)
            let address: String
            if let given = args["address"] as? String {
                address = given
            } else {
                guard let source = lock.withLock({ nearby }) else { throw TailPocketError(code: "nearby-not-started") }
                address = try await source.address(of: try Self.string(a, "name"))
            }
            let p = try await TailPocketLocalPairing.begin(address: address, device: args["device"] as? String ?? "Phone", label: args["label"] as? String ?? "")
            let op = next()
            lock.withLock { pending[op] = p }
            return ["op": op, "code": p.code] as [String: Any]
        },
        "tailPocket.local.finish": { [unowned self] a in
            let args = Self.dict(a)
            let op = (args["op"] as? NSNumber)?.int64Value ?? -1
            guard let p = lock.withLock({ pending.removeValue(forKey: op) }) else { throw TailPocketError(code: "unknown-operation") }
            let timeout = (args["timeoutMs"] as? NSNumber).map { $0.doubleValue / 1000 } ?? 300
            return try Self.value(try await p.finish(timeout: timeout))
        },
        "tailPocket.local.cancel": { [unowned self] a in
            let op = (Self.dict(a)["op"] as? NSNumber)?.int64Value ?? -1
            lock.withLock { pending.removeValue(forKey: op) }?.cancel()
            return nil
        },
        "tailPocket.scan": { [unowned self] a in
            guard let scanner else { throw TailPocketError(code: "unsupported") }
            return try await scanner(Self.scanPrefixes(a))
        },
        "tailPocket.store.phoneKey": { [unowned self] _ in try store.phoneKey() },
        "tailPocket.store.saveLink": { [unowned self] a in
            let args = Self.dict(a)
            guard let object = args["link"] as? [String: Any] else { throw TailPocketError(code: "invalid-arguments") }
            try store.saveLink(try Self.decode(TailPocketHostLink.self, object), slot: args["slot"] as? String ?? "default")
            return nil
        },
        "tailPocket.store.link": { [unowned self] a in
            try store.link(slot: Self.dict(a)["slot"] as? String ?? "default").map { try Self.value($0) }
        },
        "tailPocket.store.deleteLink": { [unowned self] a in
            try store.deleteLink(slot: Self.dict(a)["slot"] as? String ?? "default")
            return nil
        },
    ]

    public func call(_ name: String, _ args: Any?) async throws -> Any? {
        guard let fn = functions[name] else { throw TailPocketError(code: "unknown-function") }
        return try await fn(args)
    }

    /// Stops discovery and closes every connection.
    public func close() {
        let (source, open, waiting) = lock.withLock { () -> ((any TailPocketNearbySource)?, [(connection: TailPocketConnection, slot: String?)], [TailPocketPendingLocalPairing]) in
            defer { nearby = nil; connections = [:]; pending = [:] }
            return (nearby, Array(connections.values), Array(pending.values))
        }
        source?.stop()
        open.forEach { $0.connection.close() }
        waiting.forEach { $0.cancel() }
    }

    private func next() -> Int64 { lock.withLock { serial += 1; return serial } }

    private func connection(_ args: [String: Any]) throws -> (connection: TailPocketConnection, slot: String?) {
        let handle = (args["handle"] as? NSNumber)?.int64Value ?? -1
        guard let open = lock.withLock({ connections[handle] }) else { throw TailPocketError(code: "unknown-handle") }
        return open
    }

    // MARK: - values

    static func dict(_ a: Any?) -> [String: Any] { a as? [String: Any] ?? [:] }
    static func string(_ a: Any?, _ key: String) throws -> String {
        guard let s = dict(a)[key] as? String else { throw TailPocketError(code: "invalid-arguments") }
        return s
    }
    static func prefix(_ a: Any?) -> String { dict(a)["prefix"] as? String ?? TailPocket.defaultPrefix }
    /// The prefixes "tailPocket.scan" looks for: `prefix` and `prefix2` as given, or both defaults when neither is.
    static func scanPrefixes(_ a: Any?) -> [String] {
        let d = dict(a)
        let given = [d["prefix"] as? String, d["prefix2"] as? String].compactMap { $0 }.filter { !$0.isEmpty }
        return given.isEmpty ? [TailPocket.defaultPrefix, TailPocket.defaultPrefix2] : given
    }
    static func value<T: Encodable>(_ v: T) throws -> Any { try JSONSerialization.jsonObject(with: JSONEncoder().encode(v)) }
    static func decode<T: Decodable>(_ type: T.Type, _ object: [String: Any]) throws -> T {
        do { return try JSONDecoder().decode(type, from: JSONSerialization.data(withJSONObject: object)) } catch {
            throw TailPocketError(code: "invalid-arguments")
        }
    }
}

/// `TailPocketNearbyHosts` by name, resolving with `TailPocketLocalPairing.address(of:)`.
public final class TailPocketNearbyHostsSource: TailPocketNearbySource, @unchecked Sendable {
    private let hosts: TailPocketNearbyHosts
    private let lock = NSLock()
    private var byName: [String: TailPocketNearbyHost] = [:]

    public init(service: String) { hosts = TailPocketNearbyHosts(service: service) }

    public func start(onChange: @escaping @Sendable ([String]) -> Void) {
        hosts.start(onChange: { [weak self] list in
            self?.lock.withLock { self?.byName = Dictionary(uniqueKeysWithValues: list.map { ($0.name, $0) }) }
            onChange(list.map(\.name))
        })
    }

    public func stop() { hosts.stop() }

    public func address(of name: String) async throws -> String {
        guard let host = lock.withLock({ byName[name] }) else { throw TailPocketError(code: "local-failed") }
        return try await TailPocketLocalPairing.address(of: host)
    }
}
