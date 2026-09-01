import Foundation

public enum RoomPhotoOrder {
    public static func swapping(_ slots: [PhotoSlotAssignment], from: Int, to: Int) -> [PhotoSlotAssignment] {
        var ordered = normalised(slots)
        guard from != to,
              ordered.indices.contains(from),
              ordered.indices.contains(to) else {
            return ordered
        }
        ordered.swapAt(from, to)
        return reindexed(ordered)
    }

    public static func normalised(_ slots: [PhotoSlotAssignment]) -> [PhotoSlotAssignment] {
        reindexed(slots.sorted { $0.slotIndex < $1.slotIndex })
    }

    public static func assetIDs(_ slots: [PhotoSlotAssignment]) -> [String] {
        normalised(slots).map(\.photoAssetID)
    }

    public static func position(ofAssetID assetID: String, in slots: [PhotoSlotAssignment]) -> Int? {
        normalised(slots).firstIndex { $0.photoAssetID == assetID }
    }

    private static func reindexed(_ slots: [PhotoSlotAssignment]) -> [PhotoSlotAssignment] {
        slots.enumerated().map { position, slot in
            PhotoSlotAssignment(slotIndex: position, photoAssetID: slot.photoAssetID, caption: slot.caption)
        }
    }
}

public extension Room {
    func replacingPhotoSlots(_ slots: [PhotoSlotAssignment]) -> Room {
        Room(
            id: id,
            name: name,
            variantID: variantID,
            privacy: privacy,
            photoSlots: slots,
            sculptures: sculptures
        )
    }
}
