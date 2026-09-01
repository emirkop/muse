import Foundation

public struct ResolvedPhotoPlacement: Equatable, Sendable {
    public let slotIndex: Int
    public let photoAssetID: String
    public let caption: String
    public let anchor: SlotAnchor
    public let transform: SlotTransform

    public init(
        slotIndex: Int,
        photoAssetID: String,
        caption: String,
        anchor: SlotAnchor,
        transform: SlotTransform
    ) {
        self.slotIndex = slotIndex
        self.photoAssetID = photoAssetID
        self.caption = caption
        self.anchor = anchor
        self.transform = transform
    }
}

public enum RoomPlacementFailure: Equatable, Sendable {
    case slotTableUnavailable(variantID: String)
    case variantMismatch(expected: String, received: String)
    case unsupportedPhotoCount(Int)
    case anchorMissingFromTable(SlotAnchor)
}

public enum RoomPlacementResolution: Equatable, Sendable {
    case resolved([ResolvedPhotoPlacement])
    case unresolvable(RoomPlacementFailure)
}

public enum RoomPlacementResolver {
    public static func resolve(
        room: Room,
        slotTable: RoomVariantSlotTable?
    ) -> RoomPlacementResolution {
        guard let slotTable else {
            return .unresolvable(.slotTableUnavailable(variantID: room.variantID))
        }
        guard slotTable.variantID == room.variantID else {
            return .unresolvable(
                .variantMismatch(expected: room.variantID, received: slotTable.variantID)
            )
        }

        let assignments = room.photoSlots.sorted { $0.slotIndex < $1.slotIndex }
        guard RoomPhotoSlotLayout.supports(photoCount: assignments.count) else {
            return .unresolvable(.unsupportedPhotoCount(assignments.count))
        }

        let layout = RoomPhotoSlotLayout.slots(forPhotoCount: assignments.count)
        var placements: [ResolvedPhotoPlacement] = []
        placements.reserveCapacity(assignments.count)

        for (assignment, slot) in zip(assignments, layout) {
            guard let transform = slotTable.photoTransforms[slot.anchor] else {
                return .unresolvable(.anchorMissingFromTable(slot.anchor))
            }
            placements.append(
                ResolvedPhotoPlacement(
                    slotIndex: assignment.slotIndex,
                    photoAssetID: assignment.photoAssetID,
                    caption: assignment.caption,
                    anchor: slot.anchor,
                    transform: transform
                )
            )
        }

        return .resolved(placements)
    }
}
