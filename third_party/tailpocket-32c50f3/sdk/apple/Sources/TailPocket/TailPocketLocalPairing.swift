import Foundation
import Network
import TailPocketMobile

/// A computer advertising its pairing window on this Wi-Fi.
public struct TailPocketNearbyHost: Sendable, Hashable, Identifiable {
    public let name: String
    public let endpoint: NWEndpoint
    public var id: String { name }
}

/// Lists computers on this Wi-Fi with an open pairing window.
///
/// The app's Info.plist needs `NSLocalNetworkUsageDescription` and the service under
/// `NSBonjourServices` (e.g. `_myapp._tcp`); iOS asks the user for Local Network access the first time.
public final class TailPocketNearbyHosts: @unchecked Sendable {
    private let browser: NWBrowser
    private let queue = DispatchQueue(label: "tailpocket.browse")

    public init(service: String) {
        browser = NWBrowser(for: .bonjour(type: service, domain: "local."), using: .tcp)
    }

    /// Calls `onChange` with the current list whenever it changes, until `stop()`.
    public func start(onChange: @escaping @Sendable ([TailPocketNearbyHost]) -> Void, onError: (@Sendable (Error) -> Void)? = nil) {
        browser.browseResultsChangedHandler = { results, _ in
            let hosts = results.compactMap { result -> TailPocketNearbyHost? in
                guard case let .service(name, _, _, _) = result.endpoint else { return nil }
                return TailPocketNearbyHost(name: name, endpoint: result.endpoint)
            }.sorted { $0.name < $1.name }
            onChange(hosts)
        }
        browser.stateUpdateHandler = { state in
            if case let .failed(error) = state { onError?(error) }
            if case let .waiting(error) = state { onError?(error) }
        }
        browser.start(queue: queue)
    }

    public func stop() { browser.cancel() }

    /// The list as an async sequence.
    public static func stream(service: String) -> AsyncStream<[TailPocketNearbyHost]> {
        AsyncStream { continuation in
            let hosts = TailPocketNearbyHosts(service: service)
            hosts.start(onChange: { continuation.yield($0) })
            continuation.onTermination = { _ in hosts.stop() }
        }
    }
}

/// A same-Wi-Fi pairing in progress: show `code`, then `finish()` returns the invite once the
/// computer's user confirms the same code.
public final class TailPocketPendingLocalPairing: @unchecked Sendable {
    public let code: String
    private let op: Int64

    init(code: String, op: Int64) { self.code = code; self.op = op }

    public func finish(timeout: TimeInterval = 300) async throws -> TailPocketInvite {
        let op = self.op
        return try await TailPocket.blocking(cancel: { ConnectmobileCancel(op) }) { ConnectmobileLocalPairFinish(op, Int(timeout * 1000)) }
    }

    public func cancel() { ConnectmobileCancel(op) }
}

public enum TailPocketLocalPairing {
    /// Resolves a host to "ipv4:port" (the computer serves pairing on IPv4 only).
    public static func address(of host: TailPocketNearbyHost, timeout: TimeInterval = 10) async throws -> String {
        let params = NWParameters.tcp
        if let ip = params.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options { ip.version = .v4 }
        let connection = NWConnection(to: host.endpoint, using: params)
        let queue = DispatchQueue(label: "tailpocket.resolve")
        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            let once = Once()
            let finish: @Sendable (Result<String, Error>) -> Void = { result in
                guard once.claim() else { return }
                connection.cancel()
                continuation.resume(with: result)
            }
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    if case let .hostPort(host, port)? = connection.currentPath?.remoteEndpoint {
                        var text = "\(host)"
                        if let percent = text.firstIndex(of: "%") { text = String(text[..<percent]) }
                        finish(.success("\(text):\(port.rawValue)"))
                    } else {
                        finish(.failure(TailPocketError(code: "local-failed")))
                    }
                case .failed, .cancelled:
                    finish(.failure(TailPocketError(code: "local-failed")))
                default: break
                }
            }
            connection.start(queue: queue)
            queue.asyncAfter(deadline: .now() + timeout) { finish(.failure(TailPocketError(code: "timeout"))) }
        }
    }

    /// Starts pairing with a nearby host; the returned value carries the 6-digit code to show.
    public static func begin(with host: TailPocketNearbyHost, device: String, label: String = "") async throws -> TailPocketPendingLocalPairing {
        let address = try await address(of: host)
        return try await begin(address: address, device: device, label: label)
    }

    public static func begin(address: String, device: String, label: String = "") async throws -> TailPocketPendingLocalPairing {
        struct Started: Decodable { let code: String }
        let op = TailPocket.Operation.next()
        let started: Started = try await TailPocket.blocking(cancel: { ConnectmobileCancel(op) }) {
            ConnectmobileLocalPairStart(op, address, label, device)
        }
        return TailPocketPendingLocalPairing(code: started.code, op: op)
    }
}

final class Once: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false
    func claim() -> Bool {
        lock.lock(); defer { lock.unlock() }
        if done { return false }
        done = true
        return true
    }
}
