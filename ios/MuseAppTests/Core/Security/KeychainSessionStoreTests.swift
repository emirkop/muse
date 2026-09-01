import XCTest
@testable import MuseApp

final class KeychainSessionStoreTests: XCTestCase {
    private var store: KeychainSessionStore!

    override func setUp() {
        super.setUp()
        store = KeychainSessionStore(service: "com.muse.app.tests.\(UUID().uuidString)", account: "refreshToken")
    }

    override func tearDown() {
        try? store.clear()
        store = nil
        super.tearDown()
    }

    func test_loadRefreshToken_returnsNil_whenNothingSaved() throws {
        XCTAssertNil(try store.loadRefreshToken())
    }

    func test_save_thenLoadRefreshToken_returnsTheToken_forUnexpiredSession() throws {
        try store.save(unexpiredSession(refreshToken: "refresh-token-value"))

        XCTAssertEqual(try store.loadRefreshToken(), "refresh-token-value")
    }

    func test_save_ofExpiredSession_loadRefreshTokenReturnsNil() throws {
        let expiredSession = AuthSession(
            accessToken: "access",
            accessTokenExpiresAt: Date().addingTimeInterval(-1),
            refreshToken: "refresh-token-value",
            refreshTokenExpiresAt: Date().addingTimeInterval(-1)
        )

        try store.save(expiredSession)

        XCTAssertNil(try store.loadRefreshToken())
    }

    func test_clear_removesStoredSession() throws {
        try store.save(unexpiredSession())

        try store.clear()

        XCTAssertNil(try store.loadRefreshToken())
    }

    func test_save_overwritesPreviousSession_withoutDuplicateItemError() throws {
        try store.save(unexpiredSession(refreshToken: "r1"))

        try store.save(unexpiredSession(refreshToken: "r2"))

        XCTAssertEqual(try store.loadRefreshToken(), "r2")
    }

    private func unexpiredSession(refreshToken: String = "refresh-token-value") -> AuthSession {
        AuthSession(
            accessToken: "access",
            accessTokenExpiresAt: Date().addingTimeInterval(900),
            refreshToken: refreshToken,
            refreshTokenExpiresAt: Date().addingTimeInterval(60 * 60 * 24 * 30)
        )
    }
}
