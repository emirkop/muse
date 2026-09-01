import Foundation

public struct LoginResult: Equatable, Sendable {
    public let session: AuthSession
    public let isNewAccount: Bool

    public init(session: AuthSession, isNewAccount: Bool) {
        self.session = session
        self.isNewAccount = isNewAccount
    }
}

public protocol AuthenticationServicing: Sendable {
    func signInWithApple(identityToken: String, nonce: String) async throws -> LoginResult
    func signInWithGoogle(identityToken: String) async throws -> LoginResult

    func refreshSession(refreshToken: String) async throws -> AuthSession

    // MARK: - Email & password

    func signUpWithEmail(email: String, password: String) async throws

    func verifyEmail(token: String) async throws -> LoginResult

    func resendVerification(email: String) async throws

    func logInWithEmail(email: String, password: String) async throws -> LoginResult

    func requestPasswordReset(email: String) async throws

    func confirmPasswordReset(token: String, password: String) async throws
}
