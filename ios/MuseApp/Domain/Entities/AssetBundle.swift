import Foundation

public enum AssetBundleFormat {
    public static let appAssetVersion = 1
}

public enum AssetBundleKind: String, Equatable, Sendable {
    case museumStyle = "museum_style"
    case roomVariant = "room_variant"
    case sculpture
    case avatar
    case collectionDesign = "collection_design"
}

public enum AssetRole: String, Equatable, Sendable {
    case geometry
    case layout
    case material
    case texture
}

public struct AssetBundleFile: Equatable, Sendable {
    public let assetID: String
    public let role: AssetRole
    public let url: URL
    public let contentType: String
    public let byteSize: Int64
    public let checksumSHA256: String

    public init(assetID: String, role: AssetRole, url: URL, contentType: String, byteSize: Int64, checksumSHA256: String) {
        self.assetID = assetID
        self.role = role
        self.url = url
        self.contentType = contentType
        self.byteSize = byteSize
        self.checksumSHA256 = checksumSHA256
    }
}

public struct AssetBundleIdentity: Hashable, Sendable {
    public let bundleID: String
    public let version: Int

    public init(bundleID: String, version: Int) {
        self.bundleID = bundleID
        self.version = version
    }
}

public struct AssetBundleManifest: Equatable, Sendable {
    public let identity: AssetBundleIdentity
    public let kind: AssetBundleKind
    public let format: String
    public let minAppVersion: Int
    public let files: [AssetBundleFile]
    public let dependencies: [AssetBundleIdentity]

    public init(
        identity: AssetBundleIdentity,
        kind: AssetBundleKind,
        format: String,
        minAppVersion: Int,
        files: [AssetBundleFile],
        dependencies: [AssetBundleIdentity] = []
    ) {
        self.identity = identity
        self.kind = kind
        self.format = format
        self.minAppVersion = minAppVersion
        self.files = files
        self.dependencies = dependencies
    }

    public var bundleID: String { identity.bundleID }
    public var version: Int { identity.version }

    public var geometryFile: AssetBundleFile? {
        files.first { $0.role == .geometry }
    }

    public var layoutFile: AssetBundleFile? {
        files.first { $0.role == .layout }
    }

    public var totalByteSize: Int64 {
        files.reduce(0) { $0 + $1.byteSize }
    }
}

public struct InstalledAssetBundle: Equatable, Sendable {
    public let identity: AssetBundleIdentity
    public let kind: AssetBundleKind
    public let format: String
    public let files: [String: URL]
    public let roles: [String: AssetRole]

    public init(identity: AssetBundleIdentity, kind: AssetBundleKind, format: String, files: [String: URL], roles: [String: AssetRole]) {
        self.identity = identity
        self.kind = kind
        self.format = format
        self.files = files
        self.roles = roles
    }

    public func fileURL(forRole role: AssetRole) -> URL? {
        guard let assetID = roles.first(where: { $0.value == role })?.key else { return nil }
        return files[assetID]
    }

    public var geometryURL: URL? { fileURL(forRole: .geometry) }
    public var layoutURL: URL? { fileURL(forRole: .layout) }
}
