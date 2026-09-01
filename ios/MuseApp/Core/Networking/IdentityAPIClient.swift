import Foundation

public enum IdentityAPIClientError: Error, Equatable, Sendable {
    case invalidResponse
    case server(statusCode: Int, message: String?)
    case offline
    case transport
    case cancelled

    public static func classify(_ error: Error) -> IdentityAPIClientError {
        if let already = error as? IdentityAPIClientError { return already }
        switch NetworkResilience.classify(error) {
        case .offline: return .offline
        case .cancelled: return .cancelled
        case .unreachable, .other: return .transport
        }
    }

    public var isConnectivityFailure: Bool {
        switch self {
        case .offline, .transport: return true
        case .server, .invalidResponse, .cancelled: return false
        }
    }
}

public final class IdentityAPIClient: AuthenticationServicing, ProfileServicing, Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    public func signInWithApple(identityToken: String, nonce: String) async throws -> LoginResult {
        let body = try await post(
            path: "auth/apple",
            body: AppleLoginRequestBody(identityToken: identityToken, nonce: nonce.isEmpty ? nil : nonce)
        )
        return LoginResult(session: try Self.session(from: body), isNewAccount: body.isNewAccount)
    }

    public func signInWithGoogle(identityToken: String) async throws -> LoginResult {
        let body = try await post(path: "auth/google", body: GoogleLoginRequestBody(identityToken: identityToken))
        return LoginResult(session: try Self.session(from: body), isNewAccount: body.isNewAccount)
    }

    public func refreshSession(refreshToken: String) async throws -> AuthSession {
        let body = try await post(path: "auth/refresh", body: RefreshRequestBody(refreshToken: refreshToken))
        return try Self.session(from: body)
    }

    // MARK: - Email & password

    public func signUpWithEmail(email: String, password: String) async throws {
        try await postExpectingNoSession(
            path: "auth/email/signup",
            body: EmailCredentialRequestBody(email: email, password: password)
        )
    }

    public func verifyEmail(token: String) async throws -> LoginResult {
        let body = try await post(path: "auth/email/verify", body: TokenRequestBody(token: token))
        return LoginResult(session: try Self.session(from: body), isNewAccount: body.isNewAccount)
    }

    public func resendVerification(email: String) async throws {
        try await postExpectingNoSession(
            path: "auth/email/verification/resend",
            body: EmailOnlyRequestBody(email: email)
        )
    }

    public func logInWithEmail(email: String, password: String) async throws -> LoginResult {
        let body = try await post(
            path: "auth/email/login",
            body: EmailCredentialRequestBody(email: email, password: password)
        )
        return LoginResult(session: try Self.session(from: body), isNewAccount: body.isNewAccount)
    }

    public func requestPasswordReset(email: String) async throws {
        try await postExpectingNoSession(
            path: "auth/email/password-reset",
            body: EmailOnlyRequestBody(email: email)
        )
    }

    public func confirmPasswordReset(token: String, password: String) async throws {
        try await postExpectingNoSession(
            path: "auth/email/password-reset/confirm",
            body: PasswordResetConfirmRequestBody(token: token, password: password)
        )
    }

    private func postExpectingNoSession<Body: Encodable>(path: String, body: Body) async throws {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: request)
        } catch {
            throw IdentityAPIClientError.classify(error)
        }
        guard let httpResponse = response as? HTTPURLResponse else {
            throw IdentityAPIClientError.invalidResponse
        }
        guard httpResponse.statusCode == 200 || httpResponse.statusCode == 202 else {
            let errorBody = try? JSONDecoder().decode(ErrorResponseBody.self, from: data)
            throw IdentityAPIClientError.server(statusCode: httpResponse.statusCode, message: errorBody?.error)
        }
    }

    public func fetchOwnProfile(accessToken: String) async throws -> Profile {
        try await authorizedProfileRequest(method: "GET", path: "profile/me", accessToken: accessToken, body: nil)
    }

    public func updateOwnProfile(accessToken: String, displayName: String?, avatarID: String?) async throws -> Profile {
        let body = try JSONEncoder().encode(UpdateProfileRequestBody(displayName: displayName, avatarID: avatarID))
        return try await authorizedProfileRequest(method: "PATCH", path: "profile/me", accessToken: accessToken, body: body)
    }

    public func fetchProfile(accessToken: String, accountID: String) async throws -> Profile {
        try await authorizedProfileRequest(method: "GET", path: "profile/\(accountID)", accessToken: accessToken, body: nil)
    }

    private func authorizedProfileRequest(method: String, path: String, accessToken: String, body: Data?) async throws -> Profile {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = body
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: request)
        } catch {
            throw IdentityAPIClientError.classify(error)
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw IdentityAPIClientError.invalidResponse
        }

        guard httpResponse.statusCode == 200 else {
            let errorBody = try? JSONDecoder().decode(ErrorResponseBody.self, from: data)
            throw IdentityAPIClientError.server(statusCode: httpResponse.statusCode, message: errorBody?.error)
        }

        let decoded = try JSONDecoder().decode(ProfileResponseBody.self, from: data)
        return Profile(displayName: decoded.displayName, avatarID: decoded.avatarID)
    }

    private func post<Body: Encodable>(path: String, body: Body) async throws -> SessionResponseBody {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: request)
        } catch {
            throw IdentityAPIClientError.classify(error)
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw IdentityAPIClientError.invalidResponse
        }

        guard httpResponse.statusCode == 200 else {
            let errorBody = try? JSONDecoder().decode(ErrorResponseBody.self, from: data)
            throw IdentityAPIClientError.server(statusCode: httpResponse.statusCode, message: errorBody?.error)
        }

        return try JSONDecoder().decode(SessionResponseBody.self, from: data)
    }

    private static func session(from body: SessionResponseBody) throws -> AuthSession {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]

        guard
            let accessExpiry = formatter.date(from: body.accessTokenExpiresAt),
            let refreshExpiry = formatter.date(from: body.refreshTokenExpiresAt)
        else {
            throw IdentityAPIClientError.invalidResponse
        }

        return AuthSession(
            accessToken: body.accessToken,
            accessTokenExpiresAt: accessExpiry,
            refreshToken: body.refreshToken,
            refreshTokenExpiresAt: refreshExpiry
        )
    }
}

