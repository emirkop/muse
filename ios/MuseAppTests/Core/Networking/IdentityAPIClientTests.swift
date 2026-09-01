import XCTest
@testable import MuseApp

final class IdentityAPIClientTests: XCTestCase {
    override func tearDown() {
        StubURLProtocol.stub = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastRequestBody = nil
        super.tearDown()
    }

    private func makeClient() -> IdentityAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        let session = URLSession(configuration: configuration)
        return IdentityAPIClient(baseURL: URL(string: "https://example.invalid")!, session: session)
    }

    private func stubSessionResponse(isNewAccount: Bool) {
        StubURLProtocol.stub = .init(statusCode: 200, body: Data("""
        {
            "access_token": "access-value",
            "access_token_expires_at": "2026-08-24T19:40:00Z",
            "refresh_token": "refresh-value",
            "refresh_token_expires_at": "2026-09-23T19:40:00Z",
            "is_new_account": \(isNewAccount)
        }
        """.utf8))
    }

    func test_signInWithApple_success_decodesSessionAndIsNewAccount() async throws {
        stubSessionResponse(isNewAccount: true)

        let result = try await makeClient().signInWithApple(identityToken: "token", nonce: "nonce")

        XCTAssertEqual(result.session.accessToken, "access-value")
        XCTAssertEqual(result.session.refreshToken, "refresh-value")
        XCTAssertTrue(result.isNewAccount)
    }

    func test_signInWithApple_existingIdentity_decodesIsNewAccountFalse() async throws {
        stubSessionResponse(isNewAccount: false)

        let result = try await makeClient().signInWithApple(identityToken: "token", nonce: "nonce")

        XCTAssertFalse(result.isNewAccount)
    }

    func test_signInWithApple_requestShape_matchesBackendContract() async throws {
        stubSessionResponse(isNewAccount: true)

        _ = try await makeClient().signInWithApple(identityToken: "identity-token-value", nonce: "raw-nonce-value")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/auth/apple")
        XCTAssertEqual(request.httpMethod, "POST")

        let bodyData = try XCTUnwrap(StubURLProtocol.lastRequestBody)
        let decoded = try JSONDecoder().decode([String: String].self, from: bodyData)
        XCTAssertEqual(decoded["identity_token"], "identity-token-value")
        XCTAssertEqual(decoded["nonce"], "raw-nonce-value")
    }

    func test_signInWithApple_serverError_throwsServerError() async {
        StubURLProtocol.stub = .init(statusCode: 401, body: Data(#"{"error":"authentication failed"}"#.utf8))

        do {
            _ = try await makeClient().signInWithApple(identityToken: "token", nonce: "nonce")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 401)
            XCTAssertEqual(message, "authentication failed")
        } catch {
            XCTFail("expected IdentityAPIClientError.server, got \(error)")
        }
    }

    func test_signInWithApple_malformedResponse_throwsAnError() async {
        StubURLProtocol.stub = .init(statusCode: 200, body: Data("not json".utf8))

        do {
            _ = try await makeClient().signInWithApple(identityToken: "token", nonce: "nonce")
            XCTFail("expected an error")
        } catch {
        }
    }

    // MARK: - Google

    func test_signInWithGoogle_success_decodesSessionAndIsNewAccount() async throws {
        stubSessionResponse(isNewAccount: true)

        let result = try await makeClient().signInWithGoogle(identityToken: "token")

        XCTAssertEqual(result.session.accessToken, "access-value")
        XCTAssertEqual(result.session.refreshToken, "refresh-value")
        XCTAssertTrue(result.isNewAccount)
    }

    func test_signInWithGoogle_requestShape_matchesBackendContract() async throws {
        stubSessionResponse(isNewAccount: true)

        _ = try await makeClient().signInWithGoogle(identityToken: "identity-token-value")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/auth/google")
        XCTAssertEqual(request.httpMethod, "POST")

        let bodyData = try XCTUnwrap(StubURLProtocol.lastRequestBody)
        let decoded = try JSONDecoder().decode([String: String].self, from: bodyData)
        XCTAssertEqual(decoded["identity_token"], "identity-token-value")
        XCTAssertNil(decoded["nonce"], "Google's request body has no nonce field, per interfaces/contracts.go's googleLoginRequest")
    }

    func test_signInWithGoogle_serverError_throwsServerError() async {
        StubURLProtocol.stub = .init(statusCode: 401, body: Data(#"{"error":"authentication failed"}"#.utf8))

        do {
            _ = try await makeClient().signInWithGoogle(identityToken: "token")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 401)
            XCTAssertEqual(message, "authentication failed")
        } catch {
            XCTFail("expected IdentityAPIClientError.server, got \(error)")
        }
    }

    // MARK: - Refresh

    func test_refreshSession_success_decodesSession() async throws {
        stubSessionResponse(isNewAccount: false)

        let session = try await makeClient().refreshSession(refreshToken: "stored-refresh-token")

        XCTAssertEqual(session.accessToken, "access-value")
        XCTAssertEqual(session.refreshToken, "refresh-value")
    }

    func test_refreshSession_requestShape_matchesBackendContract() async throws {
        stubSessionResponse(isNewAccount: false)

        _ = try await makeClient().refreshSession(refreshToken: "stored-refresh-token-value")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/auth/refresh")
        XCTAssertEqual(request.httpMethod, "POST")

        let bodyData = try XCTUnwrap(StubURLProtocol.lastRequestBody)
        let decoded = try JSONDecoder().decode([String: String].self, from: bodyData)
        XCTAssertEqual(decoded["refresh_token"], "stored-refresh-token-value")
    }

    func test_refreshSession_serverError_throwsServerError() async {
        StubURLProtocol.stub = .init(statusCode: 401, body: Data(#"{"error":"refresh failed"}"#.utf8))

        do {
            _ = try await makeClient().refreshSession(refreshToken: "expired-or-reused-token")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 401)
            XCTAssertEqual(message, "refresh failed")
        } catch {
            XCTFail("expected IdentityAPIClientError.server, got \(error)")
        }
    }

    // MARK: - Profile

    private func stubProfileResponse(displayName: String, avatarID: String) {
        StubURLProtocol.stub = .init(statusCode: 200, body: Data("""
        {"display_name": "\(displayName)", "avatar_id": "\(avatarID)"}
        """.utf8))
    }

    func test_fetchOwnProfile_success_decodesProfile() async throws {
        stubProfileResponse(displayName: "Ada", avatarID: "")

        let profile = try await makeClient().fetchOwnProfile(accessToken: "access-token-value")

        XCTAssertEqual(profile, Profile(displayName: "Ada", avatarID: ""))
    }

    func test_fetchOwnProfile_requestShape_matchesBackendContract() async throws {
        stubProfileResponse(displayName: "Ada", avatarID: "")

        _ = try await makeClient().fetchOwnProfile(accessToken: "access-token-value")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/profile/me")
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer access-token-value")
    }

    func test_updateOwnProfile_displayNameOnly_requestShape_matchesBackendContract() async throws {
        stubProfileResponse(displayName: "New Name", avatarID: "")

        let profile = try await makeClient().updateOwnProfile(accessToken: "access-token-value", displayName: "New Name", avatarID: nil)

        XCTAssertEqual(profile.displayName, "New Name")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/profile/me")
        XCTAssertEqual(request.httpMethod, "PATCH")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer access-token-value")

        let bodyData = try XCTUnwrap(StubURLProtocol.lastRequestBody)
        let decoded = try JSONDecoder().decode([String: String].self, from: bodyData)
        XCTAssertEqual(decoded["display_name"], "New Name")
        XCTAssertNil(decoded["avatar_id"], "an omitted avatarID must not be encoded at all, per the backend's pointer-shaped fields")
    }

    // MARK: - Avatar

    func test_updateOwnProfile_avatarOnly_requestShape_omitsDisplayName() async throws {
        stubProfileResponse(displayName: "", avatarID: "avatar_2")

        let profile = try await makeClient().updateOwnProfile(accessToken: "access-token-value", displayName: nil, avatarID: "avatar_2")

        XCTAssertEqual(profile.avatarID, "avatar_2")

        let bodyData = try XCTUnwrap(StubURLProtocol.lastRequestBody)
        let decoded = try JSONDecoder().decode([String: String].self, from: bodyData)
        XCTAssertEqual(decoded["avatar_id"], "avatar_2")
        XCTAssertNil(decoded["display_name"], "an omitted displayName must not be encoded at all, per the backend's pointer-shaped fields")
    }

    func test_updateOwnProfile_invalidAvatar_throwsServerError() async {
        StubURLProtocol.stub = .init(statusCode: 400, body: Data(#"{"error":"invalid avatar_id"}"#.utf8))

        do {
            _ = try await makeClient().updateOwnProfile(accessToken: "access-token-value", displayName: nil, avatarID: "not-a-real-avatar")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 400)
            XCTAssertEqual(message, "invalid avatar_id")
        } catch {
            XCTFail("expected IdentityAPIClientError.server, got \(error)")
        }
    }

    func test_fetchProfile_otherAccount_requestShape_matchesBackendContract() async throws {
        stubProfileResponse(displayName: "Museum Owner", avatarID: "")

        let profile = try await makeClient().fetchProfile(accessToken: "visitor-access-token", accountID: "owner-account-id")

        XCTAssertEqual(profile.displayName, "Museum Owner")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/profile/owner-account-id")
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer visitor-access-token")
    }

    func test_fetchOwnProfile_unauthorized_throwsServerError() async {
        StubURLProtocol.stub = .init(statusCode: 401, body: Data(#"{"error":"authentication required"}"#.utf8))

        do {
            _ = try await makeClient().fetchOwnProfile(accessToken: "invalid-token")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 401)
            XCTAssertEqual(message, "authentication required")
        } catch {
            XCTFail("expected IdentityAPIClientError.server, got \(error)")
        }
    }
}

private final class StubURLProtocol: URLProtocol {
    struct Stub {
        let statusCode: Int
        let body: Data
    }

    nonisolated(unsafe) static var stub: Stub?
    nonisolated(unsafe) static var lastRequest: URLRequest?
    nonisolated(unsafe) static var lastRequestBody: Data?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lastRequest = request
        Self.lastRequestBody = request.httpBody ?? Self.readBody(from: request.httpBodyStream)

        guard let stub = Self.stub else {
            client?.urlProtocolDidFinishLoading(self)
            return
        }

        let response = HTTPURLResponse(url: request.url!, statusCode: stub.statusCode, httpVersion: "HTTP/1.1", headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: stub.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static func readBody(from stream: InputStream?) -> Data? {
        guard let stream else { return nil }
        stream.open()
        defer { stream.close() }

        var data = Data()
        let bufferSize = 1024
        var buffer = [UInt8](repeating: 0, count: bufferSize)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: bufferSize)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}

// MARK: -

extension IdentityAPIClientTests {
    func test_fetchProfile_ofAnotherAccount_decodesTheAvatarOnlyPayload() async throws {
        StubURLProtocol.stub = .init(statusCode: 200, body: Data(#"{"avatar_id":"avatar_2"}"#.utf8))

        let profile = try await makeClient().fetchProfile(accessToken: "token", accountID: "other-account")

        XCTAssertEqual(profile.avatarID, "avatar_2")
        XCTAssertEqual(profile.displayName, "", "no display_name key → empty name, never a decode failure")
    }
}
