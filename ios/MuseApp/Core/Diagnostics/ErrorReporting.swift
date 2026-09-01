import Foundation

public protocol ErrorReporting: Sendable {
    func report(_ report: ErrorReport)
}

public struct ErrorReport: Equatable, Sendable {
    public enum Domain: String, Sendable {
        case assetDelivery = "asset_delivery"
        case assetCache = "asset_cache"
        case runtimeScene = "runtime_scene"
        case photoTexture = "photo_texture"
    }

    public enum Reason: String, Sendable {
        case offline
        case unreachable
        case refused
        case notPublished
        case integrityFailed = "integrity_failed"
        case storageFailed = "storage_failed"
        case malformedBundle = "malformed_bundle"
        case sceneLoadFailed = "scene_load_failed"
        case decodeFailed = "decode_failed"
    }

    public let domain: Domain
    public let reason: Reason
    public let bundle: String?

    public init(domain: Domain, reason: Reason, bundle: String? = nil) {
        self.domain = domain
        self.reason = reason
        self.bundle = bundle
    }
}

public struct NoErrorReporting: ErrorReporting {
    public init() {}
    public func report(_ report: ErrorReport) {}
}

public struct ConsoleErrorReporting: ErrorReporting {
    public init() {}

    public func report(_ report: ErrorReport) {
        var line = "[muse.diagnostic] domain=\(report.domain.rawValue) reason=\(report.reason.rawValue)"
        if let bundle = report.bundle {
            line += " bundle=\(bundle)"
        }
        print(line)
    }
}

// MARK: - Mapping the existing failure types

public extension ErrorReport.Reason {
    init(deliveryError: AssetBundleDeliveryError) {
        switch deliveryError {
        case .offline: self = .offline
        case .manifestUnreachable, .downloadFailed: self = .unreachable
        case .notPublished: self = .notPublished
        case .deliveryUnconfigured: self = .refused
        case .integrityCheckFailed: self = .integrityFailed
        case .storageFailed: self = .storageFailed
        case .malformedBundle: self = .malformedBundle
        }
    }

    init(roomDesignReason: RoomDesignUnavailableReason) {
        switch roomDesignReason {
        case .offline: self = .offline
        case .network: self = .unreachable
        case .notPublished, .variantUnknown: self = .notPublished
        case .deliveryUnconfigured: self = .refused
        case .corruptDownload: self = .integrityFailed
        case .storage: self = .storageFailed
        case .malformedBundle, .layoutMismatch: self = .malformedBundle
        }
    }
}
