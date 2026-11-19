import Foundation
import Security

/// Keeps the phone key and the TailPocketHostLink in the Keychain (this device only, after first unlock).
public struct TailPocketKeychainStore: Sendable {
    public let service: String

    public init(service: String = "tailpocket") { self.service = service }

    /// The phone key, created on first use.
    public func phoneKey() throws -> String {
        if let data = try read("phone-key"), let key = String(data: data, encoding: .utf8), TailPocket.publicKey(for: key) != nil {
            return key
        }
        let key = TailPocket.newKey()
        try write("phone-key", Data(key.utf8))
        return key
    }

    /// Forgets the phone key; the next phoneKey() makes a new one, so every computer must pair again.
    public func deleteKey() throws { try delete("phone-key") }

    public func saveLink(_ link: TailPocketHostLink, name: String = "default") throws {
        try write("link." + name, try JSONEncoder().encode(link))
    }

    public func link(name: String = "default") throws -> TailPocketHostLink? {
        guard let data = try read("link." + name) else { return nil }
        return try JSONDecoder().decode(TailPocketHostLink.self, from: data)
    }

    public func deleteLink(name: String = "default") throws { try delete("link." + name) }

    private func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: account]
    }

    private func read(_ account: String) throws -> Data? {
        var q = query(account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &out)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else { throw TailPocketKeychainError(status: status) }
        return out as? Data
    }

    private func write(_ account: String, _ data: Data) throws {
        let status = SecItemUpdate(query(account) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecSuccess { return }
        guard status == errSecItemNotFound else { throw TailPocketKeychainError(status: status) }
        var add = query(account)
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let added = SecItemAdd(add as CFDictionary, nil)
        guard added == errSecSuccess else { throw TailPocketKeychainError(status: added) }
    }

    private func delete(_ account: String) throws {
        let status = SecItemDelete(query(account) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw TailPocketKeychainError(status: status) }
    }
}

public struct TailPocketKeychainError: Error, Equatable {
    public let status: OSStatus
}
