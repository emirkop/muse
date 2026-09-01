import Foundation

public struct AssetBundleLease: Hashable, Sendable {
    public let identity: AssetBundleIdentity
    public let token: UUID

    public init(identity: AssetBundleIdentity, token: UUID = UUID()) {
        self.identity = identity
        self.token = token
    }
}

public protocol AssetBundleRetaining: Sendable {
    func retain(_ identity: AssetBundleIdentity) -> AssetBundleLease

    func release(_ lease: AssetBundleLease)

    func isActive(_ identity: AssetBundleIdentity) -> Bool

    var activeIdentities: Set<AssetBundleIdentity> { get }
}
