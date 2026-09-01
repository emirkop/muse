import Foundation
import Security

public enum KeychainSessionStoreError: Error, Equatable, Sendable {
    case unexpectedStatus(OSStatus)
    case invalidStoredData
}

public final class KeychainSessionStore: SessionStoring, Sendable {
    private let service: String
    private let account: String

    public init(service: String = "com.muse.app.session", account: String = "refreshToken") {
        self.service = service
        self.account = account
    }

    public func save(_ session: AuthSession) throws {
        let record = StoredRefreshTokenRecord(refreshToken: session.refreshToken, expiresAt: session.refreshTokenExpiresAt)
        let data = try JSONEncoder().encode(record)

        try clear()

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock
        ]

        let status = SecItemAdd(query as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw KeychainSessionStoreError.unexpectedStatus(status)
        }
    }

    public func loadRefreshToken() throws -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        switch status {
        case errSecSuccess:
            guard let data = result as? Data else {
                throw KeychainSessionStoreError.invalidStoredData
            }
            let record = try JSONDecoder().decode(StoredRefreshTokenRecord.self, from: data)
            guard record.expiresAt > Date() else {
                return nil
            }
            return record.refreshToken
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainSessionStoreError.unexpectedStatus(status)
        }
    }

    public func clear() throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]

        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainSessionStoreError.unexpectedStatus(status)
        }
    }
}

private struct StoredRefreshTokenRecord: Codable {
    let refreshToken: String
    let expiresAt: Date
}
