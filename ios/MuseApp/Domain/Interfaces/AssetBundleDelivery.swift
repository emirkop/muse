import Foundation

public protocol AssetBundleManifestFetching: Sendable {
    func manifest(accessToken: String, bundleID: String, appAssetVersion: Int) async throws -> AssetBundleManifest
}

public protocol RoomVariantCatalogLookup: Sendable {
    func variant(accessToken: String, variantID: String) async throws -> RoomVariant?
}

public enum AssetBundleDeliveryState: Equatable, Sendable {
    case notStarted
    case downloading(fractionComplete: Double)
    case geometryReady(fractionComplete: Double)
    case installed(InstalledAssetBundle)
    case failed(AssetBundleDeliveryError)
}

public enum AssetBundleDeliveryError: Error, Equatable, Sendable {
    case notPublished
    case deliveryUnconfigured
    case manifestUnreachable
    case offline
    case downloadFailed
    case integrityCheckFailed(assetID: String)
    case storageFailed
    case malformedBundle
}

public protocol AssetBundleProviding: Sendable {
    func bundle(
        accessToken: String,
        bundleID: String,
        progress: (@Sendable (AssetBundleDeliveryState) -> Void)?
    ) async -> Result<InstalledAssetBundle, AssetBundleDeliveryError>
}
