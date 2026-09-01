import Foundation

public final class ActiveBundleRegistry: AssetBundleRetaining, @unchecked Sendable {
    private let lock = NSLock()
    private var leases: [AssetBundleIdentity: Set<UUID>] = [:]

    public init() {}

    public func retain(_ identity: AssetBundleIdentity) -> AssetBundleLease {
        let lease = AssetBundleLease(identity: identity)
        lock.lock()
        leases[identity, default: []].insert(lease.token)
        lock.unlock()
        return lease
    }

    public func release(_ lease: AssetBundleLease) {
        lock.lock()
        defer { lock.unlock() }
        guard var tokens = leases[lease.identity] else { return }
        tokens.remove(lease.token)
        if tokens.isEmpty {
            leases[lease.identity] = nil
        } else {
            leases[lease.identity] = tokens
        }
    }

    public func isActive(_ identity: AssetBundleIdentity) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return !(leases[identity]?.isEmpty ?? true)
    }

    public var activeIdentities: Set<AssetBundleIdentity> {
        lock.lock()
        defer { lock.unlock() }
        return Set(leases.keys)
    }

    public func leaseCount(for identity: AssetBundleIdentity) -> Int {
        lock.lock()
        defer { lock.unlock() }
        return leases[identity]?.count ?? 0
    }
}
