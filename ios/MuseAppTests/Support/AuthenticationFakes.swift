import Foundation
@testable import MuseApp

@MainActor
final class FakeAppleIdentityProvider: AppleIdentityProviding {
    var result: Result<AppleIdentity, Error> = .success(AppleIdentity(identityToken: "apple-token", rawNonce: "nonce"))

    func requestIdentity() async throws -> AppleIdentity {
        try result.get()
    }
}

@MainActor
final class FakeGoogleIdentityProvider: GoogleIdentityProviding {
    var result: Result<GoogleIdentity, Error> = .success(GoogleIdentity(identityToken: "google-token"))

    func requestIdentity() async throws -> GoogleIdentity {
        try result.get()
    }
}

final class FakeAuthenticationService: AuthenticationServicing, @unchecked Sendable {
    var loginResult: Result<LoginResult, Error> = .success(
        LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: true
        )
    )
    var refreshResult: Result<AuthSession, Error> = .success(
        AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date())
    )
    private(set) var receivedIdentityToken: String?
    private(set) var receivedNonce: String?
    private(set) var receivedRefreshToken: String?
    private(set) var googleCallCount = 0

    func signInWithApple(identityToken: String, nonce: String) async throws -> LoginResult {
        receivedIdentityToken = identityToken
        receivedNonce = nonce
        return try loginResult.get()
    }

    func signInWithGoogle(identityToken: String) async throws -> LoginResult {
        receivedIdentityToken = identityToken
        googleCallCount += 1
        return try loginResult.get()
    }

    func refreshSession(refreshToken: String) async throws -> AuthSession {
        receivedRefreshToken = refreshToken
        return try refreshResult.get()
    }

    // MARK: - Email & password

    var signUpResult: Result<Void, Error> = .success(())
    var verifyResult: Result<LoginResult, Error> = .success(
        LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: true
        )
    )
    var emailLoginResult: Result<LoginResult, Error> = .success(
        LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: false
        )
    )
    var resendResult: Result<Void, Error> = .success(())
    var resetRequestResult: Result<Void, Error> = .success(())
    var resetConfirmResult: Result<Void, Error> = .success(())

    private(set) var signUpCalls: [(email: String, password: String)] = []
    private(set) var emailLoginCalls: [(email: String, password: String)] = []
    private(set) var verifyTokens: [String] = []
    private(set) var resendEmails: [String] = []
    private(set) var resetRequestEmails: [String] = []
    private(set) var resetConfirmCalls: [(token: String, password: String)] = []

    func signUpWithEmail(email: String, password: String) async throws {
        signUpCalls.append((email: email, password: password))
        try signUpResult.get()
    }

    func verifyEmail(token: String) async throws -> LoginResult {
        verifyTokens.append(token)
        return try verifyResult.get()
    }

    func resendVerification(email: String) async throws {
        resendEmails.append(email)
        try resendResult.get()
    }

    func logInWithEmail(email: String, password: String) async throws -> LoginResult {
        emailLoginCalls.append((email: email, password: password))
        return try emailLoginResult.get()
    }

    func requestPasswordReset(email: String) async throws {
        resetRequestEmails.append(email)
        try resetRequestResult.get()
    }

    func confirmPasswordReset(token: String, password: String) async throws {
        resetConfirmCalls.append((token: token, password: password))
        try resetConfirmResult.get()
    }
}

final class FakeSessionStore: SessionStoring, @unchecked Sendable {
    private(set) var savedSession: AuthSession?
    var refreshTokenOverride: String??

    func save(_ session: AuthSession) throws {
        savedSession = session
    }

    func loadRefreshToken() throws -> String? {
        if let override = refreshTokenOverride {
            return override
        }
        return savedSession?.refreshToken
    }

    func clear() throws {
        savedSession = nil
    }
}
