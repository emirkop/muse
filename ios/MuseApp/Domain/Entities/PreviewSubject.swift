import Foundation

public struct PreviewSubject: Equatable, Sendable {
    public let id: String
    public let displayName: String
    public let assetBundle: AssetBundleRef

    public init(id: String, displayName: String, assetBundle: AssetBundleRef) {
        self.id = id
        self.displayName = displayName
        self.assetBundle = assetBundle
    }
}

public extension MuseumStyle {
    var previewSubject: PreviewSubject {
        PreviewSubject(id: id, displayName: displayName, assetBundle: assetBundle)
    }
}

public extension RoomVariant {
    var previewSubject: PreviewSubject {
        PreviewSubject(id: id, displayName: displayName, assetBundle: assetBundle)
    }
}
