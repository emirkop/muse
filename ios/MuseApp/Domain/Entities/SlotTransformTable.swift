import Foundation
import simd

public struct SlotTransform: Equatable, Sendable {
    public let position: SIMD3<Float>
    public let rotation: simd_quatf
    public let scale: SIMD3<Float>

    public init(
        position: SIMD3<Float>,
        rotation: simd_quatf = simd_quatf(ix: 0, iy: 0, iz: 0, r: 1),
        scale: SIMD3<Float> = SIMD3<Float>(repeating: 1)
    ) {
        self.position = position
        self.rotation = rotation
        self.scale = scale
    }

    public static func == (lhs: SlotTransform, rhs: SlotTransform) -> Bool {
        lhs.position == rhs.position
            && lhs.rotation.vector == rhs.rotation.vector
            && lhs.scale == rhs.scale
    }
}

public struct RoomVariantSlotTable: Equatable, Sendable {
    public let variantID: String
    public let photoTransforms: [SlotAnchor: SlotTransform]
    public let sculptureTransforms: [Int: SlotTransform]

    public init(
        variantID: String,
        photoTransforms: [SlotAnchor: SlotTransform],
        sculptureTransforms: [Int: SlotTransform] = [:]
    ) {
        self.variantID = variantID
        self.photoTransforms = photoTransforms
        self.sculptureTransforms = sculptureTransforms
    }

    public var supportsFullRoom: Bool {
        RoomPhotoSlotLayout.requiredAnchorsForFullRoom.isSubset(of: Set(photoTransforms.keys))
    }
}

public protocol RoomVariantSlotTableProviding: Sendable {
    func slotTable(forVariantID variantID: String) async -> RoomVariantSlotTable?
}

public struct UnavailableSlotTableProvider: RoomVariantSlotTableProviding {
    public init() {}

    public func slotTable(forVariantID variantID: String) async -> RoomVariantSlotTable? {
        nil
    }
}
