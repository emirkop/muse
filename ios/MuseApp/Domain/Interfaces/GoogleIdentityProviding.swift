import Foundation

public struct GoogleIdentity: Equatable, Sendable {
    public let identityToken: String

    public init(identityToken: String) {
        self.identityToken = identityToken
    }
}

public enum GoogleSignInProviderError: Error, Equatable, Sendable {
    case cancelled
    case failed
}

@MainActor
public protocol GoogleIdentityProviding: Sendable {
    func requestIdentity() async throws -> GoogleIdentity
}
