import Foundation

public final class SignInWithAppleUseCase: Sendable {
    private let identityProvider: AppleIdentityProviding
    private let authService: AuthenticationServicing
    private let sessionStore: SessionStoring

    public init(identityProvider: AppleIdentityProviding, authService: AuthenticationServicing, sessionStore: SessionStoring) {
        self.identityProvider = identityProvider
        self.authService = authService
        self.sessionStore = sessionStore
    }

    public func execute() async throws -> LoginResult {
        let identity = try await identityProvider.requestIdentity()
        let result = try await authService.signInWithApple(identityToken: identity.identityToken, nonce: identity.rawNonce)
        try sessionStore.save(result.session)
        return result
    }
}
