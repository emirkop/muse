import Foundation

public protocol CollectionItemPresentationResolving: Sendable {
    func resolvePresentation(
        for items: [CollectionItem],
        accessToken: String
    ) async -> CollectionItemPresentationResolution
}

public protocol CollectionPresentationAssetReading: Sendable {
    func fetchPresentationAssets(
        accessToken: String,
        catalogModelIDs: [String]
    ) async throws -> [CollectionPresentationAssetEntry]
}

public struct CollectionPresentationAssetEntry: Equatable, Sendable {
    public let catalogModelID: String
    public let asset: CollectionItemPresentationAsset?

    public init(catalogModelID: String, asset: CollectionItemPresentationAsset?) {
        self.catalogModelID = catalogModelID
        self.asset = asset
    }
}

public enum CollectionPresentationAssetLookup {
    public static let maxPerRequest = 100
}
