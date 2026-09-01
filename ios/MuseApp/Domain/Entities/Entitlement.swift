import Foundation

public enum EntitlementState: String, Equatable, Sendable {
    case free
    case paid
    case revoked
    case unavailable
    case unknown
}

public struct AccountEntitlement: Equatable, Sendable {
    public let state: EntitlementState
    public let itemCapacity: Int
    public let itemCount: Int

    public init(state: EntitlementState, itemCapacity: Int, itemCount: Int) {
        self.state = state
        self.itemCapacity = itemCapacity
        self.itemCount = itemCount
    }

    public var isAtCapacity: Bool { itemCount >= itemCapacity }

    public var canUpgrade: Bool { state != .paid }
}

public struct CapacityProduct: Equatable, Sendable {
    public let id: String
    public let displayName: String
    public let displayPrice: String

    public init(id: String, displayName: String, displayPrice: String) {
        self.id = id
        self.displayName = displayName
        self.displayPrice = displayPrice
    }
}