private struct AppleLoginRequestBody: Encodable {
    let identityToken: String
    let nonce: String?

    enum CodingKeys: String, CodingKey {
        case identityToken = "identity_token"
        case nonce
    }
}

private struct GoogleLoginRequestBody: Encodable {
    let identityToken: String

    enum CodingKeys: String, CodingKey {
        case identityToken = "identity_token"
    }
}

private struct EmailCredentialRequestBody: Encodable {
    let email: String
    let password: String
}

private struct EmailOnlyRequestBody: Encodable {
    let email: String
}

private struct TokenRequestBody: Encodable {
    let token: String
}

private struct PasswordResetConfirmRequestBody: Encodable {
    let token: String
    let password: String
}

private struct RefreshRequestBody: Encodable {
    let refreshToken: String

    enum CodingKeys: String, CodingKey {
        case refreshToken = "refresh_token"
    }
}

private struct SessionResponseBody: Decodable {
    let accessToken: String
    let accessTokenExpiresAt: String
    let refreshToken: String
    let refreshTokenExpiresAt: String
    let isNewAccount: Bool

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case accessTokenExpiresAt = "access_token_expires_at"
        case refreshToken = "refresh_token"
        case refreshTokenExpiresAt = "refresh_token_expires_at"
        case isNewAccount = "is_new_account"
    }
}

private struct ErrorResponseBody: Decodable {
    let error: String
}

private struct UpdateProfileRequestBody: Encodable {
    let displayName: String?
    let avatarID: String?

    enum CodingKeys: String, CodingKey {
        case displayName = "display_name"
        case avatarID = "avatar_id"
    }
}

private struct ProfileResponseBody: Decodable {
    let displayName: String
    let avatarID: String

    enum CodingKeys: String, CodingKey {
        case displayName = "display_name"
        case avatarID = "avatar_id"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        displayName = try container.decodeIfPresent(String.self, forKey: .displayName) ?? ""
        avatarID = try container.decode(String.self, forKey: .avatarID)
    }
}
