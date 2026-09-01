import Foundation

public struct CollectionRoom: Equatable, Sendable, Identifiable {
    public let id: String

    public let name: String

    public let categoryID: String?

    public let designID: String?

    public let currentTier: CollectionTier

    public let items: [CollectionItem]

    public let musicTrackID: String?

    public init(
        id: String,
        name: String,
        categoryID: String? = nil,
        designID: String? = nil,
        currentTier: CollectionTier = .base,
        items: [CollectionItem] = [],
        musicTrackID: String? = nil
    ) {
        self.id = id
        self.name = name
        self.categoryID = categoryID
        self.designID = designID
        self.currentTier = currentTier
        self.items = items
        self.musicTrackID = musicTrackID
    }

    public var hasMusic: Bool { musicTrackID != nil }

    public var itemCount: Int { items.count }

    public var needsCategory: Bool { categoryID == nil }
    public var needsDesign: Bool { designID == nil }
}

public struct CollectionItem: Equatable, Sendable, Identifiable {
    public let id: String
    public let slotIndex: Int

    public let catalogModelID: String

    public init(id: String, slotIndex: Int, catalogModelID: String) {
        self.id = id
        self.slotIndex = slotIndex
        self.catalogModelID = catalogModelID
    }
}
