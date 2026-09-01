import Foundation

public struct CollectionRoomShareLink: Equatable, Sendable {
    public let collectionRoomID: String
    public let code: String
    public let url: URL
    public let createdAt: Date

    public init(collectionRoomID: String, code: String, url: URL, createdAt: Date) {
        self.collectionRoomID = collectionRoomID
        self.code = code
        self.url = url
        self.createdAt = createdAt
    }
}

public struct SharedCollectionRoomContent: Equatable, Sendable {
    public let collectionRoomID: String
    public let name: String
    public let categoryID: String?
    public let designID: String?
    public let currentTier: CollectionTier
    public let items: [CollectionItem]
    public let musicTrackID: String?

    public init(
        collectionRoomID: String,
        name: String,
        categoryID: String?,
        designID: String?,
        currentTier: CollectionTier,
        items: [CollectionItem],
        musicTrackID: String? = nil
    ) {
        self.collectionRoomID = collectionRoomID
        self.name = name
        self.categoryID = categoryID
        self.designID = designID
        self.currentTier = currentTier
        self.items = items
        self.musicTrackID = musicTrackID
    }
}
