import Foundation

public struct CollectionItemPresentationAsset: Equatable, Sendable {
    public let catalogModelID: String

    public let assetBundle: AssetBundleRef

    public let isDevelopmentFixture: Bool

    public init(catalogModelID: String, assetBundle: AssetBundleRef, isDevelopmentFixture: Bool) {
        self.catalogModelID = catalogModelID
        self.assetBundle = assetBundle
        self.isDevelopmentFixture = isDevelopmentFixture
    }
}

public enum CollectionItemPresentationState: Equatable, Sendable {
    case available(CollectionItemPresentationAsset)
    case notMapped(catalogModelID: String)
    case unavailable(catalogModelID: String, reason: UnavailableReason)

    public enum UnavailableReason: Equatable, Sendable {
        case modelUnknown
        case lookupFailed
    }

    public var catalogModelID: String {
        switch self {
        case .available(let asset): return asset.catalogModelID
        case .notMapped(let id): return id
        case .unavailable(let id, _): return id
        }
    }

    public var asset: CollectionItemPresentationAsset? {
        if case .available(let asset) = self { return asset }
        return nil
    }

    public var hasResolvableAsset: Bool { asset != nil }
}

public struct CollectionItemPresentationResolution: Equatable, Sendable {
    public let statesByModelID: [String: CollectionItemPresentationState]

    public init(statesByModelID: [String: CollectionItemPresentationState]) {
        self.statesByModelID = statesByModelID
    }

    public func state(for item: CollectionItem) -> CollectionItemPresentationState {
        state(forModelID: item.catalogModelID)
    }

    public func state(forModelID modelID: String) -> CollectionItemPresentationState {
        statesByModelID[modelID]
            ?? .unavailable(catalogModelID: modelID, reason: .lookupFailed)
    }

    public func resolvable(
        among items: [CollectionItem]
    ) -> [(item: CollectionItem, asset: CollectionItemPresentationAsset)] {
        items.sorted { $0.slotIndex < $1.slotIndex }.compactMap { item in
            guard let asset = state(for: item).asset else { return nil }
            return (item, asset)
        }
    }

    public func awaitingAuthoredAsset(among items: [CollectionItem]) -> [CollectionItem] {
        items.sorted { $0.slotIndex < $1.slotIndex }.filter {
            if case .notMapped = state(for: $0) { return true }
            return false
        }
    }

    public func unresolvable(among items: [CollectionItem]) -> [CollectionItem] {
        items.sorted { $0.slotIndex < $1.slotIndex }.filter {
            if case .unavailable = state(for: $0) { return true }
            return false
        }
    }
}
