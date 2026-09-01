import Foundation

public struct CollectionCategory: Equatable, Sendable, Identifiable {
    public let id: String

    public let displayName: String

    public let sortOrder: Int

    public init(id: String, displayName: String, sortOrder: Int) {
        self.id = id
        self.displayName = displayName
        self.sortOrder = sortOrder
    }
}
