#if os(iOS)
import SwiftUI
import TailPocket

/// A ready-made pairing screen: computers on this Wi-Fi, a QR scanner, and a paste field. It returns
/// the TailPocketHostLink through `onPaired`; storing it (e.g. `TailPocketKeychainStore`) and the look of your app stay yours.
/// Build your own screen from `TailPocketNearbyHosts`, `TailPocketLocalPairing`, `TailPocketQRScanner` and `TailPocket.pair` instead
/// if this one does not fit.
@available(iOS 16.0, *)
public struct TailPocketPairingView: View {
    public struct Options {
        public var service: String?
        public var prefix: String = TailPocket.defaultPrefix
        public var label: String = ""
        public var device: String = UIDevice.current.name
        public init(service: String? = nil) { self.service = service }
    }

    private let options: Options
    private let phoneKey: String
    private let onPaired: (TailPocketHostLink) -> Void

    @State private var hosts: [TailPocketNearbyHost] = []
    @State private var browser: TailPocketNearbyHosts?
    @State private var scanning = false
    @State private var pasted = ""
    @State private var status: Status = .idle

    enum Status: Equatable { case idle, code(String, String), working(String), failed(String) }

    public init(options: Options, phoneKey: String, onPaired: @escaping (TailPocketHostLink) -> Void) {
        self.options = options
        self.phoneKey = phoneKey
        self.onPaired = onPaired
    }

    public var body: some View {
        List {
            if case let .code(code, host) = status {
                Section("Confirm on \(host)") {
                    Text(code).font(.system(size: 40, weight: .semibold, design: .monospaced)).frame(maxWidth: .infinity)
                    Text("Check that the computer shows the same code, then approve it there.").font(.footnote).foregroundStyle(.secondary)
                }
            }
            if case let .working(text) = status { Section { HStack { ProgressView(); Text(text) } } }
            if case let .failed(text) = status { Section { Text(text).foregroundStyle(.red) } }
            if options.service != nil {
                Section("On this Wi-Fi") {
                    if hosts.isEmpty { Text("Looking for computers…").foregroundStyle(.secondary) }
                    ForEach(hosts) { host in
                        Button(host.name) { Task { await pairNearby(host) } }
                    }
                }
            }
            Section("Pairing code") {
                if TailPocketQRScanner.isAvailable { Button("Scan QR code") { scanning = true } }
                TextField("Paste code", text: $pasted).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .onSubmit { Task { await pairText(pasted) } }
            }
        }
        .sheet(isPresented: $scanning) {
            TailPocketQRScanner(prefix: options.prefix) { text in
                scanning = false
                Task { await pairText(text) }
            }
            .ignoresSafeArea()
        }
        .onAppear {
            guard let service = options.service, browser == nil else { return }
            let b = TailPocketNearbyHosts(service: service)
            b.start(onChange: { list in Task { @MainActor in hosts = list } })
            browser = b
        }
        .onDisappear { browser?.stop(); browser = nil }
    }

    @MainActor private func pairText(_ text: String) async {
        guard let code = TailPocket.inviteText(in: text, prefix: options.prefix) else {
            status = .failed("That is not a pairing code.")
            return
        }
        do { await finish(try TailPocket.decodeInvite(code, prefix: options.prefix)) } catch { status = .failed(message(error)) }
    }

    @MainActor private func pairNearby(_ host: TailPocketNearbyHost) async {
        status = .working("Connecting to \(host.name)…")
        do {
            let pending = try await TailPocketLocalPairing.begin(with: host, device: options.device, label: options.label)
            status = .code(pending.code, host.name)
            await finish(try await pending.finish())
        } catch {
            status = .failed(message(error))
        }
    }

    @MainActor private func finish(_ invite: TailPocketInvite) async {
        status = .working("Pairing with \(invite.hostName.isEmpty ? "the computer" : invite.hostName)…")
        do {
            let link = try await TailPocket.pair(invite, phoneKey: phoneKey, device: options.device)
            status = .idle
            onPaired(link)
        } catch {
            status = .failed(message(error))
        }
    }

    private func message(_ error: Error) -> String {
        switch (error as? TailPocketError)?.code {
        case "expired": return "This pairing code has expired. Show a new one on the computer."
        case "taken": return "Another phone already used this code."
        case "denied": return "The computer did not approve this phone."
        case "unreachable", "timeout": return "Could not reach the computer. Check that it is online."
        case "invalid-invite": return "That is not a pairing code."
        default: return "Pairing failed."
        }
    }
}
#endif
