import Foundation

public struct AssetBundleRef: Equatable, Sendable {
    public let id: String
    public let version: Int

    public init(id: String, version: Int) {
        self.id = id
        self.version = version
    }
}

public struct MuseumStyle: Equatable, Sendable, Identifiable {
    public let id: String
    public let displayName: String
    public let assetBundle: AssetBundleRef

    public init(id: String, displayName: String, assetBundle: AssetBundleRef) {
        self.id = id
        self.displayName = displayName
        self.assetBundle = assetBundle
    }
}

public struct SculptureCatalogEntry: Equatable, Sendable, Identifiable {
    public let id: String
    public let displayName: String
    public let assetBundle: AssetBundleRef

    public init(id: String, displayName: String, assetBundle: AssetBundleRef) {
        self.id = id
        self.displayName = displayName
        self.assetBundle = assetBundle
    }
}

public struct RoomVariant: Equatable, Sendable, Identifiable {
    public let id: String
    public let styleID: String
    public let displayName: String
    public let assetBundle: AssetBundleRef

    public init(id: String, styleID: String, displayName: String, assetBundle: AssetBundleRef) {
        self.id = id
        self.styleID = styleID
        self.displayName = displayName
        self.assetBundle = assetBundle
    }
}
