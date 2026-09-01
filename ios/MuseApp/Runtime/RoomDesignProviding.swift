import Foundation

public enum RoomDesignLoadState: Equatable, Sendable {
    case checking
    case downloading(fractionComplete: Double)
    case geometryReady(fractionComplete: Double)
    case ready

    public var fractionComplete: Double {
        switch self {
        case .checking: return 0
        case .downloading(let fraction), .geometryReady(let fraction): return fraction
        case .ready: return 1
        }
    }
}

public enum RoomDesignUnavailableReason: Error, Equatable, Sendable {
    case notPublished
    case deliveryUnconfigured
    case variantUnknown
    case offline
    case network
    case corruptDownload
    case storage
    case malformedBundle
    case layoutMismatch
}

public struct RoomDesign: Sendable {
    public let slotTable: RoomVariantSlotTable
    public let geometry: RoomRuntimeContent.Geometry

    public init(slotTable: RoomVariantSlotTable, geometry: RoomRuntimeContent.Geometry) {
        self.slotTable = slotTable
        self.geometry = geometry
    }
}

public enum RoomDesignResolution: Sendable {
    case available(RoomDesign)
    case unavailable(RoomDesignUnavailableReason)
}

public protocol RoomDesignProviding: Sendable {
    func design(
        forVariantID variantID: String,
        progress: @escaping @Sendable (RoomDesignLoadState) -> Void
    ) async -> RoomDesignResolution
}

public struct UnavailableRoomDesignProvider: RoomDesignProviding {
    public init() {}

    public func design(
        forVariantID variantID: String,
        progress: @escaping @Sendable (RoomDesignLoadState) -> Void
    ) async -> RoomDesignResolution {
        .unavailable(.notPublished)
    }
}
