import Foundation

public final class SignInWithGoogleUseCase: Sendable {
    private let identityProvider: GoogleIdentityProviding
    private let authService: AuthenticationServicing
    private let sessionStore: SessionStoring

    public init(identityProvider: GoogleIdentityProviding, authService: AuthenticationServicing, sessionStore: SessionStoring) {
        self.identityProvider = identityProvider
        self.authService = authService
        self.sessionStore = sessionStore
    }

    public func execute() async throws -> LoginResult {
        let identity = try await identityProvider.requestIdentity()
        let result = try await authService.signInWithGoogle(identityToken: identity.identityToken)
        try sessionStore.save(result.session)
        return result
    }
}
