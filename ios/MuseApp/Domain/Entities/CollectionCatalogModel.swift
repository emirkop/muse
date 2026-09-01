import Foundation

public struct CollectionCatalogModel: Equatable, Sendable, Identifiable {
    public let id: String

    public let brandID: String
    public let brandDisplayName: String
    public let categoryID: String
    public let displayName: String

    public let metadata: Data

    public let hasAsset: Bool
    public let assetBundle: AssetBundleRef?

    public let isDevelopmentFixture: Bool

    public init(
        id: String,
        brandID: String,
        brandDisplayName: String,
        categoryID: String,
        displayName: String,
        metadata: Data = Data("{}".utf8),
        hasAsset: Bool = false,
        assetBundle: AssetBundleRef? = nil,
        isDevelopmentFixture: Bool = false
    ) {
        self.id = id
        self.brandID = brandID
        self.brandDisplayName = brandDisplayName
        self.categoryID = categoryID
        self.displayName = displayName
        self.metadata = metadata
        self.hasAsset = hasAsset
        self.assetBundle = assetBundle
        self.isDevelopmentFixture = isDevelopmentFixture
    }
}

public struct CollectionModelSearchPage: Equatable, Sendable {
    public let models: [CollectionCatalogModel]
    public let nextCursor: CollectionModelSearchCursor?

    public init(models: [CollectionCatalogModel], nextCursor: CollectionModelSearchCursor? = nil) {
        self.models = models
        self.nextCursor = nextCursor
    }

    public var hasMore: Bool { nextCursor != nil }
}

public struct CollectionModelSearchCursor: Equatable, Sendable {
    public let displayName: String
    public let id: String

    public init(displayName: String, id: String) {
        self.displayName = displayName
        self.id = id
    }
}
