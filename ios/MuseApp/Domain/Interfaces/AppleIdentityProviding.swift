import Foundation

public struct AppleIdentity: Equatable, Sendable {
    public let identityToken: String
    public let rawNonce: String

    public init(identityToken: String, rawNonce: String) {
        self.identityToken = identityToken
        self.rawNonce = rawNonce
    }
}

public enum AppleSignInError: Error, Equatable, Sendable {
    case cancelled
    case failed
}

@MainActor
public protocol AppleIdentityProviding: Sendable {
    func requestIdentity() async throws -> AppleIdentity
}
