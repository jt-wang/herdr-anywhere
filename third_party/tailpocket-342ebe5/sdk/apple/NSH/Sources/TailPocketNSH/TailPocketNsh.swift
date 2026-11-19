import NativeShell
import TailPocket
import TailPocketUI
import UIKit

/// Registers every bridge function on an NshAppView, so the NSH app calls them with
/// `host.call('tailPocket.pair', {...})` (see sdk/js/nsh.ts) and receives `tailPocket.nearby` and `tailPocket.pair.emoji` events.
///
///     TailPocketNsh.attach(view)
@MainActor
public enum TailPocketNsh {
    @discardableResult
    public static func attach(_ view: NshAppView,
                              store: any TailPocketLinkStore = TailPocketKeychainStore(),
                              nearby: @escaping @Sendable (String) -> any TailPocketNearbySource = { TailPocketNearbyHostsSource(service: $0) },
                              scanner: (@Sendable ([String]) async throws -> String)? = nil) -> TailPocketBridge {
        let scan: @Sendable ([String]) async throws -> String = scanner ?? { [weak view] prefixes in
            guard #available(iOS 16.0, *) else { throw TailPocketError(code: "unsupported") }
            return try await TailPocketQRScanner.scan(from: view?.window?.rootViewController, prefixes: prefixes)
        }
        let bridge = TailPocketBridge(store: store, nearby: nearby, scanner: scan, emit: { [weak view] event, data in
            Task { @MainActor in view?.send(event, data) }
        })
        for name in TailPocketBridge.names {
            view.handle(name) { data, reply in
                Task {
                    do {
                        reply.resolve(try await bridge.call(name, data))
                    } catch let error as TailPocketError {
                        reply.reject(error.code)
                    } catch {
                        reply.reject("failed")
                    }
                }
            }
        }
        return bridge
    }
}
