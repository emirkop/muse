import Security
import XCTest
@testable import MuseApp

@MainActor
final class ProductionConfigurationTests: XCTestCase {

    // MARK: - Transport

    func test_nonDevelopmentAPIBaseURLs_areHTTPSOnly() {
        for environment in [AppEnvironment.staging, .production] {
            XCTAssertEqual(environment.apiBaseURL.scheme, "https",
                           "\(environment) must never reach the backend over plaintext HTTP")
        }
    }

    func test_infoPlist_declaresNoAppTransportSecurityException() {
        let info = Bundle(for: AppDelegate.self).infoDictionary ?? [:]
        XCTAssertNil(info["NSAppTransportSecurity"],
                     "an ATS exception would let a Release build fall back to plaintext or unvalidated TLS")
    }

    func test_noURLSessionDelegate_overridesServerTrust() throws {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("MuseApp")
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: root.path, isDirectory: &isDirectory), isDirectory.boolValue,
              let walker = FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil) else {
            throw XCTSkip("app sources not found at \(root.path)")
        }
        var scanned = 0
        for case let url as URL in walker where url.pathExtension == "swift" {
            let source = try String(contentsOf: url, encoding: .utf8)
            scanned += 1
            for forbidden in ["serverTrust", "URLAuthenticationChallenge", "NSAllowsArbitraryLoads", "allowsInsecureHTTP"] {
                XCTAssertFalse(source.contains(forbidden), "\(url.lastPathComponent) references \(forbidden)")
            }
        }
        XCTAssertGreaterThan(scanned, 100, "the scan must actually have read the app target")
    }

    // MARK: - Share-link host binding

    func test_nonDevelopmentShareLinkHosts_excludeLoopback() {
        for environment in [AppEnvironment.staging, .production] {
            let hosts = environment.shareLinkHosts
            XCTAssertFalse(hosts.contains("localhost"), "\(environment) accepts localhost share links")
            XCTAssertFalse(hosts.contains("127.0.0.1"), "\(environment) accepts loopback share links")
            XCTAssertFalse(hosts.isEmpty)
        }
    }

    // MARK: - DEV paths cannot exist in production

    func test_devMockRecognizer_cannotBeConstructedOutsideDevelopment() {
        let catalog = FakeCollectionCatalog()
        XCTAssertNil(DevMockCollectionItemRecognizer.make(catalog: catalog, accessToken: { "t" }, environment: .production))
        XCTAssertNil(DevMockCollectionItemRecognizer.make(catalog: catalog, accessToken: { "t" }, environment: .staging))
        XCTAssertNotNil(DevMockCollectionItemRecognizer.make(catalog: catalog, accessToken: { "t" }, environment: .development))
    }

    // MARK: - Credential storage

    func test_keychainRecord_containsTheRefreshTokenAndNotTheAccessToken() throws {
        let service = "com.muse.app.session.-test-\(UUID().uuidString)"
        let store = KeychainSessionStore(service: service, account: "refreshToken")
        defer { try? store.clear() }

        let accessToken = "ACCESS-TOKEN-MUST-NOT-BE-PERSISTED-\(UUID().uuidString)"
        let refreshToken = "refresh-\(UUID().uuidString)"
        try store.save(AuthSession(
            accessToken: accessToken,
            accessTokenExpiresAt: Date().addingTimeInterval(900),
            refreshToken: refreshToken,
            refreshTokenExpiresAt: Date().addingTimeInterval(86_400)
        ))

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "refreshToken",
            kSecReturnData as String: true,
            kSecReturnAttributes as String: true
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess, let item = result as? [String: Any],
              let data = item[kSecValueData as String] as? Data else {
            throw XCTSkip("Keychain unavailable in this test host (status \(status))")
        }
        let stored = String(decoding: data, as: UTF8.self)
        XCTAssertTrue(stored.contains(refreshToken), "the refresh token is what the Keychain is for")
        XCTAssertFalse(stored.contains(accessToken), "the access token is short-lived and must never be persisted")
        XCTAssertEqual(item[kSecAttrAccessible as String] as? String, kSecAttrAccessibleAfterFirstUnlock as String,
                       "protection class must not be weakened to Always")
        XCTAssertNotEqual(item[kSecAttrSynchronizable as String] as? Bool, true,
                          "the session must not be iCloud-synchronised to other devices")
    }

    func test_appTarget_neverWritesUserDefaults() throws {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("MuseApp")
        guard let walker = FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil) else {
            throw XCTSkip("app sources not found")
        }
        var offenders: [String] = []
        for case let url as URL in walker where url.pathExtension == "swift" {
            let source = try String(contentsOf: url, encoding: .utf8)
            if source.contains("UserDefaults.standard") || source.contains("UserDefaults(") {
                offenders.append(url.lastPathComponent)
            }
        }
        XCTAssertEqual(offenders, [], "UserDefaults is not an approved store for anything in this app")
    }
}
